// Package apperror define erros de domínio com um código HTTP associado, pra
// o handler nunca precisar decidir "isso é um 404 ou um 400" — o service já
// devolve isso junto com o erro.
package apperror

import "net/http"

type AppError struct {
	Code    int
	Message string
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

func Unauthorized(message string) *AppError {
	return &AppError{Code: http.StatusUnauthorized, Message: message}
}

func Forbidden(message string) *AppError {
	return &AppError{Code: http.StatusForbidden, Message: message}
}

func Internal(message string) *AppError {
	return &AppError{Code: http.StatusInternalServerError, Message: message}
}
