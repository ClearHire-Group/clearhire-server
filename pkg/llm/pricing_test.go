package llm

import (
	"math"
	"testing"
)

func TestEstimatedCostKnownModel(t *testing.T) {
	// 1M de entrada + 1M de saída no Haiku 4.5 = $1,00 + $5,00.
	cost := EstimatedCostUSD(Usage{Model: "claude-haiku-4-5", InputTokens: 1_000_000, OutputTokens: 1_000_000})

	if cost == nil {
		t.Fatal("modelo conhecido devolveu custo nil")
	}
	if math.Abs(*cost-6.00) > 0.000001 {
		t.Errorf("custo = %f, esperava 6.00", *cost)
	}
}

// A distinção que mais importa nesta tabela: modelo fora dela é DESCONHECIDO, nunca gratuito.
// Devolver 0 aqui faria um gasto real aparecer como zero em todo relatório.
func TestEstimatedCostUnknownModelIsNilNotZero(t *testing.T) {
	if cost := EstimatedCostUSD(Usage{Model: "modelo-que-nao-existe", InputTokens: 999_999}); cost != nil {
		t.Errorf("modelo desconhecido devolveu %f — deveria ser nil (desconhecido ≠ grátis)", *cost)
	}
}

// Caminho determinístico e cache hit não têm modelo: custo desconhecido é a resposta honesta, e a
// gratuidade fica evidente pelos zero tokens somados ao Provider.
func TestEstimatedCostWithoutModelIsNil(t *testing.T) {
	if cost := EstimatedCostUSD(Free("deterministic")); cost != nil {
		t.Errorf("uso sem modelo devolveu %f, esperava nil", *cost)
	}
}

// Trava a premissa central da auditoria: o tier barato do Gemini é ordens de grandeza abaixo do
// Haiku para a mesma carga. Se alguém editar a tabela e inverter isso, a recomendação de
// arquitetura deixa de valer.
func TestGeminiFlashLiteIsMuchCheaperThanHaiku(t *testing.T) {
	load := Usage{InputTokens: 1800, OutputTokens: 500}

	gemini := load
	gemini.Model = "gemini-2.5-flash-lite"
	haiku := load
	haiku.Model = "claude-haiku-4-5"

	g, h := EstimatedCostUSD(gemini), EstimatedCostUSD(haiku)
	if g == nil || h == nil {
		t.Fatal("modelo ausente da tabela de preços")
	}
	if *g >= *h/5 {
		t.Errorf("gemini flash-lite (%f) deveria ser bem abaixo de haiku (%f)", *g, *h)
	}
}
