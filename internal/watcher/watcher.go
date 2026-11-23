// Package watcher monitora os diretórios dos providers (fsnotify) e avisa,
// com espera por silêncio, quais providers tiveram fontes alteradas. Não
// lê nem interpreta arquivos: quem decide o que reler é o indexador, que
// compara tamanho, data e assinatura de cada fonte.
package watcher

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
)

// Target são os diretórios de um provider.
type Target struct {
	Provider domain.ProviderID
	Dirs     []provider.WatchDir
}

// Options configura o watcher. Zero = padrões.
type Options struct {
	Quiet   time.Duration // silêncio antes de avisar (padrão 1 s)
	MaxWait time.Duration // aviso no máximo após isto, mesmo sem silêncio (padrão 5 s)
}

// Watcher agrega eventos e chama onChange com os providers afetados.
// onLost é chamado quando eventos podem ter se perdido (fila do kernel
// cheia): o certo é uma indexação completa.
type Watcher struct {
	targets  []Target
	onChange func([]domain.ProviderID)
	onLost   func()
	log      *slog.Logger
	quiet    time.Duration
	maxWait  time.Duration

	mu       sync.Mutex
	fs       *fsnotify.Watcher
	watched  map[string]domain.ProviderID // diretório → provider
	degraded bool                         // alguma pasta não pôde ser monitorada
	done     chan struct{}
	wg       sync.WaitGroup
}

// New cria o watcher (sem iniciar).
func New(targets []Target, onChange func([]domain.ProviderID), onLost func(), log *slog.Logger, o Options) *Watcher {
	if o.Quiet <= 0 {
		o.Quiet = time.Second
	}
	if o.MaxWait <= 0 {
		o.MaxWait = 5 * time.Second
	}
	return &Watcher{
		targets: targets, onChange: onChange, onLost: onLost, log: log,
		quiet: o.Quiet, maxWait: o.MaxWait, watched: map[string]domain.ProviderID{},
	}
}

// Start cria o monitor e adiciona os diretórios existentes. Raízes ainda
// inexistentes (provider não instalado) entram em Refresh.
func (w *Watcher) Start() error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		w.mu.Lock()
		w.degraded = true
		w.mu.Unlock()
		return err
	}
	done := make(chan struct{})
	w.mu.Lock()
	w.fs, w.done = fw, done
	w.mu.Unlock()
	w.Refresh()
	w.wg.Add(1)
	go w.loop(fw, done)
	return nil
}

// Close encerra o monitor e espera a goroutine.
func (w *Watcher) Close() error {
	w.mu.Lock()
	fw, done := w.fs, w.done
	w.fs = nil
	w.mu.Unlock()
	if fw == nil {
		return nil
	}
	close(done)
	err := fw.Close()
	w.wg.Wait()
	return err
}

// Degraded informa se alguma pasta não está monitorada (sem watcher, ou
// limite de inotify atingido).
func (w *Watcher) Degraded() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.degraded || w.fs == nil
}

// Refresh adiciona as raízes (e subpastas, nas recursivas) que ainda não
// estão monitoradas, inclusive as criadas depois do início.
func (w *Watcher) Refresh() {
	for _, t := range w.targets {
		for _, d := range t.Dirs {
			w.add(t.Provider, d.Path, d.Recursive)
		}
	}
}

// add monitora dir (e, se recursive, as subpastas). Links simbólicos não
// são seguidos, igual aos adaptadores. Devolve se algo novo entrou.
func (w *Watcher) add(id domain.ProviderID, dir string, recursive bool) bool {
	added := false
	walk := func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir {
				return filepath.SkipDir // raiz ausente: Refresh tenta de novo
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if w.addOne(id, p) {
			added = true
		}
		if !recursive && p != dir {
			return filepath.SkipDir
		}
		return nil
	}
	if !recursive {
		if info, err := os.Lstat(dir); err == nil && info.IsDir() && w.addOne(id, dir) {
			added = true
		}
		return added
	}
	filepath.WalkDir(dir, walk) //nolint:errcheck // erros tratados no walk
	return added
}

func (w *Watcher) addOne(id domain.ProviderID, dir string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fs == nil {
		return false
	}
	if _, ok := w.watched[dir]; ok {
		return false
	}
	if err := w.fs.Add(dir); err != nil {
		if !w.degraded {
			// Ex.: limite fs.inotify.max_user_watches. Registra uma vez.
			w.log.Warn("watcher: pasta não monitorada; a reconciliação periódica cobre", "path", dir, "err", err)
		}
		w.degraded = true
		return false
	}
	w.watched[dir] = id
	return true
}

// loop junta os eventos e avisa após Quiet de silêncio (ou MaxWait).
// fw e done chegam como parâmetros: Close pode zerar w.fs antes desta
// goroutine começar.
func (w *Watcher) loop(fw *fsnotify.Watcher, done <-chan struct{}) {
	defer w.wg.Done()

	pending := map[domain.ProviderID]bool{}
	var quiet, deadline <-chan time.Time
	flush := func() {
		if len(pending) == 0 {
			return
		}
		ids := make([]domain.ProviderID, 0, len(pending))
		for _, id := range domain.AllProviders() {
			if pending[id] {
				ids = append(ids, id)
			}
		}
		clear(pending)
		quiet, deadline = nil, nil
		w.onChange(ids)
	}
	for {
		select {
		case <-done:
			return
		case ev, ok := <-fw.Events:
			if !ok {
				return
			}
			id, relevant := w.handle(ev)
			if !relevant {
				continue
			}
			if len(pending) == 0 {
				deadline = time.After(w.maxWait)
			}
			pending[id] = true
			quiet = time.After(w.quiet)
		case err, ok := <-fw.Errors:
			if !ok {
				return
			}
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				w.log.Warn("watcher: eventos perdidos; reindexando tudo")
				if w.onLost != nil {
					w.onLost()
				}
				continue
			}
			w.log.Warn("watcher", "err", err)
		case <-quiet:
			flush()
		case <-deadline:
			flush()
		}
	}
}

// handle decide se o evento importa e a qual provider pertence. Pasta nova
// numa raiz recursiva entra no monitor (e conta como mudança: arquivos
// podem ter sido criados antes do monitor dela).
func (w *Watcher) handle(ev fsnotify.Event) (domain.ProviderID, bool) {
	dir := filepath.Dir(ev.Name)
	w.mu.Lock()
	id, ok := w.watched[dir]
	_, isWatchedDir := w.watched[ev.Name]
	w.mu.Unlock()
	if !ok {
		if isWatchedDir && ev.Has(fsnotify.Remove|fsnotify.Rename) {
			w.forget(ev.Name)
		}
		return "", false
	}
	switch {
	case ev.Has(fsnotify.Create) && w.recursive(id, ev.Name):
		if info, err := os.Lstat(ev.Name); err == nil && info.IsDir() {
			w.add(id, ev.Name, true)
			return id, true
		}
	case ev.Has(fsnotify.Remove | fsnotify.Rename):
		if isWatchedDir {
			w.forget(ev.Name)
			return id, true
		}
	}
	return id, relevantFile(ev)
}

// recursive informa se path está sob uma raiz recursiva do provider.
func (w *Watcher) recursive(id domain.ProviderID, path string) bool {
	for _, t := range w.targets {
		if t.Provider != id {
			continue
		}
		for _, d := range t.Dirs {
			if d.Recursive && (path == d.Path || strings.HasPrefix(path, d.Path+string(filepath.Separator))) {
				return true
			}
		}
	}
	return false
}

// forget tira do monitor a pasta removida ou renomeada e as subpastas
// dela. Removida, o kernel já soltou o watch; renomeada, o watch seguiria
// a pasta no novo lugar com o nome antigo, então é removido aqui.
func (w *Watcher) forget(dir string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for p := range w.watched {
		if p == dir || strings.HasPrefix(p, dir+string(filepath.Separator)) {
			delete(w.watched, p)
			if w.fs != nil {
				w.fs.Remove(p) //nolint:errcheck // já removido pelo kernel
			}
		}
	}
}

// relevantFile: só arquivos que são (ou compõem) fontes. -shm fica de fora
// de propósito: a leitura somente-leitura do banco do OpenCode atualiza o
// -shm, e reagir a ele faria um laço de releituras. Mudança só de
// permissão (Chmod) não altera conteúdo.
func relevantFile(ev fsnotify.Event) bool {
	if ev.Op == fsnotify.Chmod {
		return false
	}
	name := filepath.Base(ev.Name)
	switch {
	case strings.HasSuffix(name, "-shm"), strings.HasSuffix(name, "-journal"):
		return false
	case strings.HasSuffix(name, ".jsonl"), strings.HasSuffix(name, ".jsonl.zst"),
		strings.HasSuffix(name, ".db"), strings.HasSuffix(name, ".db-wal"),
		strings.HasSuffix(name, ".sqlite"), strings.HasSuffix(name, ".sqlite-wal"):
		return true
	}
	return false
}
