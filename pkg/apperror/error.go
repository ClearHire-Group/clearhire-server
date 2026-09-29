// Package apperror define erros de domínio com um código HTTP associado, pra
// o handler nunca precisar decidir "isso é um 404 ou um 400" — o service já
// devolve isso junto com o erro.
package apperror

import "net/http"

type AppError struct {
	Code    int
	Message string
	// Field é o campo do formulário a que o erro se refere ("email"), quando há um. O handler o
	// devolve em Envelope.Fields para o front mostrar a mensagem no campo certo.
	Field string
	// Fields é o erro de cada campo de um formulário inteiro inválido (campo -> mensagem).
	Fields map[string]string
}

func (e *AppError) Error() string {
	return e.Message
}

func NotFound(message string) *AppError {
	return &AppError{Code: http.StatusNotFound, Message: message}
}

func BadRequest(message string) *AppError {
	return &AppError{Code: http.StatusBadRequest, Message: message}
}

// BadRequestField é BadRequest ligado a um campo do formulário.
func BadRequestField(field, message string) *AppError {
	return &AppError{Code: http.StatusBadRequest, Message: message, Field: field}
}

// Validation é o formulário com um ou mais campos inválidos.
func Validation(message string, fields map[string]string) *AppError {
	return &AppError{Code: http.StatusBadRequest, Message: message, Fields: fields}
}

func Unauthorized(message string) *AppError {
	return &AppError{Code: http.StatusUnauthorized, Message: message}
}

func Forbidden(message string) *AppError {
	return &AppError{Code: http.StatusForbidden, Message: message}
}

func Internal(message string) *AppError {
	return &AppError{Code: http.StatusInternalServerError, Message: message}
}

// Unavailable é "o recurso existe mas não está habilitado/disponível agora" — por exemplo a
// avaliação por IA num ambiente sem provedor configurado. Diferente de Internal: não é um bug.
func Unavailable(message string) *AppError {
	return &AppError{Code: http.StatusServiceUnavailable, Message: message}
}
