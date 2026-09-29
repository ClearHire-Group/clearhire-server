package candidate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

// maliciousExtractor simula um modelo que devolveu (ou foi induzido a devolver) dado fora do formato.
type maliciousExtractor struct{ profile llm.ExtractedProfile }

func (m maliciousExtractor) Extract(_ context.Context, in llm.Input) (*llm.ExtractedProfile, llm.Usage, error) {
	p := m.profile
	p.RawText = in.Text
	return &p, llm.Usage{Provider: "fake", Model: "claude-haiku-4-5", InputTokens: 100, OutputTokens: 50}, nil
}

// A vulnerabilidade real: um currículo que contém o e-mail de OUTRA pessoa registrava a
// candidatura em nome dela e, pela checagem de duplicidade, bloqueava a candidatura verdadeira dela
// a esta vaga. A identidade é sempre a do formulário.
func TestResumeEmailNeverBecomesTheApplicantIdentity(t *testing.T) {
	victim := "vitima@example.test"
	s := &service{
		repo:      newMemoryCacheRepo(),
		extractor: maliciousExtractor{profile: llm.ExtractedProfile{Name: "Outro Nome", Email: victim}},
		usage:     newRecordingUsage(),
	}

	profile, err := s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1",
		PublicApplicationInput{Consent: true, Name: "Marina", Email: "marina@example.test", ResumeText: "cv contendo " + victim})

	if err != nil {
		t.Fatal(err)
	}
	if profile.Email != "marina@example.test" {
		t.Errorf("e-mail = %q — o e-mail achado no currículo virou a identidade da candidatura", profile.Email)
	}
	if profile.Name != "Marina" {
		t.Errorf("nome = %q, esperava o do formulário", profile.Name)
	}
}

// O mesmo vale quando o perfil vem do cache: um caminho sem override devolveria a identidade errada.
func TestFormIdentityAlsoWinsOnCacheHit(t *testing.T) {
	repo := newMemoryCacheRepo()
	s := &service{
		repo:      repo,
		extractor: maliciousExtractor{profile: llm.ExtractedProfile{Email: "vitima@example.test"}},
		usage:     newRecordingUsage(),
	}
	cv := "mesmo currículo"
	if _, err := s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput(cv)); err != nil {
		t.Fatal(err)
	}

	second := PublicApplicationInput{Consent: true, Name: "Outra Pessoa", Email: "outra@example.test", ResumeText: cv}
	profile, err := s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-2", second)

	if err != nil {
		t.Fatal(err)
	}
	if profile.Email != "outra@example.test" || profile.Name != "Outra Pessoa" {
		t.Errorf("cache hit devolveu a identidade de outro envio: %q / %q", profile.Email, profile.Name)
	}
}

// A saída do modelo é sanitizada ANTES de ir para o cache: dado ruim gravado lá seria servido para
// sempre, e uma data inválida derrubaria o INSERT depois de a extração já ter sido paga.
func TestModelOutputIsSanitizedBeforeCaching(t *testing.T) {
	badDate := "2020-13-45"
	repo := newMemoryCacheRepo()
	s := &service{
		repo: repo,
		extractor: maliciousExtractor{profile: llm.ExtractedProfile{
			AvailableFrom: &badDate, Modality: "quando eu quiser", LinkedInURL: "https://evil.example/x",
		}},
		usage: newRecordingUsage(),
	}

	profile, err := s.resolveApplicationProfile(context.Background(), "empresa-1", "vaga-1", resumeInput("cv"))

	if err != nil {
		t.Fatal(err)
	}
	if profile.AvailableFrom != nil || profile.Modality != "" || profile.LinkedInURL != "" {
		t.Errorf("perfil não foi sanitizado: %+v", profile)
	}
	for key, cached := range repo.stored {
		if cached.AvailableFrom != nil || cached.LinkedInURL != "" {
			t.Errorf("dado inválido foi gravado no cache (%s): %+v", key, cached)
		}
	}
}

func TestUnsupportedInputIsOpaqueToApplicant(t *testing.T) {
	got := mapExtractionError(llm.ErrUnsupportedInput)
	want := mapExtractionError(llm.ErrProviderUnavailable)
	if got.Error() != want.Error() {
		t.Errorf("PDF escaneado num provedor sem OCR deve cair na mesma mensagem genérica:\n %s\n %s", got, want)
	}
}

func TestIsUniqueViolation(t *testing.T) {
	unique := &pgconn.PgError{Code: "23505", ConstraintName: "uq_candidates_campaign_email"}

	if !isUniqueViolation(unique) {
		t.Error("não reconheceu a violação de unicidade")
	}
	if !isUniqueViolation(fmt.Errorf("criando candidato: %w", unique)) {
		t.Error("não reconheceu a violação embrulhada em outro erro")
	}
	if isUniqueViolation(&pgconn.PgError{Code: "23503"}) || isUniqueViolation(errors.New("outro erro")) || isUniqueViolation(nil) {
		t.Error("tratou como duplicidade algo que não é")
	}
}

func TestMentionsTermWordBoundaries(t *testing.T) {
	cases := []struct {
		text, term string
		want       bool
		why        string
	}{
		{"Desenvolvo em Go e Python", "Go", true, "skill curta com a caixa certa, isolada"},
		{"Gosto de trabalhar com Google Cloud", "Go", false, "'Go' dentro de 'Google'"},
		{"vamos gostar disso", "Go", false, "'go' dentro de 'gostar'"},
		{"vou go para casa", "Go", false, "termo curto só casa com a caixa exata da taxonomia"},
		{"Experiência com R e Python", "R", true, "R isolado"},
		{"Trabalhei em pesquisa e desenvolvimento", "R", false, "'r' em qualquer palavra"},
		{"Programação em C++ e Rust", "C", false, "'C' não é 'C++'"},
		{"Programação em C++ e Rust", "C++", true, "C++ com símbolo"},
		{"Escrevo C# há anos", "C#", true, "C# com símbolo"},
		{"Escrevo C# há anos", "C", false, "'C' não é 'C#'"},
		{"Sei JavaScript e TypeScript", "Java", false, "'Java' dentro de 'JavaScript'"},
		{"Sei Java e Kotlin", "Java", true, "Java isolado"},
		{"Backend em Node.js com Express", "Node.js", true, "termo com ponto"},
		{"Stack: ASP.NET e SQL Server", ".NET", true, "termo que começa com símbolo"},
		{"Experiência em KUBERNETES e docker", "Kubernetes", true, "termo longo não diferencia caixa"},
		{"Ele é pythonista", "Python", false, "termo dentro de outra palavra"},
		{"Python, Django, SQL.", "SQL", true, "pontuação ao redor"},
		{"", "Python", false, "texto vazio"},
		{"qualquer coisa", "", false, "termo vazio"},
		{"Trabalho com São Paulo e ação", "ação", true, "acentos são letras"},
		{"reação", "ação", false, "acento não quebra a fronteira"},
	}
	for _, tc := range cases {
		lower := lowerFor(tc.text)
		if got := mentionsTerm(tc.text, lower, tc.term); got != tc.want {
			t.Errorf("mentionsTerm(%q, %q) = %v, esperava %v (%s)", tc.text, tc.term, got, tc.want, tc.why)
		}
	}
}

func lowerFor(s string) string { return strings.ToLower(s) }

// fakeSkillsRepo implementa só o que a resolução de skills usa.
type fakeSkillsRepo struct {
	Repository
	taxonomy  map[string]string // termo (minúsculo) -> id da skill
	textHits  []ResolvedSkill
	queued    []string
	scannedIn string
}

func (r *fakeSkillsRepo) ResolveSkills(_ context.Context, terms []llm.SkillMention) ([]ResolvedSkill, []llm.SkillMention, error) {
	var resolved []ResolvedSkill
	var unmapped []llm.SkillMention
	for _, t := range terms {
		if id, ok := r.taxonomy[strings.ToLower(t.Term)]; ok {
			resolved = append(resolved, ResolvedSkill{SkillID: id, Level: t.Level, YearsExperience: t.YearsExperience})
		} else {
			unmapped = append(unmapped, t)
		}
	}
	return resolved, unmapped, nil
}

func (r *fakeSkillsRepo) FindSkillMentionsInText(_ context.Context, text string) ([]ResolvedSkill, error) {
	r.scannedIn = text
	return r.textHits, nil
}

func (r *fakeSkillsRepo) QueueSkillReview(_ context.Context, _, term string) error {
	r.queued = append(r.queued, term)
	return nil
}

// Com um modelo de verdade, as skills que ele extraiu não podem ser jogadas fora: as da taxonomia
// são gravadas e as que ela não conhece vão para a fila de revisão (é assim que a taxonomia cresce).
func TestModelExtractedSkillsAreResolvedAndUnknownOnesQueued(t *testing.T) {
	years := 6
	repo := &fakeSkillsRepo{taxonomy: map[string]string{"python": "sk-python"}}
	profile := &llm.ExtractedProfile{
		RawText: "cv",
		Skills:  []llm.SkillMention{{Term: "Python", Level: "avançado", YearsExperience: &years}, {Term: "Airflow"}},
	}

	got, err := resolveProfileSkills(context.Background(), repo, "empresa-1", profile)

	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SkillID != "sk-python" || got[0].Level != "avançado" || *got[0].YearsExperience != 6 {
		t.Errorf("skills resolvidas = %+v", got)
	}
	if len(repo.queued) != 1 || repo.queued[0] != "Airflow" {
		t.Errorf("fila de revisão = %v, esperava [Airflow]", repo.queued)
	}
}

// O extrator determinístico não devolve lista: só o dicionário sobre o texto vale.
func TestDeterministicProfileStillUsesTheDictionaryScan(t *testing.T) {
	repo := &fakeSkillsRepo{textHits: []ResolvedSkill{{SkillID: "sk-python"}}}

	got, err := resolveProfileSkills(context.Background(), repo, "empresa-1", &llm.ExtractedProfile{RawText: "Python"})

	if err != nil || len(got) != 1 || got[0].SkillID != "sk-python" {
		t.Errorf("got=%+v err=%v", got, err)
	}
	if len(repo.queued) != 0 {
		t.Errorf("a varredura de dicionário não gera fila de revisão: %v", repo.queued)
	}
}

// O modo manual não tem texto bruto: só a lista conta, e o dicionário nem é consultado.
func TestManualProfileDoesNotScanText(t *testing.T) {
	repo := &fakeSkillsRepo{taxonomy: map[string]string{"python": "sk-python"}}

	got, err := resolveProfileSkills(context.Background(), repo, "empresa-1",
		&llm.ExtractedProfile{Skills: []llm.SkillMention{{Term: "python"}}})

	if err != nil || len(got) != 1 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if repo.scannedIn != "" {
		t.Error("varreu texto num perfil sem RawText")
	}
}

// candidate_skills tem unique (candidate_id, skill_id): a mesma skill vinda da lista e do dicionário
// (ou listada duas vezes pelo modelo) derrubaria a candidatura inteira. E a versão da lista, que tem
// nível e anos, tem que vencer a do dicionário, que não tem.
func TestSkillsFromListAndTextAreMergedWithoutDuplicates(t *testing.T) {
	repo := &fakeSkillsRepo{
		taxonomy: map[string]string{"python": "sk-python", "sql": "sk-sql"},
		textHits: []ResolvedSkill{{SkillID: "sk-python"}, {SkillID: "sk-sql"}, {SkillID: "sk-docker"}},
	}
	profile := &llm.ExtractedProfile{
		RawText: "cv",
		Skills:  []llm.SkillMention{{Term: "Python", Level: "avançado"}, {Term: "python"}, {Term: "SQL"}},
	}

	got, err := resolveProfileSkills(context.Background(), repo, "empresa-1", profile)

	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int{}
	for _, s := range got {
		ids[s.SkillID]++
	}
	if len(got) != 3 || ids["sk-python"] != 1 || ids["sk-sql"] != 1 || ids["sk-docker"] != 1 {
		t.Errorf("skills = %+v", got)
	}
	if got[0].SkillID != "sk-python" || got[0].Level != "avançado" {
		t.Errorf("a versão da lista (com nível) deveria vencer a do dicionário: %+v", got[0])
	}
}
