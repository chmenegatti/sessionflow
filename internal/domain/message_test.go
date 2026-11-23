package domain

import "testing"

func TestCountsAsMessage(t *testing.T) {
	text := Block{Kind: BlockText, Text: "olá"}
	for name, c := range map[string]struct {
		m    Message
		want bool
	}{
		"usuário com texto":          {Message{Role: RoleUser, Blocks: []Block{text}}, true},
		"usuário só com imagem":      {Message{Role: RoleUser, Blocks: []Block{{Kind: BlockImageRef}}}, false},
		"agente com texto e chamada": {Message{Role: RoleAssistant, Blocks: []Block{text, {Kind: BlockToolCall, ToolName: "Bash"}}}, true},
		"agente só com ferramenta":   {Message{Role: RoleAssistant, Blocks: []Block{{Kind: BlockToolCall, ToolName: "Bash"}}}, false},
		"resultado de ferramenta":    {Message{Role: RoleTool, Blocks: []Block{{Kind: BlockToolResult, Text: "ok"}}}, false},
		"sistema com texto":          {Message{Role: RoleSystem, Blocks: []Block{text}}, false},
		"texto só com espaços":       {Message{Role: RoleUser, Blocks: []Block{{Kind: BlockText, Text: " \n\t"}}}, false},
	} {
		if got := CountsAsMessage(c.m); got != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}
