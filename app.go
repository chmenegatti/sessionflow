package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"

	"sessionflow/internal/config"
	"sessionflow/internal/domain"
	"sessionflow/internal/indexer"
	"sessionflow/internal/lockfile"
	"sessionflow/internal/logging"
	"sessionflow/internal/provider"
	"sessionflow/internal/provider/sqliteread"
	"sessionflow/internal/repository/sqlite"
	"sessionflow/internal/service"
	"sessionflow/internal/terminal"
	"sessionflow/internal/watcher"
)

// appID é usado pelo GTK/Wayland e é obrigatório em builds sandbox
// (docs/discovery/06-linux-distribuicao.md).
const appID = "com.cesarmenegatti.sessionflow"

func init() {
	// Evento sem dados: o frontend recarrega o estado ao recebê-lo.
	application.RegisterEvent[application.Void](service.EventIndexUpdated)
	application.RegisterEvent[application.Void](service.EventCostsUpdated)
}

// run faz o wiring explícito: config → logging → bancos → serviços → Wails.
func run() error {
	ctx := context.Background()

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("diretório home: %w", err)
	}
	paths, err := config.ResolvePaths(runtime.GOOS, os.Getenv, home)
	if err != nil {
		return err
	}

	logFile = paths.LogFile()

	// Instância única: duas instâncias escreveriam no mesmo índice. A
	// segunda pede à primeira que mostre a janela e sai.
	lock, err := lockfile.Acquire(filepath.Join(paths.DataDir, "sessionflow.lock"))
	if errors.Is(err, lockfile.ErrLocked) {
		activateRunningInstance()
		return errAlreadyRunning
	}
	if err != nil {
		return fmt.Errorf("criar trava de instância em %s: %w", paths.DataDir, err)
	}
	defer lock.Release()

	cfg, cfgErr := config.Load(paths.ConfigFile())

	log, closeLog, err := logging.New(logging.Options{
		Level:    cfg.SlogLevel(),
		FilePath: paths.LogFile(),
		Console:  os.Stderr,
	})
	if err != nil {
		// Sem arquivo de log o app ainda funciona; registra só no stderr.
		fmt.Fprintln(os.Stderr, "sessionflow: log em arquivo indisponível:", err)
		log, closeLog, _ = logging.New(logging.Options{Level: cfg.SlogLevel(), Console: os.Stderr})
	}
	defer closeLog.Close()
	log.Info("iniciando", "version", version)

	switch {
	case cfgErr != nil:
		// Configuração inválida não impede o uso; segue com o padrão e não
		// sobrescreve o arquivo do usuário.
		log.Warn("configuração inválida; usando padrão", "path", paths.ConfigFile(), "err", cfgErr)
	case !fileExists(paths.ConfigFile()):
		if err := config.Save(paths.ConfigFile(), cfg); err != nil {
			log.Warn("não foi possível criar config padrão", "err", err)
		}
	}

	index, rebuilt, err := sqlite.OpenIndex(ctx, paths.IndexDB(), log)
	if err != nil {
		return fmt.Errorf("abrir índice: %w", err)
	}
	defer index.Close()

	user, err := sqlite.OpenUser(ctx, paths.UserDB())
	if err != nil {
		return fmt.Errorf("abrir user.db: %w", err)
	}
	defer user.Close()

	penv := service.ProviderEnv{Config: cfg, Env: os.Getenv, Home: home, Paths: paths, LookPath: exec.LookPath}
	if err := sqliteread.CleanStale(penv.SnapshotDir()); err != nil {
		log.Warn("limpar cópias antigas", "err", err)
	}
	providers := service.NewProviders(penv)
	ix := indexer.New(sqlite.NewIndexStore(index), log, providers...).WithDisabled(penv.Disabled()...)

	var app *application.App // atribuído abaixo; a indexação só roda depois de app.Run
	var costSvc *service.CostService
	saveDialog := func(name string) (string, error) {
		return app.Dialog.SaveFile().SetFilename(name).PromptForSingleSelection()
	}
	// Depois de cada sync: avisa o frontend e atualiza as estimativas de
	// custo em segundo plano (o CostService serializa com o cadastro).
	afterSync := func() {
		app.Event.Emit(service.EventIndexUpdated)
		go func() {
			// O index:updated já recarrega as telas; o evento só sai quando
			// alguma estimativa mudou.
			if rep, err := costSvc.Refresh(ctx); err == nil && rep.Calculated+rep.Updated > 0 {
				app.Event.Emit(service.EventCostsUpdated)
			}
		}()
	}
	syncSvc := service.NewWatchedSync(ix, log, afterSync,
		func(changed func([]domain.ProviderID), lost func()) service.Watcher {
			return watcher.New(watchTargets(providers), changed, lost, log, watcher.Options{})
		})
	cfgStore := service.NewConfigStore(paths.ConfigFile(), cfg, cfgErr)
	resumeSvc := service.NewResumeService(service.ResumeDeps{
		Index: sqlite.NewIndexStore(index), Providers: providers, Config: cfgStore,
		Launcher: terminal.ExecLauncher{}, LookPath: exec.LookPath, Logger: log,
	})
	settingsSvc := service.NewSettingsService(cfgStore, penv, providers, exec.LookPath)
	sessionSvc := service.NewSessionService(sqlite.NewIndexStore(index), sqlite.NewUserStore(user), log)
	transcriptSvc := service.NewTranscriptService(sqlite.NewIndexStore(index), providers, log)
	searchSvc := service.NewSearchService(sqlite.NewIndexStore(index), log)
	statsSvc := service.NewStatsService(sqlite.NewIndexStore(index), sessionSvc, sqlite.NewUserStore(user), log)
	analyticsSvc := service.NewAnalyticsService(sqlite.NewIndexStore(index), statsSvc, log)
	costSvc = service.NewCostService(service.CostDeps{
		Index: sqlite.NewIndexStore(index), Costs: sqlite.NewCostStore(index, paths.UserDB()),
		Prices: sqlite.NewPriceStore(user), Analytics: analyticsSvc, Config: cfgStore, Log: log,
		Notify: func() { app.Event.Emit(service.EventCostsUpdated) },
		Save:   saveDialog,
	})
	patternsSvc := service.NewPatternsService(sqlite.NewIndexStore(index), analyticsSvc, log, saveDialog)
	appSvc := service.NewAppService(service.AppDeps{
		Version: version, Providers: penv, Index: index, IndexRebuilt: rebuilt, Sync: syncSvc, Logger: log,
	})

	var win *application.WebviewWindow
	app = application.New(application.Options{
		Name: "SessionFlow",
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: appID,
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if win != nil {
					win.Show()
					win.Focus()
				}
			},
		},
		Description: "Gerenciador desktop de sessões de agentes de programação",
		// Piso WARN: em debug o Wails registra payloads das chamadas.
		Logger: logging.WithMinLevel(log, slog.LevelWarn).With("component", "wails"),
		Services: []application.Service{
			application.NewService(appSvc),
			application.NewService(syncSvc),
			application.NewService(sessionSvc),
			application.NewService(transcriptSvc),
			application.NewService(searchSvc),
			application.NewService(statsSvc),
			application.NewService(analyticsSvc),
			application.NewService(costSvc),
			application.NewService(patternsSvc),
			application.NewService(resumeSvc),
			application.NewService(settingsSvc),
			application.NewService(service.NewStartupService(nil, paths.LogFile())),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Linux: application.LinuxOptions{ApplicationID: appID},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	win = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "SessionFlow",
		Width:            1280,
		Height:           800,
		MinWidth:         960,
		MinHeight:        600,
		BackgroundColour: application.NewRGB(10, 10, 10), // igual ao --background do tema dark
		URL:              "/",
	})

	err = app.Run()
	log.Info("encerrando", "err", err)
	return err
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return !errors.Is(err, fs.ErrNotExist)
}

// watchTargets: diretórios que cada adaptador pede para monitorar.
func watchTargets(providers []provider.Provider) []watcher.Target {
	var out []watcher.Target
	for _, p := range providers {
		if w, ok := p.(provider.Watchable); ok {
			out = append(out, watcher.Target{Provider: p.ID(), Dirs: w.WatchDirs()})
		}
	}
	return out
}

// errAlreadyRunning: outra instância está aberta (e foi trazida à frente).
var errAlreadyRunning = errors.New("o SessionFlow já está aberto")

// logFile é o log desta execução (exibido na tela de erro).
var logFile string

// activateRunningInstance pede à instância aberta que mostre a janela. O
// Wails detecta a outra instância dentro de application.New e encerra este
// processo; se voltar, a outra ainda está subindo.
func activateRunningInstance() {
	application.New(application.Options{
		Name:           "SessionFlow",
		Logger:         slog.New(slog.DiscardHandler),
		SingleInstance: &application.SingleInstanceOptions{UniqueID: appID},
	})
}

// showStartupError abre uma janela com o erro de inicialização: aberto
// pelo menu do desktop, o stderr não é visto por ninguém.
func showStartupError(cause error) {
	app := application.New(application.Options{
		Name:     "SessionFlow",
		Logger:   slog.New(slog.DiscardHandler),
		Services: []application.Service{application.NewService(service.NewStartupService(cause, logFile))},
		Assets:   application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		Linux:    application.LinuxOptions{ApplicationID: appID},
	})
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "SessionFlow", Width: 720, Height: 480,
		BackgroundColour: application.NewRGB(10, 10, 10), URL: "/",
	})
	_ = app.Run()
}
