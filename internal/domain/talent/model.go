package talent

import "time"

// Talent é a pessoa no Banco de Talentos — existe independente de vaga e atravessa campanhas (ver
// migrations/0001, tabela talents; e 0012 para o que significa "estar no banco"). NUNCA guarda score
// de match: isso é sempre recalculado contra os critérios de uma campanha específica.
type Talent struct {
	ID, Name, Email, LinkedInURL, City, State string
	Modality, Seniority                       string
	YearsExperience                           *int
	SalaryMin, SalaryMax                      *float64
	AvailableFrom                             *time.Time
	AvailabilityNote                          string
	Origin, LegalBasis, ConsentState          string
	ConsentDate                               *time.Time
	Summary, RecruiterNotes                   string
	EducationDegree, EducationInstitution     string
	EducationPeriod                           string
	ProfileReviewedAt                         time.Time

	Skills     []Skill
	Languages  []Language
	Sectors    []string
	Experience []ExperienceEntry
	History    []HistoryEntry
}

type Skill struct {
	Term            string
	Level           string
	YearsExperience *int
}

type Language struct {
	Name, Proficiency string
}

type ExperienceEntry struct {
	Role, Company, PeriodLabel, Description string
}

// HistoryEntry é uma participação da pessoa numa campanha — o histórico é um join com candidates, não
// uma tabela (a pessoa "atravessa" campanhas; cada candidatura guarda seu próprio desfecho).
type HistoryEntry struct {
	CampaignID, CampaignTitle string
	PhaseKey, Status          string
	RejectionReasonKey        *string
}

// CoverageEntry é quantas pessoas do banco têm cada skill — o "mapa de cobertura".
type CoverageEntry struct {
	SkillTerm string
	Count     int
}

// ManualInput é o cadastro manual de talento (POST /talents).
type ManualInput struct {
	Name, RawProfileText, ContextNote string
}
