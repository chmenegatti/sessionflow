package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCSVEscaping(t *testing.T) {
	var buf bytes.Buffer
	row := csvRow{"2026-11-01T00:00:00-03:00", "", "claude", csvText("-x"), csvText(`/home/x,"y"/=cmd|ação`), "", "custo_total", "-5", "USD", "complete", "100.0"}
	if err := writeCSV(&buf, []csvRow{row}); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimPrefix(buf.String(), utf8BOM)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if lines[0] != strings.Join(csvHeader, ",") {
		t.Errorf("cabeçalho %q", lines[0])
	}
	if !strings.Contains(lines[1], `"/home/x,""y""/=cmd|ação"`) || !strings.Contains(lines[1], ",'-x,") || !strings.Contains(lines[1], ",-5,") {
		t.Errorf("linha %q", lines[1])
	}
	if csvText("=1+1") != "'=1+1" || csvText("+1") != "'+1" || csvText("@x") != "'@x" || csvText("/p") != "/p" || csvText("") != "" {
		t.Error("prefixo contra fórmula")
	}
	// Ida e volta pelo leitor de CSV preserva o texto (com o prefixo).
	r, _ := csv.NewReader(strings.NewReader(out)).ReadAll()
	if r[1][4] != `'/home/x,"y"/=cmd|ação` && r[1][4] != `/home/x,"y"/=cmd|ação` {
		t.Errorf("projeto lido %q", r[1][4])
	}
}

func csvFile(t *testing.T) (func(string) (string, error), *string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "out.csv")
	var name string
	return func(n string) (string, error) { name = n; return p, nil }, &name
}

func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}) || bytes.HasPrefix(b[3:], []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("arquivo sem BOM UTF-8 único no início: % x", b[:min(6, len(b))])
	}
	rows, err := csv.NewReader(bytes.NewReader(b[3:])).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestCostCSVContent(t *testing.T) {
	c := newCostEnv(t)
	c.add(t, claudeUSD)
	save, name := csvFile(t)
	c.svc.d.Save = save
	path, err := c.svc.ExportCSV(context.Background(), allClaude)
	if err != nil || path == "" {
		t.Fatalf("%q %v", path, err)
	}
	if !strings.HasPrefix(*name, "sessionflow-custos-") || !strings.HasSuffix(*name, ".csv") {
		t.Errorf("nome %q", *name)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("permissão %v", st.Mode().Perm())
	}
	rows := readCSV(t, path)
	if strings.Join(rows[0], ",") != strings.Join(csvHeader, ",") {
		t.Errorf("cabeçalho %v", rows[0])
	}
	v, _ := c.svc.Costs(context.Background(), allClaude, "")
	var total, sessions int
	for _, r := range rows[1:] {
		if r[6] == "custo_total" && r[5] == "" && r[8] == "USD" {
			total++
			if r[2] != "claude" || r[7] != v.Totals[0].Total.String() || len(strings.Split(r[7], ".")[1]) != 9 || r[9] == "" {
				t.Errorf("linha %v (total %s)", r, v.Totals[0].Total)
			}
		}
		if r[5] != "" {
			sessions++
			if !strings.HasPrefix(r[5], "claude:") {
				t.Errorf("sessão %q", r[5])
			}
		}
	}
	if total != 1 || sessions == 0 {
		t.Errorf("linhas de total %d, de sessão %d", total, sessions)
	}
	// Nenhum título de sessão no arquivo.
	b, _ := os.ReadFile(path)
	titles, _ := c.index.Query("SELECT title FROM sessions WHERE title != ''")
	defer titles.Close()
	for titles.Next() {
		var ti string
		titles.Scan(&ti)
		if strings.Contains(string(b), ti) {
			t.Errorf("título %q exportado", ti)
		}
	}
}

func TestCostCSVCancelled(t *testing.T) {
	c := newCostEnv(t)
	c.svc.d.Save = func(string) (string, error) { return "", nil }
	path, err := c.svc.ExportCSV(context.Background(), allClaude)
	if path != "" || err != nil {
		t.Errorf("%q %v", path, err)
	}
}

func TestPatternsCSVContent(t *testing.T) {
	e := newStatsEnv(t)
	e.put(t, sessOf("a", "/p"), at(21, 23, 0), at(22, 8, 0))
	a := NewAnalyticsService(e.store, e.svc, discard)
	a.now = func() time.Time { return e.now }
	save, name := csvFile(t)
	p := NewPatternsService(e.store, a, discard, save)
	path, err := p.ExportCSV(context.Background(), AnalyticsQuery{Period: "7d"})
	if err != nil {
		t.Fatal(err)
	}
	v, _ := p.Patterns(context.Background(), AnalyticsQuery{Period: "7d"})
	found := false
	for _, r := range readCSV(t, path)[1:] {
		if r[6] == "sessoes_retomadas" && r[7] == strconv.Itoa(v.Resumed.Value) {
			found = true
		}
		if r[8] != "" || r[9] != "" || r[10] != "" {
			t.Errorf("moeda/status/cobertura em padrões: %v", r)
		}
	}
	if !found || !strings.HasPrefix(*name, "sessionflow-padroes-") {
		t.Errorf("sessoes_retomadas ausente (%s)", *name)
	}
}

// Grupo com respostas com e sem preço: status e cobertura do grupo inteiro,
// não só da parte com custo (que sempre daria "complete, 100.0").
func TestCostCSVMixedGroupCoverage(t *testing.T) {
	c := newCostEnv(t)
	ctx := context.Background()
	// Uma resposta do Claude sem horário: fica "sem dados" no mesmo grupo.
	if _, err := c.index.Exec(`UPDATE usage SET ts = NULL WHERE id = (SELECT u.id FROM usage u JOIN sessions s ON s.id = u.session_id
	  WHERE s.provider = 'claude' AND s.parent_native_id IS NULL
	    AND (SELECT count(*) FROM usage x WHERE x.response_id = u.response_id) = 1 ORDER BY u.id LIMIT 1)`); err != nil {
		t.Fatal(err)
	}
	c.add(t, claudeUSD)
	save, _ := csvFile(t)
	c.svc.d.Save = save
	path, err := c.svc.ExportCSV(ctx, allClaude)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range readCSV(t, path)[1:] {
		if r[6] == "custo_total" && r[5] == "" && r[8] == "USD" {
			if r[9] != "insufficient" || r[10] == "100.0" {
				t.Errorf("status %q cobertura %q", r[9], r[10])
			}
			return
		}
	}
	t.Error("linha custo_total ausente")
}

// Hardening, item 7: BOM e neutralização de fórmula.
func TestCSVBOMOnce(t *testing.T) {
	var buf bytes.Buffer
	if err := writeCSV(&buf, []csvRow{{"a"}}); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if !bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("bytes iniciais % x", b[:3])
	}
	if bytes.Count(b, []byte{0xEF, 0xBB, 0xBF}) != 1 {
		t.Error("BOM repetido")
	}
	if !bytes.HasPrefix(b[3:], []byte("periodo_inicio,")) {
		t.Errorf("cabeçalho não vem logo após o BOM: %q", b[3:20])
	}
}

func TestCSVTextNeutralizes(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"São João/ação", "São João/ação"},
		{"=1+1", "'=1+1"},
		{"+55 11", "'+55 11"},
		{"-cmd", "'-cmd"},
		{"@SUM(A1)", "'@SUM(A1)"},
		{"\t=1", "'\t=1"},
		{"\tx", "'\tx"},
		{"\r=1", "'\r=1"},
		{"\rx", "'\rx"},
		{"   =HYPERLINK(\"x\")", "'   =HYPERLINK(\"x\")"},
		{" \u00a0+1", "' \u00a0+1"},
		{"\x00@x", "'\x00@x"},
		{"a,b", "a,b"},
		{`diz "oi"`, `diz "oi"`},
		{"linha1\nlinha2", "linha1\nlinha2"},
		{"/home/x", "/home/x"},
		{"x=1", "x=1"},
		{"", ""},
		{"   ", "   "},
	} {
		if got := csvText(tc.in); got != tc.want {
			t.Errorf("csvText(%q) = %q, esperado %q", tc.in, got, tc.want)
		}
	}
}

func TestCSVQuotingRoundTrip(t *testing.T) {
	fields := []string{"a,b", `diz "oi"`, "linha1\nlinha2", "São João", "\tx"}
	var buf bytes.Buffer
	row := csvRow{"2026-11-01T00:00:00-03:00", "", csvText(fields[0]), csvText(fields[1]), csvText(fields[2]), csvText(fields[3]), "custo_total", "12.500000000", "USD", "complete", "100.0"}
	row2 := csvRow{"", "", csvText(fields[4]), "", "", "", "respostas", "-3", "", "", ""}
	if err := writeCSV(&buf, []csvRow{row, row2}); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(buf.Bytes(), []byte(utf8BOM)))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if rows[1][2] != "a,b" || rows[1][3] != `diz "oi"` || rows[1][4] != "linha1\nlinha2" || rows[1][5] != "São João" {
		t.Errorf("texto não sobreviveu à ida e volta: %q", rows[1])
	}
	if rows[2][2] != "'\tx" {
		t.Errorf("tabulação inicial não neutralizada: %q", rows[2][2])
	}
	// Numéricos ficam como vieram (sem apóstrofo), inclusive negativo.
	if rows[1][7] != "12.500000000" || rows[2][7] != "-3" || rows[1][10] != "100.0" {
		t.Errorf("número alterado: %q %q %q", rows[1][7], rows[2][7], rows[1][10])
	}
}

func TestCSVTextFormatChars(t *testing.T) {
	for _, in := range []string{"\u200b=1+1", "\ufeff@x", "\u2060-cmd", " \u200b+1"} {
		if got := csvText(in); got != "'"+in {
			t.Errorf("csvText(%q) = %q: caractere invisível antes da fórmula", in, got)
		}
	}
}
