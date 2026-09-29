package candidate

import "github.com/ClearHire-Group/clearhire-server/pkg/skillmatch"

// mentionsTerm é um alias fino para skillmatch.MentionsTerm — a implementação de verdade mora lá
// porque talent/reversematch.go (filtro determinístico de recomendação) precisa da MESMA regra de
// fronteira de palavra, e os dois domínios não devem importar um do outro. Mantido aqui, sem
// exportar, só para não reescrever os call sites deste pacote nem hardening_test.go.
func mentionsTerm(text, lowerText, term string) bool {
	return skillmatch.MentionsTerm(text, lowerText, term)
}
