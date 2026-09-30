package candidate

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Regras de validação do formulário público de candidatura, campo a campo.
//
// ESPELHADAS em clearhire-app/src/app/core/application-validation.ts: o front valida enquanto a
// pessoa digita (feedback imediato, no campo certo) e o backend valida de novo porque o front nunca é
// barreira — qualquer um chama a API direto. As duas implementações têm que concordar: uma regra só
// num lado vira ou um erro genérico no fim do envio (o problema que isto resolve) ou dado ruim
// aceito. Ao mudar uma regra aqui, mude lá e os testes dos dois lados.
//
// Cada validador devolve o valor NORMALIZADO (é ele que é gravado) e a mensagem de erro, vazia
// quando o valor é válido. As chaves de campo são as mesmas do JSON e do front.

// FieldErrors é "campo -> mensagem", devolvido ao front em Envelope.Fields.
type FieldErrors map[string]string

func (f FieldErrors) add(field, msg string) {
	if msg == "" {
		return
	}
	if _, exists := f[field]; !exists {
		f[field] = msg
	}
}

const (
	maxNameRunes        = 200
	maxEmailLen         = 254
	maxCityRunes        = 100
	maxSummaryRunes     = 4000
	maxEducationRunes   = 200
	maxPeriodRunes      = 100
	maxSkills           = 40
	maxSkillRunes       = 100
	maxYearsExperience  = 60
	minResumeTextRunes  = 100
	maxLinkedInURLRunes = 500
	minPeriodYear       = 1950
	periodYearsAhead    = 8
)

// MaxResumeTextRunes é o teto do currículo colado. Igual a llm.MaxResumeTextChars — não importado
// para o domínio não depender do pacote de IA só por uma constante; há teste garantindo que batem.
const MaxResumeTextRunes = 14000

func collapseSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

func runeLen(s string) int { return utf8.RuneCountInString(s) }

func hasLetterOrDigit(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// personLikePattern: nome de pessoa ou de cidade — letras (com acento), espaço, apóstrofo, hífen e
// ponto, começando por letra. Sem dígitos nem símbolos.
var personLikePattern = regexp.MustCompile(`^\p{L}[\p{L}\p{M}'’.\- ]*$`)

func validateName(raw string) (string, string) {
	name := collapseSpaces(raw)
	switch {
	case name == "":
		return name, "Informe seu nome completo."
	case runeLen(name) > maxNameRunes:
		return name, fmt.Sprintf("O nome pode ter no máximo %d caracteres.", maxNameRunes)
	case !personLikePattern.MatchString(name):
		return name, "Use apenas letras no nome, sem números ou símbolos."
	}
	words := 0
	for _, w := range strings.Split(name, " ") {
		if hasLetter(w) {
			words++
		}
	}
	if words < 2 {
		return name, "Informe nome e sobrenome."
	}
	return name, ""
}

var emailPattern = regexp.MustCompile(
	`^[a-z0-9!#$%&'*+/=?^_` + "`" + `{|}~-]+(\.[a-z0-9!#$%&'*+/=?^_` + "`" + `{|}~-]+)*@([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,24}$`)

// typoTLDs são terminações que ninguém tem de verdade — nenhuma é um domínio de topo existente —
// e que aparecem quando se erra ".com" ao digitar. Bloquear é seguro. (.co, .cm e .om existem e
// ficam de fora.)
var typoTLDs = map[string]bool{
	"con": true, "cmo": true, "ocm": true, "comm": true, "coom": true, "como": true, "cpm": true,
	"vom": true, "xom": true, "comn": true, "coml": true, "cim": true, "clm": true, "cok": true,
	"ccom": true, "conm": true, "copm": true,
}

// singleDomainProviders são provedores que só existem com UM domínio no mundo todo: qualquer outra
// terminação depois do nome deles é erro de digitação ("gmail.com.br", "gmail.como").
var singleDomainProviders = map[string]string{"gmail": "gmail.com", "icloud": "icloud.com"}

// emailTypoSuggestion devolve o endereço corrigido quando o domínio tem um erro de digitação
// CERTO — nunca um palpite. Palpites (distância de edição para domínios conhecidos) ficam só no
// front, como aviso que a pessoa pode ignorar.
func emailTypoSuggestion(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return ""
	}
	local, domain := email[:at], email[at+1:]
	labels := strings.Split(domain, ".")
	if canonical, ok := singleDomainProviders[labels[0]]; ok && domain != canonical {
		return local + "@" + canonical
	}
	if len(labels) >= 2 && typoTLDs[labels[len(labels)-1]] {
		labels[len(labels)-1] = "com"
		return local + "@" + strings.Join(labels, ".")
	}
	return ""
}

func validateEmail(raw string) (string, string) {
	email := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case email == "":
		return email, "Informe seu e-mail."
	case len(email) > maxEmailLen:
		return email, fmt.Sprintf("O e-mail pode ter no máximo %d caracteres.", maxEmailLen)
	case !emailPattern.MatchString(email) || strings.Index(email, "@") > 64:
		return email, "E-mail inválido. Confira o formato, ex.: nome@provedor.com."
	}
	if suggestion := emailTypoSuggestion(email); suggestion != "" {
		return email, fmt.Sprintf("Confira o e-mail: você quis dizer %s?", suggestion)
	}
	return email, ""
}

// validDDDs são os DDDs existentes no Brasil (Anatel).
var validDDDs = map[string]bool{}

func init() {
	for _, d := range strings.Fields(`11 12 13 14 15 16 17 18 19 21 22 24 27 28 31 32 33 34 35 37 38
		41 42 43 44 45 46 47 48 49 51 53 54 55 61 62 63 64 65 66 67 68 69 71 73 74 75 77 79
		81 82 83 84 85 86 87 88 89 91 92 93 94 95 96 97 98 99`) {
		validDDDs[d] = true
	}
}

var phoneAllowedChars = regexp.MustCompile(`^[0-9()+\-. ]*$`)

const phoneFormatHint = "Informe DDD + número, ex.: (61) 99999-9999."

// validatePhone aceita telefone brasileiro (com ou sem +55, com qualquer pontuação) ou
// internacional começando com + e código do país. Devolve o número formatado — é o que se grava.
func validatePhone(raw string) (string, string) {
	phone := collapseSpaces(raw)
	if phone == "" {
		return "", ""
	}
	if !phoneAllowedChars.MatchString(phone) {
		return phone, "Use apenas números no telefone, ex.: (61) 99999-9999."
	}
	plus := strings.HasPrefix(phone, "+")
	if strings.Count(phone, "+") > 1 || (strings.Contains(phone, "+") && !plus) {
		return phone, phoneFormatHint
	}
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	digits := b.String()

	if plus && !strings.HasPrefix(digits, "55") {
		if len(digits) < 8 || len(digits) > 15 {
			return phone, "Telefone internacional inválido: use + código do país e o número."
		}
		return "+" + digits, ""
	}
	if !plus {
		digits = strings.TrimLeft(digits, "0") // prefixo de longa distância ("061 ...")
	}
	if (len(digits) == 12 || len(digits) == 13) && strings.HasPrefix(digits, "55") {
		digits = digits[2:]
	}
	if len(digits) != 10 && len(digits) != 11 {
		return phone, phoneFormatHint
	}
	ddd, number := digits[:2], digits[2:]
	if !validDDDs[ddd] {
		return phone, fmt.Sprintf("DDD %s não existe. %s", ddd, phoneFormatHint)
	}
	if strings.Count(number, number[:1]) == len(number) {
		return phone, "Número de telefone inválido."
	}
	if len(number) == 9 {
		if number[0] != '9' {
			return phone, "Celular com 9 dígitos deve começar com 9, ex.: (61) 99999-9999."
		}
		return fmt.Sprintf("(%s) %s-%s", ddd, number[:5], number[5:]), ""
	}
	switch number[0] {
	case '2', '3', '4', '5':
		return fmt.Sprintf("(%s) %s-%s", ddd, number[:4], number[4:]), ""
	case '6', '7', '8', '9':
		return phone, fmt.Sprintf("Celular precisa do 9 na frente: (%s) 9%s-%s.", ddd, number[:4], number[4:])
	default:
		return phone, "Número de telefone inválido."
	}
}

var (
	linkedInPattern = regexp.MustCompile(`(?i)^(?:https?://)?(?:(?:www|[a-z]{2})\.)?linkedin\.com/in/([^/\s]+)$`)
	linkedInSlug    = regexp.MustCompile(`^[\p{L}\p{N}_%-]{3,100}$`)
)

// validateLinkedIn aceita o link do perfil com ou sem https/www/subdomínio de país e devolve a
// forma canônica https://www.linkedin.com/in/<perfil>.
func validateLinkedIn(raw string) (string, string) {
	link := strings.TrimSpace(raw)
	if link == "" {
		return "", ""
	}
	if runeLen(link) > maxLinkedInURLRunes {
		return link, "Link muito longo. Use o link do seu perfil, ex.: linkedin.com/in/seu-nome."
	}
	cleaned := link
	if i := strings.IndexAny(cleaned, "?#"); i >= 0 {
		cleaned = cleaned[:i]
	}
	cleaned = strings.TrimRight(cleaned, "/")
	m := linkedInPattern.FindStringSubmatch(cleaned)
	if m == nil || !linkedInSlug.MatchString(m[1]) {
		return link, "Use o link do seu perfil, ex.: linkedin.com/in/seu-nome."
	}
	return "https://www.linkedin.com/in/" + m[1], ""
}

func validateCity(raw string) (string, string) {
	city := collapseSpaces(raw)
	switch {
	case city == "":
		return "", ""
	case runeLen(city) > maxCityRunes:
		return city, fmt.Sprintf("A cidade pode ter no máximo %d caracteres.", maxCityRunes)
	case runeLen(city) < 2 || !personLikePattern.MatchString(city):
		return city, "Cidade inválida: use apenas letras."
	}
	return city, ""
}

// BrazilianStates são as UFs aceitas no campo estado (o front mostra uma lista fechada).
var BrazilianStates = map[string]bool{}

func init() {
	for _, uf := range strings.Fields("AC AL AP AM BA CE DF ES GO MA MT MS MG PA PB PR PE PI RJ RN RS RO RR SC SP SE TO") {
		BrazilianStates[uf] = true
	}
}

func validateState(raw string) (string, string) {
	state := strings.ToUpper(strings.TrimSpace(raw))
	if state == "" {
		return "", ""
	}
	if !BrazilianStates[state] {
		return state, "Selecione um estado da lista."
	}
	return state, ""
}

func validateYearsExperience(years *int) string {
	if years != nil && (*years < 0 || *years > maxYearsExperience) {
		return fmt.Sprintf("Informe um número inteiro de 0 a %d.", maxYearsExperience)
	}
	return ""
}

func validateSummary(raw string) (string, string) {
	summary := strings.TrimSpace(raw)
	if runeLen(summary) > maxSummaryRunes {
		return summary, fmt.Sprintf("O resumo pode ter no máximo %d caracteres (você usou %d).", maxSummaryRunes, runeLen(summary))
	}
	return summary, ""
}

var periodYearPattern = regexp.MustCompile(`\b(?:19|20)\d{2}\b`)

func validateEducationPeriod(raw string) (string, string) {
	period := collapseSpaces(raw)
	if period == "" {
		return "", ""
	}
	if runeLen(period) > maxPeriodRunes {
		return period, fmt.Sprintf("O período pode ter no máximo %d caracteres.", maxPeriodRunes)
	}
	matches := periodYearPattern.FindAllString(period, -1)
	if len(matches) == 0 {
		return period, "Informe o período com anos, ex.: 2015 – 2019 ou 2021 – atual."
	}
	maxYear := time.Now().Year() + periodYearsAhead
	years := make([]int, len(matches))
	for i, m := range matches {
		_, _ = fmt.Sscanf(m, "%d", &years[i]) // m casou com periodYearPattern: não tem como falhar
		if years[i] < minPeriodYear || years[i] > maxYear {
			return period, fmt.Sprintf("Ano fora do intervalo aceito (%d a %d).", minPeriodYear, maxYear)
		}
	}
	if len(years) >= 2 && years[0] > years[1] {
		return period, "O ano de início não pode ser depois do ano de conclusão."
	}
	return period, ""
}

func validateEducationText(raw, label string) (string, string) {
	value := collapseSpaces(raw)
	switch {
	case value == "":
		return "", ""
	case runeLen(value) > maxEducationRunes:
		return value, fmt.Sprintf("%s pode ter no máximo %d caracteres.", label, maxEducationRunes)
	case !hasLetter(value):
		return value, fmt.Sprintf("%s inválida.", label)
	}
	return value, ""
}

// validateSkills limpa a lista (espaços, vazios, itens sem letra nem número, repetidos sem
// distinguir caixa) e só então aplica os tetos — "Python, python" conta como uma skill.
func validateSkills(raw []string) ([]string, string) {
	skills := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, s := range raw {
		s = collapseSpaces(s)
		if !hasLetterOrDigit(s) {
			continue
		}
		key := strings.ToLower(s)
		if seen[key] {
			continue
		}
		seen[key] = true
		skills = append(skills, s)
	}
	for _, s := range skills {
		if runeLen(s) > maxSkillRunes {
			return skills, fmt.Sprintf("Cada skill pode ter no máximo %d caracteres — separe as skills por vírgula.", maxSkillRunes)
		}
	}
	if len(skills) > maxSkills {
		return skills, fmt.Sprintf("Informe no máximo %d skills (você informou %d).", maxSkills, len(skills))
	}
	return skills, ""
}

func validateResumeText(raw string) (string, string) {
	text := strings.TrimSpace(raw)
	switch n := runeLen(text); {
	case n == 0:
		return text, "Cole o texto do seu currículo."
	case n < minResumeTextRunes:
		return text, fmt.Sprintf("O texto parece curto demais para um currículo (mínimo de %d caracteres).", minResumeTextRunes)
	case n > MaxResumeTextRunes:
		return text, fmt.Sprintf("O currículo pode ter no máximo %d caracteres (você usou %d). Resuma o texto ou use o formulário manual.", MaxResumeTextRunes, n)
	}
	return text, ""
}

const consentRequiredMessage = "É preciso autorizar o uso dos seus dados para se candidatar."

// normalizeAndValidate valida o pedido e o reescreve com os valores normalizados. Só olha os
// campos do modo escolhido: o que o modo não usa é ignorado (e descartado por toInput).
func (r *SubmitApplicationRequest) normalizeAndValidate() FieldErrors {
	errs := FieldErrors{}
	var msg string

	r.Name, msg = validateName(r.Name)
	errs.add("name", msg)
	r.Email, msg = validateEmail(r.Email)
	errs.add("email", msg)
	if !r.Consent {
		errs.add("consent", consentRequiredMessage)
	}

	switch r.Mode {
	case "manual":
		r.Phone, msg = validatePhone(r.Phone)
		errs.add("phone", msg)
		r.LinkedInURL, msg = validateLinkedIn(r.LinkedInURL)
		errs.add("linkedinUrl", msg)
		r.City, msg = validateCity(r.City)
		errs.add("city", msg)
		r.State, msg = validateState(r.State)
		errs.add("state", msg)
		errs.add("yearsExperience", validateYearsExperience(r.YearsExperience))
		r.Summary, msg = validateSummary(r.Summary)
		errs.add("summary", msg)
		r.EducationDegree, msg = validateEducationText(r.EducationDegree, "Formação")
		errs.add("educationDegree", msg)
		r.EducationInstitution, msg = validateEducationText(r.EducationInstitution, "Instituição")
		errs.add("educationInstitution", msg)
		r.EducationPeriod, msg = validateEducationPeriod(r.EducationPeriod)
		errs.add("educationPeriod", msg)
		if r.EducationDegree == "" && (r.EducationInstitution != "" || r.EducationPeriod != "") {
			errs.add("educationDegree", "Informe o curso ou formação.")
		}
		r.Skills, msg = validateSkills(r.Skills)
		errs.add("skills", msg)
	case "resume_text":
		r.ResumeText, msg = validateResumeText(r.ResumeText)
		errs.add("resumeText", msg)
	}
	return errs
}

// normalizeAndValidate do upload de PDF: só os campos de texto (o arquivo é validado no handler).
func (f *ResumeFileForm) normalizeAndValidate() FieldErrors {
	errs := FieldErrors{}
	var msg string
	f.Name, msg = validateName(f.Name)
	errs.add("name", msg)
	f.Email, msg = validateEmail(f.Email)
	errs.add("email", msg)
	if !f.Consent {
		errs.add("consent", consentRequiredMessage)
	}
	return errs
}
