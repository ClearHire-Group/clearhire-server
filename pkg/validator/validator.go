// Package validator dá um único ponto de validação de payload de entrada
// (DTOs) pra todos os handlers, em vez de cada um validar campo a campo.
package validator

import "github.com/go-playground/validator/v10"

var instance = validator.New()

// Validate roda as tags `validate:"..."` de um DTO e devolve o primeiro
// conjunto de erros encontrado, ou nil se o payload é válido.
func Validate(payload any) error {
	return instance.Struct(payload)
}
