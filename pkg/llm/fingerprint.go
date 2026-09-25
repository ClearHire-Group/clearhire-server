package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Fingerprint identifica um currículo pelo CONTEÚDO, para que o mesmo documento nunca seja extraído
// (e pago) duas vezes. Ver migrations/0007_resume_extraction_cache.sql.
//
// Chame sempre DEPOIS de PreprocessInput: assim o mesmo PDF e o mesmo texto colado convergem para o
// mesmo hash, em vez de gerarem duas entradas de cache para o mesmo currículo.
//
// Espaço em branco é normalizado porque extrator de PDF não é estável nisso — o mesmo arquivo pode
// render quebras de linha e espaçamento diferentes entre versões da biblioteca, e um hash que muda
// por causa disso não serve para nada. O conteúdo em si é preservado byte a byte (sem lowercase,
// sem remover acento): duas pessoas diferentes não podem colidir.
func Fingerprint(in Input) string {
	var payload []byte
	if in.Text != "" {
		payload = []byte(strings.Join(strings.Fields(in.Text), " "))
	} else {
		// PDF escaneado: não há texto para normalizar, então o próprio arquivo é a identidade.
		payload = in.PDFBytes
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
