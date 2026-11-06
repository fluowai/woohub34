package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/internal/config"
	"go.mau.fi/whatsmeow/internal/core"
	"go.mau.fi/whatsmeow/internal/database"
)

// RecursosPlanoChat define os super-poderes desbloqueados na interface WhatsApp Web por plano
type RecursosPlanoChat struct {
	PlanoID             string `json:"plano_id"`
	NomePlano           string `json:"nome_plano"`
	RespostasRapidas    bool   `json:"respostas_rapidas"`
	MaxRespostasRapidas int    `json:"max_respostas_rapidas"`
	BotoesInterativos   bool   `json:"botoes_interativos"`
	Enquetes            bool   `json:"enquetes"`
	Agendamento         bool   `json:"agendamento"`
	TranscricaoIA       bool   `json:"transcricao_ia"`
	CopilotIA           bool   `json:"copilot_ia"`
	GhostMode           bool   `json:"ghost_mode"`
	DisparoMassa        bool   `json:"disparo_massa"`
	MaxAtendentes       int    `json:"max_atendentes"`
}

// ObterRecursosPorPlano mapeia os planos comerciais para seus recursos
func ObterRecursosPorPlano(planoNome string) RecursosPlanoChat {
	p := strings.ToLower(planoNome)
	switch {
	case strings.Contains(p, "enterprise"), strings.Contains(p, "revend"), strings.Contains(p, "stack"), strings.Contains(p, "ilimitad"):
		return RecursosPlanoChat{
			PlanoID:             "enterprise",
			NomePlano:           "Enterprise / White-Label",
			RespostasRapidas:    true,
			MaxRespostasRapidas: 9999,
			BotoesInterativos:   true,
			Enquetes:            true,
			Agendamento:         true,
			TranscricaoIA:       true,
			CopilotIA:           true,
			GhostMode:           true,
			DisparoMassa:        true,
			MaxAtendentes:       999,
		}
	case strings.Contains(p, "pro"), strings.Contains(p, "business"), strings.Contains(p, "profis"):
		return RecursosPlanoChat{
			PlanoID:             "profissional",
			NomePlano:           "Business Pro",
			RespostasRapidas:    true,
			MaxRespostasRapidas: 50,
			BotoesInterativos:   true,
			Enquetes:            true,
			Agendamento:         true,
			TranscricaoIA:       true,
			CopilotIA:           true,
			GhostMode:           true,
			DisparoMassa:        true,
			MaxAtendentes:       5,
		}
	default:
		// Plano Starter / Básico
		return RecursosPlanoChat{
			PlanoID:             "basico",
			NomePlano:           "Starter / Básico",
			RespostasRapidas:    true,
			MaxRespostasRapidas: 5,
			BotoesInterativos:   false,
			Enquetes:            true,
			Agendamento:         false,
			TranscricaoIA:       false,
			CopilotIA:           false,
			GhostMode:           false,
			DisparoMassa:        false,
			MaxAtendentes:       1,
		}
	}
}

// HandlerRecursosPlanoChat retorna os recursos permitidos para a conta logada
func HandlerRecursosPlanoChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}
	cli, _ := validarAutenticacao(r)
	plano := "Starter / Básico"
	if cli != nil && cli.Papel == database.PapelSuperAdmin {
		plano = "Enterprise / White-Label"
	} else if cli != nil && cli.LimiteInstancias > 5 {
		plano = "Business Pro"
	}

	recursos := ObterRecursosPorPlano(plano)
	responderJSON(w, http.StatusOK, map[string]interface{}{
		"sucesso":  true,
		"recursos": recursos,
	})
}

// HandlerListarConversasChat retorna a lista de conversas da instância
func HandlerListarConversasChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}
	instanciaID := r.URL.Query().Get("instancia_id")
	if instanciaID == "" {
		responderErro(w, http.StatusBadRequest, "Parâmetro 'instancia_id' é obrigatório")
		return
	}

	if _, ok := validarPropriedadeInstanciaPorID(w, r, instanciaID); !ok {
		return
	}

	conversas, err := database.DB.ListarConversasChat(instanciaID, 150)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, "Erro ao listar conversas: "+err.Error())
		return
	}

	// Se a lista de conversas do chat estiver vazia, sincroniza com os contatos salvos no SQLite do whatsmeow
	if len(conversas) == 0 && core.Gerenciador != nil {
		if inst, _ := core.Gerenciador.ObterInstanciaAtiva(instanciaID); inst != nil && inst.Cliente != nil && inst.Cliente.Store != nil {
			if contatos, errCont := inst.Cliente.Store.Contacts.GetAllContacts(r.Context()); errCont == nil {
				count := 0
				for jid, info := range contatos {
					if jid.Server == "s.whatsapp.net" && count < 30 {
						nome := info.PushName
						if nome == "" {
							nome = info.FullName
						}
						if nome == "" {
							nome = jid.User
						}
						c, _ := database.DB.ObterOuCriarConversaChat(instanciaID, jid.String(), nome, false)
						if c != nil {
							conversas = append(conversas, *c)
							count++
						}
					}
				}
			}
		}
	}

	responderJSON(w, http.StatusOK, map[string]interface{}{
		"sucesso":   true,
		"total":     len(conversas),
		"conversas": conversas,
	})
}

// HandlerListarMensagensChat retorna o histórico de mensagens de uma conversa
func HandlerListarMensagensChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}
	instanciaID := r.URL.Query().Get("instancia_id")
	chatJID := r.URL.Query().Get("chat_jid")

	if instanciaID == "" || chatJID == "" {
		responderErro(w, http.StatusBadRequest, "Parâmetros 'instancia_id' e 'chat_jid' são obrigatórios")
		return
	}

	if _, ok := validarPropriedadeInstanciaPorID(w, r, instanciaID); !ok {
		return
	}

	mensagens, err := database.DB.ListarMensagensChat(instanciaID, chatJID, 200)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, "Erro ao listar mensagens: "+err.Error())
		return
	}

	// Zera contador de não lidas ao abrir a conversa
	_ = database.DB.ZerarNaoLidasConversa(instanciaID, chatJID)

	responderJSON(w, http.StatusOK, map[string]interface{}{
		"sucesso":   true,
		"chat_jid":  chatJID,
		"total":     len(mensagens),
		"mensagens": mensagens,
	})
}

// HandlerStreamChatWeb estabelece conexão SSE para receber mensagens em tempo real
func HandlerStreamChatWeb(w http.ResponseWriter, r *http.Request) {
	instanciaID := r.URL.Query().Get("instancia_id")
	if instanciaID == "" {
		responderErro(w, http.StatusBadRequest, "Parâmetro 'instancia_id' é obrigatório")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		responderErro(w, http.StatusInternalServerError, "Streaming não suportado pelo servidor")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	canal := core.HubChatGlobal.Assinar(instanciaID)
	defer core.HubChatGlobal.Desassinar(instanciaID, canal)

	// Notificação inicial de conexão estabelecida
	fmt.Fprintf(w, "data: {\"tipo\":\"conectado\",\"instancia_id\":\"%s\",\"timestamp\":%d}\n\n", instanciaID, time.Now().Unix())
	flusher.Flush()

	tickerHeartbeat := time.NewTicker(20 * time.Second)
	defer tickerHeartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tickerHeartbeat.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case evt, aberta := <-canal:
			if !aberta {
				return
			}
			bytesJSON, err := json.Marshal(evt)
			if err == nil {
				fmt.Fprintf(w, "data: %s\n\n", string(bytesJSON))
				flusher.Flush()
			}
		}
	}
}

// HandlerMarcarConversaLida marca mensagens como lidas na conversa
func HandlerMarcarConversaLida(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}
	var body struct {
		InstanciaID string `json:"instancia_id"`
		ChatJID     string `json:"chat_jid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responderErro(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if _, ok := validarPropriedadeInstanciaPorID(w, r, body.InstanciaID); !ok {
		return
	}

	_ = database.DB.ZerarNaoLidasConversa(body.InstanciaID, body.ChatJID)
	responderJSON(w, http.StatusOK, map[string]interface{}{
		"sucesso":  true,
		"mensagem": "Conversa marcada como lida",
	})
}

// HandlerListarRespostasRapidas lista atalhos cadastrados
func HandlerListarRespostasRapidas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}
	cli, _ := validarAutenticacao(r)
	clienteID := "cli_demo_admin"
	if cli != nil {
		clienteID = cli.ID
	}

	lista, err := database.DB.ListarRespostasRapidas(clienteID)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, "Erro ao buscar respostas rápidas: "+err.Error())
		return
	}

	responderJSON(w, http.StatusOK, map[string]interface{}{
		"sucesso": true,
		"total":   len(lista),
		"atalhos": lista,
	})
}

// HandlerSalvarRespostaRapida cadastra resposta rápida respeitando limite do plano
func HandlerSalvarRespostaRapida(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}
	cli, _ := validarAutenticacao(r)
	clienteID := "cli_demo_admin"
	plano := "Starter / Básico"
	if cli != nil {
		clienteID = cli.ID
		if cli.Papel == database.PapelSuperAdmin {
			plano = "Enterprise / White-Label"
		} else if cli.LimiteInstancias > 5 {
			plano = "Business Pro"
		}
	}

	recursos := ObterRecursosPorPlano(plano)

	var body struct {
		ID        string `json:"id"`
		Atalho    string `json:"atalho"`
		Texto     string `json:"texto"`
		Categoria string `json:"categoria"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responderErro(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	body.Atalho = strings.TrimSpace(body.Atalho)
	body.Texto = strings.TrimSpace(body.Texto)
	if body.Atalho == "" || body.Texto == "" {
		responderErro(w, http.StatusBadRequest, "Atalho e texto são obrigatórios")
		return
	}
	if !strings.HasPrefix(body.Atalho, "/") {
		body.Atalho = "/" + body.Atalho
	}

	// Validação de limite por plano
	existentes, _ := database.DB.ListarRespostasRapidas(clienteID)
	if len(existentes) >= recursos.MaxRespostasRapidas && body.ID == "" {
		responderErro(w, http.StatusForbidden, fmt.Sprintf("Seu plano (%s) permite até %d respostas rápidas. Faça upgrade para aumentar o limite.", recursos.NomePlano, recursos.MaxRespostasRapidas))
		return
	}

	resp := database.RespostaRapidaChat{
		ID:        body.ID,
		ClienteID: clienteID,
		Atalho:    body.Atalho,
		Texto:     body.Texto,
		Categoria: body.Categoria,
		CriadoEm:  time.Now(),
	}

	if err := database.DB.SalvarRespostaRapida(resp); err != nil {
		responderErro(w, http.StatusInternalServerError, "Erro ao salvar resposta rápida: "+err.Error())
		return
	}

	responderJSON(w, http.StatusOK, map[string]interface{}{
		"sucesso":  true,
		"mensagem": "Resposta rápida salva com sucesso",
		"atalho":   resp,
	})
}

// HandlerExcluirRespostaRapida remove um atalho
func HandlerExcluirRespostaRapida(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}
	cli, _ := validarAutenticacao(r)
	clienteID := "cli_demo_admin"
	if cli != nil {
		clienteID = cli.ID
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		var body struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		id = body.ID
	}
	if id == "" {
		responderErro(w, http.StatusBadRequest, "Parâmetro 'id' é obrigatório")
		return
	}

	if err := database.DB.ExcluirRespostaRapida(clienteID, id); err != nil {
		responderErro(w, http.StatusInternalServerError, "Erro ao excluir: "+err.Error())
		return
	}

	responderJSON(w, http.StatusOK, map[string]interface{}{
		"sucesso":  true,
		"mensagem": "Resposta rápida removida com sucesso",
	})
}

// HandlerMidiaLocal entrega arquivos de mídia locais baixados automaticamente
func HandlerMidiaLocal(w http.ResponseWriter, r *http.Request) {
	caminho := r.URL.Query().Get("path")
	if caminho == "" || strings.Contains(caminho, "..") {
		responderErro(w, http.StatusBadRequest, "Caminho inválido")
		return
	}

	dirMidia := "./armazenamento_midia"
	if config.AppConfig != nil && config.AppConfig.DiretorioMidia != "" {
		dirMidia = config.AppConfig.DiretorioMidia
	}

	caminhoCompleto := filepath.Join(dirMidia, filepath.Base(caminho))
	if _, err := os.Stat(caminhoCompleto); os.IsNotExist(err) {
		responderErro(w, http.StatusNotFound, "Mídia não encontrada")
		return
	}

	http.ServeFile(w, r, caminhoCompleto)
}