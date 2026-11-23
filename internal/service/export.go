package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"sessionflow/internal/domain"
	"sessionflow/internal/repository/sqlite"
)

// Exportação CSV (docs/ANALYTICS.md §Exportação): gerada e gravada
// localmente; formato longo, uma linha por grupo e métrica. Nunca contém
// texto de conversa nem título de sessão.

var csvHeader = []string{"periodo_inicio", "periodo_fim", "agente", "modelo", "projeto", "sessao", "metrica", "valor", "moeda", "status", "cobertura"}

type csvRow [11]string

// utf8BOM abre o arquivo: sem ele o Excel no Windows lê o CSV como
// Windows-1252 e estraga os acentos.
const utf8BOM = "\uFEFF"

// writeCSV grava o BOM, o cabeçalho e as linhas (UTF-8, vírgula, LF). O
// quoting (vírgula, aspas, quebra de linha) é do encoding/csv.
func writeCSV(w io.Writer, rows []csvRow) error {
	if _, err := io.WriteString(w, utf8BOM); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return err
	}
	for _, r := range rows {
		if err := cw.Write(r[:]); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// csvText neutraliza uma célula de texto que a planilha leria como
// fórmula (CSV injection): começa com tabulação ou CR, ou o primeiro
// caractere depois de espaços e controles é =, +, - ou @. A neutralização
// é um apóstrofo na frente (recomendação OWASP); o texto original fica
// intacto no banco. Só para campos de texto: números nunca passam aqui.
func csvText(s string) string {
	if s == "" {
		return s
	}
	if s[0] == '\t' || s[0] == '\r' {
		return "'" + s
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) { // inclui invisíveis (U+200B, U+FEFF)
			continue
		}
		if strings.ContainsRune("=+-@", r) {
			return "'" + s
		}
		return s
	}
	return s
}

// exportFile pede o caminho e grava o arquivo; "" = cancelado.
func exportFile(save func(string) (string, error), name string, rows []csvRow) (string, error) {
	if save == nil {
		return "", fmt.Errorf("diálogo de salvar indisponível")
	}
	path, err := save(name)
	if err != nil || path == "" {
		return "", err
	}
	var buf bytes.Buffer
	if err := writeCSV(&buf, rows); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func isoLocal(t time.Time, loc *time.Location) string { return t.In(loc).Format(time.RFC3339) }

// worstStatus: o status mais fraco do grupo.
func worstStatus(s sqlite.CostSums) string {
	switch {
	case s.Insufficient > 0:
		return string(domain.EstimateInsufficient)
	case s.NoPrice > 0 || s.Unestimated > 0:
		return string(domain.EstimateNoPrice)
	case s.Partial > 0:
		return string(domain.EstimatePartial)
	}
	return string(domain.EstimateComplete)
}

func coverage(s sqlite.CostSums) string {
	if s.Records == 0 {
		return ""
	}
	return strconv.FormatFloat(float64(s.Complete+s.Partial)*100/float64(s.Records), 'f', 1, 64)
}

// costRows: uma linha por métrica de cada grupo (agente × modelo × projeto
// × moeda) e o custo total de cada sessão raiz.
func costRows(w Window, groups []sqlite.CostRow, sessions []sqlite.CostSessionRow) []csvRow {
	loc := w.Now.Location()
	from, to := isoLocal(w.From, loc), isoLocal(w.To, loc)
	// Status e cobertura valem para o grupo inteiro (todas as moedas e as
	// respostas sem custo, que o Breakdown separa em Currency ""): olhar só
	// a linha da moeda daria sempre "complete, 100.0".
	type gkey struct {
		p      domain.ProviderID
		m, prj string
	}
	whole := map[gkey]*sqlite.CostSums{}
	for _, g := range groups {
		k := gkey{g.Provider, g.Model, g.Project}
		if whole[k] == nil {
			whole[k] = &sqlite.CostSums{}
		}
		whole[k].Add(g.Sums)
	}
	var out []csvRow
	for _, g := range groups {
		s := g.Sums
		w := *whole[gkey{g.Provider, g.Model, g.Project}]
		base := csvRow{from, to, csvText(string(g.Provider)), csvText(g.Model), csvText(g.Project), "", "", "", csvText(s.Currency), worstStatus(w), coverage(w)}
		add := func(metric, value string) {
			r := base
			r[6], r[7] = metric, value
			out = append(out, r)
		}
		if s.Currency != "" {
			add("custo_total", s.Total.String())
			add("custo_entrada", s.Input.String())
			add("custo_saida", s.Output.String())
			add("custo_cache_lido", s.CacheRead.String())
			add("custo_cache_criado", s.CacheCreation.String())
		}
		add("respostas", strconv.Itoa(s.Records))
		add("respostas_completas", strconv.Itoa(s.Complete))
		add("respostas_parciais", strconv.Itoa(s.Partial))
		add("respostas_sem_preco", strconv.Itoa(s.NoPrice+s.Unestimated))
		add("respostas_sem_dados", strconv.Itoa(s.Insufficient))
	}
	for _, r := range sessions {
		var w sqlite.CostSums
		for _, s := range r.Sums {
			w.Add(s)
		}
		for _, s := range r.Sums {
			if s.Currency == "" {
				continue
			}
			out = append(out, csvRow{from, to, csvText(string(r.Key.Provider)), "", csvText(r.Project),
				csvText(string(r.Key.Provider) + ":" + r.Key.NativeID), "custo_total", s.Total.String(), csvText(s.Currency), worstStatus(w), coverage(w)})
		}
	}
	return out
}

func (c *CostService) exportRead(ctx context.Context, f sqlite.UsageFilter) ([]sqlite.CostRow, []sqlite.CostSessionRow, error) {
	if _, err := c.refreshLocked(ctx); err != nil { // nada fica "ainda não calculado" no arquivo
		return nil, nil, err
	}
	groups, err := c.d.Costs.Breakdown(ctx, f, false)
	if err != nil {
		return nil, nil, err
	}
	c.stage("export:read")
	sessions, _, err := c.d.Costs.Sessions(ctx, f, 0, 1<<30)
	return groups, sessions, err
}

// ExportCSV exporta o custo do recorte para um arquivo escolhido pelo
// usuário. Devolve o caminho gravado ("" se cancelado).
func (c *CostService) ExportCSV(ctx context.Context, q AnalyticsQuery) (string, error) {
	w, f, err := c.filter(ctx, q)
	if err != nil {
		return "", err
	}
	// Sob o mutex: grupos e sessões saem do mesmo estado (nenhum
	// Recalcular grava entre as duas leituras).
	c.mu.Lock()
	groups, sessions, err := c.exportRead(ctx, f)
	c.mu.Unlock()
	if err != nil {
		return "", c.fail("exportar custos", err)
	}
	name := "sessionflow-custos-" + c.now().Format(time.DateOnly) + ".csv"
	path, err := exportFile(c.d.Save, name, costRows(w, groups, sessions))
	if err != nil {
		return "", c.fail("gravar CSV", err)
	}
	return path, nil
}

// patternRows: indicadores gerais, por projeto, por agente e projetos com
// vários agentes.
func patternRows(w Window, v PatternsView) []csvRow {
	loc := w.Now.Location()
	from, to := isoLocal(w.From, loc), isoLocal(w.To, loc)
	var out []csvRow
	add := func(provider, project, metric string, value string) {
		out = append(out, csvRow{from, to, csvText(provider), "", csvText(project), "", metric, value, "", "", ""})
	}
	n := strconv.Itoa
	i64 := func(v int64) string { return strconv.FormatInt(v, 10) }
	add("", "", "sessoes_iniciadas", n(v.Started.Value))
	add("", "", "sessoes_retomadas", n(v.Resumed.Value))
	add("", "", "dias_ativos", n(v.Frequency.ActiveDays))
	add("", "", "semanas_ativas", n(v.Frequency.ActiveWeeks))
	add("", "", "sequencia_atual_dias", n(v.Frequency.CurrentStreak))
	add("", "", "maior_sequencia_dias", n(v.Frequency.LongestStreak))
	add("", "", "sessoes_por_dia_ativo", strconv.FormatFloat(v.Frequency.SessionsPerActiveDay, 'f', 1, 64))
	add("", "", "projetos_recorrentes", n(v.RecurringProjects))
	add("", "", "duracao_mediana_ms", i64(v.Duration.Median))
	add("", "", "duracao_p90_ms", i64(v.Duration.P90))
	add("", "", "mensagens_por_sessao_mediana", i64(v.MessagesPerSession.Median))
	add("", "", "intervalo_mediano_ms", i64(v.Gaps.Median))
	for _, p := range v.TopProjects {
		add("", p.Path, "sessoes", n(p.Sessions))
		add("", p.Path, "mensagens", n(p.Messages))
		add("", p.Path, "dias_ativos", n(p.ActiveDays))
	}
	for _, p := range v.ByProvider {
		add(string(p.Provider), "", "sessoes", n(p.Sessions))
		add(string(p.Provider), "", "mensagens", n(p.Messages))
		add(string(p.Provider), "", "dias_ativos", n(p.ActiveDays))
		add(string(p.Provider), "", "duracao_mediana_ms", i64(p.MedianDuration))
	}
	for _, m := range v.MultiProvider {
		for _, p := range m.Providers {
			add(string(p.Provider), m.Path, "sessoes_no_projeto", n(p.Sessions))
		}
	}
	return out
}

// ExportCSV exporta os padrões de atividade do recorte.
func (p *PatternsService) ExportCSV(ctx context.Context, q AnalyticsQuery) (string, error) {
	v, err := p.Patterns(ctx, q)
	if err != nil {
		return "", err
	}
	w, _, err := p.analytics.activityWindow(ctx, q)
	if err != nil {
		return "", err
	}
	name := "sessionflow-padroes-" + w.Now.Format(time.DateOnly) + ".csv"
	path, err := exportFile(p.save, name, patternRows(w, v))
	if err != nil {
		return "", p.analytics.fail("gravar CSV", err)
	}
	return path, nil
}
