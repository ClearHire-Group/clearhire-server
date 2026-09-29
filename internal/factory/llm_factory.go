package factory

import (
	"github.com/ClearHire-Group/clearhire-server/internal/config"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm/anthropicadapter"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm/deterministic"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm/groqadapter"
)

// llmMaxRetries é quantas re-tentativas, além da primeira, para erro SEM resposta (429/5xx/rede).
// Resposta cobrada nunca é re-tentada — ver llm.retryable.
const llmMaxRetries = 2

// newLLM monta o Extractor e o Assessor do provedor escolhido em LLM_PROVIDER. Tudo que chama um
// provedor pago passa por llm.Limits (concorrência limitada + re-tentativa segura), compartilhado
// entre extração e avaliação porque as duas gastam o mesmo limite de taxa da mesma chave.
//
// Assessor nil significa "avaliação por IA não habilitada neste provedor" — só o adapter da Groq a
// implementa hoje. O service trata nil com uma mensagem clara em vez de derrubar nada.
//
// config.Load já recusou a configuração inválida (provedor sem chave, modelo sem preço), então aqui
// não há o que validar de novo.
func newLLM(cfg *config.Config) (llm.Extractor, llm.Assessor) {
	limits := llm.NewLimits(llm.LimitsConfig{MaxConcurrent: cfg.LLMMaxConcurrency, MaxRetries: llmMaxRetries})

	switch cfg.LLMProvider {
	case config.ProviderGroq:
		adapter := groqadapter.NewWithEndpoint(cfg.GroqAPIKey, cfg.GroqModel, cfg.GroqBaseURL)
		return llm.WithLimits(adapter, limits), llm.AssessorWithLimits(adapter, limits)
	case config.ProviderAnthropic:
		return llm.WithLimits(anthropicadapter.New(cfg.AnthropicAPIKey, cfg.AnthropicModel), limits), nil
	default:
		// Regex + dicionário: zero custo, zero rede, e por isso sem limites nem avaliador.
		return deterministic.New(), nil
	}
}
