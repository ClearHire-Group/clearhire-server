package llm

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func readFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/resume.pdf")
	if err != nil {
		t.Fatalf("lendo fixture: %v", err)
	}
	return data
}

func TestExtractPDFTextReadsTextLayer(t *testing.T) {
	text, err := ExtractPDFText(readFixture(t))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	for _, want := range []string{"Marina Albuquerque", "marina.albuquerque@example.test", "Kubernetes"} {
		if !strings.Contains(text, want) {
			t.Errorf("texto extraído não contém %q\nextraído: %q", want, text)
		}
	}
}

// O caminho que economiza dinheiro: PDF com camada de texto nunca deve chegar ao provedor como
// arquivo, porque página de PDF é cobrada como imagem.
func TestPreprocessInputConvertsPDFToText(t *testing.T) {
	out := PreprocessInput(Input{PDFBytes: readFixture(t)})

	if len(out.PDFBytes) != 0 {
		t.Error("PDFBytes deveria ter sido descartado — senão o provedor cobra o arquivo como imagem")
	}
	if !strings.Contains(out.Text, "Marina Albuquerque") {
		t.Errorf("texto não foi promovido para Input.Text: %q", out.Text)
	}
}

// PDF escaneado (sem camada de texto) é o único caso em que vale mandar o arquivo: ali o modelo é
// o OCR. Bytes ilegíveis exercitam o mesmo galho — o importante é não perder o input.
func TestPreprocessInputKeepsBytesWhenNoTextLayer(t *testing.T) {
	garbage := []byte("isto não é um PDF")
	out := PreprocessInput(Input{PDFBytes: garbage})

	if len(out.PDFBytes) == 0 {
		t.Fatal("PDFBytes foi descartado sem ter texto extraído — o caminho de OCR ficaria sem entrada")
	}
	if out.Text != "" {
		t.Errorf("Text deveria ficar vazio, veio %q", out.Text)
	}
}

func TestPreprocessInputLeavesTextInputAlone(t *testing.T) {
	out := PreprocessInput(Input{Text: "currículo em texto puro"})

	if out.Text != "currículo em texto puro" {
		t.Errorf("texto foi alterado: %q", out.Text)
	}
	if len(out.PDFBytes) != 0 {
		t.Error("PDFBytes apareceu do nada")
	}
}

// pdfWithPages monta um PDF mínimo e válido com n páginas em branco (xref com offsets reais), para
// exercitar o teto de páginas sem depender de um arquivo grande no repositório.
func pdfWithPages(n int) []byte {
	var b strings.Builder
	offsets := []int{}
	add := func(obj string) {
		offsets = append(offsets, b.Len())
		b.WriteString(obj)
	}
	b.WriteString("%PDF-1.4\n")
	kids := make([]string, n)
	for i := 0; i < n; i++ {
		kids[i] = fmt.Sprintf("%d 0 R", 3+i)
	}
	add("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	add(fmt.Sprintf("2 0 obj\n<< /Type /Pages /Kids [%s] /Count %d >>\nendobj\n", strings.Join(kids, " "), n))
	for i := 0; i < n; i++ {
		add(fmt.Sprintf("%d 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>\nendobj\n", 3+i))
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return []byte(b.String())
}

func TestValidatePDFAcceptsRealResume(t *testing.T) {
	if err := ValidatePDF(readFixture(t)); err != nil {
		t.Errorf("currículo real recusado: %v", err)
	}
}

// Um arquivo qualquer renomeado para .pdf tem que ser barrado pelo CONTEÚDO — nome e Content-Type
// vêm do cliente anônimo.
func TestValidatePDFRejectsNonPDF(t *testing.T) {
	for name, data := range map[string][]byte{
		"texto":      []byte("isto não é um PDF"),
		"vazio":      {},
		"executável": append([]byte("MZ"), make([]byte, 100)...),
	} {
		if err := ValidatePDF(data); !errors.Is(err, ErrNotPDF) {
			t.Errorf("%s: erro = %v, esperava ErrNotPDF", name, err)
		}
	}
}

// Cabeçalho certo, corpo podre: o parser não pode derrubar a requisição (pânico) nem deixar passar.
func TestValidatePDFRejectsCorruptedBody(t *testing.T) {
	err := ValidatePDF([]byte("%PDF-1.4\n\x00\x01\x02 lixo sem estrutura nenhuma"))
	if !errors.Is(err, ErrPDFUnreadable) {
		t.Errorf("erro = %v, esperava ErrPDFUnreadable", err)
	}
}

// O pior caso de custo: PDF válido, dentro do limite de bytes, com páginas demais. Precisa ser
// recusado ANTES de qualquer processamento.
func TestValidatePDFRejectsTooManyPages(t *testing.T) {
	if err := ValidatePDF(pdfWithPages(MaxPDFPages)); err != nil {
		t.Fatalf("PDF no limite deveria passar: %v", err)
	}
	if err := ValidatePDF(pdfWithPages(MaxPDFPages + 1)); !errors.Is(err, ErrPDFTooLong) {
		t.Errorf("erro = %v, esperava ErrPDFTooLong", err)
	}
}

// Antes só o texto colado tinha teto; o extraído de PDF podia ser arbitrariamente grande.
func TestPreprocessInputCapsTextLength(t *testing.T) {
	long := strings.Repeat("a", MaxResumeTextChars+5000)
	out := PreprocessInput(Input{Text: long})

	if got := len([]rune(out.Text)); got != MaxResumeTextChars {
		t.Errorf("texto tem %d caracteres, esperava o teto de %d", got, MaxResumeTextChars)
	}
}

func TestTruncateRunesNeverSplitsACharacter(t *testing.T) {
	got := TruncateRunes("ação ação ação", 5)
	if got != "ação " {
		t.Errorf("truncou errado: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Error("cortou no meio de um caractere e produziu UTF-8 inválido")
	}
}
