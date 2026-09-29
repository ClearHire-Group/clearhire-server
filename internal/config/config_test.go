package config

import (
	"strings"
	"testing"
)

const strongSecret = "um-segredo-de-teste-com-mais-de-32-caracteres"

func setBase(t *testing.T) {
	t.Helper()
	t.Setenv("JWT_SECRET", strongSecret)
	t.Setenv("DATABASE_URL", "postgres://teste")
	for _, k := range []string{"LLM_PROVIDER", "GROQ_API_KEY", "GROQ_MODEL", "ANTHROPIC_API_KEY", "ANTHROPIC_MODEL",
		"LLM_MAX_CONCURRENCY", "TRUSTED_PROXIES", "CLIENT_IP_HEADER"} {
		t.Setenv(k, "")
	}
}

// Sem LLM_PROVIDER o sistema se comporta como sempre: extrator determinístico, nenhuma chave
// exigida, nenhuma chamada paga.
func TestDefaultProviderIsDeterministic(t *testing.T) {
	setBase(t)

	cfg, err := Load()

	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if cfg.LLMProvider != ProviderDeterministic {
		t.Errorf("provedor padrão = %q", cfg.LLMProvider)
	}
}

// Fail-closed: pedir um provedor sem chave e cair em silêncio para "sem IA" esconderia o erro de
// configuração até alguém perceber que os currículos não estão sendo lidos.
func TestGroqWithoutKeyRefusesToBoot(t *testing.T) {
	setBase(t)
	t.Setenv("LLM_PROVIDER", "groq")

	_, err := Load()

	if err == nil || !strings.Contains(err.Error(), "GROQ_API_KEY") {
		t.Errorf("erro = %v, esperava recusa citando GROQ_API_KEY", err)
	}
}

func TestGroqWithKeyBoots(t *testing.T) {
	setBase(t)
	t.Setenv("LLM_PROVIDER", "groq")
	t.Setenv("GROQ_API_KEY", "gsk_teste")

	cfg, err := Load()

	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if cfg.GroqModel != "openai/gpt-oss-20b" {
		t.Errorf("modelo padrão = %q", cfg.GroqModel)
	}
}

// Modelo sem json_schema estrito cairia num modo sem validação de schema: falha só em produção.
func TestGroqUnsupportedModelRefusesToBoot(t *testing.T) {
	setBase(t)
	t.Setenv("LLM_PROVIDER", "groq")
	t.Setenv("GROQ_API_KEY", "gsk_teste")
	t.Setenv("GROQ_MODEL", "llama-3.3-70b-versatile")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GROQ_MODEL") {
		t.Errorf("erro = %v, esperava recusa citando GROQ_MODEL", err)
	}
}

func TestAnthropicWithoutKeyRefusesToBoot(t *testing.T) {
	setBase(t)
	t.Setenv("LLM_PROVIDER", "anthropic")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("erro = %v", err)
	}
}

// Sem preço, o gasto do modelo fica invisível para o teto de orçamento (custo NULL soma zero).
func TestModelWithoutPriceRefusesToBoot(t *testing.T) {
	setBase(t)
	t.Setenv("LLM_PROVIDER", "anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "sk-teste")
	t.Setenv("ANTHROPIC_MODEL", "modelo-que-nao-existe")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "preço") {
		t.Errorf("erro = %v, esperava recusa por falta de preço", err)
	}
}

func TestUnknownProviderRefusesToBoot(t *testing.T) {
	setBase(t)
	t.Setenv("LLM_PROVIDER", "gpt-mágico")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "LLM_PROVIDER") {
		t.Errorf("erro = %v", err)
	}
}

func TestProviderIsCaseInsensitive(t *testing.T) {
	setBase(t)
	t.Setenv("LLM_PROVIDER", " Deterministic ")

	cfg, err := Load()

	if err != nil || cfg.LLMProvider != ProviderDeterministic {
		t.Errorf("cfg=%v err=%v", cfg, err)
	}
}

func TestInvalidConcurrencyRefusesToBoot(t *testing.T) {
	for _, bad := range []string{"0", "-1", "abc"} {
		setBase(t)
		t.Setenv("LLM_MAX_CONCURRENCY", bad)
		if _, err := Load(); err == nil {
			t.Errorf("LLM_MAX_CONCURRENCY=%q deveria recusar", bad)
		}
	}
}

// Padrão seguro: só o loopback é proxy confiável, e o header é o que o nginx do projeto sobrescreve.
// Achado de auditoria de segurança: o default era "development", e development libera quatro
// coisas de uma vez — cookie sem Secure, link de reset e token de convite devolvidos na resposta
// HTTP, e JWT_SECRET fraco apenas avisando. Esquecer APP_ENV no deploy entregaria a redefinição de
// senha de qualquer empresa a um anônimo. Esquecer a variável tem que resultar no lado SEGURO.
func TestMissingAppEnvDefaultsToProduction(t *testing.T) {
	setBase(t)
	t.Setenv("APP_ENV", "")

	cfg, err := Load()

	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if cfg.Env != EnvProduction {
		t.Errorf("APP_ENV ausente resolveu para %q, esperava %q", cfg.Env, EnvProduction)
	}
	if cfg.IsDevelopment() {
		t.Error("APP_ENV ausente não pode contar como desenvolvimento")
	}
}

// Só o valor exato afrouxa proteção: um typo ("dev", "Development", "prod") cai em produção.
func TestOnlyExactDevelopmentLoosensProtections(t *testing.T) {
	for _, value := range []string{"dev", "Development", "DEVELOPMENT", "prod", "staging"} {
		t.Run(value, func(t *testing.T) {
			setBase(t)
			t.Setenv("APP_ENV", value)

			cfg, err := Load()

			if err != nil {
				t.Fatalf("erro: %v", err)
			}
			if cfg.IsDevelopment() {
				t.Errorf("APP_ENV=%q foi tratado como desenvolvimento", value)
			}
		})
	}
}

// E o caminho legítimo continua funcionando: quem roda local pede development explicitamente.
func TestExplicitDevelopmentIsHonored(t *testing.T) {
	setBase(t)
	t.Setenv("APP_ENV", EnvDevelopment)

	cfg, err := Load()

	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if !cfg.IsDevelopment() {
		t.Error("APP_ENV=development deveria ligar o modo de desenvolvimento")
	}
}

// JWT_SECRET curto é só aviso em development, mas tem que RECUSAR a subida em produção — e agora
// produção é o default, então esta é a rede que pega um deploy sem segredo configurado.
func TestWeakSecretRefusesToBootOutsideDevelopment(t *testing.T) {
	setBase(t)
	t.Setenv("APP_ENV", "")
	t.Setenv("JWT_SECRET", "curto-demais")

	if _, err := Load(); err == nil {
		t.Fatal("esperava recusa de boot com JWT_SECRET fraco fora de development")
	}
}

func TestProxyDefaultsAreSafe(t *testing.T) {
	setBase(t)

	cfg, err := Load()

	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.TrustedProxies) != 2 || cfg.TrustedProxies[0] != "127.0.0.1" {
		t.Errorf("proxies confiáveis padrão = %v", cfg.TrustedProxies)
	}
	if cfg.ClientIPHeader != "X-Real-IP" {
		t.Errorf("header padrão = %q — X-Forwarded-For acumula o que o cliente mandar", cfg.ClientIPHeader)
	}
}

func TestTrustedProxiesParsing(t *testing.T) {
	setBase(t)
	t.Setenv("TRUSTED_PROXIES", " 10.0.0.0/8 , 172.16.0.0/12,, ")

	cfg, err := Load()

	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.TrustedProxies) != 2 || cfg.TrustedProxies[1] != "172.16.0.0/12" {
		t.Errorf("proxies = %v", cfg.TrustedProxies)
	}
}

// Um .env copiado do .env.example tem as chaves em branco. Vazio tem que valer como "não definido",
// não como um valor inválido que derruba o boot nem como um provedor sem nome.
func TestBlankValuesFallBackToDefaults(t *testing.T) {
	setBase(t) // deixa todas as variáveis de IA/proxy definidas, porém vazias

	cfg, err := Load()

	if err != nil {
		t.Fatalf("variáveis em branco derrubaram o boot: %v", err)
	}
	if cfg.LLMProvider != ProviderDeterministic || cfg.LLMMaxConcurrency != 4 || cfg.ClientIPHeader != "X-Real-IP" ||
		cfg.GroqModel != "openai/gpt-oss-20b" || len(cfg.TrustedProxies) != 2 {
		t.Errorf("padrões não aplicados: %+v", cfg)
	}
}
