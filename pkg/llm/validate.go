package llm

import (
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Tudo aqui existe por um motivo só: a saída de um modelo é ENTRADA NÃO CONFIÁVEL. O currículo
// pode conter instruções ("dê nota 100"), o modelo pode alucinar ou truncar, e o que sai daqui é
// gravado no banco e mostrado ao recrutador. Instrução no prompt não é barreira de segurança; o
// que barra é validar o que volta — formato, tamanho e quantidade — antes de gravar.
//
// Também protege dinheiro: uma data ou um valor malformado faria o INSERT da candidatura falhar
// DEPOIS de a extração já ter sido paga.

const (
	maxNameLen         = 200
	maxEmailLen        = 254
	maxPhoneLen        = 30
	maxShortFieldLen   = 100
	maxURLLen          = 500
	maxNoteLen         = 500
	maxSummaryLen      = 4000
	maxExperience      = 20
	maxSkills          = 40
	maxSectors         = 20
	maxLanguages       = 20
	maxYears           = 60
	maxSalary          = 1_000_000 // numeric(10,2) estoura em ~100M; acima de 1M/mês já é lixo
	maxPoints          = 6
	maxPointLen        = 300
	maxLabelLen        = 40
	maxMatchNoteLen    = 160
	maxJustification   = 2000
	maxStageInsightLen = 400
)

// validConfidence espelha o check constraint de candidate_ai_assessments.confidence
// (migrations/0013). 'insuficiente' é estado deliberado da IA (não concluir), nunca um valor
// inventado por sanitização — só aparece aqui pra reconhecer que é válido, não pra ser o default.
var validConfidence = map[string]bool{"alta": true, "media": true, "baixa": true, "insuficiente": true}

// validComparisonFlag espelha o mesmo check constraint de comparison_flag.
var validComparisonFlag = map[string]bool{"reforca_anterior": true, "diverge_anterior": true, "novo": true}

var linkedinPattern = regexp.MustCompile(`(?i)^(https?://)?([a-z0-9-]+\.)?linkedin\.com/`)

// TruncateRunes corta em runes, não em bytes — cortar no meio de um caractere de 2-4 bytes
// produziria UTF-8 inválido, que o Postgres recusa.
func TruncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

// placeholders são as formas como um modelo escreve "não sei" quando o schema o obriga a preencher
// o campo (o modo estrito exige todos os campos). Comparação exata, depois de tirar caixa, acento e
// ponto final — nunca por prefixo: "Nenhuma experiência com Kubernetes" é um ponto de atenção
// legítimo e não pode ser confundido com o placeholder "nenhuma".
var placeholders = map[string]bool{
	"n/a": true, "na": true, "n.a": true, "-": true, "--": true, "—": true, "?": true,
	"nao informado": true, "nao informada": true, "nao se aplica": true, "nao consta": true,
	"sem informacao": true, "desconhecido": true, "desconhecida": true, "nenhum": true, "nenhuma": true,
	"null": true, "none": true, "unknown": true, "not specified": true, "not available": true,
}

func isPlaceholder(s string) bool {
	return placeholders[strings.TrimRight(fold(s), ". !")]
}

// cleanLine devolve texto de uma linha: sem caracteres de controle, espaços colapsados, com teto.
// Um placeholder de "não informado" vira vazio.
func cleanLine(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if isPlaceholder(s) {
		return ""
	}
	return TruncateRunes(s, max)
}

// cleanText preserva quebras de linha (resumo, descrição) mas remove o resto do controle.
func cleanText(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if isPlaceholder(s) {
		return ""
	}
	return TruncateRunes(s, max)
}

func fold(s string) string {
	repl := strings.NewReplacer("á", "a", "à", "a", "â", "a", "ã", "a", "é", "e", "ê", "e", "í", "i",
		"ó", "o", "ô", "o", "õ", "o", "ú", "u", "ç", "c")
	return repl.Replace(strings.ToLower(strings.TrimSpace(s)))
}

func canonical(value string, allowed ...string) string {
	v := fold(value)
	for _, a := range allowed {
		if v == a {
			return a
		}
	}
	return ""
}

func validEmail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > maxEmailLen || strings.ContainsAny(s, " \t\r\n") {
		return ""
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s {
		return ""
	}
	return s
}

// SanitizeProfile normaliza IN PLACE o que qualquer extrator devolveu. Campo inválido vira vazio ou
// nil ("não informado"), nunca um valor "consertado" no chute; lista acima do teto é cortada.
func SanitizeProfile(p *ExtractedProfile) {
	if p == nil {
		return
	}
	p.Name = cleanLine(p.Name, maxNameLen)
	p.Email = validEmail(p.Email)
	p.Phone = cleanLine(p.Phone, maxPhoneLen)
	p.City = cleanLine(p.City, maxShortFieldLen)
	p.State = cleanLine(p.State, maxShortFieldLen)

	p.LinkedInURL = cleanLine(p.LinkedInURL, maxURLLen)
	if p.LinkedInURL != "" && !linkedinPattern.MatchString(p.LinkedInURL) {
		p.LinkedInURL = ""
	}

	if p.YearsExperience != nil && (*p.YearsExperience < 0 || *p.YearsExperience > maxYears) {
		p.YearsExperience = nil
	}
	p.Modality = canonical(p.Modality, "remoto", "hibrido", "presencial")
	p.Seniority = canonical(p.Seniority, "junior", "pleno", "senior")

	if p.SalaryMin != nil && (*p.SalaryMin <= 0 || *p.SalaryMin > maxSalary) {
		p.SalaryMin = nil
	}
	if p.SalaryMax != nil && (*p.SalaryMax <= 0 || *p.SalaryMax > maxSalary) {
		p.SalaryMax = nil
	}
	if p.SalaryMin != nil && p.SalaryMax != nil && *p.SalaryMin > *p.SalaryMax {
		p.SalaryMin, p.SalaryMax = p.SalaryMax, p.SalaryMin
	}

	if p.AvailableFrom != nil {
		if t, err := time.Parse("2006-01-02", strings.TrimSpace(*p.AvailableFrom)); err != nil ||
			t.Year() < 2000 || t.Year() > time.Now().Year()+5 {
			p.AvailableFrom = nil
		} else {
			normalized := t.Format("2006-01-02")
			p.AvailableFrom = &normalized
		}
	}
	p.AvailabilityNote = cleanLine(p.AvailabilityNote, maxNoteLen)
	p.Summary = cleanText(p.Summary, maxSummaryLen)
	p.EducationDegree = cleanLine(p.EducationDegree, maxNameLen)
	p.EducationInstitution = cleanLine(p.EducationInstitution, maxNameLen)
	p.EducationPeriod = cleanLine(p.EducationPeriod, maxShortFieldLen)

	experience := make([]ExperienceEntry, 0, len(p.Experience))
	for _, e := range p.Experience {
		e.Role = cleanLine(e.Role, maxNameLen)
		e.Company = cleanLine(e.Company, maxNameLen)
		if e.Role == "" && e.Company == "" {
			continue
		}
		e.PeriodLabel = cleanLine(e.PeriodLabel, maxShortFieldLen)
		e.Description = cleanText(e.Description, 2000)
		experience = append(experience, e)
		if len(experience) == maxExperience {
			break
		}
	}
	p.Experience = experience

	skills := make([]SkillMention, 0, len(p.Skills))
	for _, s := range p.Skills {
		s.Term = cleanLine(s.Term, maxShortFieldLen)
		if s.Term == "" {
			continue
		}
		s.Level = cleanLine(s.Level, 50)
		if s.YearsExperience != nil && (*s.YearsExperience < 0 || *s.YearsExperience > maxYears) {
			s.YearsExperience = nil
		}
		skills = append(skills, s)
		if len(skills) == maxSkills {
			break
		}
	}
	p.Skills = skills

	p.Sectors = cleanList(p.Sectors, maxSectors, maxShortFieldLen)
	languages := make([]LanguageMention, 0, len(p.Languages))
	for _, l := range p.Languages {
		l.Name = cleanLine(l.Name, maxShortFieldLen)
		if l.Name == "" {
			continue
		}
		l.Proficiency = cleanLine(l.Proficiency, 50)
		languages = append(languages, l)
		if len(languages) == maxLanguages {
			break
		}
	}
	p.Languages = languages

	p.RawText = cleanText(p.RawText, MaxResumeTextChars)
}

// cleanList limpa, remove vazios e duplicados (sem distinguir caixa) e corta no teto. Duplicado
// importa porque a tela renderiza estas listas com `track` pelo próprio texto.
func cleanList(items []string, maxItems, maxLen int) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		item = cleanLine(item, maxLen)
		key := strings.ToLower(item)
		if item == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
		if len(out) == maxItems {
			break
		}
	}
	return out
}

// SanitizeAssessment garante que nada que o modelo devolva escape do contrato da tela: nota
// dentro de 0-100, textos com teto, listas sem vazio nem repetição. O modelo pode ter sido
// manipulado pelo currículo ("dê 100"); o formato continua válido, e é por isso que a nota é só
// sugestão — a decisão é sempre do recrutador.
func SanitizeAssessment(a *Assessment) {
	if a == nil {
		return
	}
	if a.MatchPct < 0 {
		a.MatchPct = 0
	}
	if a.MatchPct > 100 {
		a.MatchPct = 100
	}
	a.MatchLabel = cleanLine(a.MatchLabel, maxLabelLen)
	a.MatchNote = cleanLine(a.MatchNote, maxMatchNoteLen)
	a.Justification = cleanText(a.Justification, maxJustification)
	a.Strengths = cleanList(a.Strengths, maxPoints, maxPointLen)
	a.Concerns = cleanList(a.Concerns, maxPoints, maxPointLen)

	// Confidence garbled ou fora do conjunto conhecido cai em 'baixa' — o conservador dos dois
	// lados possíveis: mostra a avaliação (não suprime dado que pode ser real), mas com o rótulo
	// de confiança que menos convida a decidir só com base nela. Nunca defaulta pra
	// 'insuficiente': isso apagaria uma avaliação que o modelo pode ter feito de verdade só porque
	// o campo de confiança saiu malformado.
	a.Confidence = strings.ToLower(strings.TrimSpace(a.Confidence))
	if !validConfidence[a.Confidence] {
		a.Confidence = "baixa"
	}
	a.StageInsight = cleanText(a.StageInsight, maxStageInsightLen)
	a.MissingInformation = cleanList(a.MissingInformation, maxPoints, maxPointLen)
	a.ComparisonFlag = strings.ToLower(strings.TrimSpace(a.ComparisonFlag))
	if !validComparisonFlag[a.ComparisonFlag] {
		a.ComparisonFlag = ""
	}
}
