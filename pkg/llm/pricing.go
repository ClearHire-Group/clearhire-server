package llm

// Preços por 1 milhão de tokens, em dólares. Conferidos em 2026-09-17.
//
// Isto vive no código e não em config de propósito: o custo estimado é CONGELADO na linha de
// llm_usage no momento da chamada, então o que importa é o preço vigente naquele instante. Um preço
// em variável de ambiente mudaria silenciosamente o significado de números já gravados.
//
// Ao mudar de modelo ou provedor, adicione a linha aqui. Modelo ausente da tabela produz custo
// desconhecido (nil), nunca zero — "não sei quanto custou" e "foi de graça" são coisas diferentes, e
// tratá-las como iguais esconderia exatamente o gasto que queremos enxergar.
type price struct {
	inputPerMillion  float64
	outputPerMillion float64
}

var priceTable = map[string]price{
	// Anthropic
	"claude-haiku-4-5": {inputPerMillion: 1.00, outputPerMillion: 5.00},
	"claude-sonnet-5":  {inputPerMillion: 2.00, outputPerMillion: 10.00},
	"claude-opus-5":    {inputPerMillion: 5.00, outputPerMillion: 25.00},
	// Groq (hospedagem de modelos abertos). ATENÇÃO: valores de referência, NÃO conferidos na
	// pagina de preços da Groq nesta data (a consulta não retornou a tabela) — confirmar em
	// console.groq.com antes de usar fora de teste. No free tier o gasto real é zero; estes números
	// só alimentam o teto de orçamento (superestimar é o lado seguro).
	"openai/gpt-oss-20b":  {inputPerMillion: 0.075, outputPerMillion: 0.30},
	"openai/gpt-oss-120b": {inputPerMillion: 0.15, outputPerMillion: 0.60},
	// Google — tier barato, candidato a padrão da extração (ver auditoria)
	"gemini-2.5-flash-lite": {inputPerMillion: 0.10, outputPerMillion: 0.40},
	"gemini-3.5-flash-lite": {inputPerMillion: 0.30, outputPerMillion: 2.50},
	"gemini-3.8-flash":      {inputPerMillion: 0.75, outputPerMillion: 3.75},
}

// IsPriced diz se o modelo tem preço conhecido. O boot usa isto para recusar subir com um provedor
// pago cujo modelo não esteja na tabela: sem preço, toda chamada gravaria custo NULL, o teto de
// gasto somaria zero e existiria um caminho de gasto invisível ao controle de orçamento. Falhar no
// deploy é barato; descobrir isso pela fatura não é.
func IsPriced(model string) bool {
	_, known := priceTable[model]
	return known
}

// EstimatedCostUSD devolve nil quando o modelo não está na tabela — o chamador grava NULL, que lê
// como "desconhecido". Ver comentário acima sobre por que isso não pode virar zero.
//
// Token de cache é cobrado a uma fração do preço de entrada; como o desconto varia por provedor e
// esta estimativa serve para ordem de grandeza e alerta, ele entra aqui pelo preço cheio. O erro é
// conservador (superestima), que é o lado seguro para um número que dispara teto de orçamento.
func EstimatedCostUSD(u Usage) *float64 {
	p, known := priceTable[u.Model]
	if !known {
		return nil
	}
	cost := (float64(u.InputTokens)*p.inputPerMillion + float64(u.OutputTokens)*p.outputPerMillion) / 1_000_000
	return &cost
}
