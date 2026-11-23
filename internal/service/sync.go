package service

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"sessionflow/internal/domain"
	"sessionflow/internal/indexer"
)

// EventIndexUpdated é emitido quando uma indexação muda o índice (e ao fim
// de cada indexação completa).
const EventIndexUpdated = "index:updated"

// Intervalos da reconciliação: uma indexação completa periódica (só stat
// nas fontes sem mudança) recupera alterações que o watcher perdeu. Sem
// watcher (limite do inotify, erro), o intervalo encurta.
const (
	reconcileEvery         = 5 * time.Minute
	reconcileEveryDegraded = time.Minute
)

// Runner executa uma indexação (indexer.Indexer em produção).
type Runner interface {
	Run(ctx context.Context, o indexer.RunOptions) indexer.Report
}

// Watcher avisa mudanças nas fontes (watcher.Watcher em produção).
type Watcher interface {
	Start() error
	Close() error
	Refresh()       // monitora raízes que passaram a existir
	Degraded() bool // parte das pastas não está monitorada
}

// SyncService mantém o índice em dia: indexação completa ao abrir, a
// pedido e periodicamente; parcial (só os providers alterados) quando o
// watcher avisa. Pedidos feitos durante uma execução são juntados e rodam
// em seguida, nunca em paralelo.
type SyncService struct {
	runner  Runner
	log     *slog.Logger
	notify  func() // avisa o frontend; pode ser nil
	watcher Watcher
	every   func() time.Duration // nil = reconcileEvery/…Degraded
	retry   time.Duration        // nova tentativa de fontes adiadas

	mu       sync.Mutex
	running  bool // há uma execução (de qualquer tipo)
	fullRun  bool // a execução atual é completa (exibido como "indexando")
	closed   bool // após o shutdown o banco é fechado: nada mais roda
	pending  *syncRequest
	retrying map[domain.ProviderID]bool // nova tentativa já agendada
	last     *indexer.Report            // último relatório de cada provider, mesclado
	cancel   context.CancelFunc
	stopLoop chan struct{}
	wg       sync.WaitGroup
}

// syncRequest acumula pedidos. full = todos os providers, sem adiamento.
// quiet = completa de rotina (reconciliação): só avisa a interface se
// algo mudou.
type syncRequest struct {
	full  bool
	quiet bool
	ids   []domain.ProviderID
}

// NewSyncService cria o serviço. w (pode ser nil) é ligado no startup.
// Métodos exportados viram bindings Wails: configuração fica no construtor.
func NewSyncService(r Runner, w Watcher, log *slog.Logger, notify func()) *SyncService {
	return &SyncService{
		runner: r, watcher: w, log: log, notify: notify, retry: indexer.ThrottleEvery,
		retrying: map[domain.ProviderID]bool{}, stopLoop: make(chan struct{}),
	}
}

// setWatcher liga o watcher depois de criado (o watcher precisa de
// Changed/Start deste serviço).
func (s *SyncService) setWatcher(w Watcher) { s.watcher = w }

// NewWatchedSync cria o serviço já com o watcher montado sobre ele.
func NewWatchedSync(r Runner, log *slog.Logger, notify func(), newWatcher func(changed func([]domain.ProviderID), lost func()) Watcher) *SyncService {
	s := NewSyncService(r, nil, log, notify)
	s.setWatcher(newWatcher(func(ids []domain.ProviderID) { s.Changed(ids...) }, s.Start))
	return s
}

// ServiceStartup (ciclo de vida Wails) dispara a indexação inicial, liga
// o watcher e a reconciliação periódica.
func (s *SyncService) ServiceStartup(context.Context, application.ServiceOptions) error {
	s.Start()
	if s.watcher != nil {
		if err := s.watcher.Start(); err != nil {
			// Sem watcher o app segue: a reconciliação periódica cobre.
			s.log.Warn("watcher indisponível; usando só a reconciliação periódica", "err", err)
		}
	}
	s.wg.Add(1)
	go s.reconcile()
	return nil
}

// ServiceShutdown para o watcher, cancela a indexação e espera o fim.
func (s *SyncService) ServiceShutdown() error {
	if s.watcher != nil {
		s.watcher.Close()
	}
	s.stop()
	return nil
}

// Start pede uma indexação completa (inicialização, botão "Atualizar
// índice", reconciliação).
func (s *SyncService) Start() { s.request(syncRequest{full: true}) }

// Changed pede a indexação dos providers alterados (watcher). Fontes
// grandes relidas há pouco são adiadas e voltam sozinhas depois.
func (s *SyncService) Changed(ids ...domain.ProviderID) {
	if len(ids) > 0 {
		s.request(syncRequest{ids: ids})
	}
}

func (s *SyncService) request(r syncRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if s.pending == nil {
		s.pending = &syncRequest{quiet: true}
	}
	p := s.pending
	p.full = p.full || r.full
	// Basta um pedido explícito (início, botão) para avisar no fim.
	p.quiet = p.quiet && (r.quiet || !r.full)
	for _, id := range r.ids {
		if !slices.Contains(p.ids, id) {
			p.ids = append(p.ids, id)
		}
	}
	if !s.running {
		s.running = true
		s.wg.Add(1)
		go s.loop()
	}
}

// loop executa pedidos até a fila esvaziar.
func (s *SyncService) loop() {
	defer s.wg.Done()
	for {
		s.mu.Lock()
		req := s.pending
		s.pending = nil
		if req == nil || s.closed {
			s.running, s.fullRun = false, false
			s.mu.Unlock()
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel, s.fullRun = cancel, req.full
		s.mu.Unlock()

		opts := indexer.RunOptions{Throttle: !req.full}
		if !req.full {
			opts.Providers = req.ids
		}
		rep := s.runner.Run(ctx, opts)
		canceled := ctx.Err() != nil
		cancel()

		s.mu.Lock()
		s.last = mergeReport(s.last, rep)
		s.mu.Unlock()
		if canceled {
			continue // shutdown: a próxima volta encerra
		}
		if ((req.full && !req.quiet) || rep.Changed()) && s.notify != nil {
			s.notify()
		}
		s.scheduleRetry(rep.Deferred())
	}
}

// scheduleRetry agenda uma nova tentativa para providers com fontes
// adiadas, no máximo uma por provider: uma sessão ativa adia a cada poucos
// segundos e cada adiamento criaria um timer.
func (s *SyncService) scheduleRetry(ids []domain.ProviderID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var fresh []domain.ProviderID
	for _, id := range ids {
		if !s.retrying[id] {
			s.retrying[id] = true
			fresh = append(fresh, id)
		}
	}
	if len(fresh) == 0 || s.closed {
		return
	}
	time.AfterFunc(s.retry, func() {
		s.mu.Lock()
		for _, id := range fresh {
			delete(s.retrying, id)
		}
		s.mu.Unlock()
		s.Changed(fresh...)
	})
}

// reconcile roda indexações completas periódicas até o shutdown.
func (s *SyncService) reconcile() {
	defer s.wg.Done()
	for {
		every := reconcileEvery
		switch {
		case s.every != nil:
			every = s.every()
		case s.watcher == nil || s.watcher.Degraded():
			every = reconcileEveryDegraded
		}
		select {
		case <-s.stopLoop:
			return
		case <-time.After(every):
		}
		if s.watcher != nil {
			s.watcher.Refresh()
		}
		s.request(syncRequest{full: true, quiet: true})
	}
}

// stop cancela a execução em andamento e espera o fim.
func (s *SyncService) stop() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.stopLoop)
	}
	s.pending = nil
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// wait espera a fila esvaziar sem cancelar.
func (s *SyncService) wait() {
	for {
		s.mu.Lock()
		busy := s.running
		s.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// snapshot devolve o último relatório e se há indexação completa rodando
// (as parciais do watcher são rápidas e não aparecem como "indexando").
// Não exportado: não vira binding.
func (s *SyncService) snapshot() (*indexer.Report, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last, s.fullRun
}

// mergeReport junta o relatório novo ao anterior: providers que não
// rodaram (execução parcial) mantêm o último resultado.
func mergeReport(prev *indexer.Report, rep indexer.Report) *indexer.Report {
	if prev == nil {
		return &rep
	}
	out := rep
	out.Providers = slices.Clone(rep.Providers)
	for _, old := range prev.Providers {
		if !slices.ContainsFunc(out.Providers, func(p indexer.ProviderReport) bool {
			return p.Detection.Provider == old.Detection.Provider
		}) {
			out.Providers = append(out.Providers, old)
		}
	}
	slices.SortStableFunc(out.Providers, func(a, b indexer.ProviderReport) int {
		return slices.Index(domain.AllProviders(), a.Detection.Provider) - slices.Index(domain.AllProviders(), b.Detection.Provider)
	})
	return &out
}
