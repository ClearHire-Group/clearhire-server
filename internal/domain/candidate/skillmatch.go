package candidate

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// mentionsTerm diz se o texto de um currículo menciona uma skill da taxonomia. Substitui o
// strings.Contains que havia antes, que dava falso-positivo em qualquer palavra que CONTIVESSE o
// termo: "Go" em "gostar" e "Google", "R" em qualquer palavra com r, "Java" em "JavaScript". Isso
// polui o dado que alimenta a avaliação — uma skill inventada vira "ponto forte".
//
// Regras, escolhidas para não perder as skills com símbolo (C++, C#, .NET, Node.js):
//   - O termo tem que estar cercado por não-letras/não-dígitos (fronteira de palavra Unicode).
//   - Um termo terminado em letra/dígito não casa se for seguido de '+' ou '#': "C" não é "C++".
//   - Termos de 1-2 letras ("R", "Go", "C") só casam com a caixa EXATA da taxonomia. Em minúsculas
//     são palavras comuns ("go", "r") e a capitalização é o único sinal que resta.
func mentionsTerm(text, lowerText, term string) bool {
	if term == "" {
		return false
	}
	haystack, needle := lowerText, strings.ToLower(term)
	if utf8.RuneCountInString(term) <= 2 {
		haystack, needle = text, term
	}

	first, _ := utf8.DecodeRuneInString(needle)
	last, _ := utf8.DecodeLastRuneInString(needle)

	for from := 0; from < len(haystack); {
		i := strings.Index(haystack[from:], needle)
		if i < 0 {
			return false
		}
		start := from + i
		end := start + len(needle)

		beforeOK := start == 0 || !isWordRune(lastRuneBefore(haystack, start)) || !isWordRune(first)
		afterOK := end == len(haystack)
		if !afterOK {
			next, _ := utf8.DecodeRuneInString(haystack[end:])
			afterOK = !isWordRune(next)
			if isWordRune(last) && (next == '+' || next == '#') {
				afterOK = false
			}
		}
		if beforeOK && afterOK {
			return true
		}
		from = start + 1
	}
	return false
}

func lastRuneBefore(s string, i int) rune {
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return r
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
