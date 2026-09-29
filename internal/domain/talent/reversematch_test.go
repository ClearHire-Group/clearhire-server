package talent

import (
	"context"
	"errors"
	"testing"
)

// fakeMatchRepo é o dobro de Repository usado só pelos testes de ReverseMatch — devolve o pool e a
// resolução de taxonomia fixados pelo teste, sem tocar em banco nenhum.
type fakeMatchRepo struct {
	Repository // embutido: painel de métodos não usados neste arquivo nunca é chamado
	pool       []Talent
	skills     []string
	sector     string
}

func (f *fakeMatchRepo) ListInBank(ctx context.Context, companyID string) ([]Talent, error) {
	return f.pool, nil
}
func (f *fakeMatchRepo) ResolveSkillTermsInText(ctx context.Context, text string) ([]string, error) {
	return f.skills, nil
}
func (f *fakeMatchRepo) ResolveSectorInText(ctx context.Context, text string) (string, error) {
	return f.sector, nil
}

func skillTalent(id string, skills ...string) Talent {
	sk := make([]Skill, len(skills))
	for i, s := range skills {
		sk[i] = Skill{Term: s}
	}
	return Talent{ID: id, Name: "Pessoa " + id, Seniority: "Pleno", ConsentState: "consentido", Skills: sk}
}

// runMatch monta o service com um fakeMatchRepo já resolvido (skills/setor fixados pelo teste, não
// pela taxonomia real) e chama ReverseMatch — o que está sob teste é a FÓRMULA de score, não a
// resolução de taxonomia (que tem sua própria responsabilidade, em repository.go).
func runMatch(t *testing.T, pool []Talent, resolvedSkills []string, sector string, criteria MatchCriteria) []TalentMatch {
	t.Helper()
	svc := &service{repo: &fakeMatchRepo{pool: pool, skills: resolvedSkills, sector: sector}}
	matches, err := svc.ReverseMatch(context.Background(), "company-1", criteria)
	if err != nil {
		t.Fatalf("ReverseMatch: %v", err)
	}
	return matches
}

func findMatch(matches []TalentMatch, id string) (TalentMatch, bool) {
	for _, m := range matches {
		if m.Talent.ID == id {
			return m, true
		}
	}
	return TalentMatch{}, false
}

// Espelha talent-matching.spec.ts: "skill é casada como palavra inteira".
func TestReverseMatchSkillWordBoundary(t *testing.T) {
	pool := []Talent{skillTalent("a", "Skill 1"), skillTalent("b", "Skill 12"), skillTalent("c", "Go")}
	matches := runMatch(t, pool, []string{"Skill 12"}, "", MatchCriteria{Title: "vaga Skill 12"})

	if len(matches) != 1 || matches[0].Talent.ID != "b" {
		t.Fatalf("esperava só 'b' (Skill 12), obtive %+v", matches)
	}
}

// Espelha: quem pediu exclusão nunca aparece, mesmo com skill batendo perfeitamente.
func TestReverseMatchExcludesOptedOut(t *testing.T) {
	a := skillTalent("a", "Python")
	b := skillTalent("b", "Python")
	b.ConsentState = "oposicao_exclusao"
	matches := runMatch(t, []Talent{a, b}, []string{"Python"}, "", MatchCriteria{Title: "vaga Python"})

	if len(matches) != 1 || matches[0].Talent.ID != "a" {
		t.Fatalf("esperava só 'a', obtive %+v", matches)
	}
}

// Espelha: resultados vêm do maior match pro menor.
func TestReverseMatchSortedDescending(t *testing.T) {
	a := skillTalent("a", "SQL")
	b := skillTalent("b", "Python", "SQL")
	c := skillTalent("c", "Python")
	matches := runMatch(t, []Talent{a, b, c}, []string{"Python", "SQL"}, "", MatchCriteria{Title: "vaga Python SQL"})

	if len(matches) == 0 || matches[0].Talent.ID != "b" {
		t.Fatalf("esperava 'b' (as duas skills) em 1º, obtive %+v", matches)
	}
	for i := 1; i < len(matches); i++ {
		if matches[i].MatchPct > matches[i-1].MatchPct {
			t.Fatalf("ordem decrescente violada: %+v", matches)
		}
	}
}

// Espelha: atender só 2 de 3 skills nunca chega a 100% nem empata com quem atende as 3, mesmo em
// nível máximo.
func TestReverseMatchPartialNeverBeatsComplete(t *testing.T) {
	completo := Talent{ID: "completo", ConsentState: "consentido", Skills: []Skill{
		{Term: "Python", Level: "especialista"}, {Term: "SQL", Level: "especialista"}, {Term: "Java", Level: "especialista"},
	}}
	parcial := Talent{ID: "parcial", ConsentState: "consentido", Skills: []Skill{
		{Term: "Python", Level: "especialista"}, {Term: "SQL", Level: "especialista"},
	}}
	matches := runMatch(t, []Talent{completo, parcial}, []string{"Python", "SQL", "Java"}, "",
		MatchCriteria{Title: "vaga exige Python, SQL e Java"})

	mc, ok1 := findMatch(matches, "completo")
	mp, ok2 := findMatch(matches, "parcial")
	if !ok1 || !ok2 {
		t.Fatalf("esperava os dois talentos no resultado, obtive %+v", matches)
	}
	if mc.MatchPct != 100 {
		t.Errorf("completo deveria ser 100%%, foi %d", mc.MatchPct)
	}
	if mp.MatchPct >= 100 || mp.MatchPct >= mc.MatchPct {
		t.Errorf("parcial (%d) deveria ficar abaixo de completo (%d) e abaixo de 100", mp.MatchPct, mc.MatchPct)
	}
}

// Modalidade e senioridade entram como critérios binários — quem não bate nenhuma das duas fica
// bem abaixo de quem bate as duas, mesmo com a mesma skill.
func TestReverseMatchModalityAndSeniority(t *testing.T) {
	// Level "especialista" de propósito: skill sem nível explícito usa o peso padrão (25/35, ver
	// weightForLevel) e nunca atinge achieved=1 sozinha — não é o que este teste quer isolar.
	bate := Talent{ID: "bate", Modality: "Remoto", Seniority: "Pleno", ConsentState: "consentido",
		Skills: []Skill{{Term: "Python", Level: "especialista"}}}
	naoBate := Talent{ID: "naobate", Modality: "Presencial", Seniority: "Júnior", ConsentState: "consentido",
		Skills: []Skill{{Term: "Python", Level: "especialista"}}}

	matches := runMatch(t, []Talent{bate, naoBate}, []string{"Python"}, "",
		MatchCriteria{Title: "vaga Python", Modality: "remoto", Seniority: "pleno"})

	mb, _ := findMatch(matches, "bate")
	mn, _ := findMatch(matches, "naobate")
	if mb.MatchPct <= mn.MatchPct {
		t.Errorf("quem bate modalidade+senioridade (%d) deveria ficar acima de quem não bate (%d)", mb.MatchPct, mn.MatchPct)
	}
	if mb.MatchPct != 100 {
		t.Errorf("quem bate tudo (skill+modalidade+senioridade) deveria ser 100%%, foi %d", mb.MatchPct)
	}
}

// Sem nenhum sinal reconhecível no título (nem skill nem setor nem modalidade), cai no fallback de
// correspondência textual — não deve estourar nem devolver negativo.
func TestReverseMatchFallbackWhenNoSignal(t *testing.T) {
	a := Talent{ID: "a", Name: "Ana Engenheira", Seniority: "Pleno", ConsentState: "consentido"}
	matches := runMatch(t, []Talent{a}, nil, "", MatchCriteria{Title: "engenheira de dados pleno"})

	if len(matches) != 1 {
		t.Fatalf("esperava 1 resultado do fallback textual, obtive %+v", matches)
	}
	if matches[0].MatchPct < 0 || matches[0].MatchPct > 100 {
		t.Errorf("pct fora de faixa: %d", matches[0].MatchPct)
	}
}

// Regressão de segurança: erro do repositório sobe como erro, nunca é engolido silenciosamente.
func TestReverseMatchPropagatesRepositoryError(t *testing.T) {
	svc := &service{repo: &erroringRepo{}}
	if _, err := svc.ReverseMatch(context.Background(), "company-1", MatchCriteria{Title: "x"}); err == nil {
		t.Fatal("esperava erro quando o repositório falha")
	}
}

type erroringRepo struct{ Repository }

func (erroringRepo) ListInBank(ctx context.Context, companyID string) ([]Talent, error) {
	return nil, errors.New("db indisponível")
}
