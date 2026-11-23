package provider

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"sessionflow/internal/domain"
)

// LookPath encontra um executável no PATH (exec.LookPath em produção).
type LookPath func(file string) (string, error)

// FindCLI devolve o executável do provider. Ordem: caminho configurado
// (absoluto e executável), PATH, candidatos conhecidos. Apps abertos pelo
// menu do desktop costumam ter um PATH sem ~/.local/bin, daí os candidatos.
// Devolve "" se não encontrar. Nada é executado.
func FindCLI(configured, name string, look LookPath, candidates ...string) string {
	if configured != "" {
		if isExecutable(configured) {
			return configured
		}
		return ""
	}
	if look != nil {
		if p, err := look(name); err == nil && filepath.IsAbs(p) {
			return p
		}
	}
	for _, c := range candidates {
		if filepath.IsAbs(c) && isExecutable(c) {
			return c
		}
	}
	return ""
}

func isExecutable(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// IsDir informa se p existe e é diretório.
func IsDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// StatSource monta uma Source a partir de um arquivo.
func StatSource(path string, kind domain.SourceKind) (Source, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Source{}, err
	}
	if !info.Mode().IsRegular() {
		return Source{}, &fs.PathError{Op: "stat", Path: path, Err: errors.New("não é arquivo regular")}
	}
	return Source{Path: path, Kind: kind, Size: info.Size(), ModTime: info.ModTime()}, nil
}

// Truncate corta s em no máximo n bytes sem quebrar um caractere UTF-8.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// ParseTime interpreta timestamps ISO-8601 (RFC 3339 com ou sem fração).
// Devolve nil se vazio ou inválido.
func ParseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}
	t = t.UTC()
	return &t
}

// MillisTime converte epoch em milissegundos. 0 = desconhecido.
func MillisTime(ms int64) *time.Time {
	if ms <= 0 {
		return nil
	}
	t := time.UnixMilli(ms).UTC()
	return &t
}

// Span acumula o primeiro e o último timestamp vistos.
type Span struct{ First, Last time.Time }

// Add registra t (nil é ignorado).
func (s *Span) Add(t *time.Time) {
	if t == nil {
		return
	}
	if s.First.IsZero() || t.Before(s.First) {
		s.First = *t
	}
	if t.After(s.Last) {
		s.Last = *t
	}
}

// MaxRaw limita o JSON original guardado num bloco.
const MaxRaw = 64 << 10

// RawJSON copia raw se for JSON válido de até MaxRaw bytes; senão nil.
// A cópia é necessária porque o leitor JSONL reutiliza o buffer da linha.
func RawJSON(raw []byte) json.RawMessage {
	if len(raw) == 0 || len(raw) > MaxRaw || !json.Valid(raw) {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

// Unknown monta o bloco de um registro de tipo desconhecido. O original é
// sempre preservado (dentro do limite).
func Unknown(typ string, raw []byte) domain.Block {
	return domain.Block{Kind: domain.BlockUnknown, Text: typ, Raw: RawJSON(raw)}
}

// Metadata monta um bloco de metadado; o original só vai quando a fonte
// pede (Source.Raw).
func Metadata(label string, raw []byte, src Source) domain.Block {
	b := domain.Block{Kind: domain.BlockMetadata, Text: label}
	if src.Raw {
		b.Raw = RawJSON(raw)
	}
	return b
}

// TitleMax é o tamanho máximo de títulos e primeiros prompts derivados.
const TitleMax = 200

// FirstLine devolve a primeira linha não vazia de s, truncada em TitleMax.
func FirstLine(s string) string {
	for line := range strings.Lines(s) {
		if t := strings.TrimSpace(line); t != "" {
			return Truncate(t, TitleMax)
		}
	}
	return ""
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IsUUID informa se s é um UUID canônico (36 caracteres com hífens).
func IsUUID(s string) bool { return uuidRe.MatchString(s) }
