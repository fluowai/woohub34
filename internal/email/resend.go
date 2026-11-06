package email

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Cliente struct {
	APIKey   string
	From     string
	FromName string
}

type DominioResend struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"` // "verified", "not_started", "pending", "failed"
	CreatedAt string `json:"created_at,omitempty"`
	Region    string `json:"region,omitempty"`
}

type RespostaOAuthToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

func (c Cliente) Enviar(to, subject, html string) error {
	if strings.TrimSpace(c.APIKey) == "" {
		return fmt.Errorf("Resend não configurado")
	}
	from := c.From
	if c.FromName != "" {
		from = c.FromName + " <" + c.From + ">"
	}
	b, _ := json.Marshal(map[string]interface{}{
		"from":    from,
		"to":      []string{to},
		"subject": subject,
		"html":    html,
	})
	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		corpo, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Resend retornou HTTP %d: %s", resp.StatusCode, string(corpo))
	}
	return nil
}

// ListarDominios busca todos os domínios registrados na conta do Resend
func ListarDominios(token string) ([]DominioResend, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("token ou API Key não informada")
	}
	req, err := http.NewRequest(http.MethodGet, "https://api.resend.com/domains", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	cli := &http.Client{Timeout: 15 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	corpo, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Resend retornou HTTP %d: %s", resp.StatusCode, string(corpo))
	}

	var resultado struct {
		Data []DominioResend `json:"data"`
	}
	if err := json.Unmarshal(corpo, &resultado); err != nil {
		return nil, fmt.Errorf("erro ao decodificar domínios: %w", err)
	}
	return resultado.Data, nil
}

// GerarPKCE gera code_verifier e code_challenge (S256) compatíveis com RFC 7636
func GerarPKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

// ConstruirURLAutorizacao monta a URL para login/consentimento no Resend
func ConstruirURLAutorizacao(clientID, redirectURI, state, challenge string) string {
	u, _ := url.Parse("https://resend.com/oauth/authorize")
	q := u.Query()
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", "full_access")
	q.Set("state", state)
	if challenge != "" {
		q.Set("code_challenge", challenge)
		q.Set("code_challenge_method", "S256")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// TrocarCodigoOAuth troca o authorization_code retornado pelo Resend por access_token
func TrocarCodigoOAuth(clientID, clientSecret, code, redirectURI, codeVerifier string) (*RespostaOAuthToken, error) {
	valores := url.Values{}
	valores.Set("grant_type", "authorization_code")
	valores.Set("code", code)
	valores.Set("redirect_uri", redirectURI)
	valores.Set("client_id", clientID)
	if clientSecret != "" {
		valores.Set("client_secret", clientSecret)
	}
	if codeVerifier != "" {
		valores.Set("code_verifier", codeVerifier)
	}

	urls := []string{
		"https://api.resend.com/oauth/token",
		"https://resend.com/oauth/token",
	}

	cli := &http.Client{Timeout: 15 * time.Second}
	var ultimoErro error

	for _, endpoint := range urls {
		req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(valores.Encode()))
		if err != nil {
			ultimoErro = err
			continue
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")

		resp, err := cli.Do(req)
		if err != nil {
			ultimoErro = err
			continue
		}
		corpo, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var tok RespostaOAuthToken
			if err := json.Unmarshal(corpo, &tok); err == nil && tok.AccessToken != "" {
				return &tok, nil
			}
		}
		ultimoErro = fmt.Errorf("endpoint %s retornou HTTP %d: %s", endpoint, resp.StatusCode, string(corpo))
	}

	return nil, fmt.Errorf("falha ao trocar código OAuth no Resend: %v", ultimoErro)
}
