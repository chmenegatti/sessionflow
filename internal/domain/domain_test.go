package domain

import (
	"errors"
	"testing"
)

func TestProviderID(t *testing.T) {
	for _, p := range AllProviders() {
		if !p.Valid() {
			t.Errorf("%q deveria ser válido", p)
		}
		if p.DisplayName() == string(p) {
			t.Errorf("%q sem nome de exibição", p)
		}
	}
	if ProviderID("gemini").Valid() {
		t.Error("provider desconhecido aceito")
	}
}

func TestSessionKeyRoundTrip(t *testing.T) {
	cases := []struct {
		in      string
		want    SessionKey
		wantErr bool
	}{
		{in: "claude:11111111-1111-4111-8111-111111111111", want: SessionKey{ProviderClaude, "11111111-1111-4111-8111-111111111111"}},
		{in: "opencode:ses_abc", want: SessionKey{ProviderOpenCode, "ses_abc"}},
		{in: "codex:id:com:dois-pontos", want: SessionKey{ProviderCodex, "id:com:dois-pontos"}},
		{in: "semseparador", wantErr: true},
		{in: "gemini:123", wantErr: true},
		{in: "claude:", wantErr: true},
		{in: "claude:   ", wantErr: true},
	}
	for _, c := range cases {
		got, err := ParseSessionKey(c.in)
		if c.wantErr {
			if !errors.Is(err, ErrInvalidSessionKey) {
				t.Errorf("%q: esperava ErrInvalidSessionKey, veio %v", c.in, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q: got %+v, %v", c.in, got, err)
		}
		if got.String() != c.in {
			t.Errorf("String() = %q, quer %q", got.String(), c.in)
		}
	}
}

func TestSameNativeIDDifferentProviders(t *testing.T) {
	a, _ := NewSessionKey(ProviderClaude, "x")
	b, _ := NewSessionKey(ProviderCodex, "x")
	if a == b {
		t.Fatal("chaves de providers diferentes não podem colidir")
	}
}

func TestNormalizeProjectPath(t *testing.T) {
	cases := map[string]string{
		"/home/u/proj":      "/home/u/proj",
		"/home/u/proj/":     "/home/u/proj",
		"/home/u/./proj//":  "/home/u/proj",
		"/home/u/x/../proj": "/home/u/proj",
		"/":                 "/",
	}
	for in, want := range cases {
		got, err := NormalizeProjectPath(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; quer %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "rel/proj", "./proj"} {
		if _, err := NormalizeProjectPath(bad); !errors.Is(err, ErrRelativeProjectPath) {
			t.Errorf("%q: esperava erro de caminho relativo, veio %v", bad, err)
		}
	}
}

func TestProjectName(t *testing.T) {
	if got := ProjectName("/home/u/demo-app"); got != "demo-app" {
		t.Errorf("got %q", got)
	}
	if got := ProjectName("/"); got != "/" {
		t.Errorf("raiz: got %q", got)
	}
}
