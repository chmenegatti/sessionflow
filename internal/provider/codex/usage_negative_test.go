package codex

import (
	"os"
	"path/filepath"
	"testing"

	"sessionflow/internal/domain"
)

// Contagem negativa não é consumo: vira indisponível, nunca entra em somas
// nem em campo derivado.
func TestCodexNegativeIsUnavailable(t *testing.T) {
	id := "88888888-8888-7888-8888-888888888887"
	line := `{"timestamp":"2026-11-20T10:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{` +
		`"last_token_usage":{"input_tokens":100,"cached_input_tokens":300,"output_tokens":-7,"reasoning_output_tokens":0,"total_tokens":393},` +
		`"total_token_usage":{"input_tokens":100,"cached_input_tokens":300,"output_tokens":-7,"reasoning_output_tokens":0,"total_tokens":393}}}}`
	data := `{"timestamp":"2026-11-20T10:00:00Z","type":"session_meta","payload":{"id":"` + id + `","cwd":"/p"}}` + "\n" + line + "\n"
	path := filepath.Join(t.TempDir(), "rollout-2026-11-20T10-00-00-"+id+".jsonl")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	us, _ := usageParse(t, path)
	if len(us) != 1 {
		t.Fatalf("%d registros", len(us))
	}
	u := us[0]
	if u.Input != nil {
		t.Errorf("entrada derivada = %d; quer indisponível (cached > input)", *u.Input)
	}
	if u.Output != nil {
		t.Errorf("saída = %d; quer indisponível (negativa)", *u.Output)
	}
	if u.Status != domain.UsageStatusPartial {
		t.Errorf("status %s; quer partial", u.Status)
	}
}
