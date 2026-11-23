package domain

import (
	"encoding/json"
	"strings"
	"time"
)

// Role é o autor de uma mensagem normalizada.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool"
)

// BlockKind é o tipo de um bloco de conteúdo.
type BlockKind string

const (
	BlockText       BlockKind = "text"
	BlockCode       BlockKind = "code"
	BlockToolCall   BlockKind = "tool_call"
	BlockToolResult BlockKind = "tool_result"
	BlockReasoning  BlockKind = "reasoning"
	BlockImageRef   BlockKind = "image_ref" // Language = tipo MIME; o conteúdo não é copiado
	BlockFile       BlockKind = "file"      // anexo não imagem; Text = nome, Language = tipo MIME
	BlockMetadata   BlockKind = "metadata"
	BlockUnknown    BlockKind = "unknown" // tipo novo/desconhecido do provider; nunca é erro
)

// Block é uma parte do conteúdo de uma mensagem.
type Block struct {
	Kind       BlockKind       `json:"kind"`
	Text       string          `json:"text,omitempty"`
	Language   string          `json:"language,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolInput  json.RawMessage `json:"toolInput,omitempty"`
	IsError    bool            `json:"isError,omitempty"`
	Raw        json.RawMessage `json:"raw,omitempty"` // original preservado para Metadata/Unknown
}

// Message é uma mensagem normalizada, em ordem física da fonte (Seq).
type Message struct {
	Seq       int        `json:"seq"`
	NativeID  string     `json:"nativeId,omitempty"`
	ParentID  string     `json:"parentId,omitempty"`
	Role      Role       `json:"role"`
	Timestamp *time.Time `json:"timestamp,omitempty"`
	Blocks    []Block    `json:"blocks"`
}

// Transcript é uma página de mensagens de uma sessão.
type Transcript struct {
	Session  Session   `json:"session"`
	Messages []Message `json:"messages"`
	HasMore  bool      `json:"hasMore"`
}

// CountsAsMessage é a regra de "mensagem" em todo o app (MessageCount, índice,
// estatísticas): fala do usuário ou resposta do agente com texto. Passos só
// de ferramenta, resultados, metadados e contexto do sistema não contam.
func CountsAsMessage(m Message) bool {
	if m.Role != RoleUser && m.Role != RoleAssistant {
		return false
	}
	for _, b := range m.Blocks {
		if b.Kind == BlockText && strings.TrimSpace(b.Text) != "" {
			return true
		}
	}
	return false
}
