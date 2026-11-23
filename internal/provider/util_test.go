package provider

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindCLI(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tool")
	os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755)
	plain := filepath.Join(dir, "plain")
	os.WriteFile(plain, nil, 0o644)
	notFound := func(string) (string, error) { return "", errors.New("não") }
	inPath := func(string) (string, error) { return "/usr/bin/tool", nil }

	cases := []struct {
		name, configured, want string
		look                   LookPath
		cands                  []string
	}{
		{"configurado vence", exe, exe, inPath, nil},
		{"configurado não executável", plain, "", inPath, nil},
		{"PATH", "", "/usr/bin/tool", inPath, []string{exe}},
		{"candidato", "", exe, notFound, []string{"relativo/tool", plain, exe}},
		{"nada", "", "", notFound, []string{plain}},
	}
	for _, c := range cases {
		if got := FindCLI(c.configured, "tool", c.look, c.cands...); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestTruncateKeepsUTF8(t *testing.T) {
	if got := Truncate("ação", 2); got != "a" {
		t.Errorf("got %q", got)
	}
	if got := Truncate("abc", 10); got != "abc" {
		t.Errorf("got %q", got)
	}
}

func TestFirstLineAndTimes(t *testing.T) {
	if got := FirstLine("\n  \n  olá mundo \nresto"); got != "olá mundo" {
		t.Errorf("FirstLine = %q", got)
	}
	if ParseTime("") != nil || ParseTime("ontem") != nil || MillisTime(0) != nil {
		t.Error("valores inválidos devem dar nil")
	}
	var s Span
	s.Add(ParseTime("2026-01-02T00:00:00Z"))
	s.Add(nil)
	s.Add(MillisTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()))
	if s.First.Day() != 1 || s.Last.Day() != 2 {
		t.Errorf("span = %+v", s)
	}
}

func TestStatSourceRejectsDir(t *testing.T) {
	if _, err := StatSource(t.TempDir(), "jsonl"); err == nil {
		t.Error("diretório não é fonte")
	}
}
