package integracoes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtrairTextoMensagemTypebot(t *testing.T) {
	// String pura
	if ExtrairTextoMensagemTypebot("Olá mundo") != "Olá mundo" {
		t.Errorf("Falha ao extrair texto simples")
	}

	// Rich text
	rich := map[string]interface{}{
		"richText": []interface{}{
			map[string]interface{}{
				"children": []interface{}{
					map[string]interface{}{"text": "Primeira linha. "},
					map[string]interface{}{"text": "Segunda linha."},
				},
			},
		},
	}
	txt := ExtrairTextoMensagemTypebot(rich)
	if txt != "Primeira linha. Segunda linha." {
		t.Errorf("Falha ao extrair rich text: %s", txt)
	}
}

func TestN8NDespacharEvento(t *testing.T) {
	var payloadRecebido map[string]interface{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-N8N-Token") != "segredo123" {
			t.Errorf("Header X-N8N-Token inválido")
		}
		_ = json.NewDecoder(r.Body).Decode(&payloadRecebido)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer ts.Close()

	cli := NovoClienteN8N()
	cfg := ConfigN8N{
		WebhookURL:   ts.URL,
		TokenSecreto: "segredo123",
	}

	err := cli.DespacharEvento(context.Background(), cfg, "mensagem_recebida", map[string]string{
		"texto": "teste n8n",
	})
	if err != nil {
		t.Fatalf("Erro ao despachar evento para n8n: %v", err)
	}

	if payloadRecebido["evento"] != "mensagem_recebida" {
		t.Errorf("Evento incorreto: %v", payloadRecebido["evento"])
	}
}

func TestChatwootEnviarMensagem(t *testing.T) {
	contatoCriado := false
	mensagemCriada := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/contacts/search":
			// Contato não existe ainda
			_, _ = w.Write([]byte(`{"payload":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/contacts":
			contatoCriado = true
			_, _ = w.Write([]byte(`{"payload":{"contact":{"id":99}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/contacts/99/conversations":
			_, _ = w.Write([]byte(`{"payload":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/conversations":
			_, _ = w.Write([]byte(`{"id":555}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/conversations/555/messages":
			mensagemCriada = true
			_, _ = w.Write([]byte(`{"id":1001,"content":"Olá"}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer ts.Close()

	cli := NovoClienteChatwoot()
	cfg := ConfigChatwoot{
		URL:       ts.URL,
		AccountID: "1",
		Token:     "token_fake",
		InboxID:   "10",
	}

	payload := MensagemRecebidaPayload{
		RemetenteJID: "5511999998888@s.whatsapp.net",
		RemetenteBR:  "+55 (11) 99999-8888",
		NomeContato:  "Cliente Teste",
		Texto:        "Olá, gostaria de saber mais",
		Timestamp:    123456789,
	}

	err := cli.EnviarMensagemRecebida(context.Background(), cfg, payload)
	if err != nil {
		t.Fatalf("Erro ao enviar para Chatwoot: %v", err)
	}

	if !contatoCriado || !mensagemCriada {
		t.Errorf("Chatwoot fluxo incompleto: contatoCriado=%v, mensagemCriada=%v", contatoCriado, mensagemCriada)
	}
}
