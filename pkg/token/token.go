// Package token emite e valida os access tokens (JWT) da sessão. O refresh
// token é um segredo opaco separado, gerado e guardado (hash) pelo domínio
// auth — este pacote não conhece refresh token, só o JWT de vida curta.
package token

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const AccessTokenTTL = 15 * time.Minute

// Claims é o que todo handler protegido enxerga sobre quem fez a requisição.
// CompanyID é a base de todo o escopo de tenant — nunca vem de outro lugar
// além de um token validado (ver README, seção Multi-tenancy).
type Claims struct {
	UserID    string
	CompanyID string
	Role      string
}

type jwtClaims struct {
	UserID    string `json:"uid"`
	CompanyID string `json:"cid"`
	Role      string `json:"role"`
	jwt.RegisteredClaims
}

func Issue(secret string, claims Claims) (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwtClaims{
		UserID:    claims.UserID,
		CompanyID: claims.CompanyID,
		Role:      claims.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenTTL)),
		},
	})
	return token.SignedString([]byte(secret))
}

func Parse(secret string, raw string) (*Claims, error) {
	parsed := &jwtClaims{}
	_, err := jwt.ParseWithClaims(raw, parsed, func(t *jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return nil, err
	}
	if parsed.UserID == "" || parsed.CompanyID == "" {
		return nil, errors.New("token sem claims obrigatórias")
	}
	return &Claims{UserID: parsed.UserID, CompanyID: parsed.CompanyID, Role: parsed.Role}, nil
}
