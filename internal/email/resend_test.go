package email

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func TestGerarPKCE(t *testing.T) {
	verifier, challenge, err := GerarPKCE()
	if err != nil {
		t.Fatalf("Erro ao gerar PKCE: %v", err)
	}
	if len(verifier) < 40 {
		t.Errorf("Verifier muito curto: %s", verifier)
	}
	if len(challenge) < 40 {
		t.Errorf("Challenge muito curto: %s", challenge)
	}
	// Testar se challenge é base64url válida
	if _, err := base64.RawURLEncoding.DecodeString(challenge); err != nil {
		t.Errorf("Challenge não é base64url válida: %v", err)
	}
}

func TestConstruirURLAutorizacao(t *testing.T) {
	uStr := ConstruirURLAutorizacao("client_123", "https://app.com/callback", "state_xyz", "chal_abc")
	u, err := url.Parse(uStr)
	if err != nil {
		t.Fatalf("URL inválida: %v", err)
	}
	if u.Host != "resend.com" {
		t.Errorf("Host incorreto: %s", u.Host)
	}
	if u.Path != "/oauth/authorize" {
		t.Errorf("Path incorreto: %s", u.Path)
	}
	q := u.Query()
	if q.Get("client_id") != "client_123" {
		t.Errorf("client_id incorreto: %s", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "https://app.com/callback" {
		t.Errorf("redirect_uri incorreto: %s", q.Get("redirect_uri"))
	}
	if q.Get("code_challenge") != "chal_abc" {
		t.Errorf("code_challenge incorreto: %s", q.Get("code_challenge"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method incorreto: %s", q.Get("code_challenge_method"))
	}
	if q.Get("scope") != "full_access" {
		t.Errorf("scope incorreto: %s", q.Get("scope"))
	}
}

func TestClienteEnviarValidacao(t *testing.T) {
	cli := Cliente{APIKey: ""}
	err := cli.Enviar("dest@teste.com", "Assunto", "Corpo")
	if err == nil || !strings.Contains(err.Error(), "Resend não configurado") {
		t.Errorf("Deveria retornar erro de não configurado, obteve: %v", err)
	}
}
