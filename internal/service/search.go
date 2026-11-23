package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"sessionflow/internal/domain"
	"sessionflow/internal/repository/sqlite"
)

// Erros da busca exibidos ao usuário.
var (
	ErrQueryTooLong = errors.New("consulta longa demais: use até 200 caracteres")
	ErrSearchFailed = errors.New("não foi possível pesquisar; veja o log")
)

// Limites da busca.
const (
	maxQueryRunes   = 200
	maxQueryTerms   = 16
	minPrefixRunes  = 3
	searchPageSize  = 30
	maxSearchPage   = 100
	searchCountCap  = 1000 // total exato até aqui; acima, "1000+"
	searchTimeout   = 10 * time.Second
	maxSnippetRunes = 400
)

// SearchQuery é uma busca global. Text aceita palavras (todas precisam
// aparecer; com 3+ letras, casa também como prefixo) e "frases entre aspas"
// (sequência exata). Operadores do FTS5 não são interpretados.
type SearchQuery struct {
	Text          string              `json:"text"`
	Providers     []domain.ProviderID `json:"providers"`     // vazio = todos
	Project       string              `json:"project"`       // caminho; "" = todos
	IncludeHidden bool                `json:"includeHidden"` // subagentes e automáticas
	Limit         int                 `json:"limit"`
	Offset        int                 `json:"offset"`
}

// SnippetPart é um pedaço do trecho; Match marca um termo encontrado. A
// interface renderiza texto puro, nunca HTML.
type SnippetPart struct {
	Text  string `json:"text"`
	Match bool   `json:"match"`
}

// SearchHit é uma mensagem encontrada. Seq leva o viewer até ela.
type SearchHit struct {
	SessionID   string            `json:"sessionId"`
	Provider    domain.ProviderID `json:"provider"`
	Title       string            `json:"title"`
	ProjectPath string            `json:"projectPath"`
	ProjectName string            `json:"projectName"`
	Subagent    bool              `json:"subagent"`
	Automated   bool              `json:"automated"`
	Seq         int               `json:"seq"`
	Role        domain.Role       `json:"role"`
	Timestamp   int64             `json:"timestamp"` // ms; 0 = desconhecido
	Snippet     []SnippetPart     `json:"snippet"`
}

// SearchResult é uma página de resultados. Total é exato até TotalCapped
// ser false; com TotalCapped, há ao menos Total resultados.
type SearchResult struct {
	Hits        []SearchHit `json:"hits"`
	Total       int         `json:"total"`
	TotalCapped bool        `json:"totalCapped"`
	HasMore     bool        `json:"hasMore"`
}

// SearchService pesquisa o texto das mensagens no índice FTS5 local.
type SearchService struct {
	index *sqlite.IndexStore
	log   *slog.Logger
}

// NewSearchService cria o serviço.
func NewSearchService(index *sqlite.IndexStore, log *slog.Logger) *SearchService {
	return &SearchService{index: index, log: log}
}

// Search devolve uma página de mensagens por relevância. Consulta sem
// termos devolve resultado vazio (não é erro: o usuário ainda digita).
func (s *SearchService) Search(ctx context.Context, q SearchQuery) (SearchResult, error) {
	empty := SearchResult{Hits: []SearchHit{}}
	if utf8.RuneCountInString(q.Text) > maxQueryRunes {
		return empty, ErrQueryTooLong
	}
	match := BuildMatch(q.Text)
	if match == "" {
		return empty, nil
	}
	for _, p := range q.Providers {
		if !p.Valid() {
			return empty, ErrInvalidFilter
		}
	}
	limit := q.Limit
	if limit <= 0 {
		limit = searchPageSize
	}
	limit = min(limit, maxSearchPage)
	offset := max(q.Offset, 0)

	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	rows, total, err := s.index.SearchMessages(ctx, sqlite.SearchFilter{
		Match: match, Providers: q.Providers, ProjectPath: q.Project, IncludeHidden: q.IncludeHidden,
		Limit: limit, Offset: offset, CountCap: searchCountCap + 1,
	})
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return empty, ctx.Err() // nova busca substituiu esta
		}
		// O erro do SQLite não contém texto das conversas; a consulta,
		// sim (o que o usuário digitou): não vai para o log.
		s.log.Error("busca", "err", err)
		return empty, ErrSearchFailed
	}
	res := SearchResult{Hits: make([]SearchHit, 0, len(rows)), Total: min(total, searchCountCap), TotalCapped: total > searchCountCap}
	for _, r := range rows {
		res.Hits = append(res.Hits, SearchHit{
			SessionID: r.Session.Key.String(), Provider: r.Session.Key.Provider,
			Title:       DisplayTitle(r.Session.Title, r.Session.FirstPrompt),
			ProjectPath: r.Session.ProjectPath, ProjectName: projectName(r.Session.ProjectPath),
			Subagent: r.Session.Parent != nil, Automated: r.Session.Automated,
			Seq: r.Seq, Role: r.Role, Timestamp: r.Timestamp, Snippet: snippetParts(r.Snippet),
		})
	}
	res.HasMore = offset+len(rows) < total
	return res, nil
}

// BuildMatch converte o texto digitado numa consulta FTS5 segura: cada
// palavra vira uma string entre aspas, com prefixo a partir de 3 letras
// ("migra"*), e cada trecho
// entre aspas vira uma frase exata. Aspas, operadores (AND, OR, NOT, NEAR),
// colunas, parênteses e curingas digitados são tratados como texto, então
// a consulta nunca tem erro de sintaxe. Devolve "" se não houver termos.
func BuildMatch(text string) string {
	var terms []string
	add := func(s string, phrase bool) {
		if len(terms) >= maxQueryTerms || !hasWord(s) {
			return
		}
		t := `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
		// Prefixo curto ("a*") expande para boa parte do vocabulário e
		// torna a busca lenta; palavras curtas casam só inteiras. O FTS
		// aplica o prefixo ao último token ("foo-ba"* → ba*).
		if !phrase && lastTokenRunes(s) >= minPrefixRunes {
			t += "*"
		}
		terms = append(terms, t)
	}
	parts := strings.Split(text, `"`)
	for i, part := range parts {
		// Ímpar = dentro de aspas, desde que a aspa tenha sido fechada.
		if i%2 == 1 && i < len(parts)-1 {
			add(strings.Join(strings.Fields(part), " "), true)
			continue
		}
		for _, w := range strings.Fields(part) {
			add(w, false)
		}
	}
	return strings.Join(terms, " ")
}

// hasWord: termo sem letras nem números viraria frase vazia.
func hasWord(s string) bool { return strings.ContainsFunc(s, func(r rune) bool { return !notWord(r) }) }

// lastTokenRunes conta letras e números do último token de s.
func lastTokenRunes(s string) int {
	s = strings.TrimRightFunc(s, notWord)
	if i := strings.LastIndexFunc(s, notWord); i >= 0 {
		_, size := utf8.DecodeRuneInString(s[i:])
		s = s[i+size:]
	}
	return utf8.RuneCountInString(s)
}

// notWord: o tokenizador unicode61 só indexa letras e números (e marcas
// combinantes, presentes em texto decomposto, NFD).
func notWord(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.Is(unicode.Mn, r)
}

// snippetParts separa o trecho do FTS em texto e termos encontrados,
// juntando espaços e quebras de linha e limitando o tamanho.
func snippetParts(s string) []SnippetPart {
	s = strings.Join(strings.Fields(s), " ")
	var out []SnippetPart
	left := maxSnippetRunes
	match := false
	for s != "" && left > 0 {
		mark := sqlite.MarkOpen
		if match {
			mark = sqlite.MarkClose
		}
		i := strings.Index(s, mark)
		chunk := s
		if i >= 0 {
			chunk = s[:i]
		}
		if chunk != "" {
			if n := utf8.RuneCountInString(chunk); n > left {
				chunk = string([]rune(chunk)[:left]) + "…"
			}
			left -= utf8.RuneCountInString(chunk)
			out = append(out, SnippetPart{Text: chunk, Match: match})
		}
		if i < 0 {
			break
		}
		s = s[i+len(mark):]
		match = !match
	}
	if out == nil {
		out = []SnippetPart{}
	}
	return out
}
