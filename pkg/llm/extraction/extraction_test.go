package extraction

import (
	"reflect"
	"strings"
	"testing"
)

// A economia principal da extração é não pedir o currículo de volta. Se rawText reaparecer no
// schema padrão, o custo de saída volta a ~3x sem que nada mais quebre — por isso isto é teste.
func TestDefaultSchemaDoesNotAskForRawText(t *testing.T) {
	if _, found := SchemaProperties["rawText"]; found {
		t.Error("rawText voltou ao schema padrão: o modelo passaria a ecoar o currículo inteiro na saída")
	}
	if _, found := SchemaProperties["name"]; !found {
		t.Error("schema padrão perdeu o campo name")
	}
}

func TestOCRSchemaAsksForRawText(t *testing.T) {
	if _, found := OCRSchemaProperties["rawText"]; !found {
		t.Error("schema de OCR precisa de rawText — é a única forma de obter o texto de um PDF escaneado")
	}
	// A variante de OCR é o schema padrão + 1 campo; divergir significa que o modelo devolveria
	// menos dado estruturado justamente no caminho mais caro.
	if len(OCRSchemaProperties) != len(SchemaProperties)+1 {
		t.Errorf("OCR deveria ter exatamente 1 campo a mais: padrão=%d ocr=%d",
			len(SchemaProperties), len(OCRSchemaProperties))
	}
}

// WithRawText copia o map. Sem a cópia, construir a variante de OCR teria mutado o schema padrão
// no init e o rawText voltaria para todo mundo — falha silenciosa e cara.
func TestWithRawTextDoesNotMutateBase(t *testing.T) {
	base := map[string]any{"name": map[string]any{"type": "string"}}
	WithRawText(base)

	if _, found := base["rawText"]; found {
		t.Error("WithRawText mutou o map de origem")
	}
}

func TestOCRInstructionsExtendBaseInstructions(t *testing.T) {
	if len(OCRInstructions) <= len(BaseInstructions) {
		t.Error("instruções de OCR deveriam estender as padrão")
	}
	if !strings.Contains(OCRInstructions, "rawText") {
		t.Error("instruções de OCR não explicam rawText, mas o schema pede o campo")
	}
	if strings.Contains(BaseInstructions, "rawText") {
		t.Error("instruções padrão ainda mencionam rawText, que não existe mais nesse schema")
	}
}

// Modo estrito da Groq recusa (400) schema em que algum campo não seja obrigatório ou em que um
// objeto aceite campo extra — em QUALQUER nível. Um nível esquecido só apareceria em produção.
func TestStrictObjectSchemaIsStrictAtEveryLevel(t *testing.T) {
	schema := StrictObjectSchema(SchemaProperties)

	var walk func(path string, node map[string]any)
	walk = func(path string, node map[string]any) {
		if node["type"] == "object" {
			props, _ := node["properties"].(map[string]any)
			if node["additionalProperties"] != false {
				t.Errorf("%s: additionalProperties deveria ser false", path)
			}
			required, _ := node["required"].([]string)
			if len(required) != len(props) {
				t.Errorf("%s: required tem %d de %d campos", path, len(required), len(props))
			}
			for name, child := range props {
				if m, ok := child.(map[string]any); ok {
					walk(path+"."+name, m)
				}
			}
		}
		if node["type"] == "array" {
			if items, ok := node["items"].(map[string]any); ok {
				walk(path+"[]", items)
			}
		}
	}
	walk("$", schema)
}

func TestStrictObjectSchemaDoesNotMutateInput(t *testing.T) {
	before := map[string]any{"a": map[string]any{"type": "object", "properties": map[string]any{"b": map[string]any{"type": "string"}}}}
	snapshot := map[string]any{"a": map[string]any{"type": "object", "properties": map[string]any{"b": map[string]any{"type": "string"}}}}

	StrictObjectSchema(before)

	if !reflect.DeepEqual(before, snapshot) {
		t.Error("StrictObjectSchema mutou o schema de entrada")
	}
}

// A defesa em instrução é fraca, mas é a única em nível de prompt — não pode sumir sem ninguém ver.
func TestInstructionsTreatResumeAsData(t *testing.T) {
	if !strings.Contains(BaseInstructions, "DADO") {
		t.Error("instruções perderam a regra de que o currículo é dado, não instrução")
	}
}
