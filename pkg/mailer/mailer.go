// Package mailer isola quem "envia e-mail" atrás de uma interface — hoje só existe LogSender
// (loga no lugar de mandar de verdade, sem SMTP configurado). Trocar por um sender real (SES,
// Postmark, etc.) não deve tocar em nenhum domínio que já injeta Sender.
package mailer

import (
	"context"

	"github.com/ClearHire-Group/clearhire-server/pkg/logger"
)

type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// LogSender é o sender de desenvolvimento: não manda nada de verdade, só registra a tentativa —
// quem precisa do link (convite, redefinição de senha) pega no log do servidor. Suficiente
// enquanto não existe integração SMTP real.
type LogSender struct {
	// IncludeBody controla se o CORPO da mensagem vai para o log. Só pode ser true em
	// desenvolvimento: o corpo carrega o link de redefinição de senha e o token de convite, e
	// quem lê o log (operador, serviço de observabilidade, quem vazar um arquivo de log) assume
	// qualquer conta com ele. Em produção o log prova que a tentativa existiu, sem o segredo.
	IncludeBody bool
}

func (s LogSender) Send(ctx context.Context, to, subject, body string) error {
	if s.IncludeBody {
		logger.L().Info("email (log mailer — SMTP real não configurado)", "to", to, "subject", subject, "body", body)
		return nil
	}
	logger.L().Info("email (log mailer — SMTP real não configurado; corpo omitido fora de development)",
		"to", to, "subject", subject)
	return nil
}
