package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"go.mau.fi/whatsmeow/internal/database"
	"go.mau.fi/whatsmeow/internal/integracoes"
)

type mockWhatsAppSender struct{}

func (m *mockWhatsAppSender) EnviarMensagemTexto(ctx context.Context, id, destinatario, texto string) (string, error) {
	return "msg_fake_123", nil
}
func (m *mockWhatsAppSender) EnviarMensagemAudio(ctx context.Context, id, destinatario string, dadosAudio []byte, ptt bool) (string, error) {
	return "msg_fake_aud", nil
}
func (m *mockWhatsAppSender) EnviarMensagemImagem(ctx context.Context, id, destinatario string, dadosImagem []byte, legenda string) (string, error) {
	return "msg_fake_img", nil
}
func (m *mockWhatsAppSender) EnviarMensagemEnquete(ctx context.Context, id, destinatario, pergunta string, opcoes []string, maxEscolhas int) (string, error) {
	return "msg_fake_poll", nil
}

func TestIntegracoesEAtribuicoesAPI(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_api_int.db")
	defer os.Remove(dbPath)

	gdb, err := database.InicializarBanco("sqlite", dbPath, database.OpcoesSeed{SeedDadosDemo: true})
	if err != nil {
		t.Fatalf("Erro ao inicializar banco: %v", err)
	}
	defer gdb.Fechar()
	database.DB = gdb

	mockWpp := &mockWhatsAppSender{}
	integracoes.InicializarGerenciadorIntegracoes(gdb, mockWpp)

	// Instância de teste seeded: inst_demo_brasil, chave: woo_br_...
	instDB, _ := gdb.BuscarInstanciaPorId("inst_demo_brasil")
	if instDB == nil {
		t.Fatalf("Instância demo não encontrada")
	}

	handler := ConfigurarRotas("./web")

	// 1. Testar Listar Caixas: GET /api/v1/instancia/inst_demo_brasil/integracoes
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/instancia/inst_demo_brasil/integracoes", nil)
	req1.Header.Set("Authorization", "Bearer "+instDB.ChaveAPI)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("Esperava status 200 ao listar integrações, obteve %d: %s", rec1.Code, rec1.Body.String())
	}

	var resCaixas struct {
		Caixas []struct {
			Tipo  string `json:"tipo"`
			Nome  string `json:"nome"`
			Ativo bool   `json:"ativo"`
		} `json:"caixas"`
	}
	if err := json.NewDecoder(rec1.Body).Decode(&resCaixas); err != nil || len(resCaixas.Caixas) < 5 {
		t.Fatalf("Caixas retornadas inválidas: %+v", resCaixas)
	}

	// 2. Testar Salvar Caixa: POST /api/v1/instancia/inst_demo_brasil/integracoes/chatwoot
	payloadChatwoot := map[string]interface{}{
		"ativo": true,
		"configuracoes": map[string]interface{}{
			"url":        "https://chat.meudominio.com",
			"account_id": "1",
			"token":      "fake_token_123",
			"inbox_id":   "5",
		},
	}
	bodyJSON, _ := json.Marshal(payloadChatwoot)
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/instancia/inst_demo_brasil/integracoes/chatwoot", bytes.NewBuffer(bodyJSON))
	req2.Header.Set("Authorization", "Bearer "+instDB.ChaveAPI)
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("Esperava status 200 ao salvar integração Chatwoot, obteve %d: %s", rec2.Code, rec2.Body.String())
	}

	// 3. Testar Rastreamento de Origem: Inserir e Listar Atribuições
	_ = gdb.SalvarOuAtualizarAtribuicaoContato(database.AtribuicaoContato{
		InstanciaID:      "inst_demo_brasil",
		JID:              "5548988003260@s.whatsapp.net",
		NumeroFormatado:  "+55 (48) 98800-3260",
		OrigemCanal:      "Meta Ads (Instagram)",
		Campanha:         "Anúncio Stories Vendas",
		UtmSource:        "instagram",
		UtmCampaign:      "campanha_vendas_2026",
		PrimeiraMensagem: "Vi no Instagram e quero contratar",
	})

	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/instancia/inst_demo_brasil/atribuicoes", nil)
	req3.Header.Set("Authorization", "Bearer "+instDB.ChaveAPI)
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusOK {
		t.Fatalf("Esperava status 200 ao listar atribuições, obteve %d: %s", rec3.Code, rec3.Body.String())
	}

	var resAtrib struct {
		Total       int                          `json:"total"`
		Atribuicoes []database.AtribuicaoContato `json:"atribuicoes"`
	}
	if err := json.NewDecoder(rec3.Body).Decode(&resAtrib); err != nil || resAtrib.Total != 1 {
		t.Fatalf("Atribuições retornadas incorretas: %+v", resAtrib)
	}
	if resAtrib.Atribuicoes[0].OrigemCanal != "Meta Ads (Instagram)" {
		t.Errorf("Origem canal incorreta: %s", resAtrib.Atribuicoes[0].OrigemCanal)
	}

	// 4. Testar Webhook Reverso Chatwoot (Atendente respondeu no Chatwoot)
	payloadReversoChatwoot := integracoes.PayloadWebhookChatwoot{
		Event:       "message_created",
		MessageType: "outgoing",
		Private:     false,
		Content:     "Olá! Meu nome é Ana, como posso te ajudar?",
	}
	payloadReversoChatwoot.Conversation.ContactInbox.SourceID = "5548988003260@s.whatsapp.net"

	bodyCW, _ := json.Marshal(payloadReversoChatwoot)
	reqCW := httptest.NewRequest(http.MethodPost, "/api/v1/integracoes/chatwoot/webhook?instancia_id=inst_demo_brasil", bytes.NewBuffer(bodyCW))
	reqCW.Header.Set("Content-Type", "application/json")
	recCW := httptest.NewRecorder()
	handler.ServeHTTP(recCW, reqCW)

	if recCW.Code != http.StatusOK {
		t.Fatalf("Esperava status 200 no webhook reverso do Chatwoot, obteve %d: %s", recCW.Code, recCW.Body.String())
	}
}
