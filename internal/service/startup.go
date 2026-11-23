package service

// StartupError descreve uma falha que impediu o app de iniciar.
type StartupError struct {
	Message string `json:"message"` // "" = iniciou normalmente
	LogFile string `json:"logFile"`
}

// StartupService informa ao frontend se a inicialização falhou. No modo de
// erro é o único serviço registrado; a interface mostra só a tela de erro.
type StartupService struct{ err StartupError }

// NewStartupService cria o serviço. cause nil = inicialização normal. A
// mensagem vem de erros de abertura (caminhos, permissões), nunca de
// conteúdo de conversas.
func NewStartupService(cause error, logFile string) *StartupService {
	s := &StartupService{err: StartupError{LogFile: logFile}}
	if cause != nil {
		s.err.Message = cause.Error()
	}
	return s
}

// Error devolve a falha de inicialização (Message vazio = nenhuma).
func (s *StartupService) Error() StartupError { return s.err }
