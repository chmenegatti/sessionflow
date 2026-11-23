// Package domain contém o modelo interno do SessionFlow, independente dos
// formatos de armazenamento de cada provider. Não depende de nenhum outro
// pacote do projeto.
package domain

// ProviderID identifica um agente de programação suportado.
type ProviderID string

const (
	ProviderClaude   ProviderID = "claude"
	ProviderCodex    ProviderID = "codex"
	ProviderOpenCode ProviderID = "opencode"
)

// AllProviders devolve os providers suportados em ordem estável.
func AllProviders() []ProviderID {
	return []ProviderID{ProviderClaude, ProviderCodex, ProviderOpenCode}
}

// Valid informa se o ID corresponde a um provider suportado.
func (p ProviderID) Valid() bool {
	switch p {
	case ProviderClaude, ProviderCodex, ProviderOpenCode:
		return true
	}
	return false
}

// DisplayName devolve o nome exibido na interface.
func (p ProviderID) DisplayName() string {
	switch p {
	case ProviderClaude:
		return "Claude Code"
	case ProviderCodex:
		return "OpenAI Codex"
	case ProviderOpenCode:
		return "OpenCode"
	}
	return string(p)
}
