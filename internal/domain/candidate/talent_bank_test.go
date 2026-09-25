package candidate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/activity"
	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

// fakeBankRepo simula os talentos de UMA empresa: quem existe, quem consentiu e quem está no banco.
type fakeBankRepo struct {
	Repository
	byEmail  map[string]string // e-mail -> id do talento
	consent  map[string]string // id -> consent_state
	inBank   map[string]string // id -> origem de entrada
	created  []string          // candidatos que viraram talento novo
	links    map[string]string // candidato -> talento
	nextID   int
	phases   []PhaseRow
	advanced string
}

func newFakeBankRepo() *fakeBankRepo {
	return &fakeBankRepo{byEmail: map[string]string{}, consent: map[string]string{}, inBank: map[string]string{}, links: map[string]string{}}
}

func (r *fakeBankRepo) FindTalentIDByEmail(_ context.Context, _, email string) (string, error) {
	return r.byEmail[email], nil
}

func (r *fakeBankRepo) CreateTalentFromCandidate(_ context.Context, _, candidateID, _ string) (string, error) {
	r.nextID++
	id := "novo-" + string(rune('0'+r.nextID))
	r.created = append(r.created, candidateID)
	r.consent[id] = "nao_notificado"
	return id, nil
}

// Mesma regra do SQL de PromoteTalentToBank.
func (r *fakeBankRepo) PromoteTalentToBank(_ context.Context, _, talentID, origin string, requireConsent bool) (bool, error) {
	state := r.consent[talentID]
	if state == "oposicao_exclusao" || (requireConsent && state != "consentido") {
		return false, nil
	}
	if _, already := r.inBank[talentID]; !already {
		r.inBank[talentID] = origin
	}
	if state != "consentido" {
		r.consent[talentID] = "notificado"
	}
	return true, nil
}

func (r *fakeBankRepo) LinkCandidateTalent(_ context.Context, _, candidateID, talentID string) error {
	r.links[candidateID] = talentID
	return nil
}

func ptr(s string) *string { return &s }

func TestRejectionReusesTheApplicantsOwnRecord(t *testing.T) {
	repo := newFakeBankRepo()
	repo.consent["t-1"] = "consentido"
	cand := &Candidate{ID: "c-1", Email: "ana@example.test", TalentID: ptr("t-1")}

	id, err := addToBank(context.Background(), repo, "empresa", cand, OriginRejection, false)

	if err != nil || id == nil || *id != "t-1" {
		t.Fatalf("id=%v err=%v", id, err)
	}
	if len(repo.created) != 0 {
		t.Error("criou um segundo talento para quem já tinha registro — é a duplicata que aparecia no banco")
	}
	if repo.consent["t-1"] != "consentido" {
		t.Errorf("consentimento rebaixado para %q", repo.consent["t-1"])
	}
}

func TestRejectionFindsTheSamePersonByEmail(t *testing.T) {
	repo := newFakeBankRepo()
	repo.byEmail["ana@example.test"] = "t-antigo"
	repo.consent["t-antigo"] = "nao_notificado"
	cand := &Candidate{ID: "c-1", Email: "ana@example.test"}

	id, _ := addToBank(context.Background(), repo, "empresa", cand, OriginRejection, false)

	if id == nil || *id != "t-antigo" || len(repo.created) != 0 {
		t.Errorf("id=%v criados=%v", id, repo.created)
	}
	if repo.links["c-1"] != "t-antigo" {
		t.Error("a candidatura não foi ligada ao registro da pessoa (o histórico ficaria incompleto)")
	}
	if repo.consent["t-antigo"] != "notificado" {
		t.Errorf("reprovação com envio ao banco = convite enviado; estado = %q", repo.consent["t-antigo"])
	}
}

func TestRejectionCreatesRecordWhenPersonIsNew(t *testing.T) {
	repo := newFakeBankRepo()
	cand := &Candidate{ID: "c-1", Email: "novo@example.test"}

	id, _ := addToBank(context.Background(), repo, "empresa", cand, OriginRejection, false)

	if id == nil || len(repo.created) != 1 || repo.inBank[*id] != OriginRejection {
		t.Errorf("id=%v criados=%v banco=%v", id, repo.created, repo.inBank)
	}
}

// "ambos com consentimento aceito": a aprovação só leva ao banco quem já consentiu, e nunca cria nada.
func TestApprovalOnlyAddsPeopleWhoConsented(t *testing.T) {
	repo := newFakeBankRepo()
	repo.consent["t-sim"], repo.consent["t-nao"] = "consentido", "nao_notificado"

	yes, _ := addToBank(context.Background(), repo, "e", &Candidate{ID: "c-1", TalentID: ptr("t-sim")}, OriginApproval, true)
	no, _ := addToBank(context.Background(), repo, "e", &Candidate{ID: "c-2", TalentID: ptr("t-nao")}, OriginApproval, true)
	none, _ := addToBank(context.Background(), repo, "e", &Candidate{ID: "c-3", Email: "x@example.test"}, OriginApproval, true)

	if yes == nil || repo.inBank["t-sim"] != OriginApproval {
		t.Error("aprovado com consentimento não entrou no banco")
	}
	if no != nil || none != nil || len(repo.created) != 0 {
		t.Errorf("aprovado sem consentimento entrou no banco: %v %v, criados %v", no, none, repo.created)
	}
	if repo.consent["t-nao"] != "nao_notificado" {
		t.Error("a aprovação mexeu no consentimento de quem não consentiu")
	}
}

func TestPersonWhoRequestedExclusionNeverReturns(t *testing.T) {
	repo := newFakeBankRepo()
	repo.consent["t-1"] = "oposicao_exclusao"

	id, _ := addToBank(context.Background(), repo, "e", &Candidate{ID: "c-1", TalentID: ptr("t-1")}, OriginRejection, false)

	if id != nil || len(repo.inBank) != 0 {
		t.Error("quem pediu exclusão voltou ao banco")
	}
}

// A origem diz por onde a pessoa ENTROU: uma segunda passagem (ex.: reprovada, depois aprovada em outra
// vaga) não a reescreve — o histórico é que mostra as duas.
func TestOriginIsTheFirstEntryPath(t *testing.T) {
	repo := newFakeBankRepo()
	repo.consent["t-1"] = "consentido"
	cand := &Candidate{ID: "c-1", TalentID: ptr("t-1")}

	_, _ = addToBank(context.Background(), repo, "e", cand, OriginRejection, false)
	_, _ = addToBank(context.Background(), repo, "e", cand, OriginApproval, true)

	if repo.inBank["t-1"] != OriginRejection {
		t.Errorf("origem = %q", repo.inBank["t-1"])
	}
}

// --- reprovação e aprovação ponta a ponta no service (com fakes) ---

type decideRepo struct {
	*fakeBankRepo
	rejectedWith *string
}

func (r *decideRepo) Reject(_ context.Context, _, _, _ string, talentID *string) (bool, error) {
	r.rejectedWith = talentID
	return true, nil
}
func (r *decideRepo) CreateDecision(context.Context, *Decision) error { return nil }
func (r *decideRepo) FindPhasesByCampaign(context.Context, string, string) ([]PhaseRow, error) {
	return r.phases, nil
}
func (r *decideRepo) AdvancePhase(_ context.Context, _, _, next string, _ Status) (bool, error) {
	r.advanced = next
	return true, nil
}

type nopFeed struct{}

func (nopFeed) Create(context.Context, *activity.Entry) error { return nil }
func (nopFeed) ListByCompany(context.Context, string, int) ([]activity.Item, error) {
	return nil, nil
}

func TestRejectWithoutSendingToBankKeepsPersonOut(t *testing.T) {
	repo := &decideRepo{fakeBankRepo: newFakeBankRepo()}
	repo.consent["t-1"] = "consentido"
	cand := &Candidate{ID: "c-1", TalentID: ptr("t-1"), PhaseKey: PhaseEntrevista}

	for _, tc := range []struct {
		reason string
		send   bool
	}{{"perdeu_outro_candidato", false}, {"reprovacao_tecnica", true}, {"fit_cultural_incompativel", true}} {
		res, err := reject(context.Background(), repo, nopFeed{}, "e", cand,
			DecideRequest{Decision: "reprovar", RejectionReasonKey: ptr(tc.reason), SendBankInvite: tc.send}, nil, "rh")
		if err != nil || res.TalentID != nil || len(repo.inBank) != 0 {
			t.Errorf("%s/envio=%v: entrou no banco (res=%+v)", tc.reason, tc.send, res)
		}
	}

	res, _ := reject(context.Background(), repo, nopFeed{}, "e", cand,
		DecideRequest{Decision: "reprovar", RejectionReasonKey: ptr("perdeu_outro_candidato"), SendBankInvite: true}, nil, "rh")
	if res.TalentID == nil || *res.TalentID != "t-1" || repo.rejectedWith == nil {
		t.Errorf("motivo qualificado + envio confirmado não levou ao banco: %+v", res)
	}
}

func TestAdvanceToFinalPhaseAddsConsentedPersonToBank(t *testing.T) {
	repo := &decideRepo{fakeBankRepo: newFakeBankRepo()}
	repo.consent["t-1"] = "consentido"
	repo.phases = []PhaseRow{{PhaseRecebidos, 1}, {PhaseEntrevista, 2}, {PhaseSelecionados, 3}}

	mid, _ := advance(context.Background(), repo, nopFeed{}, "e", &Candidate{ID: "c-1", TalentID: ptr("t-1"), PhaseKey: PhaseRecebidos}, nil, "rh")
	if mid.TalentID != nil || len(repo.inBank) != 0 {
		t.Error("avançar para uma fase intermediária pôs a pessoa no banco")
	}

	final, _ := advance(context.Background(), repo, nopFeed{}, "e", &Candidate{ID: "c-1", TalentID: ptr("t-1"), PhaseKey: PhaseEntrevista}, nil, "rh")
	if final.TalentID == nil || repo.inBank["t-1"] != OriginApproval {
		t.Errorf("aprovado (Selecionados) com consentimento não entrou no banco: %+v", final)
	}
}

// --- cadastro manual ---

func manualService(repo Repository, extractor llm.Extractor) *service {
	return &service{repo: repo, extractor: extractor, usage: newRecordingUsage(),
		withTx: func(_ context.Context, fn func(db database.DB) error) error { return fn(nil) },
		txRepo: func(database.DB) Repository { return repo }}
}

type manualRepo struct {
	Repository
	bank  *fakeBankRepo
	cache *memoryCacheRepo
	seed  *TalentSeed
}

func newManualRepo() *manualRepo {
	return &manualRepo{bank: newFakeBankRepo(), cache: newMemoryCacheRepo()}
}

func (r *manualRepo) FindTalentIDByEmail(ctx context.Context, c, e string) (string, error) {
	return r.bank.FindTalentIDByEmail(ctx, c, e)
}
func (r *manualRepo) FindCachedExtraction(ctx context.Context, c, f string) (*llm.ExtractedProfile, error) {
	return r.cache.FindCachedExtraction(ctx, c, f)
}
func (r *manualRepo) SaveExtraction(ctx context.Context, c, f string, p *llm.ExtractedProfile) error {
	return r.cache.SaveExtraction(ctx, c, f, p)
}
func (r *manualRepo) CreateManualTalent(_ context.Context, t *TalentSeed) (string, error) {
	r.seed = t
	return "t-manual", nil
}
func (r *manualRepo) ResolveSkills(context.Context, []llm.SkillMention) ([]ResolvedSkill, []llm.SkillMention, error) {
	return nil, nil, nil
}
func (r *manualRepo) FindSkillMentionsInText(context.Context, string) ([]ResolvedSkill, error) {
	return nil, nil
}
func (r *manualRepo) ReplaceTalentExperience(context.Context, string, []ExperienceEntry) error {
	return nil
}
func (r *manualRepo) InsertTalentSourceDocument(context.Context, string, string) error { return nil }

func TestManualTalentValidatesEveryField(t *testing.T) {
	s := manualService(newManualRepo(), &countingExtractor{})

	_, err := s.RegisterManualTalent(context.Background(), "e", ManualTalentInput{
		Name: "Ana", RawProfileText: strings.Repeat("a", MaxResumeTextRunes+1), ContextNote: strings.Repeat("n", 2001),
	})

	var appErr *apperror.AppError
	if !errors.As(err, &appErr) || appErr.Fields["name"] == "" || appErr.Fields["rawProfileText"] == "" || appErr.Fields["contextNote"] == "" {
		t.Errorf("erro = %v (%+v)", err, appErr)
	}
}

// Nome e nota bastam; sem perfil colado, nada é extraído (nenhum custo).
func TestManualTalentWithoutProfileDoesNotCallExtractor(t *testing.T) {
	extractor := &countingExtractor{}
	repo := newManualRepo()
	s := manualService(repo, extractor)

	id, err := s.RegisterManualTalent(context.Background(), "e", ManualTalentInput{Name: "Ana Souza", ContextNote: "Evento X"})

	if err != nil || id != "t-manual" || extractor.calls != 0 {
		t.Errorf("id=%q err=%v chamadas=%d", id, err, extractor.calls)
	}
	if repo.seed.Name != "Ana Souza" || repo.seed.RecruiterNotes != "Evento X" {
		t.Errorf("seed = %+v", repo.seed)
	}
}

// A mesma pessoa não vira dois talentos: o e-mail do perfil colado já existe na empresa.
func TestManualTalentRejectsExistingPerson(t *testing.T) {
	repo := newManualRepo()
	repo.bank.byEmail["cv@example.test"] = "t-existente" // countingExtractor devolve este e-mail
	s := manualService(repo, &countingExtractor{})

	_, err := s.RegisterManualTalent(context.Background(), "e", ManualTalentInput{Name: "Ana Souza", RawProfileText: "perfil"})

	var appErr *apperror.AppError
	if !errors.As(err, &appErr) || !strings.Contains(appErr.Fields["rawProfileText"]+appErr.Message, "cv@example.test") {
		t.Errorf("erro = %v", err)
	}
	if repo.seed != nil {
		t.Error("criou o talento duplicado mesmo assim")
	}
}

// O nome digitado pelo recrutador vence o extraído; telefone não é guardado em perfil manual (5.2).
func TestManualTalentKeepsTypedNameAndDropsPhone(t *testing.T) {
	repo := newManualRepo()
	s := manualService(repo, maliciousExtractor{profile: llm.ExtractedProfile{Name: "Outro Nome", Phone: "(61) 99999-0000", Summary: "Dev"}})

	if _, err := s.RegisterManualTalent(context.Background(), "e", ManualTalentInput{Name: "Ana Souza", RawProfileText: "perfil"}); err != nil {
		t.Fatal(err)
	}
	if repo.seed.Name != "Ana Souza" || repo.seed.Phone != "" || repo.seed.Summary != "Dev" {
		t.Errorf("seed = %+v", repo.seed)
	}
}
