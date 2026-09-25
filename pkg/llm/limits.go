package llm

import (
	"context"
	"errors"
	"time"
)

// RateLimitError é o ErrRateLimited com a informação que o provedor deu de quando tentar de novo.
// errors.Is(err, ErrRateLimited) continua valendo — quem só quer saber "foi limite de taxa" não
// muda em nada.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string        { return ErrRateLimited.Error() }
func (e *RateLimitError) Is(target error) bool { return target == ErrRateLimited }

// LimitsConfig controla o quanto podemos pressionar o provedor e o quanto insistimos nele. Zero em
// qualquer campo usa o padrão.
type LimitsConfig struct {
	// MaxConcurrent é o número de chamadas ao provedor em voo ao mesmo tempo. É proteção dupla: o
	// provedor tem limite de requisições/tokens por minuto, e cada chamada segura um worker HTTP
	// enquanto espera a resposta.
	MaxConcurrent int
	// MaxRetries é quantas re-tentativas, além da primeira, para erro SEM resposta.
	MaxRetries int
	// MaxRetryWait é o teto de espera por uma re-tentativa. Se o provedor pede mais que isso
	// (retry-after), desistimos na hora: um candidato não espera minutos com o formulário aberto.
	MaxRetryWait time.Duration
	// BaseBackoff é a espera da primeira re-tentativa quando o provedor não disse quanto esperar;
	// dobra a cada tentativa.
	BaseBackoff time.Duration
	// AcquireTimeout é quanto esperamos por uma vaga de concorrência antes de recusar.
	AcquireTimeout time.Duration
	// TotalTimeout limita a soma de todas as tentativas e esperas de UMA chamada.
	TotalTimeout time.Duration
}

// Limits é compartilhado entre Extractor e Assessor de propósito: os dois falam com o mesmo
// provedor, sob o mesmo limite de taxa da mesma chave.
type Limits struct {
	sem   chan struct{}
	cfg   LimitsConfig
	sleep func(context.Context, time.Duration) error
}

func NewLimits(cfg LimitsConfig) *Limits {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 4
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.MaxRetryWait <= 0 {
		cfg.MaxRetryWait = 5 * time.Second
	}
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = 500 * time.Millisecond
	}
	if cfg.AcquireTimeout <= 0 {
		cfg.AcquireTimeout = 10 * time.Second
	}
	if cfg.TotalTimeout <= 0 {
		cfg.TotalTimeout = 40 * time.Second
	}
	return &Limits{
		sem: make(chan struct{}, cfg.MaxConcurrent),
		cfg: cfg,
		sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-t.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
}

func billed(u Usage) bool { return u.InputTokens > 0 || u.OutputTokens > 0 }

// retryable é a regra que mais importa do arquivo: SÓ se re-tenta o que não teve resposta. Uma
// resposta que chegou já foi cobrada — re-tentar depois de ErrMalformedOutput ou de uma recusa
// pagaria de novo pela mesma coisa. E ErrBudgetExceeded/ErrUnsupportedInput nunca melhoram sozinhos.
func retryable(u Usage, err error) bool {
	if billed(u) {
		return false
	}
	return errors.Is(err, ErrRateLimited) || errors.Is(err, ErrProviderUnavailable)
}

func (l *Limits) wait(attempt int, err error) (time.Duration, bool) {
	var rl *RateLimitError
	if errors.As(err, &rl) && rl.RetryAfter > 0 {
		if rl.RetryAfter > l.cfg.MaxRetryWait {
			return 0, false
		}
		return rl.RetryAfter, true
	}
	d := l.cfg.BaseBackoff << attempt
	if d > l.cfg.MaxRetryWait {
		d = l.cfg.MaxRetryWait
	}
	return d, true
}

// run executa call sob o limite de concorrência, com re-tentativa segura. call devolve o Usage da
// tentativa; o Usage devolvido é o da última tentativa.
func (l *Limits) run(ctx context.Context, call func(ctx context.Context) (Usage, error)) (Usage, error) {
	ctx, cancel := context.WithTimeout(ctx, l.cfg.TotalTimeout)
	defer cancel()

	acquire := time.NewTimer(l.cfg.AcquireTimeout)
	defer acquire.Stop()
	select {
	case l.sem <- struct{}{}:
		defer func() { <-l.sem }()
	case <-acquire.C:
		// Recusa local, antes de qualquer chamada: nada foi cobrado. Para o resto do sistema é
		// indistinguível de o provedor ter limitado a taxa.
		return Usage{}, ErrRateLimited
	case <-ctx.Done():
		return Usage{}, ErrProviderUnavailable
	}

	var usage Usage
	var err error
	for attempt := 0; ; attempt++ {
		usage, err = call(ctx)
		if err == nil || attempt >= l.cfg.MaxRetries || !retryable(usage, err) {
			return usage, err
		}
		d, ok := l.wait(attempt, err)
		if !ok {
			return usage, err
		}
		if sleepErr := l.sleep(ctx, d); sleepErr != nil {
			return usage, err
		}
	}
}

type limitedExtractor struct {
	inner  Extractor
	limits *Limits
}

// WithLimits envolve um Extractor com concorrência limitada e re-tentativa segura.
func WithLimits(inner Extractor, l *Limits) Extractor {
	return &limitedExtractor{inner: inner, limits: l}
}

func (e *limitedExtractor) Extract(ctx context.Context, in Input) (*ExtractedProfile, Usage, error) {
	var profile *ExtractedProfile
	usage, err := e.limits.run(ctx, func(ctx context.Context) (Usage, error) {
		p, u, callErr := e.inner.Extract(ctx, in)
		profile = p
		return u, callErr
	})
	return profile, usage, err
}

type limitedAssessor struct {
	inner  Assessor
	limits *Limits
}

// AssessorWithLimits é o WithLimits do Assessor, compartilhando o mesmo *Limits.
func AssessorWithLimits(inner Assessor, l *Limits) Assessor {
	return &limitedAssessor{inner: inner, limits: l}
}

func (a *limitedAssessor) PromptVersion() string { return a.inner.PromptVersion() }

func (a *limitedAssessor) Assess(ctx context.Context, in AssessInput) (*Assessment, Usage, error) {
	var assessment *Assessment
	usage, err := a.limits.run(ctx, func(ctx context.Context) (Usage, error) {
		as, u, callErr := a.inner.Assess(ctx, in)
		assessment = as
		return u, callErr
	})
	return assessment, usage, err
}
