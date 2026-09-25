package llm

import "testing"

// A razão de existir do fingerprint: extrator de PDF não é estável em espaçamento, e um hash que
// muda por causa disso faria o cache errar e pagar de novo pelo mesmo documento.
func TestFingerprintIgnoresWhitespaceVariation(t *testing.T) {
	a := Fingerprint(Input{Text: "Marina Albuquerque\nGo, Kubernetes"})
	b := Fingerprint(Input{Text: "  Marina   Albuquerque \r\n\r\n Go,  Kubernetes  "})

	if a != b {
		t.Errorf("variação de espaço em branco mudou o hash:\n a=%s\n b=%s", a, b)
	}
}

func TestFingerprintDistinguishesDifferentContent(t *testing.T) {
	a := Fingerprint(Input{Text: "Marina Albuquerque"})
	b := Fingerprint(Input{Text: "Diego Salgado"})

	if a == b {
		t.Error("currículos diferentes colidiram — um candidato receberia a extração do outro")
	}
}

// Acento e caixa NÃO são normalizados de propósito: normalizar aumentaria a chance de colisão entre
// pessoas diferentes, e o ganho seria nulo (o mesmo arquivo produz os mesmos bytes).
func TestFingerprintIsCaseAndAccentSensitive(t *testing.T) {
	if Fingerprint(Input{Text: "joão"}) == Fingerprint(Input{Text: "joao"}) {
		t.Error("acento foi normalizado — não deveria")
	}
	if Fingerprint(Input{Text: "Marina"}) == Fingerprint(Input{Text: "marina"}) {
		t.Error("caixa foi normalizada — não deveria")
	}
}

// PDF escaneado não tem texto para normalizar, então a identidade são os próprios bytes.
func TestFingerprintFallsBackToPDFBytes(t *testing.T) {
	a := Fingerprint(Input{PDFBytes: []byte("scan-a")})
	b := Fingerprint(Input{PDFBytes: []byte("scan-b")})

	if a == "" {
		t.Fatal("hash vazio para entrada só com bytes")
	}
	if a == b {
		t.Error("PDFs escaneados diferentes colidiram")
	}
}

// PreprocessInput roda antes do Fingerprint no serviço; isto trava esse contrato, garantindo que o
// mesmo currículo enviado como PDF ou colado como texto caia na MESMA entrada de cache.
func TestFingerprintConvergesAfterPreprocess(t *testing.T) {
	pdf := PreprocessInput(Input{PDFBytes: readFixture(t)})
	text := Input{Text: pdf.Text}

	if Fingerprint(pdf) != Fingerprint(text) {
		t.Error("mesmo currículo via PDF e via texto gerou hashes distintos — cache erraria duas vezes")
	}
}
