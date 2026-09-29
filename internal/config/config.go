// Package config centraliza a leitura de variáveis de ambiente. Nenhum outro
// pacote deve chamar os.Getenv diretamente — tudo passa por aqui, uma vez.
package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"

	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm/groqadapter"
)

// Provedores de IA aceitos em LLM_PROVIDER.
const (
	ProviderDeterministic = "deterministic"
	ProviderGroq          = "groq"
	ProviderAnthropic     = "anthropic"
)

// EnvDevelopment é o único valor que afrouxa proteções (ver Env em Load e IsDevelopment); qualquer
// outro valor, inclusive vazio ou digitado errado, é tratado como produção.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

type Config struct {
	Env         string
	Port        string
	DatabaseURL string
	JWTSecret   string
	CORSOrigin  string

	// LLMProvider escolhe quem faz a extração e a avaliação por IA. "deterministic" (padrão) não
	// chama API nenhuma. Qualquer outro valor exige chave e modelo válidos — o boot recusa a
	// configuração incompleta em vez de degradar em silêncio (ver validateLLM).
	LLMProvider       string
	LLMMaxConcurrency int
	AnthropicAPIKey   string
	AnthropicModel    string
	GroqAPIKey        string
	GroqModel         string
	GroqBaseURL       string // opcional: endpoint alternativo compatível (proxy interno, testes)

	// TrustedProxies/ClientIPHeader dizem de quem o servidor aceita o IP real do cliente. Sem isto,
	// atrás de um proxy todo mundo compartilha o IP do proxy (e o rate limit por IP vira um balde
	// único); com o header errado, o cliente escolhe o próprio IP. Ver server.New.
	TrustedProxies []string
	ClientIPHeader string
}

const minJWTSecretLen = 32

// Load lê o ambiente e devolve a configuração da aplicação. Um `.env` na raiz
// é carregado se existir (sem sobrescrever variáveis já exportadas no shell —
// ambiente real de produção nunca depende de arquivo). Valores default
// cobrem o que sobrar.
func Load() (*Config, error) {
	_ = godotenv.Load() // ausência de .env não é erro — produção não tem esse arquivo

	cfg := &Config{
		// Default PRODUÇÃO, de propósito. `development` liga quatro coisas de uma vez (cookie sem
		// Secure, link de reset e de convite devolvidos na resposta HTTP, e JWT_SECRET fraco só
		// avisando em vez de recusar a subida). Se isso fosse o default, esquecer a variável no
		// deploy entregaria redefinição de senha de qualquer empresa a um anônimo. O modo inseguro
		// tem que ser escolha explícita de quem roda local, nunca o que acontece por omissão.
		// getEnvOrDefault (e não getEnv): `APP_ENV=` vazio, que acontece em plataforma que exporta
		// variável em branco, tem que virar produção de verdade, não a string vazia.
		Env:             getEnvOrDefault("APP_ENV", EnvProduction),
		Port:            getEnv("APP_PORT", "8080"),
		DatabaseURL:     getEnv("DATABASE_URL", ""),
		JWTSecret:       getEnv("JWT_SECRET", ""),
		CORSOrigin:      getEnv("CORS_ORIGIN", "http://localhost:4200"),
		LLMProvider:     strings.ToLower(strings.TrimSpace(getEnvOrDefault("LLM_PROVIDER", ProviderDeterministic))),
		AnthropicAPIKey: getEnv("ANTHROPIC_API_KEY", ""),
		AnthropicModel:  getEnv("ANTHROPIC_MODEL", "claude-haiku-4-5-20251001"),
		GroqAPIKey:      getEnv("GROQ_API_KEY", ""),
		GroqModel:       getEnvOrDefault("GROQ_MODEL", "openai/gpt-oss-20b"),
		GroqBaseURL:     getEnvOrDefault("GROQ_BASE_URL", ""),
		// Padrão seguro: só aceita header de IP vindo de loopback (dev). Atrás de nginx/plataforma,
		// liste os IPs/CIDRs do proxy em TRUSTED_PROXIES.
		TrustedProxies: splitList(getEnvOrDefault("TRUSTED_PROXIES", "127.0.0.1,::1")),
		// X-Real-IP e não X-Forwarded-For: o nginx SOBRESCREVE o primeiro com o IP real, enquanto o
		// segundo acumula o que o cliente mandar — e o Fiber, sem validação, usa o valor bruto.
		ClientIPHeader: getEnvOrDefault("CLIENT_IP_HEADER", "X-Real-IP"),
	}

	concurrency, err := strconv.Atoi(getEnvOrDefault("LLM_MAX_CONCURRENCY", "4"))
	if err != nil || concurrency < 1 {
		return nil, fmt.Errorf("LLM_MAX_CONCURRENCY inválido: precisa ser um inteiro >= 1")
	}
	cfg.LLMMaxConcurrency = concurrency

	if err := cfg.validateJWTSecret(); err != nil {
		return nil, err
	}
	if err := cfg.validateLLM(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validateLLM recusa subir com uma configuração de IA que só falharia na primeira candidatura real.
//
// Fail-closed, de propósito: pedir um provedor sem chave e cair em silêncio para "sem IA" esconderia
// o erro de configuração até alguém perceber que os currículos não estão sendo lidos.
//
// Também exige preço conhecido para o modelo. Sem preço, llm_usage grava custo NULL, o teto de gasto
// mensal soma zero e o controle de orçamento deixa de enxergar aquele gasto — exatamente o buraco
// que o teto existe para fechar.
func (c *Config) validateLLM() error {
	switch c.LLMProvider {
	case ProviderDeterministic:
		return nil
	case ProviderGroq:
		if c.GroqAPIKey == "" {
			return fmt.Errorf("LLM_PROVIDER=groq exige GROQ_API_KEY")
		}
		if !groqadapter.SupportedModel(c.GroqModel) {
			return fmt.Errorf("GROQ_MODEL=%q não suporta saída estruturada estrita — veja groqadapter.SupportedModel", c.GroqModel)
		}
		return requirePrice("GROQ_MODEL", c.GroqModel)
	case ProviderAnthropic:
		if c.AnthropicAPIKey == "" {
			return fmt.Errorf("LLM_PROVIDER=anthropic exige ANTHROPIC_API_KEY")
		}
		return requirePrice("ANTHROPIC_MODEL", c.AnthropicModel)
	default:
		return fmt.Errorf("LLM_PROVIDER=%q desconhecido (use %s, %s ou %s)", c.LLMProvider,
			ProviderDeterministic, ProviderGroq, ProviderAnthropic)
	}
}

func requirePrice(envName, model string) error {
	if !llm.IsPriced(model) {
		return fmt.Errorf(
			"%s=%q não tem preço em pkg/llm/pricing.go — adicione o preço antes de usar este modelo, "+
				"senão o gasto dele fica invisível para o teto de orçamento", envName, model)
	}
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// validateJWTSecret falha o boot se o segredo de assinatura dos JWTs for
// obviamente fraco — sem isso o servidor sobe "com sucesso" assinando tokens
// forjáveis. O placeholder do .env.example é rejeitado sempre, mesmo em dev
// (é um erro barato de pegar); o comprimento mínimo só é obrigatório fora de
// dev, pra não travar quem está só rodando local com um valor curto.
// IsDevelopment é o único lugar que decide o que conta como ambiente de desenvolvimento. Compara
// com o valor exato: `dev`, `Development` ou um typo qualquer caem em produção, que é o lado seguro
// de errar.
func (c *Config) IsDevelopment() bool {
	return c.Env == EnvDevelopment
}

func (c *Config) validateJWTSecret() error {
	if c.JWTSecret == "changeme" {
		return fmt.Errorf("JWT_SECRET ainda é o valor placeholder do .env.example — defina um segredo real")
	}
	if len(c.JWTSecret) < minJWTSecretLen {
		if !c.IsDevelopment() {
			return fmt.Errorf("JWT_SECRET ausente ou curto demais (mínimo %d caracteres) para APP_ENV=%s", minJWTSecretLen, c.Env)
		}
		log.Printf("aviso: JWT_SECRET fraco ou ausente (%d caracteres) — aceitável em development, nunca em produção", len(c.JWTSecret))
	}
	return nil
}

// getEnvOrDefault trata variável vazia como não definida. Um `.env` copiado do .env.example costuma
// ter `LLM_PROVIDER=` em branco, e com getEnv isso chegaria como "" — sem cair no padrão. Usado nas
// variáveis de IA e de proxy, onde um vazio acidental mudaria o comportamento (ou derrubaria o boot).
func getEnvOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
