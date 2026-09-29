package talent

import (
	"context"
	"strings"

	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
)

// maxReverseMatchResults é o teto de linhas devolvidas por ReverseMatch. A query já ordena por
// score; é só um LIMIT — mas sem ele um banco com milhares de talentos mandaria a lista inteira
// pro front a cada clique em "Ver talentos sugeridos".
const maxReverseMatchResults = 30

// MatchCriteria é o que o recrutador já preencheu quando pede o match reverso — bate 1:1 com o
// payload que o frontend já manda (ver clearhire-app core/data-api.ts,
// getReverseMatchForNewCampaign): título livre (pode ser vaga em rascunho, sem campanha
// persistida ainda) + modalidade/senioridade estruturadas + requisitos livres, todos opcionais
// menos o título.
//
// Requirements existe à parte de Title (não concatenado pelo chamador) porque cada um resolve uma
// coisa diferente: título curto tende a ter o cargo ("Desenvolvedor Backend Go"), requisitos tende
// a ter a skill de verdade escrita por extenso ("SQL", "PostgreSQL", "Python"...) — um título como
// "Engenheiro de dados" não menciona nenhuma skill, e sem ler os requisitos o match reverso não
// teria sinal nenhum além de modalidade/senioridade, tratando qualquer perfil daquela combinação
// como igualmente compatível.
type MatchCriteria struct {
	Title        string
	Modality     string
	Seniority    string
	Requirements string
}

// ScoreBreakdownLine espelha ScoreBreakdownLine do frontend (core/models.ts) — a explicabilidade
// do score É a fórmula, não uma narração de LLM por cima (seção 6.4 do documento de especificação
// do Banco de Talentos): cada critério aparece com o que pesou a favor ou contra.
type ScoreBreakdownLine struct {
	Label  string `json:"label"`
	Detail string `json:"detail"`
	Delta  int    `json:"delta"`
}

// TalentMatch é o resultado de UMA rodada de match — nunca persistido (o score só faz sentido
// contra os critérios desta busca específica; ver invariante na migração 0001 e no documento de
// especificação, seções 6.3/11).
type TalentMatch struct {
	Talent    Talent
	MatchPct  int
	Breakdown []ScoreBreakdownLine
}

// criterion é um requisito de match: quanto o talento atende, de 0 (não atende) a 1 (atende
// plenamente). 0.5 é reservado para "não informado" — nem soma nem penaliza (mesmo desenho do
// motor client-side em talent-matching.ts, mantido igual de propósito: os dois lados precisam
// concordar sobre o que é "bom match").
type criterion struct {
	label, detail string
	achieved      float64
}

const neutral = 0.5

var levelWeight = map[string]float64{
	"especialista":  35,
	"avancado":      30,
	"intermediario": 20,
	"basico":        10,
}

// normalize casa com o normalize() de talent-matching.ts: minúsculo, sem acento. As duas pontas
// (Go e TS) precisam concordar byte a byte sobre o que é "o mesmo termo", senão o mesmo talento
// pontuaria diferente dependendo de onde a busca roda.
func normalize(s string) string {
	s = strings.ToLower(s)
	repl := strings.NewReplacer(
		"á", "a", "à", "a", "â", "a", "ã", "a",
		"é", "e", "ê", "e",
		"í", "i",
		"ó", "o", "ô", "o", "õ", "o",
		"ú", "u",
		"ç", "c",
	)
	return repl.Replace(s)
}

func weightForLevel(level string) float64 {
	if level == "" {
		return 25
	}
	if w, ok := levelWeight[normalize(level)]; ok {
		return w
	}
	return 25
}

// skillAchievement dá piso 0.6 só por possuir a skill — o nível (básico a especialista) nuança
// dentro disso. Sem o piso, alguém júnior com a skill pontuaria como se não tivesse; não possuir
// continua sendo 0, não existe meio-termo de posse.
func skillAchievement(level string) float64 {
	return 0.6 + 0.4*(weightForLevel(level)/35)
}

func clampScore(v float64) int {
	r := int(v + 0.5) // arredonda pra cima em .5, mesmo round() do JS pra valor positivo
	if r < 0 {
		return 0
	}
	if r > 100 {
		return 100
	}
	return r
}

// finalizeScore é a média ponderada dos critérios pedidos, cada um valendo o mesmo tanto — só quem
// atende TODOS os critérios chega a 100%; atender só parte nunca empata com quem atende tudo,
// não importa o nível de cada skill isolada (mesmo formato de talent-matching.ts:finalizeScore).
func finalizeScore(t Talent, criteria []criterion) TalentMatch {
	if len(criteria) == 0 {
		return TalentMatch{Talent: t, MatchPct: 0, Breakdown: []ScoreBreakdownLine{}}
	}
	unit := 100.0 / float64(len(criteria))
	sum := 0.0
	breakdown := make([]ScoreBreakdownLine, len(criteria))
	for i, c := range criteria {
		sum += c.achieved
		breakdown[i] = ScoreBreakdownLine{Label: c.label, Detail: c.detail, Delta: clampDelta((c.achieved - neutral) * unit)}
	}
	return TalentMatch{Talent: t, MatchPct: clampScore(sum / float64(len(criteria)) * 100), Breakdown: breakdown}
}

// clampDelta arredonda o delta do breakdown sem o clamp 0-100 de clampScore (delta é +/-, não pct).
func clampDelta(v float64) int {
	if v >= 0 {
		return int(v + 0.5)
	}
	return -int(-v + 0.5)
}

// containsWholeWord é o fallback usado quando o título não trouxe nenhum critério reconhecido —
// mesma checagem simples (substring) do fallbackTextScore do frontend, não a fronteira de palavra
// completa do skillmatch (ali o custo de um falso-positivo ocasional é menor que a complexidade de
// portar o mesmo tokenizador de busca livre).
func containsWholeWord(haystack, word string) bool {
	return strings.Contains(haystack, word)
}

func fallbackTextScore(t Talent, title string) TalentMatch {
	words := []string{}
	for _, w := range strings.Fields(normalize(title)) {
		if len([]rune(w)) > 2 {
			words = append(words, w)
		}
	}
	parts := []string{t.Name, t.Summary, t.Seniority}
	for _, s := range t.Skills {
		parts = append(parts, s.Term)
	}
	parts = append(parts, t.Sectors...)
	haystack := normalize(strings.Join(parts, " "))

	hits := 0
	for _, w := range words {
		if containsWholeWord(haystack, w) {
			hits++
		}
	}
	pct := 0
	if len(words) > 0 {
		pct = clampScore(float64(hits) / float64(len(words)) * 100)
	}
	return TalentMatch{
		Talent:   t,
		MatchPct: pct,
		Breakdown: []ScoreBreakdownLine{
			{Label: "Correspondência textual", Detail: "termos do título encontrados no perfil", Delta: pct},
		},
	}
}

func seniorityCriterion(t Talent, wanted string) *criterion {
	if wanted == "" {
		return nil
	}
	match := strings.Contains(normalize(t.Seniority), normalize(wanted))
	detail := "compatível"
	if !match {
		detail = "perfil é " + t.Seniority
	}
	achieved := 0.0
	if match {
		achieved = 1
	}
	return &criterion{label: "Senioridade " + wanted, detail: detail, achieved: achieved}
}

// scoreAgainstCriteria monta os critérios de skill/setor/modalidade a partir do que foi resolvido
// no título, mais senioridade quando pedida, e devolve a média ponderada (finalizeScore).
func scoreAgainstCriteria(t Talent, skillTerms []string, sector, modality string, extra *criterion) TalentMatch {
	criteria := make([]criterion, 0, len(skillTerms)+3)
	for _, term := range skillTerms {
		var found *Skill
		for i := range t.Skills {
			if normalize(t.Skills[i].Term) == normalize(term) {
				found = &t.Skills[i]
				break
			}
		}
		label, detail, achieved := term, "não possui", 0.0
		if found != nil {
			if found.Level != "" {
				label = term + " (" + found.Level + ")"
			}
			detail = "possui"
			achieved = skillAchievement(found.Level)
		}
		criteria = append(criteria, criterion{label: label, detail: detail, achieved: achieved})
	}

	if sector != "" {
		has := false
		for _, s := range t.Sectors {
			if strings.Contains(normalize(s), normalize(sector)) {
				has = true
				break
			}
		}
		detail := "não possui"
		achieved := 0.0
		if has {
			detail, achieved = "possui", 1
		}
		criteria = append(criteria, criterion{label: "Setor " + sector, detail: detail, achieved: achieved})
	}

	if modality != "" {
		match := strings.Contains(normalize(t.Modality), normalize(modality))
		detail, achieved := "não compatível ("+t.Modality+")", 0.0
		if match {
			detail, achieved = "compatível", 1
		}
		criteria = append(criteria, criterion{label: "Modalidade " + modality, detail: detail, achieved: achieved})
	}

	if extra != nil {
		criteria = append(criteria, *extra)
	}

	return finalizeScore(t, criteria)
}

// ReverseMatch é o "match reverso" (documento de especificação, seção 8.2): dado o que já foi
// preenchido de uma vaga nova (rascunho ou campanha real), ranqueia o Banco de Talentos contra
// isso. 100% determinístico — nenhuma chamada de IA acontece aqui (ver justificativa em
// documentos/banco-de-talentos-recomendacao-plano.md, seção 2). Só isto já resolve o pedido
// original: "a IA sugere candidatos do banco pra uma vaga nova".
func (s *service) ReverseMatch(ctx context.Context, companyID string, criteria MatchCriteria) ([]TalentMatch, error) {
	pool, err := s.repo.ListInBank(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar talentos")
	}

	// Título + requisitos juntos pra resolver contra a taxonomia — ver comentário de Requirements
	// em MatchCriteria pro porquê de precisar dos dois.
	signalText := criteria.Title
	if criteria.Requirements != "" {
		signalText += "\n" + criteria.Requirements
	}
	skillTerms, err := s.repo.ResolveSkillTermsInText(ctx, signalText)
	if err != nil {
		return nil, apperror.Internal("falha ao resolver skills da vaga")
	}
	sector, err := s.repo.ResolveSectorInText(ctx, signalText)
	if err != nil {
		return nil, apperror.Internal("falha ao resolver setor da vaga")
	}

	hasSignal := len(skillTerms) > 0 || sector != "" || criteria.Modality != ""

	matches := make([]TalentMatch, 0, len(pool))
	for _, t := range pool {
		// Talento com exclusão solicitada nunca aparece em nenhum match — mesma regra já aplicada
		// em Coverage (repository.go) e no motor client-side (talent-matching.ts:eligiblePool).
		if t.ConsentState == "oposicao_exclusao" {
			continue
		}

		var m TalentMatch
		if hasSignal {
			m = scoreAgainstCriteria(t, skillTerms, sector, criteria.Modality, seniorityCriterion(t, criteria.Seniority))
		} else {
			m = fallbackTextScore(t, criteria.Title)
			if sr := seniorityCriterion(t, criteria.Seniority); sr != nil {
				delta := -10
				if sr.achieved > 0 {
					delta = 10
				}
				m.MatchPct = clampScore(float64(m.MatchPct + delta))
				m.Breakdown = append(m.Breakdown, ScoreBreakdownLine{Label: sr.label, Detail: sr.detail, Delta: delta})
			}
		}
		if m.MatchPct > 0 {
			matches = append(matches, m)
		}
	}

	sortMatchesDesc(matches)
	if len(matches) > maxReverseMatchResults {
		matches = matches[:maxReverseMatchResults]
	}
	return matches, nil
}

func sortMatchesDesc(m []TalentMatch) {
	// Poucas dezenas/centenas de talentos por empresa hoje — insertion sort é claro e rápido o
	// bastante; trocar por sort.Slice se o volume um dia justificar.
	for i := 1; i < len(m); i++ {
		for j := i; j > 0 && m[j].MatchPct > m[j-1].MatchPct; j-- {
			m[j], m[j-1] = m[j-1], m[j]
		}
	}
}
