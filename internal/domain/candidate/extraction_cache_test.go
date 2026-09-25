package candidate

import (
	"context"
	"testing"

	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

// countingExtractor é o instrumento do teste: cada chamada aqui representa dinheiro gasto com o
// provedor. O que estes testes afirmam é quantas vezes isso acontece.
type countingExtractor struct {
	calls int
}

func (e *countingExtractor) Extract(_ context.Context, in llm.Input) (*llm.ExtractedProfile, llm.Usage, error) {
	e.calls++
	usage := llm.Usage{Provider: "fake", Model: "claude-haiku-4-5", InputTokens: 1800, OutputTokens: 500}
	return &llm.ExtractedProfile{Name: "extraído pelo modelo", Email: "cv@example.test", RawText: in.Text}, usage, nil
}

// memoryCacheRepo implementa só os dois métodos de cache. O resto da Repository fica no embed: se o
// código sob teste chamar qualquer outra coisa, o teste explode — que é o comportamento desejado.
type memoryCacheRepo struct {
	Repository
	stored map[string]*llm.ExtractedProfile
	reads  int
	writes int
}

func newMemoryCacheRepo() *memoryCacheRepo {
	return &memoryCacheRepo{stored: map[string]*llm.ExtractedProfile{}}
}

func (r *memoryCacheRepo) FindCachedExtraction(_ context.Context, companyID, fingerprint string) (*llm.ExtractedProfile, error) {
	r.reads++
	if p, ok := r.stored[companyID+"|"+fingerprint]; ok {
		copied := *p
		return &copied, nil
	}
	return nil, nil
}

func (r *memoryCacheRepo) SaveExtraction(_ context.Context, companyID, fingerprint string, profile *llm.ExtractedProfile) error {
	r.writes++
	copied := *profile
	r.stored[companyID+"|"+fingerprint] = &copied
	return nil
}

func resumeInput(text string) PublicApplicationInput {
	return PublicApplicationInput{Consent: true, Name: "Marina", Email: "marina@example.test", ResumeText: text}
}

// O teste central do item 1.3: o mesmo currículo entrando duas vezes (outra vaga, outro dia) só
// pode custar UMA extração. Sem o cache, este contador daria 2.
func TestSameResumeIsExtractedOnlyOnce(t *testing.T) {
	extractor := &countingExtractor{}
	repo := newMemoryCacheRepo()
	s := &service{repo: repo, extractor: extractor}
	cv := "Marina Albuquerque\nGo, Kubernetes, PostgreSQL"

	for i := range 2 {
		if _, err := s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput(cv)); err != nil {
			t.Fatalf("envio %d falhou: %v", i+1, err)
		}
	}

	if extractor.calls != 1 {
		t.Errorf("extrator chamado %d vezes para o mesmo currículo — deveria ser 1", extractor.calls)
	}
	if repo.writes != 1 {
		t.Errorf("cache gravado %d vezes — deveria ser 1", repo.writes)
	}
}

// Espaçamento diferente é o mesmo documento: o extrator de PDF não é estável nisso.
func TestWhitespaceVariationStillHitsCache(t *testing.T) {
	extractor := &countingExtractor{}
	s := &service{repo: newMemoryCacheRepo(), extractor: extractor}

	_, _ = s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput("Marina Albuquerque\nGo"))
	_, _ = s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput("  Marina   Albuquerque \n\n Go "))

	if extractor.calls != 1 {
		t.Errorf("extrator chamado %d vezes — variação de espaço deveria bater no cache", extractor.calls)
	}
}

// Isolamento de tenant: empresas diferentes nunca compartilham cache, mesmo com o hash idêntico.
// Um hit cruzado revelaria que aquele currículo já passou por outra empresa.
func TestCacheIsScopedPerCompany(t *testing.T) {
	extractor := &countingExtractor{}
	s := &service{repo: newMemoryCacheRepo(), extractor: extractor}
	cv := "Marina Albuquerque\nGo"

	_, _ = s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput(cv))
	_, _ = s.resolveApplicationProfile(context.Background(), "empresa-2", "vaga-1", resumeInput(cv))

	if extractor.calls != 2 {
		t.Errorf("extrator chamado %d vezes — cada empresa precisa da própria extração", extractor.calls)
	}
}

func TestDifferentResumesAreBothExtracted(t *testing.T) {
	extractor := &countingExtractor{}
	s := &service{repo: newMemoryCacheRepo(), extractor: extractor}

	_, _ = s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput("Marina Albuquerque"))
	_, _ = s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput("Diego Salgado"))

	if extractor.calls != 2 {
		t.Errorf("extrator chamado %d vezes para currículos distintos — deveria ser 2", extractor.calls)
	}
}

// Modo manual não passa por IA nenhuma — e não pode nem consultar o cache, porque não há documento.
func TestManualModeNeverCallsExtractor(t *testing.T) {
	extractor := &countingExtractor{}
	repo := newMemoryCacheRepo()
	s := &service{repo: repo, extractor: extractor}

	in := PublicApplicationInput{
		Consent: true, Name: "Marina", Email: "marina@example.test",
		Manual: &ManualApplicationFields{City: "São Paulo"},
	}
	profile, err := s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", in)
	if err != nil {
		t.Fatalf("modo manual falhou: %v", err)
	}

	if extractor.calls != 0 {
		t.Errorf("modo manual chamou o extrator %d vezes", extractor.calls)
	}
	if repo.reads != 0 {
		t.Errorf("modo manual consultou o cache %d vezes", repo.reads)
	}
	if profile.City != "São Paulo" {
		t.Errorf("campos do formulário não chegaram ao perfil: %+v", profile)
	}
}

// O dado que a pessoa digitou vence o que o modelo leu — regra que já existia e que o cache não
// pode ter quebrado (o perfil vem do cache, mas Name tem que ser sempre o do formulário).
func TestFormNameWinsOverCachedExtraction(t *testing.T) {
	s := &service{repo: newMemoryCacheRepo(), extractor: &countingExtractor{}}
	cv := "Marina Albuquerque\nGo"

	_, _ = s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput(cv))
	second, err := s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput(cv))
	if err != nil {
		t.Fatalf("segundo envio falhou: %v", err)
	}

	if second.Name != "Marina" {
		t.Errorf("Name veio do cache (%q) em vez do formulário", second.Name)
	}
}
