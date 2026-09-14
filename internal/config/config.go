// Package config centraliza a leitura de variáveis de ambiente. Nenhum outro
// pacote deve chamar os.Getenv diretamente — tudo passa por aqui, uma vez.
package config

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Env             string
	Port            string
	DatabaseURL     string
	JWTSecret       string
	CORSOrigin      string
	AnthropicAPIKey string
	AnthropicModel  string
}

const minJWTSecretLen = 32

// Load lê o ambiente e devolve a configuração da aplicação. Um `.env` na raiz
// é carregado se existir (sem sobrescrever variáveis já exportadas no shell —
// ambiente real de produção nunca depende de arquivo). Valores default
// cobrem o que sobrar.
func Load() (*Config, error) {
	_ = godotenv.Load() // ausência de .env não é erro — produção não tem esse arquivo

	cfg := &Config{
		Env:         getEnv("APP_ENV", "development"),
		Port:        getEnv("APP_PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", ""),
		JWTSecret:   getEnv("JWT_SECRET", ""),
		CORSOrigin:  getEnv("CORS_ORIGIN", "http://localhost:4200"),
		// Vazia é aceitável — só o modo currículo da candidatura pública fica indisponível (erro
		// claro, "tente o formulário manual"); nada mais no servidor depende disto pra subir.
		AnthropicAPIKey: getEnv("ANTHROPIC_API_KEY", ""),
		AnthropicModel:  getEnv("ANTHROPIC_MODEL", "claude-haiku-4-5-20251001"),
	}

	if err := cfg.validateJWTSecret(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validateJWTSecret falha o boot se o segredo de assinatura dos JWTs for
// obviamente fraco — sem isso o servidor sobe "com sucesso" assinando tokens
// forjáveis. O placeholder do .env.example é rejeitado sempre, mesmo em dev
// (é um erro barato de pegar); o comprimento mínimo só é obrigatório fora de
// dev, pra não travar quem está só rodando local com um valor curto.
func (c *Config) validateJWTSecret() error {
	if c.JWTSecret == "changeme" {
		return fmt.Errorf("JWT_SECRET ainda é o valor placeholder do .env.example — defina um segredo real")
	}
	if len(c.JWTSecret) < minJWTSecretLen {
		if c.Env != "development" {
			return fmt.Errorf("JWT_SECRET ausente ou curto demais (mínimo %d caracteres) para APP_ENV=%s", minJWTSecretLen, c.Env)
		}
		log.Printf("aviso: JWT_SECRET fraco ou ausente (%d caracteres) — aceitável em development, nunca em produção", len(c.JWTSecret))
	}
	return nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
