package dashboard

import (
	"context"
	"testing"
)

// fakeSuggestionsRepo simula só o que GetSuggestions usa — as outras chamadas de Repository nunca
// disparam nesse teste, então ficam sem implementação (embedding cobre a interface).
type fakeSuggestionsRepo struct {
	Repository
	ready   []ReadyCandidate
	waiting []WaitingGroup
}

func (r *fakeSuggestionsRepo) ReadyToAdvance(_ context.Context, _ string, minMatch, limit int) ([]ReadyCandidate, error) {
	out := make([]ReadyCandidate, 0, len(r.ready))
	for _, rc := range r.ready {
		if rc.MatchPct >= minMatch {
			out = append(out, rc)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *fakeSuggestionsRepo) WaitingByPhase(_ context.Context, _ string, minCount, limit int) ([]WaitingGroup, error) {
	out := make([]WaitingGroup, 0, len(r.waiting))
	for _, g := range r.waiting {
		if g.Count >= minCount {
			out = append(out, g)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func newSuggestionsService(repo *fakeSuggestionsRepo) Service {
	return NewService(repo)
}

func TestGetSuggestions_SemDadosDevolveListaVazia(t *testing.T) {
	svc := newSuggestionsService(&fakeSuggestionsRepo{})
	out, err := svc.GetSuggestions(context.Background(), "empresa-1")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if out == nil {
		t.Fatal("esperava slice vazio, veio nil (front espera `[]`, não `null`)")
	}
	if len(out) != 0 {
		t.Fatalf("esperava 0 sugestões, veio %d", len(out))
	}
}

func TestGetSuggestions_PrimeiroCandidatoProntoVemDestacado(t *testing.T) {
	repo := &fakeSuggestionsRepo{
		ready: []ReadyCandidate{
			{CandidateID: "c1", CandidateName: "Marina Albuquerque", CampaignID: "camp-1", CampaignTitle: "Eng. Software Sênior", PhaseKey: "tecnica", NextPhaseKey: "entrevista", MatchPct: 94},
			{CandidateID: "c2", CandidateName: "João Silva", CampaignID: "camp-1", CampaignTitle: "Eng. Software Sênior", PhaseKey: "tecnica", NextPhaseKey: "entrevista", MatchPct: 85},
		},
	}
	svc := newSuggestionsService(repo)
	out, err := svc.GetSuggestions(context.Background(), "empresa-1")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("esperava 2 sugestões, veio %d", len(out))
	}
	if !out[0].Highlighted {
		t.Error("primeiro candidato pronto deveria vir destacado (highlighted=true)")
	}
	if out[1].Highlighted {
		t.Error("segundo candidato pronto não deveria vir destacado")
	}
	wantMsg := "Marina Albuquerque (94% de match) pode avançar de Triagem Técnica para Entrevista Estruturada em Eng. Software Sênior."
	if out[0].Message != wantMsg {
		t.Errorf("mensagem = %q, want %q", out[0].Message, wantMsg)
	}
	wantRoute := []string{"/campanhas", "camp-1", "candidatos", "c1"}
	if !equalRoutes(out[0].PrimaryActionRoute, wantRoute) {
		t.Errorf("rota = %v, want %v", out[0].PrimaryActionRoute, wantRoute)
	}
}

func TestGetSuggestions_UltimaFaseSemProximaMudaOTexto(t *testing.T) {
	repo := &fakeSuggestionsRepo{
		ready: []ReadyCandidate{
			{CandidateID: "c1", CandidateName: "Ana Costa", CampaignID: "camp-1", CampaignTitle: "Customer Success", PhaseKey: "selecionados", NextPhaseKey: "", MatchPct: 90},
		},
	}
	svc := newSuggestionsService(repo)
	out, err := svc.GetSuggestions(context.Background(), "empresa-1")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	want := "Ana Costa (90% de match) aguarda sua decisão em Selecionados na campanha Customer Success."
	if out[0].Message != want {
		t.Errorf("mensagem = %q, want %q", out[0].Message, want)
	}
}

func TestGetSuggestions_GrupoDeEsperaGeraMensagemPlural(t *testing.T) {
	repo := &fakeSuggestionsRepo{
		waiting: []WaitingGroup{
			{CampaignID: "camp-2", CampaignTitle: "Customer Success Pleno", PhaseKey: "fit", Count: 7},
		},
	}
	svc := newSuggestionsService(repo)
	out, err := svc.GetSuggestions(context.Background(), "empresa-1")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("esperava 1 sugestão, veio %d", len(out))
	}
	want := "7 candidatos analisados em Fit Cultural para Customer Success Pleno aguardam sua decisão."
	if out[0].Message != want {
		t.Errorf("mensagem = %q, want %q", out[0].Message, want)
	}
	if out[0].Highlighted {
		t.Error("sugestão de grupo em espera não deveria vir destacada")
	}
}

// waitingMessage é testada direto (função pura, mesmo pacote), não via GetSuggestions: o
// repositório real só devolve grupos com Count >= minWaitingGroupSize (HAVING no SQL), então o
// ramo singular nunca é alcançado pelo pipeline de hoje — mas a função deve continuar correta
// caso minWaitingGroupSize mude para 1 no futuro.
func TestWaitingMessage_UmCandidatoUsaSingular(t *testing.T) {
	got := waitingMessage(WaitingGroup{CampaignTitle: "Customer Success Pleno", PhaseKey: "fit", Count: 1})
	want := "1 candidato analisado em Fit Cultural para Customer Success Pleno aguarda sua decisão."
	if got != want {
		t.Errorf("mensagem = %q, want %q", got, want)
	}
}

func TestGetSuggestions_ProntosVemAntesDosGruposDeEspera(t *testing.T) {
	repo := &fakeSuggestionsRepo{
		ready: []ReadyCandidate{
			{CandidateID: "c1", CandidateName: "Marina", CampaignID: "camp-1", CampaignTitle: "Eng.", PhaseKey: "tecnica", NextPhaseKey: "entrevista", MatchPct: 90},
		},
		waiting: []WaitingGroup{
			{CampaignID: "camp-2", CampaignTitle: "CS", PhaseKey: "fit", Count: 5},
		},
	}
	svc := newSuggestionsService(repo)
	out, err := svc.GetSuggestions(context.Background(), "empresa-1")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("esperava 2 sugestões, veio %d", len(out))
	}
	if out[0].ID[:6] != "ready-" {
		t.Errorf("primeira sugestão deveria ser de candidato pronto, veio ID=%q", out[0].ID)
	}
	if out[1].ID[:8] != "waiting-" {
		t.Errorf("segunda sugestão deveria ser de grupo em espera, veio ID=%q", out[1].ID)
	}
}

func equalRoutes(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
