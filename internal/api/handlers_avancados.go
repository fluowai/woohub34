package api

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"go.mau.fi/whatsmeow/internal/core"
	"go.mau.fi/whatsmeow/internal/database"
)

// ============================================================================
// PACOTE 4: CONFIGURAÇÃO ANTI-BAN, PROXY E REJEIÇÃO DE CHAMADAS DA INSTÂNCIA
// ============================================================================

func HandlerConfigurarInstanciaAvancada(w http.ResponseWriter, r *http.Request) {
	id := extrairIdDaRota(r.URL.Path, "/api/v1/instancia/", "/configuracoes")
	instDB, ok := validarPropriedadeInstanciaPorID(w, r, id)
	if !ok {
		return
	}

	if r.Method == http.MethodGet {
		responderSucesso(w, "Configurações da instância recuperadas com sucesso", map[string]interface{}{
			"id":                         instDB.ID,
			"ignorar_grupos":             instDB.IgnorarGrupos,
			"rejeitar_chamadas":          instDB.RejeitarChamadas,
			"mensagem_chamada_rejeitada": instDB.MensagemChamadaRejeitar,
			"proxy_url":                  instDB.ProxyURL,
			"leitura_automatica":         instDB.LeituraAutomatica,
			"sempre_online":              instDB.SempreOnline,
		})
		return
	}

	var body core.ConfiguracaoInstanciaReq
	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido")
		return
	}

	if err := core.Gerenciador.ConfigurarInstanciaAvancada(r.Context(), id, body); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}

	atualizada, _ := database.DB.BuscarInstanciaPorId(id)
	responderSucesso(w, "Configurações Anti-Ban, Proxy e Atendimento salvas com sucesso!", atualizada)
}

// ============================================================================
// PACOTE 1: MENSAGERIA RICA, INTERATIVA E GESTÃO DE MENSAGENS
// ============================================================================

func HandlerEnviarVideo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID         string   `json:"instancia_id"`
		Destinatario        string   `json:"destinatario"`
		Numero              string   `json:"numero"`
		VideoBase64         string   `json:"video_base64"`
		Legenda             string   `json:"legenda"`
		ComoGif             bool     `json:"como_gif"`
		NotaVideoCircular   bool     `json:"nota_video_circular"`
		VisualizacaoUnica   bool     `json:"visualizacao_unica"`
		ResponderMensagemID string   `json:"responder_mensagem_id"`
		Mencionar           []string `json:"mencionar"`
		MencionarTodos      bool     `json:"mencionar_todos"`
		DelayDigitandoMs    int      `json:"delay_digitando_ms"`
		Simulacao           bool     `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyMidia, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido ou excede 25 MB")
		return
	}

	if body.Destinatario == "" && body.Numero != "" {
		body.Destinatario = body.Numero
	}
	body.Destinatario = strings.TrimSpace(body.Destinatario)
	if body.Destinatario == "" {
		responderErro(w, http.StatusBadRequest, "Destinatário é obrigatório")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		simID := fmt.Sprintf("sim_vid_%s", uuid.New().String()[:12])
		_ = database.DB.GravarLogMensagem(instDB.ID, simID, "video", body.Destinatario, "ENVIADA", "ENTREGUE (SIMULADO)", "")
		responderSucesso(w, "Vídeo simulado com sucesso!", map[string]string{
			"id_mensagem":  simID,
			"destinatario": core.FormatarNumeroBrasilVisual(body.Destinatario),
			"tipo":         "video",
			"status":       "ENTREGUE (SIMULADO)",
			"modo":         "simulacao",
		})
		return
	}

	if body.VideoBase64 == "" {
		responderErro(w, http.StatusBadRequest, "video_base64 é obrigatório")
		return
	}

	dadosVideo, err := base64.StdEncoding.DecodeString(limparHeaderBase64(body.VideoBase64))
	if err != nil {
		responderErro(w, http.StatusBadRequest, "Vídeo Base64 inválido")
		return
	}

	opcoes := &core.OpcoesEnvio{
		VisualizacaoUnica:   body.VisualizacaoUnica,
		ResponderMensagemID: body.ResponderMensagemID,
		Mencionar:           body.Mencionar,
		MencionarTodos:      body.MencionarTodos,
		DelayDigitandoMs:    body.DelayDigitandoMs,
	}

	msgID, err := core.Gerenciador.EnviarMensagemVideo(r.Context(), instDB.ID, body.Destinatario, dadosVideo, body.Legenda, body.ComoGif, body.NotaVideoCircular, opcoes)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Vídeo enviado com sucesso!", map[string]string{
		"id_mensagem": msgID,
		"tipo":        "video",
		"status":      "ENTREGUE",
		"modo":        "whatsapp_real",
	})
}

func HandlerEnviarDocumento(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID         string   `json:"instancia_id"`
		Destinatario        string   `json:"destinatario"`
		Numero              string   `json:"numero"`
		DocumentoBase64     string   `json:"documento_base64"`
		NomeArquivo         string   `json:"nome_arquivo"`
		Legenda             string   `json:"legenda"`
		Mimetype            string   `json:"mimetype"`
		ResponderMensagemID string   `json:"responder_mensagem_id"`
		Mencionar           []string `json:"mencionar"`
		MencionarTodos      bool     `json:"mencionar_todos"`
		DelayDigitandoMs    int      `json:"delay_digitando_ms"`
		Simulacao           bool     `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyMidia, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido ou excede 25 MB")
		return
	}

	if body.Destinatario == "" && body.Numero != "" {
		body.Destinatario = body.Numero
	}
	body.Destinatario = strings.TrimSpace(body.Destinatario)
	if body.Destinatario == "" {
		responderErro(w, http.StatusBadRequest, "Destinatário é obrigatório")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.NomeArquivo == "" {
		body.NomeArquivo = "documento.pdf"
	}

	if body.Simulacao {
		simID := fmt.Sprintf("sim_doc_%s", uuid.New().String()[:12])
		_ = database.DB.GravarLogMensagem(instDB.ID, simID, "documento", body.Destinatario, "ENVIADA", "ENTREGUE (SIMULADO)", "")
		responderSucesso(w, "Documento simulado com sucesso!", map[string]string{
			"id_mensagem":  simID,
			"destinatario": core.FormatarNumeroBrasilVisual(body.Destinatario),
			"nome_arquivo": body.NomeArquivo,
			"tipo":         "documento",
			"status":       "ENTREGUE (SIMULADO)",
			"modo":         "simulacao",
		})
		return
	}

	if body.DocumentoBase64 == "" {
		responderErro(w, http.StatusBadRequest, "documento_base64 é obrigatório")
		return
	}

	dadosDoc, err := base64.StdEncoding.DecodeString(limparHeaderBase64(body.DocumentoBase64))
	if err != nil {
		responderErro(w, http.StatusBadRequest, "Documento Base64 inválido")
		return
	}

	opcoes := &core.OpcoesEnvio{
		ResponderMensagemID: body.ResponderMensagemID,
		Mencionar:           body.Mencionar,
		MencionarTodos:      body.MencionarTodos,
		DelayDigitandoMs:    body.DelayDigitandoMs,
	}

	msgID, err := core.Gerenciador.EnviarMensagemDocumento(r.Context(), instDB.ID, body.Destinatario, dadosDoc, body.NomeArquivo, body.Legenda, body.Mimetype, opcoes)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Documento enviado com sucesso!", map[string]string{
		"id_mensagem":  msgID,
		"nome_arquivo": body.NomeArquivo,
		"tipo":         "documento",
		"status":       "ENTREGUE",
		"modo":         "whatsapp_real",
	})
}

func HandlerEnviarSticker(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID         string `json:"instancia_id"`
		Destinatario        string `json:"destinatario"`
		Numero              string `json:"numero"`
		StickerBase64       string `json:"sticker_base64"`
		Animado             bool   `json:"animado"`
		ResponderMensagemID string `json:"responder_mensagem_id"`
		Simulacao           bool   `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyMidia, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido")
		return
	}

	if body.Destinatario == "" && body.Numero != "" {
		body.Destinatario = body.Numero
	}
	body.Destinatario = strings.TrimSpace(body.Destinatario)
	if body.Destinatario == "" {
		responderErro(w, http.StatusBadRequest, "Destinatário é obrigatório")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		simID := fmt.Sprintf("sim_stk_%s", uuid.New().String()[:12])
		_ = database.DB.GravarLogMensagem(instDB.ID, simID, "sticker", body.Destinatario, "ENVIADA", "ENTREGUE (SIMULADO)", "")
		responderSucesso(w, "Figurinha (Sticker) simulada com sucesso!", map[string]string{
			"id_mensagem":  simID,
			"destinatario": core.FormatarNumeroBrasilVisual(body.Destinatario),
			"tipo":         "sticker",
			"status":       "ENTREGUE (SIMULADO)",
			"modo":         "simulacao",
		})
		return
	}

	if body.StickerBase64 == "" {
		responderErro(w, http.StatusBadRequest, "sticker_base64 é obrigatório")
		return
	}

	dadosSticker, err := base64.StdEncoding.DecodeString(limparHeaderBase64(body.StickerBase64))
	if err != nil {
		responderErro(w, http.StatusBadRequest, "Sticker Base64 inválido")
		return
	}

	opcoes := &core.OpcoesEnvio{ResponderMensagemID: body.ResponderMensagemID}
	msgID, err := core.Gerenciador.EnviarMensagemSticker(r.Context(), instDB.ID, body.Destinatario, dadosSticker, body.Animado, opcoes)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Figurinha enviada com sucesso!", map[string]string{
		"id_mensagem": msgID,
		"tipo":        "sticker",
		"status":      "ENTREGUE",
		"modo":        "whatsapp_real",
	})
}

func HandlerEnviarEnquete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID  string   `json:"instancia_id"`
		Destinatario string   `json:"destinatario"`
		Numero       string   `json:"numero"`
		Pergunta     string   `json:"pergunta"`
		Opcoes       []string `json:"opcoes"`
		MaxEscolhas  int      `json:"max_escolhas"`
		Simulacao    bool     `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido")
		return
	}

	if body.Destinatario == "" && body.Numero != "" {
		body.Destinatario = body.Numero
	}
	body.Destinatario = strings.TrimSpace(body.Destinatario)
	body.Pergunta = strings.TrimSpace(body.Pergunta)

	var opcoesLimpas []string
	for _, op := range body.Opcoes {
		if strings.TrimSpace(op) != "" {
			opcoesLimpas = append(opcoesLimpas, strings.TrimSpace(op))
		}
	}

	if body.Destinatario == "" || body.Pergunta == "" || len(opcoesLimpas) < 2 {
		responderErro(w, http.StatusBadRequest, "Destinatário, 'pergunta' e pelo menos 2 'opcoes' são obrigatórios")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		simID := fmt.Sprintf("sim_poll_%s", uuid.New().String()[:12])
		_ = database.DB.GravarLogMensagem(instDB.ID, simID, "enquete", body.Destinatario, "ENVIADA", "ENTREGUE (SIMULADO)", "")
		responderSucesso(w, "Enquete interativa simulada com sucesso!", map[string]interface{}{
			"id_mensagem":  simID,
			"destinatario": core.FormatarNumeroBrasilVisual(body.Destinatario),
			"pergunta":     body.Pergunta,
			"opcoes":       opcoesLimpas,
			"tipo":         "enquete",
			"status":       "ENTREGUE (SIMULADO)",
			"modo":         "simulacao",
		})
		return
	}

	msgID, err := core.Gerenciador.EnviarMensagemEnquete(r.Context(), instDB.ID, body.Destinatario, body.Pergunta, opcoesLimpas, body.MaxEscolhas)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Enquete interativa enviada com sucesso!", map[string]interface{}{
		"id_mensagem":  msgID,
		"destinatario": core.FormatarNumeroBrasilVisual(body.Destinatario),
		"pergunta":     body.Pergunta,
		"opcoes":       opcoesLimpas,
		"tipo":         "enquete",
		"status":       "ENTREGUE",
		"modo":         "whatsapp_real",
	})
}

// HandlerEnviarMensagemInterativa envia mensagem interativa com botões nativos (CTA URL, Pix Copia e Cola, Quick Reply)
func HandlerEnviarMensagemInterativa(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido. Utilize POST.")
		return
	}

	var body struct {
		InstanciaID  string                 `json:"instancia_id"`
		Destinatario string                 `json:"destinatario"`
		Numero       string                 `json:"numero"`
		Titulo       string                 `json:"titulo"`
		Corpo        string                 `json:"corpo"`
		Texto        string                 `json:"texto"`
		Rodape       string                 `json:"rodape"`
		Botoes       []core.BotaoInterativo `json:"botoes"`
		Opcoes       *core.OpcoesEnvio      `json:"opcoes"`
		Simulacao    bool                   `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido")
		return
	}

	if body.Destinatario == "" && body.Numero != "" {
		body.Destinatario = body.Numero
	}
	body.Destinatario = strings.TrimSpace(body.Destinatario)
	if body.Corpo == "" && body.Texto != "" {
		body.Corpo = body.Texto
	}
	body.Corpo = strings.TrimSpace(body.Corpo)
	body.Titulo = strings.TrimSpace(body.Titulo)
	body.Rodape = strings.TrimSpace(body.Rodape)

	if body.Destinatario == "" || (body.Corpo == "" && body.Titulo == "") || len(body.Botoes) == 0 {
		responderErro(w, http.StatusBadRequest, "Destinatário, corpo/texto e ao menos 1 botão são obrigatórios")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		simID := fmt.Sprintf("sim_btn_%s", uuid.New().String()[:12])
		_ = database.DB.GravarLogMensagem(instDB.ID, simID, "interativa", body.Destinatario, "ENVIADA", "ENTREGUE (SIMULADO)", "")
		responderSucesso(w, "Mensagem interativa com botões simulada com sucesso!", map[string]interface{}{
			"id_mensagem":  simID,
			"destinatario": core.FormatarNumeroBrasilVisual(body.Destinatario),
			"titulo":       body.Titulo,
			"corpo":        body.Corpo,
			"rodape":       body.Rodape,
			"botoes":       body.Botoes,
			"tipo":         "interativa",
			"status":       "ENTREGUE (SIMULADO)",
			"modo":         "simulacao",
		})
		return
	}

	msgID, err := core.Gerenciador.EnviarMensagemInterativa(r.Context(), instDB.ID, body.Destinatario, body.Titulo, body.Corpo, body.Rodape, body.Botoes, body.Opcoes)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Mensagem interativa com botões enviada com sucesso!", map[string]interface{}{
		"id_mensagem":  msgID,
		"destinatario": core.FormatarNumeroBrasilVisual(body.Destinatario),
		"titulo":       body.Titulo,
		"corpo":        body.Corpo,
		"rodape":       body.Rodape,
		"total_botoes": len(body.Botoes),
		"tipo":         "interativa",
		"status":       "ENTREGUE",
		"modo":         "whatsapp_real",
	})
}

func HandlerEnviarLocalizacao(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID         string  `json:"instancia_id"`
		Destinatario        string  `json:"destinatario"`
		Numero              string  `json:"numero"`
		Latitude            float64 `json:"latitude"`
		Longitude           float64 `json:"longitude"`
		Nome                string  `json:"nome"`
		NomeLocal           string  `json:"nome_local"`
		Endereco            string  `json:"endereco"`
		UrlMapa             string  `json:"url_mapa"`
		ResponderMensagemID string  `json:"responder_mensagem_id"`
		Simulacao           bool    `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido")
		return
	}

	if body.Nome == "" && body.NomeLocal != "" {
		body.Nome = body.NomeLocal
	}
	if body.Destinatario == "" && body.Numero != "" {
		body.Destinatario = body.Numero
	}
	body.Destinatario = strings.TrimSpace(body.Destinatario)
	if body.Destinatario == "" || (body.Latitude == 0 && body.Longitude == 0) {
		responderErro(w, http.StatusBadRequest, "Destinatário, 'latitude' e 'longitude' são obrigatórios")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		simID := fmt.Sprintf("sim_loc_%s", uuid.New().String()[:12])
		_ = database.DB.GravarLogMensagem(instDB.ID, simID, "localizacao", body.Destinatario, "ENVIADA", "ENTREGUE (SIMULADO)", "")
		responderSucesso(w, "Localização simulada com sucesso!", map[string]interface{}{
			"id_mensagem":  simID,
			"destinatario": core.FormatarNumeroBrasilVisual(body.Destinatario),
			"latitude":     body.Latitude,
			"longitude":    body.Longitude,
			"nome":         body.Nome,
			"tipo":         "localizacao",
			"status":       "ENTREGUE (SIMULADO)",
			"modo":         "simulacao",
		})
		return
	}

	opcoes := &core.OpcoesEnvio{ResponderMensagemID: body.ResponderMensagemID}
	msgID, err := core.Gerenciador.EnviarMensagemLocalizacao(r.Context(), instDB.ID, body.Destinatario, body.Latitude, body.Longitude, body.Nome, body.Endereco, body.UrlMapa, opcoes)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Localização enviada com sucesso!", map[string]interface{}{
		"id_mensagem":  msgID,
		"destinatario": core.FormatarNumeroBrasilVisual(body.Destinatario),
		"tipo":         "localizacao",
		"status":       "ENTREGUE",
		"modo":         "whatsapp_real",
	})
}

func HandlerEnviarContato(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID         string               `json:"instancia_id"`
		Destinatario        string               `json:"destinatario"`
		Numero              string               `json:"numero"`
		Nome                string               `json:"nome"`
		NomeContato         string               `json:"nome_contato"`
		Telefone            string               `json:"telefone"`
		Empresa             string               `json:"empresa"`
		Email               string               `json:"email"`
		Vcard               string               `json:"vcard"`
		Contatos            []core.ContatoCartao `json:"contatos"`
		ResponderMensagemID string               `json:"responder_mensagem_id"`
		Simulacao           bool                 `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido")
		return
	}

	if body.Nome == "" && body.NomeContato != "" {
		body.Nome = body.NomeContato
	}
	if body.Destinatario == "" && body.Numero != "" {
		body.Destinatario = body.Numero
	}
	body.Destinatario = strings.TrimSpace(body.Destinatario)

	contatos := body.Contatos
	if len(contatos) == 0 && (body.Nome != "" || body.Telefone != "" || body.Vcard != "") {
		contatos = append(contatos, core.ContatoCartao{
			Nome:     body.Nome,
			Telefone: body.Telefone,
			Empresa:  body.Empresa,
			Email:    body.Email,
			Vcard:    body.Vcard,
		})
	}

	if body.Destinatario == "" || len(contatos) == 0 {
		responderErro(w, http.StatusBadRequest, "Destinatário e dados do contato ('nome' + 'telefone' ou 'contatos') são obrigatórios")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		simID := fmt.Sprintf("sim_cnt_%s", uuid.New().String()[:12])
		_ = database.DB.GravarLogMensagem(instDB.ID, simID, "contato", body.Destinatario, "ENVIADA", "ENTREGUE (SIMULADO)", "")
		responderSucesso(w, "Cartão de contato (vCard) simulado com sucesso!", map[string]interface{}{
			"id_mensagem":  simID,
			"destinatario": core.FormatarNumeroBrasilVisual(body.Destinatario),
			"tipo":         "contato",
			"status":       "ENTREGUE (SIMULADO)",
			"modo":         "simulacao",
		})
		return
	}

	opcoes := &core.OpcoesEnvio{ResponderMensagemID: body.ResponderMensagemID}
	msgID, err := core.Gerenciador.EnviarMensagemContato(r.Context(), instDB.ID, body.Destinatario, contatos, opcoes)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Cartão de contato enviado com sucesso!", map[string]string{
		"id_mensagem":  msgID,
		"destinatario": core.FormatarNumeroBrasilVisual(body.Destinatario),
		"tipo":         "contato",
		"status":       "ENTREGUE",
		"modo":         "whatsapp_real",
	})
}

func HandlerEnviarReacao(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID    string `json:"instancia_id"`
		Destinatario   string `json:"destinatario"`
		Numero         string `json:"numero"`
		Remetente      string `json:"remetente"`
		IDMensagem     string `json:"id_mensagem"`
		IDMensagemAlvo string `json:"id_mensagem_alvo"`
		Emoji          string `json:"emoji"`
		Simulacao      bool   `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido")
		return
	}

	if body.IDMensagem == "" && body.IDMensagemAlvo != "" {
		body.IDMensagem = body.IDMensagemAlvo
	}
	if body.Destinatario == "" && body.Numero != "" {
		body.Destinatario = body.Numero
	}
	body.Destinatario = strings.TrimSpace(body.Destinatario)
	if body.Destinatario == "" || strings.TrimSpace(body.IDMensagem) == "" {
		responderErro(w, http.StatusBadRequest, "Destinatário e 'id_mensagem' são obrigatórios")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		simID := fmt.Sprintf("sim_react_%s", uuid.New().String()[:12])
		_ = database.DB.GravarLogMensagem(instDB.ID, simID, "reacao", body.Destinatario, "ENVIADA", "ENTREGUE (SIMULADO)", "")
		responderSucesso(w, "Reação simulada com sucesso!", map[string]string{
			"id_mensagem":      simID,
			"id_mensagem_alvo": body.IDMensagem,
			"emoji":            body.Emoji,
			"status":           "ENTREGUE (SIMULADO)",
			"modo":             "simulacao",
		})
		return
	}

	msgID, err := core.Gerenciador.EnviarReacao(r.Context(), instDB.ID, body.Destinatario, body.Remetente, body.IDMensagem, body.Emoji)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Reação enviada com sucesso!", map[string]string{
		"id_mensagem":      msgID,
		"id_mensagem_alvo": body.IDMensagem,
		"emoji":            body.Emoji,
		"status":           "ENTREGUE",
	})
}

func HandlerEditarMensagem(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID    string `json:"instancia_id"`
		Destinatario   string `json:"destinatario"`
		Numero         string `json:"numero"`
		IDMensagem     string `json:"id_mensagem"`
		IDMensagemAlvo string `json:"id_mensagem_alvo"`
		NovaMensagem   string `json:"nova_mensagem"`
		NovoTexto      string `json:"novo_texto"`
		Simulacao      bool   `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido")
		return
	}

	if body.IDMensagem == "" && body.IDMensagemAlvo != "" {
		body.IDMensagem = body.IDMensagemAlvo
	}
	if body.NovaMensagem == "" && body.NovoTexto != "" {
		body.NovaMensagem = body.NovoTexto
	}
	if body.Destinatario == "" && body.Numero != "" {
		body.Destinatario = body.Numero
	}
	body.Destinatario = strings.TrimSpace(body.Destinatario)
	if body.Destinatario == "" || strings.TrimSpace(body.IDMensagem) == "" || strings.TrimSpace(body.NovaMensagem) == "" {
		responderErro(w, http.StatusBadRequest, "Destinatário, 'id_mensagem' e 'nova_mensagem' são obrigatórios")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		_ = database.DB.GravarLogMensagem(instDB.ID, body.IDMensagem, "edicao", body.Destinatario, "ENVIADA", "ENTREGUE (SIMULADO)", "")
		responderSucesso(w, "Edição de mensagem simulada com sucesso!", map[string]string{
			"id_mensagem":   body.IDMensagem,
			"nova_mensagem": body.NovaMensagem,
			"status":        "ENTREGUE (SIMULADO)",
			"modo":          "simulacao",
		})
		return
	}

	msgID, err := core.Gerenciador.EditarMensagem(r.Context(), instDB.ID, body.Destinatario, body.IDMensagem, body.NovaMensagem)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Mensagem editada com sucesso!", map[string]string{
		"id_mensagem": msgID,
		"status":      "EDITADA",
	})
}

func HandlerRevogarMensagem(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID    string `json:"instancia_id"`
		Destinatario   string `json:"destinatario"`
		Numero         string `json:"numero"`
		Remetente      string `json:"remetente"`
		IDMensagem     string `json:"id_mensagem"`
		IDMensagemAlvo string `json:"id_mensagem_alvo"`
		Simulacao      bool   `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido")
		return
	}

	if body.IDMensagem == "" && body.IDMensagemAlvo != "" {
		body.IDMensagem = body.IDMensagemAlvo
	}

	if body.Destinatario == "" && body.Numero != "" {
		body.Destinatario = body.Numero
	}
	body.Destinatario = strings.TrimSpace(body.Destinatario)
	if body.Destinatario == "" || strings.TrimSpace(body.IDMensagem) == "" {
		responderErro(w, http.StatusBadRequest, "Destinatário e 'id_mensagem' são obrigatórios")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		_ = database.DB.GravarLogMensagem(instDB.ID, body.IDMensagem, "revogacao", body.Destinatario, "ENVIADA", "ENTREGUE (SIMULADO)", "")
		responderSucesso(w, "Exclusão de mensagem para todos simulada com sucesso!", map[string]string{
			"id_mensagem_apagada": body.IDMensagem,
			"status":              "APAGADA (SIMULADO)",
			"modo":                "simulacao",
		})
		return
	}

	msgID, err := core.Gerenciador.RevogarMensagem(r.Context(), instDB.ID, body.Destinatario, body.Remetente, body.IDMensagem)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Mensagem apagada para todos com sucesso!", map[string]string{
		"id_protocolo":        msgID,
		"id_mensagem_apagada": body.IDMensagem,
		"status":              "APAGADA",
	})
}

// ============================================================================
// PACOTE 2: VALIDADOR DE NÚMEROS BR, PERFIL, PRESENÇA, LEITURA E ETIQUETAS
// ============================================================================

func HandlerVerificarNumeros(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID string   `json:"instancia_id"`
		Numero      string   `json:"numero"`
		Numeros     []string `json:"numeros"`
		Simulacao   bool     `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido")
		return
	}

	lista := body.Numeros
	if len(lista) == 0 && strings.TrimSpace(body.Numero) != "" {
		lista = append(lista, strings.TrimSpace(body.Numero))
	}
	if len(lista) == 0 {
		responderErro(w, http.StatusBadRequest, "Informe 'numero' ou uma lista em 'numeros'")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		var simRes []map[string]interface{}
		for _, n := range lista {
			limpo, err := core.NormalizarNumeroBrasil(n)
			valido := err == nil
			simRes = append(simRes, map[string]interface{}{
				"consulta":          n,
				"possui_whatsapp":   valido,
				"jid":               limpo + "@s.whatsapp.net",
				"numero_confirmado": limpo,
				"numero_visual":     core.FormatarNumeroBrasilVisual(limpo),
				"conta_business":    true,
				"modo":              "simulacao",
			})
		}
		responderSucesso(w, "Verificação de números concluída (Modo Simulação)", simRes)
		return
	}

	res, err := core.Gerenciador.VerificarNumerosWhatsApp(r.Context(), instDB.ID, lista)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Verificação de números no WhatsApp concluída com sucesso!", res)
}

func HandlerObterPerfilContato(w http.ResponseWriter, r *http.Request) {
	instID := r.URL.Query().Get("instancia_id")
	numero := r.URL.Query().Get("numero")
	simulacao := strings.EqualFold(r.URL.Query().Get("simulacao"), "true")

	if r.Method == http.MethodPost {
		var body struct {
			InstanciaID string `json:"instancia_id"`
			Numero      string `json:"numero"`
			Simulacao   bool   `json:"simulacao"`
		}
		if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err == nil {
			if body.InstanciaID != "" {
				instID = body.InstanciaID
			}
			if body.Numero != "" {
				numero = body.Numero
			}
			if body.Simulacao {
				simulacao = true
			}
		}
	}

	if strings.TrimSpace(numero) == "" {
		responderErro(w, http.StatusBadRequest, "Parâmetro 'numero' é obrigatório")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, instID)
	if !ok {
		return
	}

	if simulacao {
		limpo, _ := core.NormalizarNumeroBrasil(numero)
		responderSucesso(w, "Perfil do contato obtido (Simulação)", map[string]interface{}{
			"jid":             limpo + "@s.whatsapp.net",
			"numero_visual":   core.FormatarNumeroBrasilVisual(limpo),
			"recado_status":   "Atendimento Comercial Online • Segunda a Sexta",
			"nome_verificado": "Empresa Verificada Brasil",
			"modo":            "simulacao",
		})
		return
	}

	dados, err := core.Gerenciador.ObterPerfilContato(r.Context(), instDB.ID, numero)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Perfil do contato obtido com sucesso!", dados)
}

func HandlerEnviarPresenca(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID  string `json:"instancia_id"`
		Destinatario string `json:"destinatario"`
		Numero       string `json:"numero"`
		Estado       string `json:"estado"` // "digitando", "gravando_audio", "pausado", "online", "offline"
		Simulacao    bool   `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido")
		return
	}

	if body.Destinatario == "" && body.Numero != "" {
		body.Destinatario = body.Numero
	}
	if body.Estado == "" {
		body.Estado = "digitando"
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		responderSucesso(w, "Estado de presença simulado com sucesso!", map[string]string{
			"estado":       body.Estado,
			"destinatario": body.Destinatario,
			"modo":         "simulacao",
		})
		return
	}

	if err := core.Gerenciador.EnviarPresencaChat(r.Context(), instDB.ID, body.Destinatario, body.Estado); err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Estado de presença enviado com sucesso!", map[string]string{
		"estado":       body.Estado,
		"destinatario": body.Destinatario,
	})
}

func HandlerMarcarComoLido(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID      string   `json:"instancia_id"`
		Destinatario     string   `json:"destinatario"`
		Numero           string   `json:"numero"`
		Remetente        string   `json:"remetente"`
		IDsMensagens     []string `json:"ids_mensagens"`
		AudioReproduzido bool     `json:"audio_reproduzido"`
		Simulacao        bool     `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido")
		return
	}

	if body.Destinatario == "" && body.Numero != "" {
		body.Destinatario = body.Numero
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		responderSucesso(w, "Confirmação de leitura simulada com sucesso!", nil)
		return
	}

	if err := core.Gerenciador.MarcarComoLido(r.Context(), instDB.ID, body.Destinatario, body.Remetente, body.IDsMensagens, body.AudioReproduzido); err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Mensagens marcadas como lidas com sucesso!", nil)
}

func HandlerGerenciarConversa(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID     string `json:"instancia_id"`
		Destinatario    string `json:"destinatario"`
		Numero          string `json:"numero"`
		Acao            string `json:"acao"` // "fixar", "arquivar", "silenciar", "marcar_lido"
		Ativar          bool   `json:"ativar"`
		DuracaoSegundos int    `json:"duracao_segundos"`
		Simulacao       bool   `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido")
		return
	}

	if body.Destinatario == "" && body.Numero != "" {
		body.Destinatario = body.Numero
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		responderSucesso(w, fmt.Sprintf("Ação '%s' na conversa simulada com sucesso!", body.Acao), nil)
		return
	}

	if err := core.Gerenciador.GerenciarConversaAppState(r.Context(), instDB.ID, body.Destinatario, body.Acao, body.Ativar, body.DuracaoSegundos); err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Estado da conversa atualizado com sucesso!", nil)
}

func HandlerGerenciarEtiqueta(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID  string `json:"instancia_id"`
		Acao         string `json:"acao"` // "criar" ou "aplicar"
		Destinatario string `json:"destinatario"`
		EtiquetaID   string `json:"etiqueta_id"`
		NomeEtiqueta string `json:"nome_etiqueta"`
		Cor          int32  `json:"cor"`
		Ativo        bool   `json:"ativo"`
		Simulacao    bool   `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		responderSucesso(w, "Etiqueta WhatsApp Business atualizada (Simulação)!", nil)
		return
	}

	if err := core.Gerenciador.GerenciarEtiquetaBusiness(r.Context(), instDB.ID, body.Acao, body.Destinatario, body.EtiquetaID, body.NomeEtiqueta, body.Cor, body.Ativo); err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Etiqueta WhatsApp Business atualizada com sucesso!", nil)
}

// ============================================================================
// PACOTE 3: GESTÃO COMPLETA DE GRUPOS, COMUNIDADES, MENÇÃO FANTASMA & STATUS 24H
// ============================================================================

func HandlerListarGrupos(w http.ResponseWriter, r *http.Request) {
	instID := r.URL.Query().Get("instancia_id")
	simulacao := strings.EqualFold(r.URL.Query().Get("simulacao"), "true")

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, instID)
	if !ok {
		return
	}

	if simulacao {
		responderSucesso(w, "Grupos listados (Modo Simulação)", []map[string]interface{}{
			{
				"jid":                 "120363045891234567@g.us",
				"nome":                "VIP Lançamento Oficial #01",
				"descricao":           "Grupo exclusivo para avisos e materiais",
				"total_participantes": 248,
				"apenas_admins_falam": true,
				"eh_comunidade":       false,
			},
		})
		return
	}

	grupos, err := core.Gerenciador.ListarGrupos(r.Context(), instDB.ID)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Grupos recuperados com sucesso!", grupos)
}

func HandlerCriarGrupo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID   string   `json:"instancia_id"`
		Nome          string   `json:"nome"`
		Descricao     string   `json:"descricao"`
		Participantes []string `json:"participantes"`
		ApenasAdmins  bool     `json:"apenas_admins"`
		Travado       bool     `json:"travado"`
		Comunidade    bool     `json:"comunidade"`
		Simulacao     bool     `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido")
		return
	}

	if strings.TrimSpace(body.Nome) == "" {
		responderErro(w, http.StatusBadRequest, "Nome do grupo é obrigatório (máx. 25 caracteres)")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		simJID := fmt.Sprintf("1203630%s@g.us", uuid.New().String()[:8])
		responderSucesso(w, "Grupo criado com sucesso (Modo Simulação)!", map[string]interface{}{
			"jid":          simJID,
			"nome":         body.Nome,
			"descricao":    body.Descricao,
			"link_convite": "https://chat.whatsapp.com/SimConviteWooHubBR",
			"modo":         "simulacao",
		})
		return
	}

	res, err := core.Gerenciador.CriarGrupo(r.Context(), instDB.ID, body.Nome, body.Descricao, body.Participantes, body.ApenasAdmins, body.Travado, body.Comunidade)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Grupo criado com sucesso!", res)
}

func HandlerInfoGrupo(w http.ResponseWriter, r *http.Request) {
	instID := r.URL.Query().Get("instancia_id")
	grupoJID := r.URL.Query().Get("grupo_jid")

	if r.Method == http.MethodPost {
		var body struct {
			InstanciaID string `json:"instancia_id"`
			GrupoJID    string `json:"grupo_jid"`
		}
		if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err == nil {
			if body.InstanciaID != "" {
				instID = body.InstanciaID
			}
			if body.GrupoJID != "" {
				grupoJID = body.GrupoJID
			}
		}
	}

	if strings.TrimSpace(grupoJID) == "" {
		responderErro(w, http.StatusBadRequest, "Informe 'grupo_jid'")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, instID)
	if !ok {
		return
	}

	info, err := core.Gerenciador.ObterInfoGrupo(r.Context(), instDB.ID, grupoJID)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Informações do grupo obtidas com sucesso!", info)
}

func HandlerParticipantesGrupo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID   string   `json:"instancia_id"`
		GrupoJID      string   `json:"grupo_jid"`
		Acao          string   `json:"acao"` // "adicionar", "remover", "promover", "rebaixar"
		Participantes []string `json:"participantes"`
		Simulacao     bool     `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido")
		return
	}

	if strings.TrimSpace(body.GrupoJID) == "" || len(body.Participantes) == 0 {
		responderErro(w, http.StatusBadRequest, "Informe 'grupo_jid', 'acao' e 'participantes'")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		responderSucesso(w, "Participantes do grupo atualizados (Modo Simulação)!", map[string]interface{}{
			"grupo_jid": body.GrupoJID,
			"acao":      body.Acao,
			"total":     len(body.Participantes),
		})
		return
	}

	res, err := core.Gerenciador.GerenciarParticipantesGrupo(r.Context(), instDB.ID, body.GrupoJID, body.Acao, body.Participantes)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Participantes do grupo atualizados com sucesso!", res)
}

func HandlerConfigurarGrupo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID string `json:"instancia_id"`
		GrupoJID    string `json:"grupo_jid"`
		Simulacao   bool   `json:"simulacao"`
		core.ConfiguracaoGrupoReq
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyMidia, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido")
		return
	}

	if strings.TrimSpace(body.GrupoJID) == "" {
		responderErro(w, http.StatusBadRequest, "Informe 'grupo_jid'")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		responderSucesso(w, "Configurações do grupo atualizadas (Simulação)!", nil)
		return
	}

	if err := core.Gerenciador.ConfigurarGrupo(r.Context(), instDB.ID, body.GrupoJID, body.ConfiguracaoGrupoReq); err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Configurações do grupo atualizadas com sucesso!", nil)
}

func HandlerLinkConviteGrupo(w http.ResponseWriter, r *http.Request) {
	instID := r.URL.Query().Get("instancia_id")
	grupoJID := r.URL.Query().Get("grupo_jid")
	resetar := strings.EqualFold(r.URL.Query().Get("resetar"), "true")

	if r.Method == http.MethodPost {
		var body struct {
			InstanciaID string `json:"instancia_id"`
			GrupoJID    string `json:"grupo_jid"`
			Resetar     bool   `json:"resetar"`
		}
		if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err == nil {
			if body.InstanciaID != "" {
				instID = body.InstanciaID
			}
			if body.GrupoJID != "" {
				grupoJID = body.GrupoJID
			}
			resetar = body.Resetar
		}
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, instID)
	if !ok {
		return
	}

	link, err := core.Gerenciador.ObterLinkConviteGrupo(r.Context(), instDB.ID, grupoJID, resetar)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Link de convite do grupo obtido com sucesso!", map[string]string{
		"grupo_jid":    grupoJID,
		"link_convite": link,
	})
}

func HandlerEntrarGrupo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID string `json:"instancia_id"`
		LinkConvite string `json:"link_convite"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil || strings.TrimSpace(body.LinkConvite) == "" {
		responderErro(w, http.StatusBadRequest, "Informe 'link_convite' (ex: https://chat.whatsapp.com/...)")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	jid, err := core.Gerenciador.EntrarGrupoPorLink(r.Context(), instDB.ID, body.LinkConvite)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Entrou no grupo com sucesso!", map[string]string{
		"grupo_jid": jid,
	})
}

func HandlerMencaoFantasmaGrupo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID string `json:"instancia_id"`
		GrupoJID    string `json:"grupo_jid"`
		Mensagem    string `json:"mensagem"`
		Simulacao   bool   `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido")
		return
	}

	if strings.TrimSpace(body.GrupoJID) == "" || strings.TrimSpace(body.Mensagem) == "" {
		responderErro(w, http.StatusBadRequest, "Informe 'grupo_jid' e 'mensagem'")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		simID := fmt.Sprintf("sim_ghost_%s", uuid.New().String()[:12])
		_ = database.DB.GravarLogMensagem(instDB.ID, simID, "mencao_fantasma", body.GrupoJID, "ENVIADA", "ENTREGUE (SIMULADO)", "")
		responderSucesso(w, "Menção Fantasma (@todos) simulada com sucesso!", map[string]string{
			"id_mensagem": simID,
			"grupo_jid":   body.GrupoJID,
			"status":      "ENTREGUE (SIMULADO)",
			"modo":        "simulacao",
		})
		return
	}

	msgID, err := core.Gerenciador.EnviarMensagemTextoComOpcoes(r.Context(), instDB.ID, body.GrupoJID, body.Mensagem, &core.OpcoesEnvio{
		MencionarTodos: true,
	})
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Mensagem com Menção Fantasma (@todos) enviada ao grupo!", map[string]string{
		"id_mensagem": msgID,
		"grupo_jid":   body.GrupoJID,
		"status":      "ENTREGUE",
	})
}

func HandlerPublicarStatus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID string `json:"instancia_id"`
		Tipo        string `json:"tipo"` // "texto", "imagem", "video"
		Texto       string `json:"texto"`
		CorFundo    string `json:"cor_fundo"` // Ex: "#059669"
		MidiaBase64 string `json:"midia_base64"`
		Legenda     string `json:"legenda"`
		Simulacao   bool   `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyMidia, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo JSON inválido ou excede 25 MB")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Tipo == "" {
		body.Tipo = "texto"
	}

	if body.Simulacao {
		simID := fmt.Sprintf("sim_status_%s", uuid.New().String()[:12])
		_ = database.DB.GravarLogMensagem(instDB.ID, simID, "status_24h", "status@broadcast", "ENVIADA", "ENTREGUE (SIMULADO)", "")
		responderSucesso(w, "Status 24h (Story) publicado em modo simulação!", map[string]string{
			"id_mensagem": simID,
			"tipo":        body.Tipo,
			"status":      "PUBLICADO (SIMULADO)",
			"modo":        "simulacao",
		})
		return
	}

	var dadosMidia []byte
	if body.MidiaBase64 != "" {
		var err error
		dadosMidia, err = base64.StdEncoding.DecodeString(limparHeaderBase64(body.MidiaBase64))
		if err != nil {
			responderErro(w, http.StatusBadRequest, "midia_base64 inválido")
			return
		}
	}

	msgID, err := core.Gerenciador.PublicarStatus(r.Context(), instDB.ID, body.Tipo, body.Texto, body.CorFundo, dadosMidia, body.Legenda)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Status 24h (Story) publicado com sucesso no WhatsApp!", map[string]string{
		"id_mensagem": msgID,
		"tipo":        body.Tipo,
		"status":      "PUBLICADO",
	})
}
