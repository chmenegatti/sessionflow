// Package service contém os serviços expostos ao frontend via Wails.
// Regras de negócio ficam aqui (e abaixo), nunca no frontend.
package service

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/indexer"
	"sessionflow/internal/provider"
	"sessionflow/internal/repository/sqlite"
)

// Estados extras de ProviderStatus.Status, além de provider.Status.
const (
	StatusPending = "pending" // habilitado, aguardando a primeira indexação
)

// ProviderStatus resume a situação de um provider para a interface.
type ProviderStatus struct {
	ID          domain.ProviderID `json:"id"`
	Name        string            `json:"name"`
	Enabled     bool              `json:"enabled"`
	RootDir     string            `json:"rootDir"`
	Status      string            `json:"status"` // ok | not_found | unsupported | error | disabled | pending
	Detail      string            `json:"detail"`
	CLIPath     string            `json:"cliPath"`  // executável encontrado ("" = não encontrado)
	Sessions    int               `json:"sessions"` // visíveis
	Hidden      int               `json:"hidden"`   // subagentes e automáticas
	Sources     int               `json:"sources"`
	Failed      int               `json:"failed"`
	Unsupported int               `json:"unsupported"`
	Error       string            `json:"error"`
}

// IndexStats são contagens do índice local.
type IndexStats struct {
	Sessions int  `json:"sessions"` // visíveis (sem subagentes nem automáticas)
	Hidden   int  `json:"hidden"`
	Projects int  `json:"projects"` // com ao menos uma sessão visível
	Rebuilt  bool `json:"rebuilt"`  // índice recriado nesta execução
}

// ScanStatus informa o andamento da indexação.
type ScanStatus struct {
	Running    bool       `json:"running"`
	FinishedAt *time.Time `json:"finishedAt"`
}

// AppStatus é o estado geral exibido no Dashboard e em Settings.
type AppStatus struct {
	Version    string           `json:"version"`
	ConfigFile string           `json:"configFile"`
	LogFile    string           `json:"logFile"`
	IndexDB    string           `json:"indexDb"`
	UserDB     string           `json:"userDb"`
	Providers  []ProviderStatus `json:"providers"`
	Index      IndexStats       `json:"index"`
	Scan       ScanStatus       `json:"scan"`
}

// AppService expõe informações gerais da aplicação.
type AppService struct {
	version string
	env     ProviderEnv
	store   *sqlite.IndexStore
	rebuilt bool
	sync    *SyncService
	log     *slog.Logger
}

// AppDeps são as dependências explícitas do AppService.
type AppDeps struct {
	Version      string
	Providers    ProviderEnv
	Index        *sql.DB
	IndexRebuilt bool
	Sync         *SyncService // pode ser nil (sem indexação)
	Logger       *slog.Logger
}

func NewAppService(d AppDeps) *AppService {
	return &AppService{
		version: d.Version, env: d.Providers, store: sqlite.NewIndexStore(d.Index),
		rebuilt: d.IndexRebuilt, sync: d.Sync, log: d.Logger,
	}
}

// Status devolve versão, caminhos, situação dos providers e contagens.
func (s *AppService) Status(ctx context.Context) (AppStatus, error) {
	paths := s.env.Paths
	st := AppStatus{
		Version:    s.version,
		ConfigFile: paths.ConfigFile(),
		LogFile:    paths.LogFile(),
		IndexDB:    paths.IndexDB(),
		UserDB:     paths.UserDB(),
		Index:      IndexStats{Rebuilt: s.rebuilt},
	}

	var report *indexer.Report
	if s.sync != nil {
		report, st.Scan.Running = s.sync.snapshot()
		if report != nil {
			t := report.FinishedAt
			st.Scan.FinishedAt = &t
		}
	}
	counts, err := s.store.SessionCounts(ctx)
	if err != nil {
		s.log.Error("contar sessões", "err", err)
		return st, err
	}

	for _, id := range domain.AllProviders() {
		pc := s.env.Config.Providers[id]
		ps := ProviderStatus{
			ID: id, Name: id.DisplayName(), Enabled: pc.Enabled, RootDir: s.env.Root(id),
			Status: StatusPending, Sessions: counts[id].Visible, Hidden: counts[id].Hidden,
		}
		st.Index.Sessions += counts[id].Visible
		st.Index.Hidden += counts[id].Hidden
		if !pc.Enabled {
			ps.Status = string(provider.StatusDisabled)
		} else if r, ok := findReport(report, id); ok {
			ps.Status, ps.Detail, ps.CLIPath = string(r.Detection.Status), r.Detection.Detail, r.Detection.CLIPath
			if r.Detection.RootDir != "" {
				ps.RootDir = r.Detection.RootDir
			}
			ps.Sources, ps.Failed, ps.Unsupported, ps.Error = r.Sources, r.Failed, r.Unsupported, r.Error
			if r.Error != "" {
				ps.Status = string(provider.StatusError)
			}
		}
		st.Providers = append(st.Providers, ps)
	}

	st.Index.Projects, err = s.store.VisibleProjects(ctx)
	if err != nil {
		s.log.Error("contar índice", "err", err)
		return st, err
	}
	return st, nil
}

func findReport(r *indexer.Report, id domain.ProviderID) (indexer.ProviderReport, bool) {
	if r == nil {
		return indexer.ProviderReport{}, false
	}
	for _, p := range r.Providers {
		if p.Detection.Provider == id {
			return p, true
		}
	}
	return indexer.ProviderReport{}, false
}
