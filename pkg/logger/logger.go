// Package logger dá uma única forma de logar pra aplicação inteira. Hoje é
// só um wrapper fino sobre log/slog — trocar de biblioteca de log no futuro
// não deve tocar em nenhum outro pacote.
package logger

import (
	"log/slog"
	"os"
)

var log *slog.Logger

func Init(env string) {
	handler := slog.NewJSONHandler(os.Stdout, nil)
	log = slog.New(handler)
}

func L() *slog.Logger {
	if log == nil {
		Init("development")
	}
	return log
}
