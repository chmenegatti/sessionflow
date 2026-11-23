package claude

import (
	"os"
	"path/filepath"
	"testing"

	"sessionflow/internal/domain"
)

// Um campo de consumo com tipo inesperado não pode derrubar a mensagem:
// ela continua no viewer e na busca; só o consumo dela é descartado.
func TestUsageMalformedKeepsMessage(t *testing.T) {
	const sid = "99999999-9999-4999-8999-999999999999"
	head := `{"type":"user","sessionId":"` + sid + `","cwd":"/home/user/p","timestamp":"2026-11-20T12:00:00.000Z","uuid":"u1","message":{"role":"user","content":"oi"}}` + "\n"
	cases := map[string]string{
		"float":  `{"input_tokens":1.5,"output_tokens":2,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}`,
		"string": `{"input_tokens":"5","output_tokens":2,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}`,
		"huge":   `{"input_tokens":99999999999999999999,"output_tokens":2,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}`,
	}
	for name, usage := range cases {
		t.Run(name, func(t *testing.T) {
			line := `{"type":"assistant","sessionId":"` + sid + `","cwd":"/home/user/p","timestamp":"2026-11-20T12:01:00.000Z","uuid":"u2","message":{"model":"claude-opus-5-5","id":"msg_X","role":"assistant","content":[{"type":"text","text":"RESPOSTA VISIVEL"}],"usage":` + usage + `}}` + "\n"
			path := filepath.Join(t.TempDir(), sid+".jsonl")
			if err := os.WriteFile(path, []byte(head+line), 0o600); err != nil {
				t.Fatal(err)
			}
			sink, stats := parse(t, New(Options{}), path)
			if stats.BadLines != 0 {
				t.Errorf("BadLines = %d; quer 0", stats.BadLines)
			}
			msgs := sink.Messages[sink.Sessions[0].Key]
			if len(msgs) != 2 {
				t.Fatalf("%d mensagens; quer 2 (a resposta não pode sumir)", len(msgs))
			}
			if n := len(sink.Usages[sink.Sessions[0].Key]); n != 0 {
				t.Errorf("%d registros de consumo; quer 0 (consumo ilegível = indisponível)", n)
			}
			if stats.SkippedItems == 0 {
				t.Error("consumo descartado deveria contar em SkippedItems")
			}
		})
	}
}

// Respostas sem message.id, requestId e uuid não podem ser juntadas num
// registro só (identidade vazia): cada linha vira o próprio registro.
func TestUsageWithoutAnyIDNotMerged(t *testing.T) {
	const sid = "99999999-9999-4999-8999-999999999998"
	line := func(text string, out int) string {
		return `{"type":"assistant","sessionId":"` + sid + `","cwd":"/home/user/p","timestamp":"2026-11-20T12:01:00.000Z","message":{"model":"claude-opus-5-5","role":"assistant","content":[{"type":"text","text":"` + text + `"}],"usage":{"input_tokens":1,"output_tokens":` + string(rune('0'+out)) + `,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}` + "\n"
	}
	path := filepath.Join(t.TempDir(), sid+".jsonl")
	if err := os.WriteFile(path, []byte(line("um", 2)+line("dois", 3)), 0o600); err != nil {
		t.Fatal(err)
	}
	sink, _ := parse(t, New(Options{}), path)
	us := sink.Usages[sink.Sessions[0].Key]
	if len(us) != 2 {
		t.Fatalf("%d registros; quer 2", len(us))
	}
	if us[0].ResponseID == "" || us[0].ResponseID == us[1].ResponseID {
		t.Errorf("identidades: %q, %q", us[0].ResponseID, us[1].ResponseID)
	}
}

// Contagem negativa vira indisponível e não deriva total.
func TestUsageNegativeIsUnavailable(t *testing.T) {
	const sid = "99999999-9999-4999-8999-999999999997"
	line := `{"type":"assistant","sessionId":"` + sid + `","cwd":"/home/user/p","timestamp":"2026-11-20T12:01:00.000Z","uuid":"u9","message":{"model":"claude-opus-5-5","id":"msg_N","role":"assistant","content":[{"type":"text","text":"x"}],"usage":{"input_tokens":-5,"output_tokens":2,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}` + "\n"
	path := filepath.Join(t.TempDir(), sid+".jsonl")
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	sink, _ := parse(t, New(Options{}), path)
	u := sink.Usages[sink.Sessions[0].Key][0]
	if u.Input != nil || u.Total != nil {
		t.Errorf("entrada %v total %v; quer indisponíveis", val(u.Input), val(u.Total))
	}
	if u.Status != domain.UsageStatusPartial {
		t.Errorf("status %s; quer partial", u.Status)
	}
}
