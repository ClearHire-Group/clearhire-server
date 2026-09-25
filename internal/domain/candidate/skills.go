package candidate

import (
	"context"

	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

// resolveProfileSkills junta as duas formas de achar skill num perfil, e usa TODAS as que o perfil
// tem, em vez de escolher uma pela origem:
//   - profile.Skills (termos soltos: digitados no modo manual, ou extraídos por um modelo de IA) são
//     casados contra a taxonomia; o que não bate vai para a fila de revisão, que é como a taxonomia
//     cresce com o uso;
//   - profile.RawText (texto do currículo) é varrido pelo dicionário da taxonomia, o único caminho
//     do extrator determinístico (que não devolve lista nenhuma).
//
// Antes o modo currículo só usava o dicionário: com um modelo de verdade, as skills que ele extraiu
// eram jogadas fora e o que não estava na taxonomia nunca chegava à fila de revisão.
func resolveProfileSkills(ctx context.Context, repo Repository, companyID string, profile *llm.ExtractedProfile) ([]ResolvedSkill, error) {
	var resolved []ResolvedSkill

	if len(profile.Skills) > 0 {
		mapped, unmapped, err := repo.ResolveSkills(ctx, profile.Skills)
		if err != nil {
			return nil, err
		}
		resolved = mapped
		for _, u := range unmapped {
			// Melhor esforço — um termo que a fila de revisão não conseguiu gravar não pode
			// derrubar a candidatura inteira.
			_ = repo.QueueSkillReview(ctx, companyID, u.Term)
		}
	}

	if profile.RawText != "" {
		found, err := repo.FindSkillMentionsInText(ctx, profile.RawText)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, found...)
	}
	return dedupeSkills(resolved), nil
}

// dedupeSkills mantém a primeira ocorrência de cada skill. A ordem importa: as que vieram da lista
// do perfil entram antes e carregam nível e anos de experiência, que a varredura do texto não tem.
// Duplicata não é só feiura — candidate_skills tem unique (candidate_id, skill_id) e uma repetida
// derrubaria a candidatura inteira.
func dedupeSkills(in []ResolvedSkill) []ResolvedSkill {
	seen := make(map[string]bool, len(in))
	out := make([]ResolvedSkill, 0, len(in))
	for _, s := range in {
		if seen[s.SkillID] {
			continue
		}
		seen[s.SkillID] = true
		out = append(out, s)
	}
	return out
}
