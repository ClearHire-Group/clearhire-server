package candidate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/llmusage"
	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

const testPromptVersion = "assessment-test-v1"

// fakeAssessor conta chamadas: cada uma é dinheiro. O que estes testes afirmam é QUANTAS vezes o
// provedor é chamado e o que acontece com o resultado.
type fakeAssessor struct {
	calls      int
	lastInput  llm.AssessInput
	assessment *llm.Assessment
	usage      llm.Usage
	err        error
}

func (a *fakeAssessor) PromptVersion() string { return testPromptVersion }

func (a *fakeAssessor) Assess(_ context.Context, in llm.AssessInput) (*llm.Assessment, llm.Usage, error) {
	a.calls++
	a.lastInput = in
	if a.err != nil {
		return nil, a.usage, a.err
	}
	copied := *a.assessment
	return &copied, a.usage, nil
}

func goodAssessor() *fakeAssessor {
	return &fakeAssessor{
		assessment: &llm.Assessment{MatchPct: 82, MatchLabel: "Bom match", MatchNote: "Forte em backend.",
			Justification: "Seis anos de Python.", Strengths: []string{"Python avançado"}, Concerns: []string{"Sem Kubernetes"}},
		usage: llm.Usage{Provider: "groq", Model: "openai/gpt-oss-20b", InputTokens: 700, OutputTokens: 180},
	}
}

// fakeAssessRepo implementa SÓ o que a avaliação usa. O resto da Repository fica nil no embed: se o
// código sob teste chamar AdvancePhase, Reject ou CreateDecision, o teste explode com nil pointer —
// que é exatamente como se prova que a IA nunca decide.
type fakeAssessRepo struct {
	Repository
	candidates map[string]*CandidateDetail // por "empresa|id"
	stored     map[string]*AIAssessment    // por "candidato|fase|versão"
	origins    []AssessmentOrigin
	saves      int
	saved      *llm.Assessment
	job        *llm.JobContext
}

func newFakeAssessRepo() *fakeAssessRepo {
	years := 6
	return &fakeAssessRepo{
		candidates: map[string]*CandidateDetail{
			"empresa-1|cand-1": {
				Candidate: Candidate{ID: "cand-1", CompanyID: "empresa-1", CampaignID: "vaga-1", Name: "Marina Albuquerque",
					Email: "marina@example.test", PhaseKey: PhaseRecebidos, Status: StatusAwaitingAI, YearsExperience: &years},
				Phone: "+55 11 99999-0000", LinkedInURL: "https://linkedin.com/in/marina",
				Summary: "Backend.", Skills: []string{"Python"},
			},
		},
		stored: map[string]*AIAssessment{},
		job:    &llm.JobContext{Title: "Engenheira Backend", Seniority: "senior", Modality: "remoto", Description: "Backend."},
	}
}

func (r *fakeAssessRepo) FindByID(_ context.Context, companyID, id string) (*CandidateDetail, error) {
	return r.candidates[companyID+"|"+id], nil
}

func (r *fakeAssessRepo) FindJobContext(_ context.Context, _, _ string) (*llm.JobContext, error) {
	return r.job, nil
}

func (r *fakeAssessRepo) FindAssessmentVersion(_ context.Context, candidateID, phaseKey, version string) (*AIAssessment, error) {
	return r.stored[candidateID+"|"+phaseKey+"|"+version], nil
}

func (r *fakeAssessRepo) SaveAssessment(_ context.Context, candidateID, phaseKey string, a *llm.Assessment, origin AssessmentOrigin) (bool, error) {
	r.saves++
	saved := *a
	r.saved = &saved
	r.origins = append(r.origins, origin)
	r.stored[candidateID+"|"+phaseKey+"|"+origin.PromptVersion] = &AIAssessment{
		MatchPct: a.MatchPct, MatchLabel: a.MatchLabel, MatchNote: a.MatchNote, Justification: a.Justification,
		Strengths: a.Strengths, Concerns: a.Concerns,
	}
	return true, nil
}

func assessService(repo *fakeAssessRepo, assessor llm.Assessor, usage llmusage.Repository) *service {
	s := &service{
		repo:     repo,
		assessor: assessor,
		usage:    usage,
		withTx:   func(_ context.Context, fn func(db database.DB) error) error { return fn(nil) },
		txRepo:   func(database.DB) Repository { return repo },
		spawn:    func(fn func()) { fn() },
	}
	return s
}

func TestAssessWithoutProviderIsUnavailable(t *testing.T) {
	s := assessService(newFakeAssessRepo(), nil, newRecordingUsage())
	s.assessor = nil

	_, err := s.Assess(context.Background(), "empresa-1", "cand-1")

	var appErr *apperror.AppError
	if !errors.As(err, &appErr) || appErr.Code != 503 {
		t.Errorf("erro = %v, esperava 503 (avaliação não habilitada)", err)
	}
}

func TestAssessSuccessSavesSanitizedAssessmentAndRecordsUsage(t *testing.T) {
	repo := newFakeAssessRepo()
	assessor := goodAssessor()
	// O modelo devolveu lixo: nota fora da escala e pontos repetidos/vazios.
	assessor.assessment.MatchPct = 250
	assessor.assessment.Strengths = []string{"Python", "python", "  "}
	usage := newRecordingUsage()
	s := assessService(repo, assessor, usage)

	got, err := s.Assess(context.Background(), "empresa-1", "cand-1")

	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if got.MatchPct != 100 {
		t.Errorf("nota = %d — o clamp de 0-100 não foi aplicado antes de gravar", got.MatchPct)
	}
	if len(repo.saved.Strengths) != 1 {
		t.Errorf("pontos repetidos/vazios foram gravados: %v", repo.saved.Strengths)
	}
	if len(usage.records) != 1 {
		t.Fatalf("registros de uso = %d, esperava 1", len(usage.records))
	}
	rec := usage.records[0]
	if rec.Operation != llmusage.OperationAssessment || rec.Status != llmusage.StatusSuccess {
		t.Errorf("registro = %+v", rec)
	}
	if rec.CandidateID == nil || *rec.CandidateID != "cand-1" || rec.CampaignID == nil || *rec.CampaignID != "vaga-1" {
		t.Errorf("o gasto não ficou ligado ao candidato/vaga: %+v", rec)
	}
	// Trilha de auditoria: quem produziu a recomendação.
	if o := repo.origins[0]; o.Provider != "groq" || o.Model != "openai/gpt-oss-20b" || o.PromptVersion != testPromptVersion {
		t.Errorf("origem gravada = %+v", o)
	}
}

// Idempotência: a segunda chamada é leitura. Sem isto, cada clique no botão seria uma cobrança.
func TestAssessIsIdempotentPerPhaseAndVersion(t *testing.T) {
	repo := newFakeAssessRepo()
	assessor := goodAssessor()
	usage := newRecordingUsage()
	s := assessService(repo, assessor, usage)

	if _, err := s.Assess(context.Background(), "empresa-1", "cand-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Assess(context.Background(), "empresa-1", "cand-1"); err != nil {
		t.Fatal(err)
	}

	if assessor.calls != 1 {
		t.Errorf("provedor chamado %d vezes — a segunda avaliação deveria ter vindo do banco", assessor.calls)
	}
	if repo.saves != 1 || len(usage.records) != 1 {
		t.Errorf("gravações=%d registros de uso=%d, esperava 1 e 1", repo.saves, len(usage.records))
	}
}

// Fase nova = avaliação nova: é o que permite reavaliar quando o candidato avança no funil.
func TestAssessAgainInANewPhaseIsAllowed(t *testing.T) {
	repo := newFakeAssessRepo()
	assessor := goodAssessor()
	s := assessService(repo, assessor, newRecordingUsage())

	if _, err := s.Assess(context.Background(), "empresa-1", "cand-1"); err != nil {
		t.Fatal(err)
	}
	repo.candidates["empresa-1|cand-1"].PhaseKey = PhaseFit
	if _, err := s.Assess(context.Background(), "empresa-1", "cand-1"); err != nil {
		t.Fatal(err)
	}

	if assessor.calls != 2 {
		t.Errorf("provedor chamado %d vezes, esperava 2 (uma por fase)", assessor.calls)
	}
}

// A regra multi-tenant do projeto: o id de outra empresa é "não encontrado", nunca uma avaliação
// (e nunca uma chamada paga sobre dado alheio).
func TestAssessOtherCompanyCandidateIsNotFound(t *testing.T) {
	assessor := goodAssessor()
	s := assessService(newFakeAssessRepo(), assessor, newRecordingUsage())

	_, err := s.Assess(context.Background(), "empresa-2", "cand-1")

	var appErr *apperror.AppError
	if !errors.As(err, &appErr) || appErr.Code != 404 {
		t.Errorf("erro = %v, esperava 404", err)
	}
	if assessor.calls != 0 {
		t.Error("chamou o provedor para um candidato de outra empresa")
	}
}

func TestAssessSkipsCandidatesWhoAlreadyLeftTheFunnel(t *testing.T) {
	for _, status := range []Status{StatusRejected, StatusHired} {
		repo := newFakeAssessRepo()
		repo.candidates["empresa-1|cand-1"].Status = status
		assessor := goodAssessor()
		s := assessService(repo, assessor, newRecordingUsage())

		_, err := s.Assess(context.Background(), "empresa-1", "cand-1")

		var appErr *apperror.AppError
		if !errors.As(err, &appErr) || appErr.Code != 400 {
			t.Errorf("%s: erro = %v, esperava 400", status, err)
		}
		if assessor.calls != 0 {
			t.Errorf("%s: gastou uma chamada com quem já saiu do funil", status)
		}
	}
}

// Mesmo teto mensal da extração: não pode haver um caminho de gasto que o teto não enxergue.
func TestAssessRespectsMonthlyBudget(t *testing.T) {
	usage := newRecordingUsage()
	usage.spent, usage.limit = 10, 10
	assessor := goodAssessor()
	s := assessService(newFakeAssessRepo(), assessor, usage)

	_, err := s.Assess(context.Background(), "empresa-1", "cand-1")

	var appErr *apperror.AppError
	if !errors.As(err, &appErr) || appErr.Code != 400 || !strings.Contains(appErr.Message, "teto") {
		t.Errorf("erro = %v, esperava 400 citando o teto", err)
	}
	if assessor.calls != 0 {
		t.Error("chamou o provedor com o orçamento estourado")
	}
	if len(usage.records) != 1 || usage.records[0].Usage.Provider != budgetProvider ||
		usage.records[0].Operation != llmusage.OperationAssessment {
		t.Errorf("a recusa por orçamento não foi registrada como avaliação: %+v", usage.records)
	}
}

// A mesma regra da extração: resposta que chegou e foi inútil JÁ FOI COBRADA e tem que aparecer.
func TestAssessBilledFailureIsRecordedAsBilledError(t *testing.T) {
	assessor := goodAssessor()
	assessor.err = llm.ErrMalformedOutput
	assessor.usage = llm.Usage{Provider: "groq", Model: "openai/gpt-oss-20b", InputTokens: 700, OutputTokens: 2000}
	usage := newRecordingUsage()
	repo := newFakeAssessRepo()
	s := assessService(repo, assessor, usage)

	_, err := s.Assess(context.Background(), "empresa-1", "cand-1")

	var appErr *apperror.AppError
	if !errors.As(err, &appErr) || appErr.Code != 503 {
		t.Errorf("erro = %v, esperava 503", err)
	}
	if len(usage.records) != 1 || usage.records[0].Status != llmusage.StatusBilledError {
		t.Errorf("registros = %v, esperava um billed_error", usage.statuses())
	}
	if repo.saves != 0 {
		t.Error("gravou uma avaliação a partir de uma saída inútil")
	}
}

func TestAssessProviderDownIsRecordedAsFailedAndOpaque(t *testing.T) {
	assessor := goodAssessor()
	assessor.err = llm.ErrProviderUnavailable
	assessor.usage = llm.Usage{Provider: "groq"}
	usage := newRecordingUsage()
	s := assessService(newFakeAssessRepo(), assessor, usage)

	_, err := s.Assess(context.Background(), "empresa-1", "cand-1")

	if err == nil || strings.Contains(err.Error(), "groq") {
		t.Errorf("erro cru do provedor vazou: %v", err)
	}
	if len(usage.records) != 1 || usage.records[0].Status != llmusage.StatusFailed {
		t.Errorf("registros = %v, esperava um failed", usage.statuses())
	}
}

// A invariante do produto (e a revisão humana que a LGPD exige): a IA sugere, o RH decide. O fake
// não implementa AdvancePhase/Reject/CreateDecision — se Assess chamasse qualquer um, este teste
// morreria com nil pointer. Aqui também se confere que nada mudou no candidato.
func TestAssessNeverDecides(t *testing.T) {
	repo := newFakeAssessRepo()
	assessor := goodAssessor()
	assessor.assessment.MatchPct = 0 // a nota mais baixa possível não reprova ninguém
	s := assessService(repo, assessor, newRecordingUsage())

	if _, err := s.Assess(context.Background(), "empresa-1", "cand-1"); err != nil {
		t.Fatal(err)
	}

	cand := repo.candidates["empresa-1|cand-1"]
	if cand.PhaseKey != PhaseRecebidos || cand.Status == StatusRejected {
		t.Errorf("a avaliação mexeu no funil: fase=%s status=%s", cand.PhaseKey, cand.Status)
	}
}

// Minimização de dados: o provedor recebe competência e experiência, nunca quem é a pessoa.
func TestAssessInputCarriesNoIdentity(t *testing.T) {
	assessor := goodAssessor()
	s := assessService(newFakeAssessRepo(), assessor, newRecordingUsage())

	if _, err := s.Assess(context.Background(), "empresa-1", "cand-1"); err != nil {
		t.Fatal(err)
	}

	dump := strings.ToLower(strings.Join([]string{
		assessor.lastInput.Candidate.Summary, assessor.lastInput.Candidate.Education,
		strings.Join(assessor.lastInput.Candidate.Skills, " "),
	}, " "))
	for _, identity := range []string{"marina", "albuquerque", "marina@example.test", "99999", "linkedin"} {
		if strings.Contains(dump, identity) {
			t.Errorf("dado de identificação %q chegou ao avaliador", identity)
		}
	}
	if assessor.lastInput.Job.Title != "Engenheira Backend" || len(assessor.lastInput.Candidate.Skills) != 1 {
		t.Errorf("o avaliador não recebeu a vaga e as skills: %+v", assessor.lastInput)
	}
}

// --- avaliação automática pós-candidatura ---

func TestAutoAssessRunsInBackgroundAndSwallowsFailure(t *testing.T) {
	assessor := goodAssessor()
	assessor.err = llm.ErrRateLimited
	repo := newFakeAssessRepo()
	s := assessService(repo, assessor, newRecordingUsage())

	// Falha da IA nunca pode propagar para a candidatura: autoAssess não devolve nada nem entra em pânico.
	s.autoAssess("empresa-1", "cand-1")

	if assessor.calls != 1 {
		t.Errorf("chamadas = %d, esperava 1", assessor.calls)
	}
}

func TestAutoAssessDoesNothingWithoutAssessor(t *testing.T) {
	s := assessService(newFakeAssessRepo(), nil, newRecordingUsage())
	s.assessor = nil
	spawned := false
	s.spawn = func(func()) { spawned = true }

	s.autoAssess("empresa-1", "cand-1")

	if spawned {
		t.Error("disparou trabalho em background sem avaliador configurado")
	}
}

func TestAutoAssessDoesNothingWithoutCandidateID(t *testing.T) {
	assessor := goodAssessor()
	s := assessService(newFakeAssessRepo(), assessor, newRecordingUsage())

	s.autoAssess("empresa-1", "")

	if assessor.calls != 0 {
		t.Error("avaliou sem candidato")
	}
}

func TestAutoAssessRecoversFromPanic(t *testing.T) {
	// Repositório vazio (nil embed) explode em FindByID: o pânico numa goroutine derrubaria o
	// processo inteiro se não fosse contido.
	s := assessService(&fakeAssessRepo{}, goodAssessor(), newRecordingUsage())
	s.repo = nil

	s.autoAssess("empresa-1", "cand-1") // não pode entrar em pânico
}

func TestMapAssessmentErrorNeverLeaksProviderDetails(t *testing.T) {
	for _, err := range []error{llm.ErrRateLimited, llm.ErrProviderUnavailable, llm.ErrRefused, llm.ErrMalformedOutput, llm.ErrUnsupportedInput} {
		var appErr *apperror.AppError
		if !errors.As(mapAssessmentError(err), &appErr) || appErr.Code != 503 {
			t.Errorf("%v não virou 503", err)
		}
	}
}
