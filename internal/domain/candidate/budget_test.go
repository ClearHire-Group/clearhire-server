package candidate

import (
	"context"
	"errors"
	"testing"

	"github.com/ClearHire-Group/clearhire-server/internal/domain/llmusage"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

// O teto bloqueia de fato — não é alerta. Sem isto, a rota pública anônima é gasto ilimitado.
func TestBudgetExceededBlocksPaidCall(t *testing.T) {
	usage := newRecordingUsage()
	usage.spent, usage.limit = 10.0, 10.0
	extractor := &countingExtractor{}
	s := &service{repo: newMemoryCacheRepo(), extractor: extractor, usage: usage}

	_, err := s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput("Marina"))

	if !errors.Is(err, llm.ErrBudgetExceeded) {
		t.Fatalf("erro = %v, esperava ErrBudgetExceeded", err)
	}
	if extractor.calls != 0 {
		t.Errorf("extrator foi chamado %d vezes com o orçamento estourado", extractor.calls)
	}
}

// Estourar o teto não pode ser silencioso: sem registro, o gasto pararia de crescer e não haveria
// como distinguir "ninguém se candidatou" de "recusamos todo mundo".
func TestBlockedCallIsRecorded(t *testing.T) {
	usage := newRecordingUsage()
	usage.spent, usage.limit = 10.0, 10.0
	s := &service{repo: newMemoryCacheRepo(), extractor: &countingExtractor{}, usage: usage}

	_, _ = s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput("Marina"))

	if len(usage.records) != 1 {
		t.Fatalf("recusa por orçamento não foi registrada: %v", usage.statuses())
	}
	if got := usage.records[0].Usage.Provider; got != budgetProvider {
		t.Errorf("provider = %q, esperava %q", got, budgetProvider)
	}
}

// Propriedade importante: extração já paga antes não gera chamada nova, então continuar servindo do
// cache com o orçamento estourado é gratuito. Bloquear ali puniria a empresa sem economizar nada.
func TestCacheStillServedWhenOverBudget(t *testing.T) {
	usage := newRecordingUsage()
	extractor := &countingExtractor{}
	s := &service{repo: newMemoryCacheRepo(), extractor: extractor, usage: usage}
	cv := "Marina Albuquerque"

	// Primeiro envio dentro do orçamento, popula o cache.
	if _, err := s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput(cv)); err != nil {
		t.Fatalf("primeiro envio falhou: %v", err)
	}

	// Orçamento estoura depois.
	usage.spent, usage.limit = 10.0, 10.0

	profile, err := s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-2", resumeInput(cv))
	if err != nil {
		t.Fatalf("cache deveria continuar servindo com orçamento estourado, veio: %v", err)
	}
	if profile == nil {
		t.Fatal("perfil nulo")
	}
	if extractor.calls != 1 {
		t.Errorf("extrator chamado %d vezes — o segundo envio deveria ter vindo do cache", extractor.calls)
	}
	if last := usage.records[len(usage.records)-1].Status; last != llmusage.StatusCacheHit {
		t.Errorf("último registro = %q, esperava cache_hit", last)
	}
}

// Fail-open deliberado: instabilidade momentânea do banco não pode recusar candidato real. O teto
// protege contra abuso sustentado, não contra o minuto em que o Postgres piscou.
func TestBudgetReadFailureAllowsCall(t *testing.T) {
	usage := newRecordingUsage()
	usage.budgetErr = errors.New("conexão caiu")
	extractor := &countingExtractor{}
	s := &service{repo: newMemoryCacheRepo(), extractor: extractor, usage: usage}

	if _, err := s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput("Marina")); err != nil {
		t.Fatalf("falha ao ler orçamento deveria liberar a chamada, veio: %v", err)
	}
	if extractor.calls != 1 {
		t.Errorf("extrator chamado %d vezes, esperava 1", extractor.calls)
	}
}

func TestSpendingUnderLimitIsAllowed(t *testing.T) {
	usage := newRecordingUsage()
	usage.spent, usage.limit = 9.99, 10.0
	extractor := &countingExtractor{}
	s := &service{repo: newMemoryCacheRepo(), extractor: extractor, usage: usage}

	if _, err := s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput("Marina")); err != nil {
		t.Fatalf("abaixo do teto deveria passar: %v", err)
	}
	if extractor.calls != 1 {
		t.Errorf("extrator chamado %d vezes, esperava 1", extractor.calls)
	}
}

// Teto zero é o desligamento explícito da extração por IA para a empresa.
func TestZeroBudgetDisablesAIExtraction(t *testing.T) {
	usage := newRecordingUsage()
	usage.spent, usage.limit = 0, 0
	extractor := &countingExtractor{}
	s := &service{repo: newMemoryCacheRepo(), extractor: extractor, usage: usage}

	if _, err := s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput("Marina")); !errors.Is(err, llm.ErrBudgetExceeded) {
		t.Fatalf("teto zero deveria bloquear, veio: %v", err)
	}
	if extractor.calls != 0 {
		t.Errorf("extrator chamado %d vezes com teto zero", extractor.calls)
	}
}

// O candidato nunca pode descobrir o estado financeiro da empresa: a recusa por orçamento tem que
// virar a MESMA mensagem de qualquer falha de IA.
func TestBudgetErrorIsOpaqueToApplicant(t *testing.T) {
	budget := mapExtractionError(llm.ErrBudgetExceeded)
	provider := mapExtractionError(llm.ErrProviderUnavailable)

	if budget.Error() != provider.Error() {
		t.Errorf("mensagens diferentes vazam o motivo:\n orçamento: %s\n provedor:  %s", budget, provider)
	}
}
