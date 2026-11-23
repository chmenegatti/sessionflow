package service

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"sessionflow/internal/domain"
	"sessionflow/internal/indexer"
	"sessionflow/internal/provider"
)

// scriptedRunner registra as execuções; gate segura a primeira.
type scriptedRunner struct {
	mu    sync.Mutex
	runs  []indexer.RunOptions
	gate  chan struct{}
	reply func(indexer.RunOptions) indexer.Report
}

func (r *scriptedRunner) Run(ctx context.Context, o indexer.RunOptions) indexer.Report {
	r.mu.Lock()
	r.runs = append(r.runs, o)
	first := len(r.runs) == 1
	r.mu.Unlock()
	if first && r.gate != nil {
		<-r.gate
	}
	if r.reply != nil {
		return r.reply(o)
	}
	return indexer.Report{}
}

func (r *scriptedRunner) all() []indexer.RunOptions {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.runs)
}

func report(id domain.ProviderID, indexed, deferred int) indexer.Report {
	return indexer.Report{Providers: []indexer.ProviderReport{{
		Detection: provider.Detection{Provider: id}, Indexed: indexed, Deferred: deferred,
	}}}
}

// Pedidos durante uma execução são juntados numa só, em seguida.
func TestSyncCoalescesRequests(t *testing.T) {
	r := &scriptedRunner{gate: make(chan struct{})}
	s := NewSyncService(r, nil, discard, nil)
	t.Cleanup(s.stop)
	s.Changed(domain.ProviderClaude)
	for len(r.all()) == 0 {
		time.Sleep(time.Millisecond)
	}
	s.Changed(domain.ProviderCodex)
	s.Changed(domain.ProviderClaude, domain.ProviderCodex)
	s.Changed(domain.ProviderOpenCode)
	close(r.gate)
	s.wait()
	runs := r.all()
	if len(runs) != 2 {
		t.Fatalf("execuções: %+v", runs)
	}
	if !slices.Equal(runs[0].Providers, []domain.ProviderID{domain.ProviderClaude}) || !runs[0].Throttle {
		t.Errorf("1ª: %+v", runs[0])
	}
	if !slices.Equal(runs[1].Providers, []domain.ProviderID{domain.ProviderCodex, domain.ProviderClaude, domain.ProviderOpenCode}) {
		t.Errorf("2ª: %+v", runs[1])
	}
	// Pedido completo engloba os parciais e não adia nada.
	r2 := &scriptedRunner{gate: make(chan struct{})}
	s2 := NewSyncService(r2, nil, discard, nil)
	t.Cleanup(s2.stop)
	s2.Changed(domain.ProviderClaude)
	for len(r2.all()) == 0 {
		time.Sleep(time.Millisecond)
	}
	s2.Changed(domain.ProviderCodex)
	s2.Start()
	close(r2.gate)
	s2.wait()
	if runs := r2.all(); len(runs) != 2 || len(runs[1].Providers) != 0 || runs[1].Throttle {
		t.Errorf("completa: %+v", runs)
	}
}

// Parcial sem mudança não avisa o frontend; completa sempre avisa.
func TestSyncNotifiesOnlyOnChange(t *testing.T) {
	var changed atomic.Bool
	r := &scriptedRunner{reply: func(indexer.RunOptions) indexer.Report {
		if changed.Load() {
			return report(domain.ProviderClaude, 1, 0)
		}
		return report(domain.ProviderClaude, 0, 0)
	}}
	var n atomic.Int32
	s := NewSyncService(r, nil, discard, func() { n.Add(1) })
	t.Cleanup(s.stop)
	s.Changed(domain.ProviderClaude)
	s.wait()
	if n.Load() != 0 {
		t.Errorf("parcial sem mudança avisou")
	}
	changed.Store(true)
	s.Changed(domain.ProviderClaude)
	s.wait()
	changed.Store(false)
	s.Start()
	s.wait()
	if n.Load() != 2 {
		t.Errorf("avisos: %d", n.Load())
	}
}

// Fonte adiada volta sozinha depois do intervalo.
func TestSyncRetriesDeferred(t *testing.T) {
	var calls atomic.Int32
	r := &scriptedRunner{reply: func(indexer.RunOptions) indexer.Report {
		if calls.Add(1) == 1 {
			return report(domain.ProviderClaude, 0, 1)
		}
		return report(domain.ProviderClaude, 1, 0)
	}}
	s := NewSyncService(r, nil, discard, nil)
	s.retry = 20 * time.Millisecond
	t.Cleanup(s.stop)
	s.Changed(domain.ProviderClaude)
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	runs := r.all()
	if len(runs) != 2 || !slices.Equal(runs[1].Providers, []domain.ProviderID{domain.ProviderClaude}) {
		t.Errorf("reexecução: %+v", runs)
	}
}

// Relatórios parciais não apagam os outros providers do estado.
func TestSyncMergesReports(t *testing.T) {
	full := indexer.Report{Providers: []indexer.ProviderReport{
		{Detection: provider.Detection{Provider: domain.ProviderClaude}, Sources: 10},
		{Detection: provider.Detection{Provider: domain.ProviderOpenCode}, Sources: 1},
	}}
	got := mergeReport(&full, report(domain.ProviderCodex, 1, 0))
	var ids []domain.ProviderID
	for _, p := range got.Providers {
		ids = append(ids, p.Detection.Provider)
	}
	if !slices.Equal(ids, domain.AllProviders()) || got.Providers[0].Sources != 10 {
		t.Errorf("mescla: %+v", got.Providers)
	}
}

// Reconciliação periódica roda indexações completas e reabre raízes no
// watcher; o shutdown encerra tudo.
func TestSyncReconcilesPeriodically(t *testing.T) {
	r := &scriptedRunner{}
	w := &fakeWatcher{}
	s := NewSyncService(r, w, discard, nil)
	s.every = func() time.Duration { return 10 * time.Millisecond }
	s.ServiceStartup(context.Background(), applicationOptions())
	deadline := time.Now().Add(2 * time.Second)
	for len(r.all()) < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	s.ServiceShutdown()
	if len(r.all()) < 3 || w.refreshes.Load() < 2 || !w.started.Load() || !w.closed.Load() {
		t.Errorf("execuções %d, refresh %d, watcher %v/%v", len(r.all()), w.refreshes.Load(), w.started.Load(), w.closed.Load())
	}
	n := len(r.all())
	s.Start()
	s.Changed(domain.ProviderClaude)
	time.Sleep(30 * time.Millisecond)
	if len(r.all()) != n {
		t.Error("rodou depois do shutdown")
	}
}

type fakeWatcher struct {
	started, closed atomic.Bool
	refreshes       atomic.Int32
}

func (f *fakeWatcher) Start() error   { f.started.Store(true); return nil }
func (f *fakeWatcher) Close() error   { f.closed.Store(true); return nil }
func (f *fakeWatcher) Refresh()       { f.refreshes.Add(1) }
func (f *fakeWatcher) Degraded() bool { return false }

func applicationOptions() application.ServiceOptions { return application.ServiceOptions{} }

// Sessão ativa adia a cada execução, mas só uma nova tentativa fica
// agendada por provider.
func TestSyncRetryScheduledOnce(t *testing.T) {
	var calls atomic.Int32
	r := &scriptedRunner{reply: func(indexer.RunOptions) indexer.Report {
		if calls.Add(1) <= 5 {
			return report(domain.ProviderClaude, 0, 1)
		}
		return report(domain.ProviderClaude, 1, 0)
	}}
	s := NewSyncService(r, nil, discard, nil)
	s.retry = 80 * time.Millisecond
	t.Cleanup(s.stop)
	for range 5 {
		s.Changed(domain.ProviderClaude)
		s.wait()
	}
	time.Sleep(250 * time.Millisecond)
	if n := calls.Load(); n != 6 {
		t.Errorf("execuções: %d (quer 5 + 1 nova tentativa)", n)
	}
}

// Reconciliação sem mudança não avisa o frontend; a inicial avisa.
func TestSyncReconcileIsQuiet(t *testing.T) {
	r := &scriptedRunner{}
	var n atomic.Int32
	s := NewSyncService(r, &fakeWatcher{}, discard, func() { n.Add(1) })
	s.every = func() time.Duration { return 10 * time.Millisecond }
	s.ServiceStartup(context.Background(), applicationOptions())
	deadline := time.Now().Add(2 * time.Second)
	for len(r.all()) < 4 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	s.ServiceShutdown()
	if len(r.all()) < 4 || n.Load() != 1 {
		t.Errorf("execuções %d, avisos %d (quer 1)", len(r.all()), n.Load())
	}
}
