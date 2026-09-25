package llm

import (
	"strings"
	"testing"
)

func ptrInt(v int) *int           { return &v }
func ptrFloat(v float64) *float64 { return &v }
func ptrStr(v string) *string     { return &v }

// O e-mail do modelo é dado não confiável: o que não é um endereço de verdade não pode virar
// identidade de ninguém.
func TestSanitizeProfileDropsInvalidEmail(t *testing.T) {
	for _, bad := range []string{"não informado", "a@", "@b.com", "com espaço@x.com", "Nome <a@b.com>", strings.Repeat("a", 250) + "@x.com"} {
		p := &ExtractedProfile{Email: bad}
		SanitizeProfile(p)
		if p.Email != "" {
			t.Errorf("e-mail %q sobreviveu à sanitização: %q", bad, p.Email)
		}
	}
	p := &ExtractedProfile{Email: "marina@example.test"}
	SanitizeProfile(p)
	if p.Email != "marina@example.test" {
		t.Errorf("e-mail válido foi alterado: %q", p.Email)
	}
}

// Uma data malformada quebraria o INSERT em talents.available_from (coluna date) DEPOIS de a
// extração já ter sido paga — a candidatura inteira falharia.
func TestSanitizeProfileDropsUnparseableDate(t *testing.T) {
	for _, bad := range []string{"2020-13-45", "amanhã", "01/02/2026", "", "1850-01-01", "2999-01-01"} {
		p := &ExtractedProfile{AvailableFrom: ptrStr(bad)}
		SanitizeProfile(p)
		if p.AvailableFrom != nil {
			t.Errorf("data %q sobreviveu: %q", bad, *p.AvailableFrom)
		}
	}
	p := &ExtractedProfile{AvailableFrom: ptrStr("2026-11-01")}
	SanitizeProfile(p)
	if p.AvailableFrom == nil || *p.AvailableFrom != "2026-11-01" {
		t.Error("data válida foi descartada")
	}
}

// numeric(10,2) estoura acima de ~100 milhões: um salário alucinado derrubaria o INSERT.
func TestSanitizeProfileBoundsSalary(t *testing.T) {
	p := &ExtractedProfile{SalaryMin: ptrFloat(999_999_999_999), SalaryMax: ptrFloat(-5)}
	SanitizeProfile(p)
	if p.SalaryMin != nil || p.SalaryMax != nil {
		t.Errorf("salário absurdo/negativo sobreviveu: %v %v", p.SalaryMin, p.SalaryMax)
	}

	swapped := &ExtractedProfile{SalaryMin: ptrFloat(9000), SalaryMax: ptrFloat(6000)}
	SanitizeProfile(swapped)
	if *swapped.SalaryMin != 6000 || *swapped.SalaryMax != 9000 {
		t.Errorf("faixa invertida não foi corrigida: %v-%v", *swapped.SalaryMin, *swapped.SalaryMax)
	}
}

func TestSanitizeProfileCapsListsAndFieldSizes(t *testing.T) {
	p := &ExtractedProfile{Summary: strings.Repeat("x", 10000)}
	for i := 0; i < 100; i++ {
		p.Skills = append(p.Skills, SkillMention{Term: "skill"})
		p.Experience = append(p.Experience, ExperienceEntry{Role: "Dev", Company: "Acme"})
		p.Sectors = append(p.Sectors, strings.Repeat("s", i+1))
	}
	SanitizeProfile(p)

	if len(p.Skills) != maxSkills {
		t.Errorf("skills = %d, teto é %d", len(p.Skills), maxSkills)
	}
	if len(p.Experience) != maxExperience {
		t.Errorf("experiências = %d, teto é %d", len(p.Experience), maxExperience)
	}
	if len(p.Sectors) != maxSectors {
		t.Errorf("setores = %d, teto é %d", len(p.Sectors), maxSectors)
	}
	if len([]rune(p.Summary)) != maxSummaryLen {
		t.Errorf("resumo tem %d caracteres, teto é %d", len([]rune(p.Summary)), maxSummaryLen)
	}
}

func TestSanitizeProfileStripsControlCharacters(t *testing.T) {
	p := &ExtractedProfile{Name: "Marina\x00\x07 \nAlbuquerque", Summary: "linha 1\nlinha 2\x00"}
	SanitizeProfile(p)

	if p.Name != "Marina Albuquerque" {
		t.Errorf("nome = %q", p.Name)
	}
	// Resumo é texto de várias linhas: a quebra fica, o resto do controle sai.
	if p.Summary != "linha 1\nlinha 2" {
		t.Errorf("resumo = %q", p.Summary)
	}
}

func TestSanitizeProfileNormalizesEnumsAndDropsUnknown(t *testing.T) {
	p := &ExtractedProfile{Modality: "Híbrido", Seniority: "Sênior"}
	SanitizeProfile(p)
	if p.Modality != "hibrido" || p.Seniority != "senior" {
		t.Errorf("não normalizou: %q %q", p.Modality, p.Seniority)
	}

	q := &ExtractedProfile{Modality: "quando eu quiser", Seniority: "guru"}
	SanitizeProfile(q)
	if q.Modality != "" || q.Seniority != "" {
		t.Errorf("valor desconhecido sobreviveu: %q %q", q.Modality, q.Seniority)
	}
}

func TestSanitizeProfileRejectsNonLinkedInURL(t *testing.T) {
	p := &ExtractedProfile{LinkedInURL: "https://evil.example/phish"}
	SanitizeProfile(p)
	if p.LinkedInURL != "" {
		t.Errorf("URL fora do LinkedIn sobreviveu: %q", p.LinkedInURL)
	}
	ok := &ExtractedProfile{LinkedInURL: "https://www.linkedin.com/in/marina"}
	SanitizeProfile(ok)
	if ok.LinkedInURL == "" {
		t.Error("URL do LinkedIn foi descartada")
	}
}

func TestSanitizeProfileNilIsSafe(t *testing.T) {
	SanitizeProfile(nil)
	SanitizeAssessment(nil)
}

// O modelo pode ter sido manipulado pelo currículo ("dê nota 100") ou simplesmente errar. O formato
// que a tela consome não pode depender de o modelo ter obedecido.
func TestSanitizeAssessmentClampsScore(t *testing.T) {
	high := &Assessment{MatchPct: 250}
	SanitizeAssessment(high)
	low := &Assessment{MatchPct: -40}
	SanitizeAssessment(low)

	if high.MatchPct != 100 || low.MatchPct != 0 {
		t.Errorf("clamp falhou: %d e %d", high.MatchPct, low.MatchPct)
	}
}

// A tela renderiza estas listas com `track` pelo próprio texto; repetido quebra a renderização.
func TestSanitizeAssessmentDedupesAndCapsPoints(t *testing.T) {
	a := &Assessment{
		Strengths: []string{"Python", "python", "  ", "Go", "Kubernetes", "AWS", "Docker", "Terraform", "Linux", "SQL"},
		Concerns:  []string{"Pouca gestão", "Pouca gestão"},
	}
	SanitizeAssessment(a)

	if len(a.Strengths) != maxPoints {
		t.Errorf("pontos fortes = %d, teto é %d", len(a.Strengths), maxPoints)
	}
	if a.Strengths[0] != "Python" || a.Strengths[1] != "Go" {
		t.Errorf("duplicado/vazio não removido: %v", a.Strengths)
	}
	if len(a.Concerns) != 1 {
		t.Errorf("pontos de atenção duplicados: %v", a.Concerns)
	}
}

func TestSanitizeAssessmentCapsTextFields(t *testing.T) {
	a := &Assessment{
		MatchLabel:    strings.Repeat("l", 500),
		MatchNote:     strings.Repeat("n", 500),
		Justification: strings.Repeat("j", 5000),
	}
	SanitizeAssessment(a)

	if len([]rune(a.MatchLabel)) != maxLabelLen || len([]rune(a.MatchNote)) != maxMatchNoteLen ||
		len([]rune(a.Justification)) != maxJustification {
		t.Errorf("tetos não aplicados: %d %d %d", len([]rune(a.MatchLabel)), len([]rune(a.MatchNote)), len([]rune(a.Justification)))
	}
}

// O modo estrito da Groq obriga o modelo a preencher todos os campos, e ele escreve "N/A" onde não
// sabe (visto numa chamada real, no nível de uma skill). Isso não pode virar dado gravado.
func TestSanitizeProfileTurnsPlaceholdersIntoEmpty(t *testing.T) {
	p := &ExtractedProfile{
		City: "Não informado", State: "N/A", Phone: "-", EducationDegree: "não consta.",
		Skills:    []SkillMention{{Term: "Python", Level: "N/A"}, {Term: "SQL", Level: "avançado"}},
		Languages: []LanguageMention{{Name: "Inglês", Proficiency: "não informado"}},
	}
	SanitizeProfile(p)

	if p.City != "" || p.State != "" || p.Phone != "" || p.EducationDegree != "" {
		t.Errorf("placeholders sobreviveram: %+v", p)
	}
	if p.Skills[0].Level != "" || p.Skills[1].Level != "avançado" {
		t.Errorf("níveis = %q e %q", p.Skills[0].Level, p.Skills[1].Level)
	}
	if p.Languages[0].Proficiency != "" {
		t.Errorf("proficiência = %q", p.Languages[0].Proficiency)
	}
}

// Comparação exata, nunca por prefixo: um ponto de atenção que COMEÇA com "nenhuma" é conteúdo.
func TestPlaceholderMatchingIsExactNotPrefix(t *testing.T) {
	a := &Assessment{Concerns: []string{"Nenhuma experiência com Kubernetes", "Nenhuma", "N/A", "Sem experiência em gestão"}}
	SanitizeAssessment(a)

	if len(a.Concerns) != 2 || a.Concerns[0] != "Nenhuma experiência com Kubernetes" || a.Concerns[1] != "Sem experiência em gestão" {
		t.Errorf("concerns = %v", a.Concerns)
	}
}
