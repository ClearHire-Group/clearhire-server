package auth

// Session representa uma sessão autenticada — o par access/refresh token
// emitido no login. Ver db/schema.sql (refresh_tokens).
type Session struct {
	AccessToken  string
	RefreshToken string
}
