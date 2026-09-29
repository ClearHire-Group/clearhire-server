package candidate

import (
	"context"
	"testing"

	"github.com/ClearHire-Group/clearhire-server/internal/domain/llmusage"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

type recordingUsage struct {
	records   []llmusage.Record
	spent     float64
	limit     float64
	budgetErr error
}

func newRecordingUsage() *recordingUsage {
	// Default generoso: os testes que não são sobre orçamento não devem esbarrar nele.
	return &recordingUsage{limit: 100}
}

func (r *recordingUsage) Record(_ context.Context, rec *llmusage.Record) error {
	r.records = append(r.records, *rec)
	return nil
}

func (r *recordingUsage) BudgetStatus(_ context.Context, _ string) (float64, float64, error) {
	return r.spent, r.limit, r.budgetErr
}

func (r *recordingUsage) statuses() []string {
	out := make([]string, len(r.records))
	for i, rec := range r.records {
		out[i] = rec.Status
	}
	return out
}

// billedFailureExtractor simula o pior caso de contabilidade: a resposta chegou (logo foi cobrada),
// mas veio inútil — JSON truncado por estourar MaxTokens, por exemplo.
type billedFailureExtractor struct{}

func (billedFailureExtractor) Extract(_ context.Context, _ llm.Input) (*llm.ExtractedProfile, llm.Usage, error) {
	usage := llm.Usage{Provider: "fake", Model: "claude-haiku-4-5", InputTokens: 1800, OutputTokens: 4096}
	return nil, usage, llm.ErrMalformedOutput
}

// networkFailureExtractor: não houve resposta, então não houve cobrança.
type networkFailureExtractor struct{}

func (networkFailureExtractor) Extract(_ context.Context, _ llm.Input) (*llm.ExtractedProfile, llm.Usage, error) {
	return nil, llm.Usage{Provider: "fake", Model: "claude-haiku-4-5"}, llm.ErrProviderUnavailable
}

func TestSuccessfulExtractionIsRecorded(t *testing.T) {
	usage := newRecordingUsage()
	s := &service{repo: newMemoryCacheRepo(), extractor: &countingExtractor{}, usage: usage}

	_, err := s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput("Marina"))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}

	if len(usage.records) != 1 {
		t.Fatalf("esperava 1 registro, veio %d", len(usage.records))
	}
	got := usage.records[0]
	if got.Status != llmusage.StatusSuccess {
		t.Errorf("status = %q", got.Status)
	}
	if got.Usage.InputTokens != 1800 || got.Usage.OutputTokens != 500 {
		t.Errorf("tokens não registrados: %+v", got.Usage)
	}
	if got.Fingerprint == "" {
		t.Error("fingerprint não registrado — sem ele não dá pra ligar gasto ao documento")
	}
}

// O caso que mais some da contabilidade: falha para o usuário, mas cobrada pelo provedor.
func TestBilledFailureIsRecordedAsBilled(t *testing.T) {
	usage := newRecordingUsage()
	s := &service{repo: newMemoryCacheRepo(), extractor: billedFailureExtractor{}, usage: usage}

	_, err := s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput("Marina"))
	if err == nil {
		t.Fatal("esperava erro de extração")
	}

	if len(usage.records) != 1 {
		t.Fatalf("gasto cobrado não foi registrado (%d registros)", len(usage.records))
	}
	got := usage.records[0]
	if got.Status != llmusage.StatusBilledError {
		t.Errorf("status = %q, esperava %q — a chamada foi cobrada", got.Status, llmusage.StatusBilledError)
	}
	if got.Usage.OutputTokens != 4096 {
		t.Errorf("tokens cobrados não registrados: %+v", got.Usage)
	}
}

func TestNetworkFailureIsRecordedAsNotBilled(t *testing.T) {
	usage := newRecordingUsage()
	s := &service{repo: newMemoryCacheRepo(), extractor: networkFailureExtractor{}, usage: usage}

	_, _ = s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput("Marina"))

	if len(usage.records) != 1 {
		t.Fatalf("esperava 1 registro, veio %d", len(usage.records))
	}
	if got := usage.records[0].Status; got != llmusage.StatusFailed {
		t.Errorf("status = %q, esperava %q — não houve resposta, logo não houve cobrança", got, llmusage.StatusFailed)
	}
}

// Sem isto não há como medir quanto o cache economizou — só supor.
func TestCacheHitIsRecorded(t *testing.T) {
	usage := newRecordingUsage()
	s := &service{repo: newMemoryCacheRepo(), extractor: &countingExtractor{}, usage: usage}
	cv := "Marina Albuquerque"

	_, _ = s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput(cv))
	_, _ = s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-2", resumeInput(cv))

	if len(usage.records) != 2 {
		t.Fatalf("esperava 2 registros (1 chamada + 1 cache hit), veio %d", len(usage.records))
	}
	if usage.records[1].Status != llmusage.StatusCacheHit {
		t.Errorf("segundo registro = %q, esperava cache_hit", usage.records[1].Status)
	}
	if usage.records[1].Usage.InputTokens != 0 {
		t.Error("cache hit registrou tokens — não houve chamada")
	}
}

// Modo manual não toca em provedor nenhum, então não pode gerar linha de uso.
func TestManualModeRecordsNothing(t *testing.T) {
	usage := newRecordingUsage()
	s := &service{repo: newMemoryCacheRepo(), extractor: &countingExtractor{}, usage: usage}

	in := PublicApplicationInput{Consent: true, Name: "Marina", Email: "m@example.test", Manual: &ManualApplicationFields{}}
	_, _ = s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", in)

	if len(usage.records) != 0 {
		t.Errorf("modo manual gerou %d registros de uso", len(usage.records))
	}
}
