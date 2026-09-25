package candidate

import "github.com/ClearHire-Group/clearhire-server/pkg/apperror"

// ExperienceEntryInput é uma experiência profissional informada no modo manual — mesma forma de
// ExperienceEntry, mas como payload de entrada (sem Position, que aqui é sempre a ordem do array).
type ExperienceEntryInput struct {
	Role        string `json:"role" validate:"required,max=200"`
	Company     string `json:"company" validate:"required,max=200"`
	PeriodLabel string `json:"periodLabel" validate:"omitempty,max=100"`
	Description string `json:"description" validate:"omitempty,max=2000"`
}

func toExperienceEntries(inputs []ExperienceEntryInput) []ExperienceEntry {
	out := make([]ExperienceEntry, len(inputs))
	for i, e := range inputs {
		out[i] = ExperienceEntry{Role: e.Role, Company: e.Company, PeriodLabel: e.PeriodLabel, Description: e.Description}
	}
	return out
}

// SubmitApplicationRequest é o payload de POST /public/campaigns/:id/applications — os dois modos
// que não envolvem upload de arquivo (o modo PDF é multipart, parseado à mão no handler, ver
// ParsePDFApplicationForm). Mode decide quais campos importam; validação condicional (igual
// DecideRequest.RejectionReasonKey só ser exigido quando Decision == "reprovar") acontece em
// toInput/validate, não dá pra expressar "obrigatório se Mode==X" só com tags.
type SubmitApplicationRequest struct {
	Mode string `json:"mode" validate:"required,oneof=manual resume_text"`
	// Name e Email são sempre exigidos, nos dois modos — nome não é algo que dá pra extrair de
	// forma confiável só com reconhecimento de padrão (ver pkg/llm/deterministic), e
	// consentimento/contato não pode depender só de o texto ter sido lido certo.
	//
	// Os campos de pessoa não têm tag `validate`: as regras deles (formato, tamanho, normalização)
	// ficam em normalizeAndValidate (application_validation.go), que devolve o erro POR CAMPO para o
	// formulário — uma tag falhando só produziria o "dados inválidos" genérico, sem dizer qual.
	Name  string `json:"name"`
	Email string `json:"email"`

	// Modo manual — o caminho mais confiável, porque não depende do reconhecimento de padrão ter
	// identificado tudo certo.
	Phone                string                 `json:"phone"`
	LinkedInURL          string                 `json:"linkedinUrl"`
	City                 string                 `json:"city"`
	State                string                 `json:"state"`
	YearsExperience      *int                   `json:"yearsExperience"`
	Summary              string                 `json:"summary"`
	EducationDegree      string                 `json:"educationDegree"`
	EducationInstitution string                 `json:"educationInstitution"`
	EducationPeriod      string                 `json:"educationPeriod"`
	Experience           []ExperienceEntryInput `json:"experience" validate:"omitempty,max=20,dive"`
	Skills               []string               `json:"skills"`

	// Modo currículo colado — a IA lê e extrai.
	ResumeText string `json:"resumeText"`

	// Comuns aos dois modos. Consent tem que vir true (conferido em normalizeAndValidate e de novo
	// no service).
	Consent bool `json:"consent"`
	// Honeypot: campo que um formulário de verdade nunca preenche — só um bot preenchendo tudo
	// automaticamente cai aqui. `max=0` faz qualquer valor não vazio já falhar a validação.
	Honeypot string `json:"website" validate:"omitempty,max=0"`
}

// toInput valida a parte condicional por modo (o que as tags struct não expressam) e monta o
// PublicApplicationInput que o service consome.
func (r SubmitApplicationRequest) toInput() (PublicApplicationInput, error) {
	base := PublicApplicationInput{Consent: r.Consent, Honeypot: r.Honeypot, Name: r.Name, Email: r.Email}

	switch r.Mode {
	case "manual":
		base.Manual = &ManualApplicationFields{
			Phone: r.Phone, LinkedInURL: r.LinkedInURL, City: r.City, State: r.State,
			YearsExperience: r.YearsExperience, Summary: r.Summary,
			EducationDegree: r.EducationDegree, EducationInstitution: r.EducationInstitution, EducationPeriod: r.EducationPeriod,
			Experience: toExperienceEntries(r.Experience), Skills: r.Skills,
		}
	case "resume_text":
		if r.ResumeText == "" {
			return base, apperror.BadRequestField("resumeText", "Cole o texto do seu currículo.")
		}
		base.ResumeText = r.ResumeText
	}
	return base, nil
}
