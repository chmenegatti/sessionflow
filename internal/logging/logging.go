// Package logging configura o slog da aplicação: JSON em arquivo
// ($XDG_STATE_HOME/sessionflow/sessionflow.log) e, em desenvolvimento,
// texto no stderr.
//
// Regra de privacidade (CLAUDE.md §10): logs nunca carregam conteúdo de
// conversa. Como rede de segurança, atributos com chaves de conteúdo são
// substituídos por "[redacted]" antes de chegar a qualquer handler.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// MaxFileSize é o tamanho a partir do qual o log é rotacionado na abertura.
const MaxFileSize = 5 << 20

// redactedKeys são chaves que podem carregar conteúdo de conversa.
var redactedKeys = map[string]struct{}{
	"content": {}, "text": {}, "prompt": {}, "message": {}, "body": {},
	"input": {}, "output": {}, "query": {},
	// O Wails registra argumentos e retornos de bindings em debug.
	"args": {}, "result": {},
}

// Options configura o logger.
type Options struct {
	Level    slog.Level
	FilePath string    // vazio = sem arquivo
	Console  io.Writer // nil = sem console
}

// New cria o logger. O io.Closer fecha o arquivo de log.
func New(o Options) (*slog.Logger, io.Closer, error) {
	hopts := &slog.HandlerOptions{Level: o.Level, ReplaceAttr: redact}
	var handlers []slog.Handler
	var closer io.Closer = nopCloser{}

	if o.FilePath != "" {
		f, err := openRotated(o.FilePath)
		if err != nil {
			return nil, nil, err
		}
		closer = f
		handlers = append(handlers, slog.NewJSONHandler(f, hopts))
	}
	if o.Console != nil {
		handlers = append(handlers, slog.NewTextHandler(o.Console, hopts))
	}
	if len(handlers) == 0 {
		return slog.New(slog.DiscardHandler), closer, nil
	}
	return slog.New(slog.NewMultiHandler(handlers...)), closer, nil
}

func redact(_ []string, a slog.Attr) slog.Attr {
	if _, ok := redactedKeys[a.Key]; ok {
		return slog.String(a.Key, "[redacted]")
	}
	return a
}

// openRotated abre o log em append; se passou de MaxFileSize, move o atual
// para "<arquivo>.1" (mantém uma geração).
func openRotated(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err == nil && info.Size() >= MaxFileSize {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

// WithMinLevel devolve um logger que descarta registros abaixo de min,
// independentemente do nível do logger original. Usado para o logger do
// Wails, que em debug registra payloads de chamadas.
func WithMinLevel(l *slog.Logger, min slog.Level) *slog.Logger {
	return slog.New(minLevelHandler{h: l.Handler(), min: min})
}

type minLevelHandler struct {
	h   slog.Handler
	min slog.Level
}

func (m minLevelHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= m.min && m.h.Enabled(ctx, l)
}
func (m minLevelHandler) Handle(ctx context.Context, r slog.Record) error { return m.h.Handle(ctx, r) }
func (m minLevelHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return minLevelHandler{h: m.h.WithAttrs(a), min: m.min}
}
func (m minLevelHandler) WithGroup(n string) slog.Handler {
	return minLevelHandler{h: m.h.WithGroup(n), min: m.min}
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }
