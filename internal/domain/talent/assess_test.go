package talent

import (
	"context"
	"errors"
	"testing"

	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

// fakeAssessRepo é o dobro de Repository usado pelos testes de AssessForCampaign — só FindInBank
// importa aqui (os talentos "existem" ou não, conforme o mapa `byID`).
type fakeAssessRepo struct {
	Repository
	byID map[string]Talent
}

func (f *fakeAssessRepo) FindInBank(ctx context.Context, companyID, id string) (*Talent, error) {
	t, ok := f.byID[id]
	if !ok {
		return nil, nil
	}
	return &t, nil
}

func newAssessService(byID map[string]Talent, assess AssessFunc) Service {
	return &service{repo: &fakeAssessRepo{byID: byID}, assess: assess}
}

// Regressão de segurança principal desta etapa: o teto nunca pode ser contornado — mesmo que o
// validate da camada HTTP algum dia saia de sincronia, o service recusa sozinho.
func TestAssessForCampaignEnforcesCap(t *testing.T) {
	called := false
	svc := newAssessService(nil, func(ctx context.Context, companyID, campaignID, talentID string, profile llm.CandidateContext) (*llm.Assessment, error) {
		called = true
		return &llm.Assessment{}, nil
	})
	ids := []string{"1", "2", "3", "4", "5", "6"} // 6 > maxAssessedTalentsPerRequest (5)

	_, err := svc.AssessForCampaign(context.Background(), "company-1", "campaign-1", ids)
	if err == nil {
		t.Fatal("esperava erro por exceder o teto de talentos por chamada")
	}
	if called {
		t.Error("AssessFunc não deveria ser chamada nenhuma vez quando o teto é violado")
	}
}

func TestAssessForCampaignRejectsEmpty(t *testing.T) {
	svc := newAssessService(nil, func(ctx context.Context, companyID, campaignID, talentID string, profile llm.CandidateContext) (*llm.Assessment, error) {
		t.Fatal("AssessFunc não deveria ser chamada com lista vazia")
		return nil, nil
	})
	if _, err := svc.AssessForCampaign(context.Background(), "company-1", "campaign-1", nil); err == nil {
		t.Fatal("esperava erro com lista vazia de talentos")
	}
}

// Id que não existe no banco desta empresa é ignorado, não aborta o lote — mesma filosofia de
// CreateCandidatesFromTalents (campaign/repository.go).
func TestAssessForCampaignSkipsUnknownIDs(t *testing.T) {
	byID := map[string]Talent{"real": {ID: "real", Name: "Existe"}}
	svc := newAssessService(byID, func(ctx context.Context, companyID, campaignID, talentID string, profile llm.CandidateContext) (*llm.Assessment, error) {
		return &llm.Assessment{MatchPct: 80}, nil
	})

	out, err := svc.AssessForCampaign(context.Background(), "company-1", "campaign-1", []string{"fantasma", "real"})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(out) != 1 || out[0].Talent.ID != "real" {
		t.Fatalf("esperava só o talento real avaliado, obtive %+v", out)
	}
}

// Erro de orçamento (ou qualquer outro) da AssessFunc propaga na hora — sem lote parcial confuso.
func TestAssessForCampaignPropagatesAssessError(t *testing.T) {
	byID := map[string]Talent{"a": {ID: "a"}, "b": {ID: "b"}}
	calls := 0
	svc := newAssessService(byID, func(ctx context.Context, companyID, campaignID, talentID string, profile llm.CandidateContext) (*llm.Assessment, error) {
		calls++
		if talentID == "a" {
			return nil, errors.New("teto de orçamento atingido")
		}
		return &llm.Assessment{}, nil
	})

	_, err := svc.AssessForCampaign(context.Background(), "company-1", "campaign-1", []string{"a", "b"})
	if err == nil {
		t.Fatal("esperava erro propagado da AssessFunc")
	}
	if calls != 1 {
		t.Errorf("esperava parar no primeiro erro (1 chamada), teve %d", calls)
	}
}

// Mapeamento Talent -> llm.CandidateContext: educação concatenada, skills achatadas em termos.
func TestCandidateContextFromTalentMapping(t *testing.T) {
	years := 4
	talent := Talent{
		YearsExperience:      &years,
		Summary:              "Resumo",
		EducationDegree:      "Bacharelado",
		EducationInstitution: "UnB",
		Skills:               []Skill{{Term: "Go"}, {Term: "SQL"}},
		Experience:           []ExperienceEntry{{Role: "Dev", Company: "X", PeriodLabel: "2020-2023"}},
	}
	ctx := candidateContextFromTalent(talent)

	if ctx.Education != "Bacharelado — UnB" {
		t.Errorf("educação = %q, esperava concatenação com travessão", ctx.Education)
	}
	if len(ctx.Skills) != 2 || ctx.Skills[0] != "Go" || ctx.Skills[1] != "SQL" {
		t.Errorf("skills = %v, esperava [Go SQL]", ctx.Skills)
	}
	if len(ctx.Experience) != 1 || ctx.Experience[0].Role != "Dev" {
		t.Errorf("experiência não mapeada corretamente: %+v", ctx.Experience)
	}
	if ctx.YearsExperience == nil || *ctx.YearsExperience != 4 {
		t.Errorf("anos de experiência não propagados")
	}
}
