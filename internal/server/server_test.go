package server

import (
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/ClearHire-Group/clearhire-server/internal/config"
	"github.com/ClearHire-Group/clearhire-server/internal/middleware"
)

// clientIP monta o app como o servidor de verdade e devolve o que c.IP() enxerga — que é o que o
// rate limit por IP usa como chave.
func clientIP(t *testing.T, cfg *config.Config, header, value string) string {
	t.Helper()
	app := New(cfg)
	app.Get("/ip", func(c *fiber.Ctx) error { return c.SendString(c.IP()) })

	req := httptest.NewRequest("GET", "/ip", nil)
	if header != "" {
		req.Header.Set(header, value)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

func testConfig(trusted ...string) *config.Config {
	return &config.Config{CORSOrigin: "http://localhost:4200", ClientIPHeader: "X-Real-IP", TrustedProxies: trusted}
}

// app.Test conecta de 0.0.0.0, então "0.0.0.0" faz o papel do proxy nestes testes.
const proxy = "0.0.0.0"

// Atrás de um proxy confiável, o IP é o do cliente — não o do proxy. Sem isto o rate limit por IP
// vira um balde único para todo mundo.
func TestClientIPComesFromTrustedProxyHeader(t *testing.T) {
	if got := clientIP(t, testConfig(proxy), "X-Real-IP", "203.0.113.7"); got != "203.0.113.7" {
		t.Errorf("IP = %q, esperava o do cliente", got)
	}
}

// De um cliente que NÃO é proxy confiável o header é ignorado: senão qualquer um escolheria o
// próprio IP e escaparia do limite.
func TestClientIPHeaderIgnoredFromUntrustedPeer(t *testing.T) {
	got := clientIP(t, testConfig("10.9.9.9"), "X-Real-IP", "203.0.113.7")
	if got == "203.0.113.7" {
		t.Errorf("IP = %q — o header de um par não confiável foi aceito", got)
	}
}

// Sem validação o Fiber devolve o valor bruto do header: cada string inventada viraria um balde
// novo de rate limit. Valor que não é IP cai no IP real da conexão.
func TestInvalidClientIPHeaderFallsBackToConnection(t *testing.T) {
	got := clientIP(t, testConfig(proxy), "X-Real-IP", "isto-nao-e-um-ip")
	if got == "isto-nao-e-um-ip" {
		t.Error("um valor que não é IP virou chave de rate limit")
	}
}

func TestDefaultLoopbackOnlyTrustsNobodyElse(t *testing.T) {
	got := clientIP(t, testConfig("127.0.0.1", "::1"), "X-Real-IP", "203.0.113.7")
	if got == "203.0.113.7" {
		t.Error("o padrão de dev aceitou header de um par que não é loopback")
	}
}

// O bug de ponta a ponta: 6 clientes distintos atrás do MESMO proxy passam do limite de 5/min se o
// IP for o do proxy; com o IP do cliente, cada um tem o seu balde.
func TestRateLimitIsPerClientBehindProxy(t *testing.T) {
	app := New(testConfig(proxy))
	app.Get("/limited", middleware.RateLimit(2, time.Minute), func(c *fiber.Ctx) error { return c.SendStatus(200) })

	hit := func(ip string) int {
		req := httptest.NewRequest("GET", "/limited", nil)
		req.Header.Set("X-Real-IP", ip)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode
	}

	for _, ip := range []string{"203.0.113.1", "203.0.113.2", "203.0.113.3", "203.0.113.4", "203.0.113.5", "203.0.113.6"} {
		if code := hit(ip); code != 200 {
			t.Fatalf("cliente %s foi limitado (%d) por causa de outros — o balde é do proxy, não do cliente", ip, code)
		}
	}
	// E o mesmo cliente insistindo ainda é contido.
	hit("203.0.113.1")
	if code := hit("203.0.113.1"); code != 429 {
		t.Errorf("o mesmo cliente passou do limite sem ser contido: %d", code)
	}
}

// Sem ReadTimeout/WriteTimeout o Fiber espera para sempre por conexões lentas (slowloris), e a
// chamada de IA pode levar até 40s — o WriteTimeout tem que ficar acima disso.
func TestServerHasTimeouts(t *testing.T) {
	cfg := New(testConfig(proxy)).Config()
	if cfg.ReadTimeout <= 0 || cfg.WriteTimeout <= 0 || cfg.IdleTimeout <= 0 {
		t.Errorf("timeouts ausentes: read=%v write=%v idle=%v", cfg.ReadTimeout, cfg.WriteTimeout, cfg.IdleTimeout)
	}
	if cfg.WriteTimeout < 45*time.Second {
		t.Errorf("WriteTimeout = %v — abaixo do teto de uma chamada de IA (40s), cortaria a resposta", cfg.WriteTimeout)
	}
}
