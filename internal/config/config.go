// Package config centraliza a leitura de variáveis de ambiente. Nenhum outro
// pacote deve chamar os.Getenv diretamente — tudo passa por aqui, uma vez.
package config

import "os"

type Config struct {
	Env         string
	Port        string
	DatabaseURL string
	JWTSecret   string
}

// Load lê o ambiente e devolve a configuração da aplicação. Valores default
// cobrem o ambiente local; produção deve sempre sobrescrever via env real.
func Load() (*Config, error) {
	cfg := &Config{
		Env:         getEnv("APP_ENV", "development"),
		Port:        getEnv("APP_PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", ""),
		JWTSecret:   getEnv("JWT_SECRET", ""),
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
