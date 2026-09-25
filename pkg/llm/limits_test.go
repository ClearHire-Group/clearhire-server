package llm

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestLimits devolve Limits sem espera real: o sleep só registra o que teria dormido.
func newTestLimits(cfg LimitsConfig) (*Limits, *[]time.Duration) {
	l := NewLimits(cfg)
	var slept []time.Duration
	l.sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	return l, &slept
}

type scriptedExtractor struct {
	results []scriptedResult
	calls   int
}

type scriptedResult struct {
	usage Usage
	err   error
}

func (s *scriptedExtractor) Extract(_ context.Context, _ Input) (*ExtractedProfile, Usage, error) {
	r := s.results[min(s.calls, len(s.results)-1)]
	s.calls++
	if r.err != nil {
		return nil, r.usage, r.err
	}
	return &ExtractedProfile{Name: "ok"}, r.usage, nil
}

// Erro SEM resposta (provedor fora, limite de taxa) é seguro re-tentar: nada foi cobrado.
func TestRetriesErrorsWithoutAResponse(t *testing.T) {
	inner := &scriptedExtractor{results: []scriptedResult{
		{err: ErrProviderUnavailable}, {err: ErrRateLimited}, {},
	}}
	limits, _ := newTestLimits(LimitsConfig{MaxRetries: 2})

	profile, _, err := WithLimits(inner, limits).Extract(context.Background(), Input{Text: "cv"})

	if err != nil || profile == nil {
		t.Fatalf("deveria ter dado certo na 3ª tentativa: %v", err)
	}
	if inner.calls != 3 {
		t.Errorf("chamadas = %d, esperava 3", inner.calls)
	}
}

// A regra que mais importa: resposta que chegou já foi COBRADA. Re-tentar depois de uma saída
// inútil pagaria de novo pela mesma coisa.
func TestNeverRetriesAfterABilledResponse(t *testing.T) {
	billedUsage := Usage{Provider: "fake", InputTokens: 1800, OutputTokens: 3000}
	for name, err := range map[string]error{
		"malformed": ErrMalformedOutput,
		"refused":   ErrRefused,
		// Até um erro "re-tentável" deixa de ser se já houve tokens cobrados na tentativa.
		"unavailable com tokens": ErrProviderUnavailable,
	} {
		inner := &scriptedExtractor{results: []scriptedResult{{usage: billedUsage, err: err}}}
		limits, _ := newTestLimits(LimitsConfig{MaxRetries: 3})

		_, usage, gotErr := WithLimits(inner, limits).Extract(context.Background(), Input{Text: "cv"})

		if !errors.Is(gotErr, err) {
			t.Errorf("%s: erro = %v", name, gotErr)
		}
		if inner.calls != 1 {
			t.Errorf("%s: %d chamadas — re-tentou depois de uma resposta cobrada", name, inner.calls)
		}
		if usage.InputTokens != 1800 {
			t.Errorf("%s: o consumo cobrado se perdeu: %+v", name, usage)
		}
	}
}

func TestDoesNotRetryErrorsThatWillNotImprove(t *testing.T) {
	for _, err := range []error{ErrBudgetExceeded, ErrUnsupportedInput} {
		inner := &scriptedExtractor{results: []scriptedResult{{err: err}}}
		limits, _ := newTestLimits(LimitsConfig{MaxRetries: 3})

		_, _, _ = WithLimits(inner, limits).Extract(context.Background(), Input{Text: "cv"})

		if inner.calls != 1 {
			t.Errorf("%v: %d chamadas, esperava 1", err, inner.calls)
		}
	}
}

func TestRetryStopsAtMaxRetries(t *testing.T) {
	inner := &scriptedExtractor{results: []scriptedResult{{err: ErrProviderUnavailable}}}
	limits, _ := newTestLimits(LimitsConfig{MaxRetries: 2})

	_, _, err := WithLimits(inner, limits).Extract(context.Background(), Input{Text: "cv"})

	if !errors.Is(err, ErrProviderUnavailable) {
		t.Errorf("erro = %v", err)
	}
	if inner.calls != 3 { // 1 tentativa + 2 re-tentativas
		t.Errorf("chamadas = %d, esperava 3", inner.calls)
	}
}

// Um candidato não espera minutos com o formulário aberto: retry-after acima do teto = desiste na
// hora, sem dormir.
func TestRetryAfterAboveCapFailsFast(t *testing.T) {
	inner := &scriptedExtractor{results: []scriptedResult{{err: &RateLimitError{RetryAfter: 45 * time.Second}}}}
	limits, slept := newTestLimits(LimitsConfig{MaxRetries: 3, MaxRetryWait: 5 * time.Second})

	_, _, err := WithLimits(inner, limits).Extract(context.Background(), Input{Text: "cv"})

	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("erro = %v, esperava ErrRateLimited", err)
	}
	if inner.calls != 1 || len(*slept) != 0 {
		t.Errorf("chamadas=%d esperas=%v — deveria ter desistido sem esperar", inner.calls, *slept)
	}
}

func TestRetryAfterWithinCapIsRespected(t *testing.T) {
	inner := &scriptedExtractor{results: []scriptedResult{{err: &RateLimitError{RetryAfter: 2 * time.Second}}, {}}}
	limits, slept := newTestLimits(LimitsConfig{MaxRetries: 2, MaxRetryWait: 5 * time.Second})

	if _, _, err := WithLimits(inner, limits).Extract(context.Background(), Input{Text: "cv"}); err != nil {
		t.Fatalf("erro: %v", err)
	}
	if len(*slept) != 1 || (*slept)[0] != 2*time.Second {
		t.Errorf("esperas = %v, esperava [2s]", *slept)
	}
}

func TestBackoffGrowsWithoutRetryAfter(t *testing.T) {
	inner := &scriptedExtractor{results: []scriptedResult{{err: ErrProviderUnavailable}}}
	limits, slept := newTestLimits(LimitsConfig{MaxRetries: 3, BaseBackoff: 100 * time.Millisecond, MaxRetryWait: time.Second})

	_, _, _ = WithLimits(inner, limits).Extract(context.Background(), Input{Text: "cv"})

	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	if len(*slept) != len(want) {
		t.Fatalf("esperas = %v", *slept)
	}
	for i := range want {
		if (*slept)[i] != want[i] {
			t.Errorf("espera %d = %v, esperava %v", i, (*slept)[i], want[i])
		}
	}
}

type blockingExtractor struct {
	current, peak atomic.Int32
	release       chan struct{}
}

func (b *blockingExtractor) Extract(_ context.Context, _ Input) (*ExtractedProfile, Usage, error) {
	n := b.current.Add(1)
	for {
		p := b.peak.Load()
		if n <= p || b.peak.CompareAndSwap(p, n) {
			break
		}
	}
	<-b.release
	b.current.Add(-1)
	return &ExtractedProfile{}, Usage{}, nil
}

// O limite de concorrência protege o provedor (limite de taxa) e os workers HTTP.
func TestConcurrencyIsBounded(t *testing.T) {
	inner := &blockingExtractor{release: make(chan struct{})}
	limits := NewLimits(LimitsConfig{MaxConcurrent: 2})
	extractor := WithLimits(inner, limits)

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = extractor.Extract(context.Background(), Input{Text: "cv"})
		}()
	}
	time.Sleep(100 * time.Millisecond)
	if peak := inner.peak.Load(); peak != 2 {
		t.Errorf("pico de chamadas simultâneas = %d, esperava 2", peak)
	}
	close(inner.release)
	wg.Wait()
	if peak := inner.peak.Load(); peak > 2 {
		t.Errorf("pico final = %d, o limite foi violado", peak)
	}
}

// Sem vaga livre por tempo demais, recusa localmente — sem chamada, sem custo.
func TestAcquireTimeoutRefusesWithoutCalling(t *testing.T) {
	inner := &blockingExtractor{release: make(chan struct{})}
	defer close(inner.release)
	limits := NewLimits(LimitsConfig{MaxConcurrent: 1, AcquireTimeout: 50 * time.Millisecond})
	extractor := WithLimits(inner, limits)

	go func() { _, _, _ = extractor.Extract(context.Background(), Input{Text: "ocupa a vaga"}) }()
	time.Sleep(20 * time.Millisecond)

	_, usage, err := extractor.Extract(context.Background(), Input{Text: "espera"})

	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("erro = %v, esperava ErrRateLimited", err)
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 {
		t.Errorf("recusa local não pode ter consumo: %+v", usage)
	}
	if inner.current.Load() != 1 {
		t.Errorf("a segunda chamada chegou ao provedor: %d em voo", inner.current.Load())
	}
}

func TestRateLimitErrorMatchesSentinel(t *testing.T) {
	var err error = &RateLimitError{RetryAfter: time.Second}
	if !errors.Is(err, ErrRateLimited) {
		t.Error("RateLimitError deveria satisfazer errors.Is(ErrRateLimited) — o resto do sistema só conhece o sentinela")
	}
}

type scriptedAssessor struct {
	calls int
	err   error
	usage Usage
}

func (s *scriptedAssessor) PromptVersion() string { return "test-v1" }
func (s *scriptedAssessor) Assess(_ context.Context, _ AssessInput) (*Assessment, Usage, error) {
	s.calls++
	if s.err != nil {
		return nil, s.usage, s.err
	}
	return &Assessment{MatchPct: 80}, s.usage, nil
}

// O Assessor tem as mesmas regras do Extractor, e compartilha o limite com ele.
func TestAssessorFollowsTheSameRetryRules(t *testing.T) {
	limits, _ := newTestLimits(LimitsConfig{MaxRetries: 2})

	transient := &scriptedAssessor{err: ErrProviderUnavailable}
	_, _, _ = AssessorWithLimits(transient, limits).Assess(context.Background(), AssessInput{})
	if transient.calls != 3 {
		t.Errorf("erro sem resposta: %d chamadas, esperava 3", transient.calls)
	}

	billed := &scriptedAssessor{err: ErrMalformedOutput, usage: Usage{InputTokens: 900, OutputTokens: 2000}}
	_, _, _ = AssessorWithLimits(billed, limits).Assess(context.Background(), AssessInput{})
	if billed.calls != 1 {
		t.Errorf("resposta cobrada: %d chamadas, esperava 1", billed.calls)
	}

	if got := AssessorWithLimits(transient, limits).PromptVersion(); got != "test-v1" {
		t.Errorf("PromptVersion não foi repassado: %q", got)
	}
}
