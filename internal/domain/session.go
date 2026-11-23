package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidSessionKey indica uma chave de sessão malformada.
var ErrInvalidSessionKey = errors.New("chave de sessão inválida")

// SessionKey identifica uma sessão de forma única entre providers:
// o mesmo ID nativo em providers diferentes gera chaves diferentes.
type SessionKey struct {
	Provider ProviderID `json:"provider"`
	NativeID string     `json:"nativeId"`
}

// NewSessionKey valida e cria uma chave.
func NewSessionKey(p ProviderID, nativeID string) (SessionKey, error) {
	if !p.Valid() {
		return SessionKey{}, fmt.Errorf("%w: provider %q desconhecido", ErrInvalidSessionKey, p)
	}
	if strings.TrimSpace(nativeID) == "" {
		return SessionKey{}, fmt.Errorf("%w: id nativo vazio", ErrInvalidSessionKey)
	}
	return SessionKey{Provider: p, NativeID: nativeID}, nil
}

// ParseSessionKey interpreta o formato "provider:idNativo".
func ParseSessionKey(s string) (SessionKey, error) {
	p, id, ok := strings.Cut(s, ":")
	if !ok {
		return SessionKey{}, fmt.Errorf("%w: separador ausente", ErrInvalidSessionKey)
	}
	return NewSessionKey(ProviderID(p), id)
}

// String devolve o formato "provider:idNativo".
func (k SessionKey) String() string {
	return string(k.Provider) + ":" + k.NativeID
}

// SourceKind descreve onde o provider guarda a sessão.
type SourceKind string

const (
	SourceJSONL  SourceKind = "jsonl"
	SourceSQLite SourceKind = "sqlite"
)

// Session é a visão normalizada de uma sessão de qualquer provider.
type Session struct {
	Key          SessionKey  `json:"key"`
	Parent       *SessionKey `json:"parent,omitempty"` // subagente / subsessão
	ProjectPath  string      `json:"projectPath"`
	Title        string      `json:"title"`
	FirstPrompt  string      `json:"firstPrompt"`
	Model        string      `json:"model"`
	CreatedAt    time.Time   `json:"createdAt"`
	UpdatedAt    time.Time   `json:"updatedAt"`
	MessageCount int         `json:"messageCount"`
	SourceKind   SourceKind  `json:"sourceKind"`
	SourcePath   string      `json:"sourcePath"`
	Archived     bool        `json:"archived"`
	Automated    bool        `json:"automated"` // sem prompt humano (SDK, hooks)
	Favorite     bool        `json:"favorite"`
	Tags         []string    `json:"tags"`
}
