package candidate

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

// Os casos abaixo são os mesmos de clearhire-app/src/app/core/application-validation.spec.ts. Ao
// mudar uma regra, mude os dois — o front e o backend têm que concordar sobre o que é válido.

type fieldCase struct {
	in, want string // want = valor normalizado quando válido
	wantErr  string // trecho da mensagem esperada; "" = válido
}

func runFieldCases(t *testing.T, name string, fn func(string) (string, string), cases []fieldCase) {
	t.Helper()
	for _, tc := range cases {
		got, msg := fn(tc.in)
		if tc.wantErr == "" {
			if msg != "" {
				t.Errorf("%s(%q): erro inesperado %q", name, tc.in, msg)
			} else if got != tc.want {
				t.Errorf("%s(%q) = %q, esperava %q", name, tc.in, got, tc.want)
			}
			continue
		}
		if !strings.Contains(msg, tc.wantErr) {
			t.Errorf("%s(%q): erro = %q, esperava conter %q", name, tc.in, msg, tc.wantErr)
		}
	}
}

func TestValidateName(t *testing.T) {
	runFieldCases(t, "validateName", validateName, []fieldCase{
		{in: "Pedro Henrique Santana", want: "Pedro Henrique Santana"},
		{in: "  maria   da  silva ", want: "maria da silva"},
		{in: "João D'Ávila-Souza", want: "João D'Ávila-Souza"},
		{in: "Ana M. Costa", want: "Ana M. Costa"},
		{in: "", wantErr: "Informe seu nome"},
		{in: "   ", wantErr: "Informe seu nome"},
		{in: "Pedro", wantErr: "nome e sobrenome"},
		{in: "Pedro -", wantErr: "nome e sobrenome"},
		{in: "Pedro 123", wantErr: "apenas letras"},
		{in: "Pedro @Silva", wantErr: "apenas letras"},
		{in: "-Pedro Silva", wantErr: "apenas letras"},
		{in: strings.Repeat("a", 150) + " " + strings.Repeat("b", 60), wantErr: "no máximo 200"},
	})
}

func TestValidateEmail(t *testing.T) {
	runFieldCases(t, "validateEmail", validateEmail, []fieldCase{
		{in: "Pedro.H@Gmail.com ", want: "pedro.h@gmail.com"},
		{in: "nome+vaga@empresa.com.br", want: "nome+vaga@empresa.com.br"},
		{in: "a@b.io", want: "a@b.io"},
		{in: "fulano@hotmail.fr", want: "fulano@hotmail.fr"},
		{in: "fulano@yahoo.co.uk", want: "fulano@yahoo.co.uk"},
		{in: "", wantErr: "Informe seu e-mail"},
		{in: "pedro", wantErr: "E-mail inválido"},
		{in: "pedro@", wantErr: "E-mail inválido"},
		{in: "pedro@gmail", wantErr: "E-mail inválido"},
		{in: "pedro@@gmail.com", wantErr: "E-mail inválido"},
		{in: "pedro..h@gmail.com", wantErr: "E-mail inválido"},
		{in: ".pedro@gmail.com", wantErr: "E-mail inválido"},
		{in: "pedro silva@gmail.com", wantErr: "E-mail inválido"},
		{in: "pedro@gmail.c", wantErr: "E-mail inválido"},
		{in: "pedró@gmail.com", wantErr: "E-mail inválido"},
		// O caso real que motivou isto: "gmail.como".
		{in: "pedro@gmail.como", wantErr: "pedro@gmail.com?"},
		{in: "pedro@gmail.com.br", wantErr: "pedro@gmail.com?"},
		{in: "pedro@icloud.co", wantErr: "pedro@icloud.com?"},
		{in: "pedro@hotmail.con", wantErr: "pedro@hotmail.com?"},
		{in: "pedro@empresa.cmo", wantErr: "pedro@empresa.com?"},
		{in: strings.Repeat("a", 65) + "@gmail.com", wantErr: "E-mail inválido"},
	})
}

func TestValidatePhone(t *testing.T) {
	runFieldCases(t, "validatePhone", validatePhone, []fieldCase{
		{in: "", want: ""},
		{in: "61999023060", want: "(61) 99902-3060"},
		{in: "(61) 99902-3060", want: "(61) 99902-3060"},
		{in: "61 9 9902 3060", want: "(61) 99902-3060"},
		{in: "+55 61 99902-3060", want: "(61) 99902-3060"},
		{in: "5561999023060", want: "(61) 99902-3060"},
		{in: "061 99902-3060", want: "(61) 99902-3060"},
		{in: "(11) 3333-4444", want: "(11) 3333-4444"},
		{in: "+1 415 555 2671", want: "+14155552671"},
		{in: "+351 912 345 678", want: "+351912345678"},
		{in: "abc", wantErr: "apenas números"},
		{in: "61 99902-3060 ramal 2", wantErr: "apenas números"},
		{in: "12345", wantErr: "DDD + número"},
		{in: "619990230601", wantErr: "DDD + número"},
		{in: "(20) 99902-3060", wantErr: "DDD 20 não existe"},
		{in: "(61) 89902-3060", wantErr: "deve começar com 9"},
		{in: "(61) 9902-3060", wantErr: "precisa do 9"},
		{in: "(61) 0902-3060", wantErr: "inválido"},
		{in: "(61) 99999-9999", wantErr: "inválido"},
		{in: "61+999023060", wantErr: "DDD + número"},
		{in: "+1 234", wantErr: "internacional"},
	})
}

func TestValidateLinkedIn(t *testing.T) {
	canonical := "https://www.linkedin.com/in/pedro-oliveira"
	runFieldCases(t, "validateLinkedIn", validateLinkedIn, []fieldCase{
		{in: "", want: ""},
		{in: "linkedin.com/in/pedro-oliveira", want: canonical},
		{in: "https://www.linkedin.com/in/pedro-oliveira/", want: canonical},
		{in: "http://br.linkedin.com/in/pedro-oliveira?originalSubdomain=br", want: canonical},
		{in: "www.LinkedIn.com/in/pedro-oliveira#top", want: canonical},
		{in: "linkedin.com/in/joão-silva-12ab", want: "https://www.linkedin.com/in/joão-silva-12ab"},
		// O caso real: faltou o /in/.
		{in: "linkedin.com/PedroDiOliveira", wantErr: "linkedin.com/in/seu-nome"},
		{in: "aaa", wantErr: "linkedin.com/in/seu-nome"},
		{in: "https://evil.example/in/pedro", wantErr: "linkedin.com/in/seu-nome"},
		{in: "https://linkedin.com.evil.example/in/pedro", wantErr: "linkedin.com/in/seu-nome"},
		{in: "linkedin.com/in/ab", wantErr: "linkedin.com/in/seu-nome"},
		{in: "linkedin.com/in/pedro/detalhes", wantErr: "linkedin.com/in/seu-nome"},
		{in: "linkedin.com/company/acme", wantErr: "linkedin.com/in/seu-nome"},
	})
}

func TestValidateCityAndState(t *testing.T) {
	runFieldCases(t, "validateCity", validateCity, []fieldCase{
		{in: "", want: ""},
		{in: "  Brasília ", want: "Brasília"},
		{in: "São José dos Campos", want: "São José dos Campos"},
		{in: "Embu-Guaçu", want: "Embu-Guaçu"},
		{in: "Santa Bárbara d'Oeste", want: "Santa Bárbara d'Oeste"},
		{in: "X", wantErr: "Cidade inválida"},
		{in: "Brasília 2", wantErr: "Cidade inválida"},
		{in: "@@@", wantErr: "Cidade inválida"},
	})
	runFieldCases(t, "validateState", validateState, []fieldCase{
		{in: "", want: ""},
		{in: "df", want: "DF"},
		{in: " SP ", want: "SP"},
		{in: "haha", wantErr: "Selecione um estado"},
		{in: "Distrito Federal", wantErr: "Selecione um estado"},
		{in: "XX", wantErr: "Selecione um estado"},
	})
}

func TestValidateYearsExperience(t *testing.T) {
	ptr := func(v int) *int { return &v }
	for _, ok := range []*int{nil, ptr(0), ptr(4), ptr(60)} {
		if msg := validateYearsExperience(ok); msg != "" {
			t.Errorf("anos %v: erro inesperado %q", ok, msg)
		}
	}
	for _, bad := range []*int{ptr(-1), ptr(61), ptr(400)} {
		if msg := validateYearsExperience(bad); !strings.Contains(msg, "0 a 60") {
			t.Errorf("anos %d: erro = %q", *bad, msg)
		}
	}
}

func TestValidateEducationPeriod(t *testing.T) {
	future := time.Now().Year() + periodYearsAhead
	runFieldCases(t, "validateEducationPeriod", validateEducationPeriod, []fieldCase{
		{in: "", want: ""},
		{in: "2015 - 2019", want: "2015 - 2019"},
		{in: "2021 – atual", want: "2021 – atual"},
		{in: "03/2015 a 12/2019", want: "03/2015 a 12/2019"},
		{in: fmt.Sprintf("2024 - %d", future), want: fmt.Sprintf("2024 - %d", future)},
		{in: "cursando", wantErr: "com anos"},
		{in: "quatro anos", wantErr: "com anos"},
		{in: "2019 - 2015", wantErr: "início não pode ser depois"},
		{in: "1900 - 1904", wantErr: "fora do intervalo"},
		{in: fmt.Sprintf("2020 - %d", future+1), wantErr: "fora do intervalo"},
	})
}

func TestValidateSkills(t *testing.T) {
	got, msg := validateSkills([]string{" Python ", "SQL", "python", "", "--", "Power  BI"})
	if msg != "" || strings.Join(got, "|") != "Python|SQL|Power BI" {
		t.Errorf("skills = %v, erro = %q", got, msg)
	}

	many := make([]string, 41)
	for i := range many {
		many[i] = fmt.Sprintf("skill %d", i)
	}
	if _, msg := validateSkills(many); !strings.Contains(msg, "no máximo 40") || !strings.Contains(msg, "41") {
		t.Errorf("41 skills: erro = %q", msg)
	}
	// 45 itens com repetição que viram 40 distintos: a pessoa não é punida por repetir.
	withDupes := append(append([]string{}, many[:40]...), "SKILL 1", "skill 2", "Skill 3", "skill 4", "skill 5")
	if got, msg := validateSkills(withDupes); msg != "" || len(got) != 40 {
		t.Errorf("repetidas contaram no teto: %d skills, erro %q", len(got), msg)
	}
	if _, msg := validateSkills([]string{strings.Repeat("x", 101)}); !strings.Contains(msg, "no máximo 100") {
		t.Errorf("skill longa: erro = %q", msg)
	}
}

func TestValidateResumeText(t *testing.T) {
	ok := strings.Repeat("a", minResumeTextRunes)
	runFieldCases(t, "validateResumeText", validateResumeText, []fieldCase{
		{in: "  " + ok + "  ", want: ok},
		{in: "", wantErr: "Cole o texto"},
		{in: "Pedro, dev Python", wantErr: "curto demais"},
		{in: strings.Repeat("a", MaxResumeTextRunes+1), wantErr: "no máximo 14000"},
	})
}

// O teto do formulário tem que ser o mesmo do pipeline de IA: foi a diferença entre os dois (20.000
// no formulário, 14.000 no adapter) que fazia um currículo passar pelo formulário e falhar no fim.
func TestResumeTextLimitMatchesLLMPipeline(t *testing.T) {
	if MaxResumeTextRunes != llm.MaxResumeTextChars {
		t.Errorf("formulário aceita %d, pipeline de IA aceita %d", MaxResumeTextRunes, llm.MaxResumeTextChars)
	}
}

// O formulário recebe TODOS os erros de uma vez, cada um no seu campo.
func TestNormalizeAndValidateManualReturnsEveryFieldError(t *testing.T) {
	years := 99
	req := SubmitApplicationRequest{
		Mode: "manual", Name: "Pedro", Email: "pedro@gmail.como", Phone: "abc", LinkedInURL: "aaa",
		City: "hahah2", State: "haha", YearsExperience: &years, EducationInstitution: "UnB",
		EducationPeriod: "2019 - 2015", Consent: false,
	}

	errs := req.normalizeAndValidate()

	for _, field := range []string{"name", "email", "phone", "linkedinUrl", "city", "state", "yearsExperience",
		"educationDegree", "educationPeriod", "consent"} {
		if errs[field] == "" {
			t.Errorf("campo %q deveria ter erro; erros = %v", field, errs)
		}
	}
	if errs["educationDegree"] != "Informe o curso ou formação." {
		t.Errorf("formação vazia com instituição preenchida: %q", errs["educationDegree"])
	}
}

func TestNormalizeAndValidateNormalizesValidRequest(t *testing.T) {
	years := 4
	req := SubmitApplicationRequest{
		Mode: "manual", Name: " pedro  henrique ", Email: " Pedro@Gmail.com", Phone: "61999023060",
		LinkedInURL: "linkedin.com/in/pedro-oliveira/", City: " Brasília", State: "df", YearsExperience: &years,
		EducationDegree: "Ciência da Computação", EducationInstitution: "UnB", EducationPeriod: "2015 - 2019",
		Skills: []string{"Go", "go", " SQL "}, Consent: true,
	}

	if errs := req.normalizeAndValidate(); len(errs) != 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
	if req.Name != "pedro henrique" || req.Email != "pedro@gmail.com" || req.Phone != "(61) 99902-3060" ||
		req.LinkedInURL != "https://www.linkedin.com/in/pedro-oliveira" || req.City != "Brasília" || req.State != "DF" ||
		strings.Join(req.Skills, "|") != "Go|SQL" {
		t.Errorf("não normalizou: %+v", req)
	}
}

// Campo que o modo não usa não pode bloquear o envio (a pessoa trocou de aba e deixou lixo lá).
func TestNormalizeAndValidateIgnoresFieldsOfTheOtherMode(t *testing.T) {
	req := SubmitApplicationRequest{
		Mode: "resume_text", Name: "Pedro Santana", Email: "pedro@example.com", Consent: true,
		ResumeText: strings.Repeat("currículo ", 20), Phone: "abc", State: "haha",
	}
	if errs := req.normalizeAndValidate(); len(errs) != 0 {
		t.Errorf("erros de campos do modo manual bloquearam o modo currículo: %v", errs)
	}

	manual := SubmitApplicationRequest{Mode: "manual", Name: "Pedro Santana", Email: "pedro@example.com", Consent: true, ResumeText: "x"}
	if errs := manual.normalizeAndValidate(); len(errs) != 0 {
		t.Errorf("texto do currículo bloqueou o modo manual: %v", errs)
	}
}

func TestResumeFileFormValidation(t *testing.T) {
	form := ResumeFileForm{Name: "Pedro", Email: "x", Consent: false}
	errs := form.normalizeAndValidate()
	if errs["name"] == "" || errs["email"] == "" || errs["consent"] == "" {
		t.Errorf("erros = %v", errs)
	}
}
