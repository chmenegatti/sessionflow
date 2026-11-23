package service

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"sessionflow/internal/domain"
	"sessionflow/internal/repository/sqlite"
)

// Erros devolvidos ao frontend (mensagens exibidas ao usuário).
var (
	ErrSessionNotFound = errors.New("sessão não encontrada; o histórico pode ter sido removido pelo provider")
	ErrInvalidTag      = errors.New("tag inválida: use de 1 a 40 caracteres, sem quebras de linha")
	ErrInvalidKey      = errors.New("identificador de sessão inválido")
	ErrInvalidFilter   = errors.New("filtro de provider inválido")
	ErrInvalidPeriod   = errors.New("período inválido: a data inicial deve ser anterior ou igual à final")
	errInternal        = errors.New("falha ao acessar o índice local; veja o log")
)

// Limites de paginação da listagem.
const (
	defaultPageSize = 50
	maxPageSize     = 1000 // "carregar mais" pede a lista inteira de novo, maior
	maxTagLen       = 40
)

// SessionQuery são os filtros da listagem.
type SessionQuery struct {
	Providers     []domain.ProviderID `json:"providers"`     // vazio = todos
	Project       string              `json:"project"`       // caminho do projeto; "" = todos
	FavoritesOnly bool                `json:"favoritesOnly"` // inclui ocultas favoritadas
	Tag           string              `json:"tag"`
	IncludeHidden bool                `json:"includeHidden"` // subagentes e automáticas
	Sort          string              `json:"sort"`          // "recent" (padrão) | "oldest"
	Limit         int                 `json:"limit"`
	Offset        int                 `json:"offset"`
}

// SessionItem é uma linha da listagem. Datas em ms Unix (0 = desconhecida).
type SessionItem struct {
	ID           string            `json:"id"` // "provider:idNativo"
	Provider     domain.ProviderID `json:"provider"`
	Title        string            `json:"title"` // "" = sem título
	ProjectPath  string            `json:"projectPath"`
	ProjectName  string            `json:"projectName"`
	Model        string            `json:"model"`
	CreatedAt    int64             `json:"createdAt"`
	UpdatedAt    int64             `json:"updatedAt"`
	MessageCount int               `json:"messageCount"`
	Archived     bool              `json:"archived"`
	Automated    bool              `json:"automated"`
	Subagent     bool              `json:"subagent"`
	Favorite     bool              `json:"favorite"`
	Tags         []string          `json:"tags"`
}

// SessionPage é uma página da listagem.
type SessionPage struct {
	Items   []SessionItem `json:"items"`
	Total   int           `json:"total"`
	HasMore bool          `json:"hasMore"`
}

// SessionDetail completa SessionItem para o painel de detalhes.
type SessionDetail struct {
	Session     SessionItem `json:"session"`
	NativeID    string      `json:"nativeId"`
	FirstPrompt string      `json:"firstPrompt"`
	SourceKind  string      `json:"sourceKind"`
	SourcePath  string      `json:"sourcePath"`
	ParentID    string      `json:"parentId"` // "" = sessão principal
	Children    int         `json:"children"` // subagentes/subsessões
}

// ProjectItem é um projeto com sessões visíveis.
type ProjectItem struct {
	Path         string              `json:"path"`
	Name         string              `json:"name"`
	Providers    []domain.ProviderID `json:"providers"`
	Sessions     int                 `json:"sessions"`
	LastActivity int64               `json:"lastActivity"`
}

// TagItem é uma tag e quantas sessões a usam.
type TagItem struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// SessionService expõe listagem, detalhes, projetos, favoritos e tags.
type SessionService struct {
	index *sqlite.IndexStore
	user  *sqlite.UserStore
	log   *slog.Logger
}

// NewSessionService cria o serviço.
func NewSessionService(index *sqlite.IndexStore, user *sqlite.UserStore, log *slog.Logger) *SessionService {
	return &SessionService{index: index, user: user, log: log}
}

// List devolve uma página de sessões por atividade.
func (s *SessionService) List(ctx context.Context, q SessionQuery) (SessionPage, error) {
	f := sqlite.SessionFilter{
		Providers: q.Providers, ProjectPath: q.Project, IncludeHidden: q.IncludeHidden,
		Oldest: q.Sort == "oldest", Limit: q.Limit, Offset: max(q.Offset, 0),
	}
	if f.Limit <= 0 {
		f.Limit = defaultPageSize
	}
	f.Limit = min(f.Limit, maxPageSize)
	for _, p := range f.Providers {
		if !p.Valid() {
			return SessionPage{}, ErrInvalidFilter
		}
	}

	if q.FavoritesOnly {
		keys, err := s.user.FavoriteKeys(ctx)
		if err != nil {
			return SessionPage{}, s.fail("favoritas", err)
		}
		f.Keys, f.IncludeHidden = keys, true // favoritar é escolha explícita
	}
	if tag := strings.TrimSpace(q.Tag); tag != "" {
		keys, err := s.user.KeysWithTag(ctx, tag)
		if err != nil {
			return SessionPage{}, s.fail("tag", err)
		}
		f.Keys = intersect(f.Keys, keys)
		f.IncludeHidden = true
	}

	rows, total, err := s.index.ListSessions(ctx, f)
	if err != nil {
		return SessionPage{}, s.fail("listar sessões", err)
	}
	items, err := s.items(ctx, rows)
	if err != nil {
		return SessionPage{}, err
	}
	return SessionPage{Items: items, Total: total, HasMore: f.Offset+len(items) < total}, nil
}

// Get devolve os detalhes de uma sessão.
func (s *SessionService) Get(ctx context.Context, id string) (SessionDetail, error) {
	key, err := parseKey(id)
	if err != nil {
		return SessionDetail{}, err
	}
	row, err := s.index.GetSession(ctx, key)
	if errors.Is(err, sqlite.ErrNotFound) {
		return SessionDetail{}, ErrSessionNotFound
	}
	if err != nil {
		return SessionDetail{}, s.fail("ler sessão", err)
	}
	items, err := s.items(ctx, []domain.Session{row})
	if err != nil {
		return SessionDetail{}, err
	}
	children, err := s.index.CountChildren(ctx, key)
	if err != nil {
		return SessionDetail{}, s.fail("contar subagentes", err)
	}
	d := SessionDetail{
		Session: items[0], NativeID: key.NativeID, FirstPrompt: row.FirstPrompt,
		SourceKind: string(row.SourceKind), SourcePath: row.SourcePath, Children: children,
	}
	if row.Parent != nil {
		d.ParentID = row.Parent.String()
	}
	return d, nil
}

// Projects devolve os projetos com sessões visíveis.
func (s *SessionService) Projects(ctx context.Context) ([]ProjectItem, error) {
	ps, err := s.index.ListProjects(ctx)
	if err != nil {
		return nil, s.fail("listar projetos", err)
	}
	out := make([]ProjectItem, 0, len(ps))
	for _, p := range ps {
		out = append(out, ProjectItem{
			Path: p.Path, Name: p.Name, Providers: p.Providers,
			Sessions: p.SessionCount, LastActivity: millis(p.LastActivity),
		})
	}
	return out, nil
}

// SetFavorite marca ou desmarca uma sessão existente como favorita.
func (s *SessionService) SetFavorite(ctx context.Context, id string, favorite bool) error {
	key, err := s.existing(ctx, id)
	if err != nil {
		return err
	}
	if err := s.user.SetFavorite(ctx, key, favorite); err != nil {
		return s.fail("favoritar", err)
	}
	return nil
}

// AddTag associa uma tag à sessão e devolve as tags atualizadas.
func (s *SessionService) AddTag(ctx context.Context, id, name string) ([]string, error) {
	key, err := s.existing(ctx, id)
	if err != nil {
		return nil, err
	}
	tag, err := normalizeTag(name)
	if err != nil {
		return nil, err
	}
	if err := s.user.AddTag(ctx, key, tag); err != nil {
		return nil, s.fail("adicionar tag", err)
	}
	return s.tagsOf(ctx, key)
}

// RemoveTag tira uma tag da sessão e devolve as tags atualizadas.
func (s *SessionService) RemoveTag(ctx context.Context, id, name string) ([]string, error) {
	key, err := parseKey(id)
	if err != nil {
		return nil, err
	}
	if err := s.user.RemoveTag(ctx, key, strings.TrimSpace(name)); err != nil {
		return nil, s.fail("remover tag", err)
	}
	return s.tagsOf(ctx, key)
}

// Tags devolve as tags com o número de sessões que existem no índice.
// Tags só de sessões apagadas pelo provider ficam de fora (o vínculo no
// user.db permanece e volta a valer se a sessão reaparecer).
func (s *SessionService) Tags(ctx context.Context) ([]TagItem, error) {
	pairs, err := s.user.TagKeys(ctx)
	if err != nil {
		return nil, s.fail("listar tags", err)
	}
	keys := make([]string, 0, len(pairs))
	for _, p := range pairs {
		keys = append(keys, p.Key)
	}
	exists, err := s.index.ExistingKeys(ctx, keys)
	if err != nil {
		return nil, s.fail("listar tags", err)
	}
	out := []TagItem{}
	for _, p := range pairs {
		if !exists[p.Key] {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Name == p.Name {
			out[n-1].Count++
		} else {
			out = append(out, TagItem{Name: p.Name, Count: 1})
		}
	}
	return out, nil
}

func (s *SessionService) tagsOf(ctx context.Context, key domain.SessionKey) ([]string, error) {
	tags, err := s.user.TagsOf(ctx, []string{key.String()})
	if err != nil {
		return nil, s.fail("ler tags", err)
	}
	return nonNil(tags[key.String()]), nil
}

// items converte linhas do índice e junta favoritos e tags do user.db.
func (s *SessionService) items(ctx context.Context, rows []domain.Session) ([]SessionItem, error) {
	keys := make([]string, len(rows))
	for i, r := range rows {
		keys[i] = r.Key.String()
	}
	favs, err := s.user.Favorites(ctx, keys)
	if err != nil {
		return nil, s.fail("ler favoritas", err)
	}
	tags, err := s.user.TagsOf(ctx, keys)
	if err != nil {
		return nil, s.fail("ler tags", err)
	}
	out := make([]SessionItem, len(rows))
	for i, r := range rows {
		k := keys[i]
		out[i] = SessionItem{
			ID: k, Provider: r.Key.Provider, Title: DisplayTitle(r.Title, r.FirstPrompt),
			ProjectPath: r.ProjectPath, ProjectName: projectName(r.ProjectPath), Model: r.Model,
			CreatedAt: millis(r.CreatedAt), UpdatedAt: millis(r.UpdatedAt), MessageCount: r.MessageCount,
			Archived: r.Archived, Automated: r.Automated, Subagent: r.Parent != nil,
			Favorite: favs[k], Tags: nonNil(tags[k]),
		}
	}
	return out, nil
}

func (s *SessionService) existing(ctx context.Context, id string) (domain.SessionKey, error) {
	key, err := parseKey(id)
	if err != nil {
		return key, err
	}
	if _, err := s.index.GetSession(ctx, key); errors.Is(err, sqlite.ErrNotFound) {
		return key, ErrSessionNotFound
	} else if err != nil {
		return key, s.fail("ler sessão", err)
	}
	return key, nil
}

// fail registra o erro técnico (sem conteúdo) e devolve uma mensagem
// genérica ao frontend.
func (s *SessionService) fail(op string, err error) error {
	s.log.Error("sessões", "op", op, "err", err)
	return errInternal
}

func parseKey(id string) (domain.SessionKey, error) {
	k, err := domain.ParseSessionKey(id)
	if err != nil {
		return k, ErrInvalidKey
	}
	return k, nil
}

// normalizeTag apara e junta espaços. Rejeita vazias, longas e com
// caracteres de controle.
func normalizeTag(name string) (string, error) {
	tag := strings.Join(strings.Fields(name), " ")
	if tag == "" || utf8.RuneCountInString(tag) > maxTagLen || !utf8.ValidString(tag) {
		return "", ErrInvalidTag
	}
	for _, r := range tag {
		if unicode.IsControl(r) {
			return "", ErrInvalidTag
		}
	}
	return tag, nil
}

// markup casa marcações que os providers injetam no prompt
// (<command-name>, <command-message>, <pasted_content>…). Só nomes com "-"
// ou "_": texto como "List<String>" ou "<div>" é conteúdo e fica.
var markup = regexp.MustCompile(`</?[A-Za-z][A-Za-z0-9]*[-_][\w-]*(\s[^>]*)?/?>`)

// DisplayTitle limpa o título para exibição; usa o primeiro prompt se o
// título ficar vazio. "" = sem título.
func DisplayTitle(title, firstPrompt string) string {
	for _, t := range []string{title, firstPrompt} {
		if clean := strings.Join(strings.Fields(markup.ReplaceAllString(t, " ")), " "); clean != "" {
			return clean
		}
	}
	return ""
}

func projectName(path string) string {
	if path == "" {
		return ""
	}
	return domain.ProjectName(path)
}

func millis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// intersect devolve a interseção de a (nil = universo) com b.
func intersect(a, b []string) []string {
	if a == nil {
		return b
	}
	in := make(map[string]bool, len(b))
	for _, x := range b {
		in[x] = true
	}
	out := []string{}
	for _, x := range a {
		if in[x] {
			out = append(out, x)
		}
	}
	return out
}
