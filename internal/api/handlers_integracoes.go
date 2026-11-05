package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"context"
	"encoding/json"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/internal/database"
	"go.mau.fi/whatsmeow/internal/integracoes"
)

func autenticarWebhookIntegracao(w http.ResponseWriter, r *http.Request, instanciaID string, corpo []byte) bool {
	if instanciaID == "" || database.DB == nil {
		responderErro(w, http.StatusUnauthorized, "instancia_id e autenticação do webhook são obrigatórios")
		return false
	}
	inst, err := database.DB.BuscarInstanciaPorId(instanciaID)
	if err != nil || inst == nil {
		responderErro(w, http.StatusNotFound, "instância não encontrada")
		return false
	}
	if inst.WebhookSegredo != "" {
		assinatura := strings.TrimSpace(r.Header.Get("X-WooHub-Signature-SHA256"))
		if assinatura == "" {
			assinatura = strings.TrimSpace(r.Header.Get("X-Hub-Signature-256"))
		}
		mac := hmac.New(sha256.New, []byte(inst.WebhookSegredo))
		_, _ = mac.Write(corpo)
		expectativa := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(strings.TrimPrefix(assinatura, "sha256=")), []byte(expectativa)) {
			responderErro(w, http.StatusUnauthorized, "assinatura de webhook inválida")
			return false
		}
	}
	return true
}

// HandlerRoteamentoIntegracoes despacha requisições da rota /api/v1/instancia/{id}/integracoes[/...]
func HandlerRoteamentoIntegracoes(w http.ResponseWriter, r *http.Request) {
	partes := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// Esperado: ["api", "v1", "instancia", "{id}", "integracoes", ...]
	if len(partes) < 5 {
		responderErro(w, http.StatusBadRequest, "Caminho de integração inválido")
		return
	}

	instanciaID := partes[3]

	// Validação de acesso à instância
	_, ok := validarPropriedadeInstanciaPorID(w, r, instanciaID)
	if !ok {
		return
	}

	// 1. GET /api/v1/instancia/{id}/integracoes -> Listar todas as caixas
	if len(partes) == 5 {
		if r.Method != http.MethodGet {
			responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
			return
		}
		listarTodasIntegracoesInstancia(w, r, instanciaID)
		return
	}

	tipo := strings.ToLower(partes[5])

	// 2. Teste de Conexão: POST /api/v1/instancia/{id}/integracoes/{tipo}/testar
	if len(partes) >= 7 && partes[6] == "testar" {
		if r.Method != http.MethodPost {
			responderErro(w, http.StatusMethodNotAllowed, "Método deve ser POST")
			return
		}
		testarIntegracao(w, r, tipo)
		return
	}

	// 3. Obter ou Salvar Configurações da Caixa
	switch r.Method {
	case http.MethodGet:
		obterDetalhesIntegracao(w, r, instanciaID, tipo)
	case http.MethodPost, http.MethodPut:
		salvarIntegracao(w, r, instanciaID, tipo)
	default:
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
	}
}

func listarTodasIntegracoesInstancia(w http.ResponseWriter, r *http.Request, instanciaID string) {
	salvas, err := database.DB.ObterIntegracoesInstancia(instanciaID)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, "Erro ao consultar integrações")
		return
	}

	mapaSalvas := make(map[string]database.IntegracaoInstancia)
	for _, it := range salvas {
		mapaSalvas[it.Tipo] = it
	}

	caixasDisponiveis := []map[string]interface{}{
		{
			"tipo":        "chatwoot",
			"nome":        "Chatwoot",
			"descricao":   "Plataforma completa de Atendimento Humano Multicanal (Livechat)",
			"cor":         "#1F93FF",
			"icone":       "headset",
			"ativo":       false,
			"configurado": false,
		},
		{
			"tipo":        "typebot",
			"nome":        "Typebot",
			"descricao":   "Construtor visual de chatbots, enquetes e fluxos interativos",
			"cor":         "#0042DA",
			"icone":       "robot",
			"ativo":       false,
			"configurado": false,
		},
		{
			"tipo":        "n8n",
			"nome":        "n8n",
			"descricao":   "Orquestrador de automação de fluxos, nós avançados e Webhooks",
			"cor":         "#EA4B71",
			"icone":       "git-merge",
			"ativo":       false,
			"configurado": false,
		},
		{
			"tipo":        "kommo",
			"nome":        "Kommo CRM",
			"descricao":   "CRM de vendas líder (antigo amoCRM) com gestão de leads e funil",
			"cor":         "#0055FE",
			"icone":       "trello",
			"ativo":       false,
			"configurado": false,
		},
		{
			"tipo":        "rdstation",
			"nome":        "RD Station",
			"descricao":   "Líder em automação de Marketing & CRM no Brasil",
			"cor":         "#00A868",
			"icone":       "target",
			"ativo":       false,
			"configurado": false,
		},
		{
			"tipo":        "webhook",
			"nome":        "Webhooks Universais",
			"descricao":   "Múltiplos destinos com filtros granulares de eventos e assinatura SHA256",
			"cor":         "#10B981",
			"icone":       "zap",
			"ativo":       false,
			"configurado": false,
		},
	}

	for _, c := range caixasDisponiveis {
		t := c["tipo"].(string)
		if salva, ok := mapaSalvas[t]; ok {
			c["ativo"] = salva.Ativo
			c["configurado"] = strings.TrimSpace(salva.Configuracoes) != "" && salva.Configuracoes != "{}"
			c["atualizado_em"] = salva.AtualizadoEm
		}
	}

	responderJSON(w, http.StatusOK, map[string]interface{}{
		"instancia_id": instanciaID,
		"caixas":       caixasDisponiveis,
	})
}

func obterDetalhesIntegracao(w http.ResponseWriter, r *http.Request, instanciaID, tipo string) {
	intg, err := database.DB.ObterIntegracaoPorTipo(instanciaID, tipo)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, "Erro ao obter detalhes da integração")
		return
	}

	if intg == nil {
		responderJSON(w, http.StatusOK, map[string]interface{}{
			"instancia_id":  instanciaID,
			"tipo":          tipo,
			"ativo":         false,
			"configuracoes": "{}",
		})
		return
	}

	responderJSON(w, http.StatusOK, intg)
}

type RequisicaoSalvarIntegracao struct {
	Ativo         bool        `json:"ativo"`
	Configuracoes interface{} `json:"configuracoes"`
}

func salvarIntegracao(w http.ResponseWriter, r *http.Request, instanciaID, tipo string) {
	var req RequisicaoSalvarIntegracao
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo da requisição JSON inválido")
		return
	}

	var configStr string
	if str, ok := req.Configuracoes.(string); ok {
		configStr = str
	} else {
		b, _ := json.Marshal(req.Configuracoes)
		configStr = string(b)
	}

	salva, err := database.DB.SalvarIntegracaoInstancia(instanciaID, tipo, req.Ativo, configStr)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, "Falha ao salvar integração: "+err.Error())
		return
	}

	responderJSON(w, http.StatusOK, map[string]interface{}{
		"mensagem":   "Integração atualizada com sucesso",
		"integracao": salva,
	})
}

func testarIntegracao(w http.ResponseWriter, r *http.Request, tipo string) {
	var req struct {
		Configuracoes interface{} `json:"configuracoes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responderErro(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	var configStr string
	if str, ok := req.Configuracoes.(string); ok {
		configStr = str
	} else {
		b, _ := json.Marshal(req.Configuracoes)
		configStr = string(b)
	}

	if integracoes.InstanciaGerenciador == nil {
		responderErro(w, http.StatusServiceUnavailable, "Gerenciador de integrações não inicializado")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	resultado, err := integracoes.InstanciaGerenciador.TestarConexao(ctx, tipo, configStr)
	if err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}

	responderJSON(w, http.StatusOK, resultado)
}

// HandlerListarAtribuicoesInstancia retorna as origens rastreadas (Meta Ads, Google Ads, UTMs) dos contatos
func HandlerListarAtribuicoesInstancia(w http.ResponseWriter, r *http.Request) {
	partes := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(partes) < 4 {
		responderErro(w, http.StatusBadRequest, "ID da instância ausente")
		return
	}
	instanciaID := partes[3]

	_, ok := validarPropriedadeInstanciaPorID(w, r, instanciaID)
	if !ok {
		return
	}

	limite := 50
	if lStr := r.URL.Query().Get("limite"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limite = l
		}
	}

	lista, err := database.DB.ListarAtribuicoesInstancia(instanciaID, limite)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, "Erro ao listar atribuições de campanha")
		return
	}

	responderJSON(w, http.StatusOK, map[string]interface{}{
		"instancia_id": instanciaID,
		"total":        len(lista),
		"atribuicoes":  lista,
	})
}

// --- Webhooks Receptores Nativos (Callbacks Externos) ---

// HandlerWebhookReversoChatwoot recebe respostas dos atendentes no Chatwoot e dispara no WhatsApp
func HandlerWebhookReversoChatwoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responderErro(w, http.StatusMethodNotAllowed, "Método deve ser POST")
		return
	}

	instanciaID := r.URL.Query().Get("instancia_id")
	if instanciaID == "" {
		instanciaID = r.Header.Get("X-Instancia-ID")
	}

	corpoBytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limiteBodyJSONPadrao))
	if err != nil || len(corpoBytes) == 0 {
		responderErro(w, http.StatusBadRequest, "JSON do Chatwoot inválido")
		return
	}
	if !autenticarWebhookIntegracao(w, r, instanciaID, corpoBytes) { return }
	var corpo integracoes.PayloadWebhookChatwoot
	if err := json.Unmarshal(corpoBytes, &corpo); err != nil {
		responderErro(w, http.StatusBadRequest, "JSON do Chatwoot inválido")
		return
	}

	if integracoes.InstanciaGerenciador == nil {
		responderErro(w, http.StatusServiceUnavailable, "Hub de integrações offline")
		return
	}

	// Se a instância não veio na query/header, tenta inferir pela primeira instância ativa conectada
	if instanciaID == "" {
		instancias, err := database.DB.ListarInstancias()
		if err == nil && len(instancias) > 0 {
			for _, it := range instancias {
				if it.Status == "CONECTADO" {
					instanciaID = it.ID
					break
				}
			}
			if instanciaID == "" {
				instanciaID = instancias[0].ID
			}
		}
	}

	err = integracoes.InstanciaGerenciador.ProcessarWebhookReversoChatwoot(r.Context(), instanciaID, corpo)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, "Falha ao despachar para WhatsApp: "+err.Error())
		return
	}

	responderJSON(w, http.StatusOK, map[string]string{"status": "sucesso", "mensagem": "Mensagem repassada para o WhatsApp"})
}

// HandlerWebhookReversoKommo recebe automações de funil do Kommo CRM
func HandlerWebhookReversoKommo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responderErro(w, http.StatusMethodNotAllowed, "Método deve ser POST")
		return
	}

	instanciaID := r.URL.Query().Get("instancia_id")
	telefone := r.URL.Query().Get("telefone")
	mensagem := r.URL.Query().Get("mensagem")
	var corpoBytes []byte

	if telefone == "" || mensagem == "" {
		var corpo struct {
			InstanciaID string `json:"instancia_id"`
			Telefone    string `json:"telefone"`
			Mensagem    string `json:"mensagem"`
		}
		corpoBytes, _ = io.ReadAll(http.MaxBytesReader(w, r.Body, limiteBodyJSONPadrao))
		if json.Unmarshal(corpoBytes, &corpo) == nil {
			if corpo.InstanciaID != "" {
				instanciaID = corpo.InstanciaID
			}
			telefone = corpo.Telefone
			mensagem = corpo.Mensagem
		}
	}
	if len(corpoBytes) == 0 { corpoBytes = []byte(r.URL.RawQuery+"|"+telefone+"|"+mensagem) }
	if !autenticarWebhookIntegracao(w, r, instanciaID, corpoBytes) { return }

	if telefone == "" || mensagem == "" {
		responderErro(w, http.StatusBadRequest, "Parâmetros 'telefone' e 'mensagem' são obrigatórios")
		return
	}

	err := integracoes.InstanciaGerenciador.ProcessarWebhookReversoKommo(r.Context(), instanciaID, telefone, mensagem)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderJSON(w, http.StatusOK, map[string]string{"status": "sucesso", "mensagem": "Disparo realizado pelo Kommo"})
}

// HandlerWebhookReversoRDStation recebe disparos de réguas de relacionamento do RD Station
func HandlerWebhookReversoRDStation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responderErro(w, http.StatusMethodNotAllowed, "Método deve ser POST")
		return
	}

	instanciaID := r.URL.Query().Get("instancia_id")
	var corpo struct {
		InstanciaID string                 `json:"instancia_id"`
		Telefone    string                 `json:"telefone"`
		Mensagem    string                 `json:"mensagem"`
		Lead        map[string]interface{} `json:"lead"`
	}

	corpoBytes, errLeitura := io.ReadAll(http.MaxBytesReader(w, r.Body, limiteBodyJSONPadrao))
	if errLeitura != nil || json.Unmarshal(corpoBytes, &corpo) != nil {
		responderErro(w, http.StatusBadRequest, "JSON do RD Station inválido")
		return
	}
	if corpo.InstanciaID != "" {
		instanciaID = corpo.InstanciaID
	}
	telefone := corpo.Telefone
	if telefone == "" && corpo.Lead != nil {
		if tel, ok := corpo.Lead["mobile_phone"].(string); ok {
			telefone = tel
		}
	}

	if telefone == "" || corpo.Mensagem == "" {
		responderErro(w, http.StatusBadRequest, "Telefone ou mensagem não fornecidos pelo RD Station")
		return
	}
	if !autenticarWebhookIntegracao(w, r, instanciaID, corpoBytes) { return }

	err := integracoes.InstanciaGerenciador.ProcessarWebhookReversoRDStation(r.Context(), instanciaID, telefone, corpo.Mensagem)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderJSON(w, http.StatusOK, map[string]string{"status": "sucesso", "mensagem": "Disparo realizado pelo RD Station"})
}
