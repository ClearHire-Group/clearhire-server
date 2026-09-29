package campaign

import (
	"context"
	"errors"
	"testing"
)

// fakeAddTalentsRepo é o dobro de Repository usado só pelos testes de AddTalentsToCampaign.
type fakeAddTalentsRepo struct {
	Repository
	campaign       *Campaign
	added          []string
	createErr      error
	eligibility    []TalentEligibility
	eligibilityErr error
}

func (f *fakeAddTalentsRepo) FindByID(ctx context.Context, companyID, id string) (*Campaign, error) {
	return f.campaign, nil
}

func (f *fakeAddTalentsRepo) CreateCandidatesFromTalents(ctx context.Context, companyID, campaignID string, talentIDs []string) ([]string, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.added, nil
}

func (f *fakeAddTalentsRepo) FindTalentsEligibility(ctx context.Context, companyID string, talentIDs []string) ([]TalentEligibility, error) {
	return f.eligibility, f.eligibilityErr
}

func newAddTalentsService(repo Repository) Service {
	return &service{repo: repo}
}

func TestAddTalentsToCampaignRejectsUnknownCampaign(t *testing.T) {
	svc := newAddTalentsService(&fakeAddTalentsRepo{campaign: nil})
	_, err := svc.AddTalentsToCampaign(context.Background(), "company-1", "campaign-x", []string{"t1"})
	if err == nil {
		t.Fatal("esperava erro para campanha inexistente/de outra empresa")
	}
}

// Todo id pedido tem que aparecer em Added OU em Skipped — nunca desaparecer sem explicação.
func TestAddTalentsToCampaignExplainsEverySkipped(t *testing.T) {
	repo := &fakeAddTalentsRepo{
		campaign: &Campaign{ID: "campaign-1"},
		added:    []string{"t1"},
		eligibility: []TalentEligibility{
			{ID: "t2", Name: "Bruno", ConsentState: "nao_notificado"},
			{ID: "t3", Name: "Carla", ConsentState: "oposicao_exclusao"},
			// t4 propositalmente ausente: simula id que não existe nesta empresa.
		},
	}
	svc := newAddTalentsService(repo)

	result, err := svc.AddTalentsToCampaign(context.Background(), "company-1", "campaign-1", []string{"t1", "t2", "t3", "t4"})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(result.Added) != 1 || result.Added[0] != "t1" {
		t.Fatalf("Added = %v, esperava [t1]", result.Added)
	}
	if len(result.Skipped) != 3 {
		t.Fatalf("esperava 3 talentos explicados em Skipped, obtive %+v", result.Skipped)
	}

	byID := map[string]SkippedTalent{}
	for _, s := range result.Skipped {
		byID[s.TalentID] = s
	}
	if got := byID["t2"].Reason; got == "" || byID["t2"].Name != "Bruno" {
		t.Errorf("t2 (nao_notificado) mal explicado: %+v", byID["t2"])
	}
	if got := byID["t3"].Reason; got == "" || byID["t3"].Name != "Carla" {
		t.Errorf("t3 (oposicao_exclusao) mal explicado: %+v", byID["t3"])
	}
	if byID["t4"].Reason == "" {
		t.Errorf("t4 (id inexistente) deveria ter uma explicação mesmo sem nome")
	}
}

// Cada consent_state produz uma mensagem DISTINTA e específica — nunca um "não deu certo" genérico
// que esconderia a diferença entre "pediu exclusão" (nunca) e "ainda não foi notificado" (dá pra
// resolver com um passo a mais).
func TestSkipReasonDistinguishesConsentStates(t *testing.T) {
	optOut := skipReason("oposicao_exclusao")
	notNotified := skipReason("nao_notificado")
	if optOut == notNotified {
		t.Fatal("oposicao_exclusao e nao_notificado não podem produzir a mesma mensagem")
	}
	if optOut == "" || notNotified == "" {
		t.Fatal("nenhum motivo pode ficar vazio")
	}
}

func TestAddTalentsToCampaignPropagatesRepositoryError(t *testing.T) {
	repo := &fakeAddTalentsRepo{campaign: &Campaign{ID: "campaign-1"}, createErr: errors.New("db fora do ar")}
	svc := newAddTalentsService(repo)
	if _, err := svc.AddTalentsToCampaign(context.Background(), "company-1", "campaign-1", []string{"t1"}); err == nil {
		t.Fatal("esperava erro propagado do repositório")
	}
}

// Nada a explicar quando todos entraram: FindTalentsEligibility não deveria nem ser chamado (e,
// mesmo que fosse, um erro nele não pode derrubar uma operação que já teve sucesso).
func TestAddTalentsToCampaignSkipsEligibilityLookupWhenAllAdded(t *testing.T) {
	repo := &fakeAddTalentsRepo{
		campaign:       &Campaign{ID: "campaign-1"},
		added:          []string{"t1", "t2"},
		eligibilityErr: errors.New("nunca deveria ser chamado"),
	}
	svc := newAddTalentsService(repo)
	result, err := svc.AddTalentsToCampaign(context.Background(), "company-1", "campaign-1", []string{"t1", "t2"})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(result.Skipped) != 0 {
		t.Errorf("esperava Skipped vazio, obtive %+v", result.Skipped)
	}
}
