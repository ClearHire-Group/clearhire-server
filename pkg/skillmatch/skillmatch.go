// Package skillmatch é o casamento de termo de taxonomia contra texto livre — a mesma regra usada
// tanto para resolver skills de um currículo (candidate/skillmatch.go) quanto para o filtro
// determinístico de recomendação de talentos (talent/reversematch.go). Um dono só, para as duas
// pontas nunca divergirem sobre o que conta como "menciona a skill".
package skillmatch

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MentionsTerm diz se o texto de um currículo/vaga menciona um termo da taxonomia. Não é
// strings.Contains: isso daria falso-positivo em qualquer palavra que CONTIVESSE o termo ("Go" em
// "gostar" e "Google", "R" em qualquer palavra com r, "Java" em "JavaScript"). Uma skill inventada
// aqui polui tanto a extração de currículo quanto o filtro de recomendação.
//
// Regras, escolhidas para não perder skills com símbolo (C++, C#, .NET, Node.js):
//   - O termo tem que estar cercado por não-letras/não-dígitos (fronteira de palavra Unicode).
//   - Um termo terminado em letra/dígito não casa se for seguido de '+' ou '#': "C" não é "C++".
//   - Termos de 1-2 letras ("R", "Go", "C") só casam com a caixa EXATA da taxonomia. Em minúsculas
//     são palavras comuns ("go", "r") e a capitalização é o único sinal que resta.
func MentionsTerm(text, lowerText, term string) bool {
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
