package llm

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
)

// MaxResumeTextChars é o teto de texto de currículo que entra no pipeline, seja colado ou extraído
// de PDF. É o mesmo teto do campo resumeText do formulário (candidate.MaxResumeTextRunes) e do
// adapter da Groq: antes o formulário aceitava 20.000 e o adapter recusava acima de 14.000, então um
// currículo nessa faixa passava pelo formulário e falhava no fim com uma mensagem genérica.
// ~14.000 caracteres são 5-6 páginas de texto; um currículo típico tem 3-8 mil.
const MaxResumeTextChars = 14000

// MaxPDFPages recusa PDFs longos demais para serem um currículo. Um currículo real tem 1-5 páginas;
// o pior caso de custo é um PDF válido, dentro do limite de bytes, com centenas de páginas.
const MaxPDFPages = 15

var (
	ErrNotPDF        = errors.New("llm: arquivo não é um PDF")
	ErrPDFTooLong    = errors.New("llm: PDF com páginas demais para um currículo")
	ErrPDFUnreadable = errors.New("llm: PDF corrompido ou ilegível")
)

// ValidatePDF confere, antes de qualquer processamento ou custo, que os bytes são mesmo um PDF e
// têm um número razoável de páginas. Vem do upload anônimo, então nada aqui confia no nome ou no
// Content-Type do arquivo — só no conteúdo.
//
// Tem que rodar na borda (handler) e não dentro de PreprocessInput: lá, um erro de leitura faz o
// PDF seguir como bytes para o caminho de OCR pago, ou seja, um PDF gigante ou malformado seria
// premiado com a chamada mais cara do pipeline.
func ValidatePDF(data []byte) (err error) {
	// A spec permite lixo antes do cabeçalho, mas só nos primeiros 1024 bytes.
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	if !bytes.Contains(head, []byte("%PDF-")) {
		return ErrNotPDF
	}

	// O parser roda sobre entrada hostil e já entrou em pânico com PDFs malformados em outros
	// projetos; um pânico aqui não pode derrubar a requisição de mais ninguém.
	defer func() {
		if recover() != nil {
			err = ErrPDFUnreadable
		}
	}()

	r, openErr := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if openErr != nil {
		return ErrPDFUnreadable
	}
	if r.NumPage() > MaxPDFPages {
		return ErrPDFTooLong
	}
	return nil
}

// ExtractPDFText lê os bytes em memória (nunca grava em disco) e devolve o texto da camada de texto
// do PDF. Não é OCR: um PDF escaneado como imagem, sem texto selecionável, devolve string vazia —
// mesma limitação de qualquer extrator sem visão computacional. O texto devolvido é limitado a
// MaxResumeTextChars.
func ExtractPDFText(data []byte) (text string, err error) {
	defer func() {
		if recover() != nil {
			text, err = "", ErrPDFUnreadable
		}
	}()

	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	reader, err := r.GetPlainText()
	if err != nil {
		return "", err
	}
	// Margem de 4 bytes por rune (UTF-8) para não montar em memória um texto que vai ser cortado.
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(io.LimitReader(reader, int64(MaxResumeTextChars)*4)); err != nil {
		return "", fmt.Errorf("lendo texto do PDF: %w", err)
	}
	return TruncateRunes(buf.String(), MaxResumeTextChars), nil
}

// PreprocessInput converte PDF em texto LOCALMENTE antes que qualquer provedor pago veja o arquivo.
//
// Isto é controle de custo, não conveniência: um provedor cobra páginas de PDF como imagem, o que
// custa tipicamente 3-6x o mesmo conteúdo enviado como texto. A esmagadora maioria dos currículos
// tem camada de texto, então o caminho caro (mandar os bytes) deve ser exceção, não padrão.
//
// Quando a camada de texto vem vazia (PDF escaneado), o Input volta intacto com os bytes: aí o
// modelo É o OCR, e mandar o arquivo é a única opção — não é desperdício, é o serviço sendo usado
// para o que só ele faz.
//
// O texto de saída tem o espaçamento compactado (extração de PDF gera muito espaço em branco, que
// só gasta o teto e tokens) e é sempre limitado a MaxResumeTextChars, venha de onde vier. Compactar
// não muda o cache: llm.Fingerprint já ignora espaçamento.
//
// Chamar isto é idempotente e seguro para qualquer Extractor, inclusive o determinístico.
func PreprocessInput(in Input) Input {
	if len(in.PDFBytes) == 0 {
		in.Text = TruncateRunes(compactWhitespace(in.Text), MaxResumeTextChars)
		return in
	}
	text, err := ExtractPDFText(in.PDFBytes)
	if err != nil || strings.TrimSpace(text) == "" {
		return in
	}
	return Input{Text: TruncateRunes(compactWhitespace(text), MaxResumeTextChars)}
}

var (
	horizontalSpace = regexp.MustCompile(`[ \t\f\v\x{00A0}]+`)
	blankLines      = regexp.MustCompile(`\n\s*\n(\s*\n)+`)
)

// compactWhitespace colapsa espaços/tabs repetidos e mais de uma linha em branco seguida, mantendo
// as quebras de linha que dão estrutura ao currículo.
func compactWhitespace(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = horizontalSpace.ReplaceAllString(s, " ")
	s = blankLines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
