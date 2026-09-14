package anthropicadapter

import "github.com/ClearHire-Group/clearhire-server/pkg/llm"

// extractionSchemaProperties é o JSON schema (formato bruto, não gerado por reflection) da tool
// extract_candidate_profile — mantido escrito à mão porque o helper de reflection do SDK
// (jsonschema.Reflect + transformSchema) não é exportado do pacote anthropic, e o schema aqui é
// pequeno e estável o bastante pra não valer a pena trazer uma dependência de reflection só pra
// isso.
var extractionSchemaProperties = map[string]any{
	"name":                 map[string]any{"type": "string"},
	"email":                map[string]any{"type": "string"},
	"phone":                map[string]any{"type": "string"},
	"city":                 map[string]any{"type": "string"},
	"state":                map[string]any{"type": "string"},
	"linkedinUrl":          map[string]any{"type": "string"},
	"yearsExperience":      map[string]any{"type": "integer", "description": "anos de experiência profissional total, 0 se não identificável"},
	"modality":             map[string]any{"type": "string", "description": "remoto, hibrido ou presencial, se a pessoa mencionar preferência"},
	"seniority":            map[string]any{"type": "string", "description": "junior, pleno ou senior, se inferível pela experiência descrita"},
	"salaryMin":            map[string]any{"type": "number", "description": "pretensão salarial mínima, 0 se não mencionada"},
	"salaryMax":            map[string]any{"type": "number", "description": "pretensão salarial máxima, 0 se não mencionada"},
	"availableFrom":        map[string]any{"type": "string", "description": "data ISO (AAAA-MM-DD) de disponibilidade, string vazia se não mencionada"},
	"availabilityNote":     map[string]any{"type": "string"},
	"summary":              map[string]any{"type": "string", "description": "resumo de 2-3 frases do perfil, na sua própria síntese"},
	"educationDegree":      map[string]any{"type": "string"},
	"educationInstitution": map[string]any{"type": "string"},
	"educationPeriod":      map[string]any{"type": "string"},
	"experience": map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"role":        map[string]any{"type": "string"},
				"company":     map[string]any{"type": "string"},
				"periodLabel": map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
			},
		},
	},
	"skills": map[string]any{
		"type":        "array",
		"description": "competências técnicas específicas (linguagens, ferramentas, módulos, frameworks) — nunca soft skills genéricas",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"term":            map[string]any{"type": "string"},
				"level":           map[string]any{"type": "string"},
				"yearsExperience": map[string]any{"type": "integer"},
			},
		},
	},
	"sectors": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	"languages": map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":        map[string]any{"type": "string"},
				"proficiency": map[string]any{"type": "string"},
			},
		},
	},
	"rawText": map[string]any{"type": "string", "description": "o texto completo do currículo, exatamente como lido, sem edição — vira o registro de auditoria"},
}

// extractionOutput espelha extractionSchemaProperties campo a campo — é nele que o
// json.Unmarshal(block.Input, ...) escreve.
type extractionOutput struct {
	Name                 string                     `json:"name"`
	Email                string                     `json:"email"`
	Phone                string                     `json:"phone"`
	City                 string                     `json:"city"`
	State                string                     `json:"state"`
	LinkedInURL          string                     `json:"linkedinUrl"`
	YearsExperience      int                        `json:"yearsExperience"`
	Modality             string                     `json:"modality"`
	Seniority            string                     `json:"seniority"`
	SalaryMin            float64                    `json:"salaryMin"`
	SalaryMax            float64                    `json:"salaryMax"`
	AvailableFrom        string                     `json:"availableFrom"`
	AvailabilityNote     string                     `json:"availabilityNote"`
	Summary              string                     `json:"summary"`
	EducationDegree      string                     `json:"educationDegree"`
	EducationInstitution string                     `json:"educationInstitution"`
	EducationPeriod      string                     `json:"educationPeriod"`
	Experience           []extractionExperienceItem `json:"experience"`
	Skills               []extractionSkillItem      `json:"skills"`
	Sectors              []string                   `json:"sectors"`
	Languages            []extractionLanguageItem   `json:"languages"`
	RawText              string                     `json:"rawText"`
}

type extractionExperienceItem struct {
	Role        string `json:"role"`
	Company     string `json:"company"`
	PeriodLabel string `json:"periodLabel"`
	Description string `json:"description"`
}

type extractionSkillItem struct {
	Term            string `json:"term"`
	Level           string `json:"level"`
	YearsExperience int    `json:"yearsExperience"`
}

type extractionLanguageItem struct {
	Name        string `json:"name"`
	Proficiency string `json:"proficiency"`
}

// toProfile converte a saída bruta da IA pro tipo de domínio — 0/"" viram nil/omitido nos campos
// que fazem sentido como "não informado" (anos, salário, data), nunca um valor inventado.
func (o *extractionOutput) toProfile() *llm.ExtractedProfile {
	p := &llm.ExtractedProfile{
		Name: o.Name, Email: o.Email, Phone: o.Phone, City: o.City, State: o.State,
		LinkedInURL: o.LinkedInURL, Modality: o.Modality, Seniority: o.Seniority,
		AvailabilityNote: o.AvailabilityNote, Summary: o.Summary,
		EducationDegree: o.EducationDegree, EducationInstitution: o.EducationInstitution, EducationPeriod: o.EducationPeriod,
		RawText: o.RawText,
	}
	if o.YearsExperience > 0 {
		p.YearsExperience = &o.YearsExperience
	}
	if o.SalaryMin > 0 {
		p.SalaryMin = &o.SalaryMin
	}
	if o.SalaryMax > 0 {
		p.SalaryMax = &o.SalaryMax
	}
	if o.AvailableFrom != "" {
		p.AvailableFrom = &o.AvailableFrom
	}
	for _, e := range o.Experience {
		p.Experience = append(p.Experience, llm.ExperienceEntry{
			Role: e.Role, Company: e.Company, PeriodLabel: e.PeriodLabel, Description: e.Description,
		})
	}
	for _, s := range o.Skills {
		mention := llm.SkillMention{Term: s.Term, Level: s.Level}
		if s.YearsExperience > 0 {
			years := s.YearsExperience
			mention.YearsExperience = &years
		}
		p.Skills = append(p.Skills, mention)
	}
	p.Sectors = o.Sectors
	for _, l := range o.Languages {
		p.Languages = append(p.Languages, llm.LanguageMention{Name: l.Name, Proficiency: l.Proficiency})
	}
	return p
}
