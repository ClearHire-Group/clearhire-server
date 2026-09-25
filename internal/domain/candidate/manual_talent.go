package candidate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

// ManualTalentInput é o cadastro manual de talento, fora de campanha (seção 4.2 da especificação): o
// recrutador achou alguém no LinkedIn, num evento ou por indicação, cola o currículo/perfil e deixa uma
// nota de contexto.
type ManualTalentInput struct {
	Name           string
	RawProfileText string
	ContextNote    string
}

const maxContextNoteRunes = 2000

// RegisterManualTalent mora aqui, e não em internal/domain/talent, porque é daqui que sai toda escrita
// de perfil de pessoa (extração, skills, experiência, idiomas, documento de origem) — o domínio talent
// só lê, e chama isto por uma interface (ver talent.Ingestor), sem import cruzado.
//
// Entra direto no banco, com base legal "legítimo interesse" e estado "não notificado" até o primeiro
// contato real (seção 5.2). Perfis de origem manual guardam só dado profissional: o telefone e a
// pretensão salarial que o currículo trouxer são descartados.
func (s *service) RegisterManualTalent(ctx context.Context, companyID string, in ManualTalentInput) (string, error) {
	errs := FieldErrors{}
	name, msg := validateName(in.Name)
	errs.add("name", msg)
	raw := strings.TrimSpace(in.RawProfileText)
	if n := runeLen(raw); n > MaxResumeTextRunes {
		errs.add("rawProfileText", fmt.Sprintf("O perfil pode ter no máximo %d caracteres (você usou %d).", MaxResumeTextRunes, n))
	}
	note := strings.TrimSpace(in.ContextNote)
	if n := runeLen(note); n > maxContextNoteRunes {
		errs.add("contextNote", fmt.Sprintf("A nota pode ter no máximo %d caracteres (você usou %d).", maxContextNoteRunes, n))
	}
	if len(errs) > 0 {
		return "", apperror.Validation(formErrorMessage, errs)
	}

	profile := &llm.ExtractedProfile{}
	if raw != "" {
		extracted, err := s.extractResume(ctx, companyID, "", llm.Input{Text: raw})
		if err != nil {
			return "", mapManualExtractionError(err)
		}
		profile = extracted
	}

	if profile.Email != "" {
		existing, err := s.repo.FindTalentIDByEmail(ctx, companyID, profile.Email)
		if err != nil {
			return "", apperror.Internal("falha ao verificar talento")
		}
		if existing != "" {
			return "", apperror.BadRequestField("rawProfileText",
				fmt.Sprintf("Já existe uma pessoa com o e-mail %s registrada nesta empresa.", profile.Email))
		}
	}

	var talentID string
	err := s.withTx(ctx, func(db database.DB) error {
		txRepo := s.newTxRepo(db)
		id, err := txRepo.CreateManualTalent(ctx, &TalentSeed{
			CompanyID: companyID, Name: name, Email: profile.Email, LinkedInURL: profile.LinkedInURL,
			City: profile.City, State: profile.State, Modality: profile.Modality, Seniority: profile.Seniority,
			YearsExperience: profile.YearsExperience, Summary: profile.Summary, RecruiterNotes: note,
			EducationDegree: profile.EducationDegree, EducationInstitution: profile.EducationInstitution,
			EducationPeriod: profile.EducationPeriod,
		})
		if err != nil {
			if isUniqueViolation(err) {
				return apperror.BadRequestField("rawProfileText", "Esta pessoa acabou de ser registrada nesta empresa.")
			}
			return apperror.Internal("falha ao registrar talento")
		}
		talentID = id

		if len(profile.Experience) > 0 {
			if err := txRepo.ReplaceTalentExperience(ctx, id, toCandidateExperience(profile.Experience)); err != nil {
				return apperror.Internal("falha ao registrar experiência")
			}
		}
		skills, err := resolveProfileSkills(ctx, txRepo, companyID, profile)
		if err != nil {
			return apperror.Internal("falha ao resolver skills")
		}
		if len(skills) > 0 {
			if err := txRepo.InsertTalentSkills(ctx, id, skills); err != nil {
				return apperror.Internal("falha ao registrar skills")
			}
		}
		if len(profile.Sectors) > 0 {
			if err := txRepo.InsertTalentSectors(ctx, id, profile.Sectors); err != nil {
				return apperror.Internal("falha ao registrar setores")
			}
		}
		if len(profile.Languages) > 0 {
			if err := txRepo.InsertTalentLanguages(ctx, id, profile.Languages); err != nil {
				return apperror.Internal("falha ao registrar idiomas")
			}
		}
		if raw != "" {
			if err := txRepo.InsertTalentSourceDocument(ctx, id, raw); err != nil {
				return apperror.Internal("falha ao registrar perfil")
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return talentID, nil
}

// mapManualExtractionError: quem vê é o recrutador da própria empresa, então o motivo pode ser dito
// (diferente da candidatura pública). O perfil não é obrigatório — dá para cadastrar só com nome e nota.
func mapManualExtractionError(err error) error {
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	switch {
	case errors.Is(err, llm.ErrBudgetExceeded):
		return apperror.BadRequestField("rawProfileText", "O teto mensal de uso de IA da empresa foi atingido. Cadastre só com nome e nota, ou tente no próximo mês.")
	case errors.Is(err, llm.ErrMalformedOutput):
		return apperror.BadRequestField("rawProfileText", "Não foi possível ler este perfil. Confira o texto colado.")
	default:
		return apperror.Unavailable("Não foi possível ler o perfil agora. Tente de novo em instantes ou cadastre só com nome e nota.")
	}
}
