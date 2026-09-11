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

// LogSender é o sender de desenvolvimento: não manda nada de verdade, só loga o conteúdo — quem
// precisa do link (convite, redefinição de senha) pega no log do servidor. Suficiente enquanto
// não existe integração SMTP real; nunca usar isto fora de development.
type LogSender struct{}

func (LogSender) Send(ctx context.Context, to, subject, body string) error {
	logger.L().Info("email (log mailer — SMTP real não configurado)", "to", to, "subject", subject, "body", body)
	return nil
}
