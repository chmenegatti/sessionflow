package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Categorias de custo. Reasoning não tem preço: já está dentro da saída
// (Codex) ou não se sabe (docs/ANALYTICS.md).
const (
	CostInput         = UsageInput
	CostOutput        = UsageOutput
	CostCacheRead     = UsageCacheRead
	CostCacheCreation = UsageCacheCreation
)

// CostCategories na ordem de exibição.
var CostCategories = []string{CostInput, CostOutput, CostCacheRead, CostCacheCreation}

var costLabels = map[string]string{
	CostInput: "entrada", CostOutput: "saída", CostCacheRead: "cache lido", CostCacheCreation: "cache criado",
}

// Motivos fixos de uma estimativa incompleta (nunca conteúdo de conversa).
const (
	ReasonNoTime   = "sem horário"
	ReasonNoModel  = "sem modelo"
	ReasonNoTokens = "sem tokens"
	ReasonNoPrice  = "sem preço vigente"
	ReasonOverflow = "valor fora do limite"
)

// Nanos é um valor monetário em 10⁻⁹ da moeda. Somas inteiras são exatas;
// nunca passa por float64. Em JSON é uma string decimal com 9 casas, para o
// frontend não perder precisão acima de 2⁵³.
type Nanos int64

const nanosPerUnit = 1_000_000_000

// String devolve o valor com 9 casas e ponto decimal ("0.108000000").
func (n Nanos) String() string {
	v := int64(n)
	sign := ""
	if v < 0 {
		sign, v = "-", -v // MinInt64 não ocorre: custos são ≥ 0
	}
	return fmt.Sprintf("%s%d.%09d", sign, v/nanosPerUnit, v%nanosPerUnit)
}

// MarshalJSON implementa json.Marshaler.
func (n Nanos) MarshalJSON() ([]byte, error) { return json.Marshal(n.String()) }

var nanosRe = regexp.MustCompile(`^(-?)(\d+)(?:\.(\d{1,9}))?$`)

// UnmarshalJSON aceita a string produzida por MarshalJSON.
func (n *Nanos) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	m := nanosRe.FindStringSubmatch(s)
	if m == nil {
		return fmt.Errorf("valor monetário inválido: %q", s)
	}
	ip, err := strconv.ParseInt(m[2], 10, 64)
	if err != nil || ip > math.MaxInt64/nanosPerUnit {
		return fmt.Errorf("valor monetário fora do limite: %q", s)
	}
	fp, _ := strconv.ParseInt((m[3] + "000000000")[:9], 10, 64)
	v := ip*nanosPerUnit + fp
	if m[1] == "-" {
		v = -v
	}
	*n = Nanos(v)
	return nil
}

// ErrInvalidPrice: preço ou tabela de preços inválidos. A mensagem completa
// (em português) vai para a interface.
var ErrInvalidPrice = errors.New("preço inválido")

// maxPriceMicros: teto de um preço (1.000.000 por unidade de cobrança).
const maxPriceMicros = 1_000_000 * 1_000_000

var decimalRe = regexp.MustCompile(`^(0|[1-9]\d*)(?:\.(\d{1,6}))?$`)

// ParseDecimal lê um preço: decimal ≥ 0, até 6 casas, sem sinal, expoente ou
// espaço, até 1.000.000. Devolve o valor em micro-unidades e a forma
// canônica (sem zeros à direita na parte decimal).
func ParseDecimal(s string) (micros int64, canonical string, err error) {
	m := decimalRe.FindStringSubmatch(s)
	if m == nil {
		return 0, "", fmt.Errorf("%w: %q não é um decimal com até 6 casas (use ponto)", ErrInvalidPrice, s)
	}
	if len(m[1]) > 7 {
		return 0, "", fmt.Errorf("%w: %s passa do limite de 1.000.000", ErrInvalidPrice, s)
	}
	ip, _ := strconv.ParseInt(m[1], 10, 64)
	fp, _ := strconv.ParseInt((m[2] + "000000")[:6], 10, 64)
	micros = ip*1_000_000 + fp
	if micros > maxPriceMicros {
		return 0, "", fmt.Errorf("%w: %s passa do limite de 1.000.000", ErrInvalidPrice, s)
	}
	canonical = m[1]
	if f := strings.TrimRight(m[2], "0"); f != "" {
		canonical += "." + f
	}
	return micros, canonical, nil
}

// UsageTokens são os tokens de uma resposta usados no cálculo (nil =
// indisponível). Input não inclui cache.
type UsageTokens struct {
	Input         *int64 `json:"input"`
	Output        *int64 `json:"output"`
	CacheRead     *int64 `json:"cacheRead"`
	CacheCreation *int64 `json:"cacheCreation"`
}

func (u UsageTokens) get(cat string) *int64 {
	switch cat {
	case CostInput:
		return u.Input
	case CostOutput:
		return u.Output
	case CostCacheRead:
		return u.CacheRead
	default:
		return u.CacheCreation
	}
}

// Equal compara campo a campo (nil só é igual a nil).
func (u UsageTokens) Equal(o UsageTokens) bool {
	for _, c := range CostCategories {
		a, b := u.get(c), o.get(c)
		if (a == nil) != (b == nil) || (a != nil && *a != *b) {
			return false
		}
	}
	return true
}

// PriceRevision é uma revisão de um preço. Depois de criada, só ReplacedBy,
// DisabledAt e o fechamento da vigência (ValidTo/ClosedAt, quando ainda sem
// fim) mudam: as estimativas que a usaram continuam válidas.
type PriceRevision struct {
	ID         int64      `json:"id"`
	PriceKey   string     `json:"priceKey"` // compartilhado pelas revisões do mesmo preço
	Provider   ProviderID `json:"provider"`
	Model      string     `json:"model"` // id exato ou prefixo terminado em '*'
	Currency   string     `json:"currency"`
	UnitTokens int64      `json:"unitTokens"`

	// Decimal canônico por unidade de cobrança; nil = categoria sem preço.
	Input         *string `json:"input"`
	Output        *string `json:"output"`
	CacheRead     *string `json:"cacheRead"`
	CacheCreation *string `json:"cacheCreation"`

	ValidFrom  time.Time  `json:"validFrom"`
	ValidTo    *time.Time `json:"validTo"` // exclusivo; nil = sem fim
	Source     string     `json:"source"`
	Note       string     `json:"note"`
	CreatedAt  time.Time  `json:"createdAt"`
	ReplacedBy *int64     `json:"replacedBy"`
	DisabledAt *time.Time `json:"disabledAt"`
	ClosedAt   *time.Time `json:"closedAt"`
}

// Current: nem substituída nem desativada (vale para cálculos novos).
func (r PriceRevision) Current() bool { return r.ReplacedBy == nil && r.DisabledAt == nil }

func (r PriceRevision) price(cat string) *string {
	switch cat {
	case CostInput:
		return r.Input
	case CostOutput:
		return r.Output
	case CostCacheRead:
		return r.CacheRead
	default:
		return r.CacheCreation
	}
}

// Covers: ts dentro da vigência [ValidFrom, ValidTo).
func (r PriceRevision) Covers(ts time.Time) bool { return r.covers(ts) }

func (r PriceRevision) covers(ts time.Time) bool {
	return !ts.Before(r.ValidFrom) && (r.ValidTo == nil || ts.Before(*r.ValidTo))
}

// EstimateStatus: qualidade de uma estimativa.
type EstimateStatus string

const (
	EstimateComplete     EstimateStatus = "complete"
	EstimatePartial      EstimateStatus = "partial"
	EstimateNoPrice      EstimateStatus = "no_price"
	EstimateInsufficient EstimateStatus = "insufficient"
)

// Estimate é o custo estimado de uma resposta com uma revisão de preço.
// Custo nil = não calculado (nunca zero).
type Estimate struct {
	RevisionID        *int64         `json:"revisionId"`
	Currency          string         `json:"currency"`
	UnitTokens        int64          `json:"unitTokens"`
	Tokens            UsageTokens    `json:"tokens"`
	InputCost         *Nanos         `json:"inputCost"`
	OutputCost        *Nanos         `json:"outputCost"`
	CacheReadCost     *Nanos         `json:"cacheReadCost"`
	CacheCreationCost *Nanos         `json:"cacheCreationCost"`
	Total             *Nanos         `json:"total"`
	Status            EstimateStatus `json:"status"`
	Reasons           []string       `json:"reasons"`
}

func (e *Estimate) setCost(cat string, v *Nanos) {
	switch cat {
	case CostInput:
		e.InputCost = v
	case CostOutput:
		e.OutputCost = v
	case CostCacheRead:
		e.CacheReadCost = v
	default:
		e.CacheCreationCost = v
	}
}

// ExpectedCostCategories: categorias que o provider informa. Categoria que
// ele nunca informa (cache criado do Codex) não torna a estimativa parcial.
func ExpectedCostCategories(p ProviderID) []string {
	if p == ProviderCodex {
		return []string{CostInput, CostOutput, CostCacheRead}
	}
	return CostCategories
}

var maxInt64 = big.NewInt(math.MaxInt64)

// categoryCost = tokens × preço / unidade, em nanos, arredondado half-even.
// ok = false quando passa de int64.
func categoryCost(tokens, priceMicros, unit int64) (Nanos, bool) {
	num := new(big.Int).Mul(big.NewInt(tokens), big.NewInt(priceMicros))
	num.Mul(num, big.NewInt(1000)) // micro → nano
	den := big.NewInt(unit)
	q, r := new(big.Int).QuoRem(num, den, new(big.Int))
	switch new(big.Int).Lsh(r, 1).Cmp(den) { // 2r contra unit
	case 1:
		q.Add(q, big.NewInt(1))
	case 0:
		if q.Bit(0) == 1 {
			q.Add(q, big.NewInt(1))
		}
	}
	if q.Cmp(maxInt64) > 0 {
		return 0, false
	}
	return Nanos(q.Int64()), true
}

// ComputeEstimate calcula o custo de uma resposta (docs/ANALYTICS.md §Custos).
// rev nil = nenhum preço vigente casou. Função pura.
func ComputeEstimate(rev *PriceRevision, p ProviderID, model string, hasTs bool, u UsageTokens) Estimate {
	e := Estimate{Tokens: u}
	if !hasTs {
		e.Reasons = append(e.Reasons, ReasonNoTime)
	}
	if model == "" {
		e.Reasons = append(e.Reasons, ReasonNoModel)
	}
	if u.Input == nil && u.Output == nil && u.CacheRead == nil && u.CacheCreation == nil {
		e.Reasons = append(e.Reasons, ReasonNoTokens)
	}
	if len(e.Reasons) > 0 {
		e.Status = EstimateInsufficient
		return e
	}
	if rev == nil {
		e.Status, e.Reasons = EstimateNoPrice, []string{ReasonNoPrice}
		return e
	}
	id := rev.ID
	e.RevisionID, e.Currency, e.UnitTokens = &id, rev.Currency, rev.UnitTokens
	expected := ExpectedCostCategories(p)
	var total int64
	computed := false
	// Alguma categoria com tokens > 0 recebeu preço? Sem isso, o "custo"
	// seria só a soma de categorias zeradas: sem preço, nunca R$ 0,00.
	positive, pricedPositive := false, false
	for _, cat := range CostCategories {
		t := u.get(cat)
		switch {
		case t == nil:
			if slices.Contains(expected, cat) {
				e.Reasons = append(e.Reasons, costLabels[cat]+" indisponível")
			}
			continue
		case *t == 0:
			zero := Nanos(0)
			e.setCost(cat, &zero)
			computed = true
			continue
		}
		positive = true
		ps := rev.price(cat)
		if ps == nil {
			e.Reasons = append(e.Reasons, costLabels[cat]+" sem preço")
			continue
		}
		micros, _, err := ParseDecimal(*ps)
		if err != nil { // revisão gravada sempre é válida; defesa
			e.Reasons = append(e.Reasons, costLabels[cat]+" sem preço")
			continue
		}
		c, ok := categoryCost(*t, micros, rev.UnitTokens)
		if ok && total > math.MaxInt64-int64(c) {
			ok = false
		}
		if !ok {
			return Estimate{Tokens: u, RevisionID: e.RevisionID, Currency: e.Currency, UnitTokens: e.UnitTokens,
				Status: EstimateInsufficient, Reasons: []string{ReasonOverflow}}
		}
		v := c
		e.setCost(cat, &v)
		total += int64(c)
		computed, pricedPositive = true, true
	}
	if !computed || (positive && !pricedPositive) {
		return Estimate{Tokens: u, Status: EstimateNoPrice, Reasons: e.Reasons}
	}
	tn := Nanos(total)
	e.Total = &tn
	e.Status = EstimateComplete
	if len(e.Reasons) > 0 {
		e.Status = EstimatePartial
	}
	return e
}

// MatchPrice escolhe a revisão vigente para uma resposta: mesmo provider,
// ts em [ValidFrom, ValidTo), id exato (sem diferenciar maiúsculas) antes de
// prefixo '*', e o prefixo mais longo entre prefixos.
func MatchPrice(revs []PriceRevision, p ProviderID, model string, ts time.Time) *PriceRevision {
	var best *PriceRevision
	bestScore := -1
	lm := strings.ToLower(model)
	for i := range revs {
		r := &revs[i]
		if !r.Current() || r.Provider != p || !r.covers(ts) {
			continue
		}
		score := -1
		pat := strings.ToLower(r.Model)
		if prefix, ok := strings.CutSuffix(pat, "*"); ok {
			if strings.HasPrefix(lm, prefix) {
				score = len(prefix)
			}
		} else if pat == lm {
			score = math.MaxInt32
		}
		if score > bestScore || (score == bestScore && score >= 0 && r.ID > best.ID) {
			best, bestScore = r, score
		}
	}
	return best
}

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

// ValidateRevision confere uma revisão antes de gravar. others são as
// demais revisões; só as atuais do mesmo provider e modelo (sem diferenciar
// maiúsculas), com ID diferente, contam na sobreposição de vigência.
func ValidateRevision(r PriceRevision, others []PriceRevision) error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{ErrInvalidPrice}, a...)...)
	}
	if !slices.Contains(AllProviders(), r.Provider) {
		return bad("agente %q desconhecido", r.Provider)
	}
	m := strings.TrimSpace(r.Model)
	if m == "" || m != r.Model || strings.Contains(strings.TrimSuffix(m, "*"), "*") || m == "*" {
		return bad("modelo deve ser um id ou um prefixo terminado em '*'")
	}
	if !currencyRe.MatchString(r.Currency) {
		return bad("moeda deve ter 3 letras maiúsculas (ex.: BRL)")
	}
	if !slices.Contains([]int64{1, 1000, 1_000_000}, r.UnitTokens) {
		return bad("unidade deve ser por token, por mil ou por milhão de tokens")
	}
	priced := false
	for _, cat := range CostCategories {
		if s := r.price(cat); s != nil {
			if _, _, err := ParseDecimal(*s); err != nil {
				return fmt.Errorf("%s: %w", costLabels[cat], err)
			}
			priced = true
		}
	}
	if !priced {
		return bad("informe o preço de pelo menos uma categoria")
	}
	if r.ValidTo != nil && !r.ValidTo.After(r.ValidFrom) {
		return bad("o fim da vigência deve ser depois do início")
	}
	for _, o := range others {
		if o.ID == r.ID || !o.Current() || o.Provider != r.Provider || !strings.EqualFold(o.Model, r.Model) {
			continue
		}
		if overlaps(r, o) {
			return bad("já existe preço de %s para %s nesse período", r.Provider, o.Model)
		}
	}
	return nil
}

func overlaps(a, b PriceRevision) bool {
	aEndsBeforeB := a.ValidTo != nil && !a.ValidTo.After(b.ValidFrom)
	bEndsBeforeA := b.ValidTo != nil && !b.ValidTo.After(a.ValidFrom)
	return !aEndsBeforeB && !bEndsBeforeA
}
