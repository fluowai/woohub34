package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/internal/config"
	"go.mau.fi/whatsmeow/internal/core"
	"go.mau.fi/whatsmeow/internal/database"
	"go.mau.fi/whatsmeow/store/sqlstore"
)

func prepararAmbienteTeste(t *testing.T) http.Handler {
	t.Helper()

	config.CarregarConfiguracao()
	dbPath := filepath.Join(t.TempDir(), "teste_woohub.db")

	banco, err := database.InicializarBanco("sqlite", dbPath, database.OpcoesSeed{
		AdminEmail:    "admin@woohub.com.br",
		AdminSenha:    "SenhaForteAdmin!2026",
		SeedDadosDemo: false,
	})
	if err != nil {
		t.Fatalf("Falha ao inicializar banco de teste: %v", err)
	}
	t.Cleanup(func() {
		_ = banco.ObterConexaoSQL().Close()
	})

	container := sqlstore.NewWithDB(banco.ObterConexaoSQL(), banco.ObterDialetoWhatsmeow(), nil)
	if err := container.Upgrade(context.Background()); err != nil {
		t.Fatalf("Falha ao migrar sqlstore: %v", err)
	}

	wh := core.InicializarDespachanteWebhooks(100, 1)
	core.InicializarGerenciador(container, banco, wh)

	return ConfigurarRotas("../../web")
}

func TestSegurancaWebhookAntiSSRF(t *testing.T) {
	urlsBloqueadas := []string{
		"http://localhost:8080/api/v1/status",
		"http://127.0.0.1:5432/",
		"http://169.254.169.254/latest/meta-data/",
		"http://192.168.1.10/admin",
		"http://10.0.0.5:8080/internal",
		"http://postgres:5432/",
		"ftp://meuservidor.com.br/hook",
	}

	for _, u := range urlsBloqueadas {
		if err := core.ValidarURLWebhook(u); err == nil {
			t.Errorf("Esperava bloqueio anti-SSRF para a URL %q, mas foi aceita", u)
		}
	}

	urlsPermitidas := []string{
		"https://webhook.site/123456",
		"https://api.empresa.com.br/webhooks/whatsapp",
	}

	for _, u := range urlsPermitidas {
		if err := core.ValidarURLWebhook(u); err != nil {
			t.Errorf("URL legítima %q foi bloqueada indevidamente: %v", u, err)
		}
	}
}

func TestSPAFallbackRoutes(t *testing.T) {
	rotas := prepararAmbienteTeste(t)

	rotasSPA := []string{"/testador", "/planos", "/revendedores", "/whitelabel", "/logs"}
	for _, rota := range rotasSPA {
		req := httptest.NewRequest(http.MethodGet, rota, nil)
		rec := httptest.NewRecorder()
		rotas.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Esperava 200 OK na rota SPA %s, obteve %d", rota, rec.Code)
		}
		contentType := rec.Header().Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			t.Errorf("Esperava text/html na rota SPA %s, obteve %s", rota, contentType)
		}
	}
}

func TestPublicDocumentationRoutes(t *testing.T) {
	rotas := prepararAmbienteTeste(t)

	rotasDocs := []string{"/docs", "/docs/", "/documentacao", "/documentacao/"}
	for _, rota := range rotasDocs {
		req := httptest.NewRequest(http.MethodGet, rota, nil)
		rec := httptest.NewRecorder()
		rotas.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Esperava 200 OK na rota pública de documentação %s, obteve %d", rota, rec.Code)
		}
		contentType := rec.Header().Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			t.Errorf("Esperava text/html na rota %s, obteve %s", rota, contentType)
		}

		body := rec.Body.String()
		if !strings.Contains(body, "WooHub API") {
			t.Errorf("A documentação em %s não retornou o título WooHub API", rota)
		}
		if !strings.Contains(body, "PT-BR") || !strings.Contains(body, "EN") || !strings.Contains(body, "ES") {
			t.Errorf("A documentação em %s deve conter suporte tri-língue (PT-BR, EN, ES)", rota)
		}
		if !strings.Contains(body, "Model Context Protocol") && !strings.Contains(body, "MCP") {
			t.Errorf("A documentação em %s deve conter a seção de MCP", rota)
		}
	}
}

func TestNormalizacaoBrasilENonoDigito(t *testing.T) {
	num, err := core.NormalizarNumeroBrasil("(11) 98765-4321")
	if err != nil || num != "5511987654321" {
		t.Fatalf("Esperava 5511987654321, obteve %q (err=%v)", num, err)
	}

	semNove, ok := core.AlternarNonoDigitoBrasil(num)
	if !ok || semNove != "551187654321" {
		t.Fatalf("Esperava 551187654321, obteve %q", semNove)
	}

	comNove, ok := core.AlternarNonoDigitoBrasil(semNove)
	if !ok || comNove != "5511987654321" {
		t.Fatalf("Esperava retorno para 5511987654321, obteve %q", comNove)
	}
}

func TestIsolamentoMultitenantERBAC(t *testing.T) {
	rotas := prepararAmbienteTeste(t)

	// 1. Login SuperAdmin
	loginBody, _ := json.Marshal(map[string]string{
		"email": "admin@woohub.com.br",
		"senha": "SenhaForteAdmin!2026",
	})
	reqLogin := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	recLogin := httptest.NewRecorder()
	rotas.ServeHTTP(recLogin, reqLogin)

	if recLogin.Code != http.StatusOK {
		t.Fatalf("Login admin falhou: status %d - %s", recLogin.Code, recLogin.Body.String())
	}

	// 2. Criar dois clientes distintos (Cliente A e Cliente B)
	cliA, err := database.DB.CriarCliente("", "Empresa A", "a@empresa.com.br", "SenhaCliA!123", "111", 5)
	if err != nil {
		t.Fatalf("Erro ao criar Cliente A: %v", err)
	}
	cliB, err := database.DB.CriarCliente("", "Empresa B", "b@empresa.com.br", "SenhaCliB!123", "222", 5)
	if err != nil {
		t.Fatalf("Erro ao criar Cliente B: %v", err)
	}

	instsA, _ := database.DB.ListarInstancias(cliA["id"].(string))
	instsB, _ := database.DB.ListarInstancias(cliB["id"].(string))
	if len(instsA) == 0 || len(instsB) == 0 {
		t.Fatalf("Instâncias padrão não foram criadas para os clientes")
	}
	instA := instsA[0]
	instB := instsB[0]

	// 3. Autenticar como Cliente A
	_, tokenCliA, err := database.DB.AutenticarUsuario("a@empresa.com.br", "SenhaCliA!123")
	if err != nil {
		t.Fatalf("Falha ao autenticar Cliente A: %v", err)
	}

	// 4. Cliente A tenta apagar a instância do Cliente B -> DEVE retornar 403 Forbidden (Anti-IDOR)
	reqDelB := httptest.NewRequest(http.MethodDelete, "/api/v1/instancia/"+instB.ID+"/apagar", nil)
	reqDelB.Header.Set("Authorization", "Bearer "+tokenCliA)
	recDelB := httptest.NewRecorder()
	rotas.ServeHTTP(recDelB, reqDelB)

	if recDelB.Code != http.StatusForbidden {
		t.Fatalf("Quebra de isolamento! Cliente A conseguiu acessar/apagar instância do Cliente B (status=%d)", recDelB.Code)
	}

	// 5. Uso de x-api-key da Instância A para tentar enviar mensagem pela Instância B -> DEVE retornar 403 Forbidden
	msgCrossBody, _ := json.Marshal(map[string]interface{}{
		"instancia_id": instB.ID,
		"destinatario": "11987654321",
		"mensagem":     "Tentativa cross-tenant",
		"simulacao":    true,
	})
	reqCross := httptest.NewRequest(http.MethodPost, "/api/v1/mensagem/texto", bytes.NewReader(msgCrossBody))
	reqCross.Header.Set("x-api-key", instA.ChaveAPI)
	recCross := httptest.NewRecorder()
	rotas.ServeHTTP(recCross, reqCross)

	if recCross.Code != http.StatusForbidden {
		t.Fatalf("Quebra de escopo de x-api-key! Chave da instância A operou na instância B (status=%d)", recCross.Code)
	}

	// 6. Uso legítimo da x-api-key da Instância A na própria Instância A -> DEVE retornar 200 OK
	msgOkBody, _ := json.Marshal(map[string]interface{}{
		"destinatario": "11987654321",
		"mensagem":     "Envio autorizado na própria instância",
		"simulacao":    true,
	})
	reqOk := httptest.NewRequest(http.MethodPost, "/api/v1/mensagem/texto", bytes.NewReader(msgOkBody))
	reqOk.Header.Set("x-api-key", instA.ChaveAPI)
	recOk := httptest.NewRecorder()
	rotas.ServeHTTP(recOk, reqOk)

	if recOk.Code != http.StatusOK {
		t.Fatalf("Envio legítimo com x-api-key falhou: status %d - %s", recOk.Code, recOk.Body.String())
	}
}

func TestPacotes1234SuiteCompletaEndpoints(t *testing.T) {
	rotas := prepararAmbienteTeste(t)

	cli, err := database.DB.CriarCliente("", "Cliente Pacotes Completos", "pacotes@empresa.com.br", "SenhaPacote!2026", "12345678000199", 10)
	if err != nil {
		t.Fatalf("Erro ao criar cliente de teste: %v", err)
	}
	insts, _ := database.DB.ListarInstancias(cli["id"].(string))
	if len(insts) == 0 {
		t.Fatalf("Nenhuma instância padrão criada para o cliente")
	}
	inst := insts[0]

	disparar := func(metodo, rota string, payload map[string]interface{}) *httptest.ResponseRecorder {
		t.Helper()
		var bodyReader *bytes.Reader
		if payload != nil {
			b, _ := json.Marshal(payload)
			bodyReader = bytes.NewReader(b)
		} else {
			bodyReader = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(metodo, rota, bodyReader)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", inst.ChaveAPI)
		rec := httptest.NewRecorder()
		rotas.ServeHTTP(rec, req)
		return rec
	}

	casos := []struct {
		nome    string
		metodo  string
		rota    string
		payload map[string]interface{}
	}{
		{
			nome:   "Pacote 1 - Texto com Quote, Mencoes, LinkPreview e Timer",
			metodo: http.MethodPost,
			rota:   "/api/v1/mensagem/texto",
			payload: map[string]interface{}{
				"destinatario":       "11987654321",
				"mensagem":           "Olá @5511987654321 confira https://woohub.com.br",
				"id_mensagem_citada": "BAE51234567890",
				"mencoes":            []string{"11987654321"},
				"link_preview": map[string]string{
					"url":       "https://woohub.com.br",
					"titulo":    "WooHub API B2B",
					"descricao": "Motor oficial multidevice",
				},
				"expiracao_segundos": 86400,
				"simulacao":          true,
			},
		},
		{
			nome:   "Pacote 1 - Documento PDF/XML com Legenda",
			metodo: http.MethodPost,
			rota:   "/api/v1/mensagem/documento",
			payload: map[string]interface{}{
				"destinatario":     "11987654321",
				"documento_base64": "JVBERi0xLjQKJeLjz9MK",
				"nome_arquivo":     "fatura_mensal.pdf",
				"legenda":          "Segue sua fatura mensal em PDF.",
				"simulacao":        true,
			},
		},
		{
			nome:   "Pacote 1 - Video MP4 / GIF / PTV",
			metodo: http.MethodPost,
			rota:   "/api/v1/mensagem/video",
			payload: map[string]interface{}{
				"destinatario":  "11987654321",
				"video_base64":  "AAAAIGZ0eXBpc29tAAACAGlzb21pc28yYXZjMW1wNDE=",
				"legenda":       "Recado em vídeo circular",
				"nota_video":    true,
				"unica_visao":   false,
				"simulacao":     true,
			},
		},
		{
			nome:   "Pacote 1 - Sticker Figurinha WebP",
			metodo: http.MethodPost,
			rota:   "/api/v1/mensagem/sticker",
			payload: map[string]interface{}{
				"destinatario":   "11987654321",
				"sticker_base64": "UklGRh4AAABXRUJQVlA4TBEAAAAvAAAAAAfQ//73v/+BiOh/AAA=",
				"animado":        false,
				"simulacao":      true,
			},
		},
		{
			nome:   "Pacote 1 - Enquete Interativa Substituta de Botoes",
			metodo: http.MethodPost,
			rota:   "/api/v1/mensagem/enquete",
			payload: map[string]interface{}{
				"destinatario":   "11987654321",
				"pergunta":       "Qual setor você deseja falar?",
				"opcoes":         []string{"1. Financeiro e 2ª Via", "2. Suporte Técnico", "3. Comercial"},
				"maximo_selecao": 1,
				"simulacao":      true,
			},
		},
		{
			nome:   "Pacote 1 - Localizacao Pino GPS",
			metodo: http.MethodPost,
			rota:   "/api/v1/mensagem/localizacao",
			payload: map[string]interface{}{
				"destinatario": "11987654321",
				"latitude":     -23.561414,
				"longitude":    -46.655881,
				"nome_local":   "Sede WooHub Brasil",
				"endereco":     "Av. Paulista, 1000 - São Paulo/SP",
				"simulacao":    true,
			},
		},
		{
			nome:   "Pacote 1 - Cartao de Contato vCard",
			metodo: http.MethodPost,
			rota:   "/api/v1/mensagem/contato",
			payload: map[string]interface{}{
				"destinatario": "11987654321",
				"nome_contato": "Consultor Comercial WooHub",
				"telefone":     "11999998888",
				"empresa":      "WooHub SaaS",
				"simulacao":    true,
			},
		},
		{
			nome:   "Pacote 1 - Reacao Emoji",
			metodo: http.MethodPost,
			rota:   "/api/v1/mensagem/reacao",
			payload: map[string]interface{}{
				"destinatario":     "11987654321",
				"id_mensagem_alvo": "3EB0123456789ABCDEF",
				"emoji":            "❤️",
				"simulacao":        true,
			},
		},
		{
			nome:   "Pacote 1 - Editar Mensagem Enviada",
			metodo: http.MethodPost,
			rota:   "/api/v1/mensagem/editar",
			payload: map[string]interface{}{
				"destinatario":     "11987654321",
				"id_mensagem_alvo": "3EB0123456789ABCDEF",
				"novo_texto":       "Texto corrigido em tempo real!",
				"simulacao":        true,
			},
		},
		{
			nome:   "Pacote 1 - Revogar Apagar Para Todos",
			metodo: http.MethodPost,
			rota:   "/api/v1/mensagem/revogar",
			payload: map[string]interface{}{
				"destinatario":     "11987654321",
				"id_mensagem_alvo": "3EB0123456789ABCDEF",
				"simulacao":        true,
			},
		},
		{
			nome:   "Pacote 2 - Validador em Lote IsOnWhatsApp com 9 Digito",
			metodo: http.MethodPost,
			rota:   "/api/v1/contato/verificar",
			payload: map[string]interface{}{
				"numeros":   []string{"(11) 98765-4321", "21998887766"},
				"simulacao": true,
			},
		},
		{
			nome:   "Pacote 2 - Presenca de Chat Digitando / Gravando Audio",
			metodo: http.MethodPost,
			rota:   "/api/v1/chat/presenca",
			payload: map[string]interface{}{
				"destinatario": "11987654321",
				"estado":       "gravando_audio",
				"simulacao":    true,
			},
		},
		{
			nome:   "Pacote 2 - Marcar Mensagens Como Lidas",
			metodo: http.MethodPost,
			rota:   "/api/v1/chat/marcar-lido",
			payload: map[string]interface{}{
				"destinatario":  "11987654321",
				"ids_mensagens": []string{"MSG_001", "MSG_002"},
				"simulacao":     true,
			},
		},
		{
			nome:   "Pacote 2 - Alterar Estado do Chat Fixar/Arquivar/Silenciar",
			metodo: http.MethodPost,
			rota:   "/api/v1/chat/estado",
			payload: map[string]interface{}{
				"destinatario": "11987654321",
				"acao":         "fixar",
				"ativo":        true,
				"simulacao":    true,
			},
		},
		{
			nome:   "Pacote 2 - Etiquetas WhatsApp Business",
			metodo: http.MethodPost,
			rota:   "/api/v1/etiqueta/gerenciar",
			payload: map[string]interface{}{
				"acao":         "atribuir",
				"destinatario": "11987654321",
				"id_etiqueta":  "1",
				"ativo":        true,
				"simulacao":    true,
			},
		},
		{
			nome:   "Pacote 3 - Criar Grupo de Lancamento",
			metodo: http.MethodPost,
			rota:   "/api/v1/grupo/criar",
			payload: map[string]interface{}{
				"nome":          "VIP Lançamento #01",
				"participantes": []string{"11987654321", "11912345678"},
				"simulacao":     true,
			},
		},
		{
			nome:   "Pacote 3 - Mencao Fantasma (@todos)",
			metodo: http.MethodPost,
			rota:   "/api/v1/grupo/mencao-fantasma",
			payload: map[string]interface{}{
				"grupo_jid": "120363025987654321@g.us",
				"mensagem":  "🚨 Estamos AO VIVO! Clique no link para entrar na aula.",
				"simulacao": true,
			},
		},
		{
			nome:   "Pacote 3 - Publicar Status 24h (Stories)",
			metodo: http.MethodPost,
			rota:   "/api/v1/status/publicar",
			payload: map[string]interface{}{
				"tipo":      "texto",
				"texto":     "Oferta Relâmpago nas próximas 24h! 🔥",
				"cor_fundo": "#059669",
				"simulacao": true,
			},
		},
		{
			nome:   "Pacote 4 - Configuracoes Blindagem Anti-Ban, Proxy e Anti-Chamadas",
			metodo: http.MethodPost,
			rota:   "/api/v1/instancia/" + inst.ID + "/configuracoes",
			payload: map[string]interface{}{
				"rejeitar_chamadas":          true,
				"mensagem_chamada_rejeitada": "Olá! Não atendemos chamadas de voz/vídeo por este número. Envie mensagem de texto.",
				"ignorar_grupos":             false,
				"leitura_automatica":         true,
				"sempre_online":              true,
				"proxy_url":                  "",
			},
		},
	}

	for _, tc := range casos {
		t.Run(tc.nome, func(t *testing.T) {
			rec := disparar(tc.metodo, tc.rota, tc.payload)
			if rec.Code != http.StatusOK {
				t.Fatalf("Falha em %s (%s %s): status=%d body=%s", tc.nome, tc.metodo, tc.rota, rec.Code, rec.Body.String())
			}
		})
	}

	// Verificar persistência das configurações Pacote 4 no banco de dados
	instAtualizada, err := database.DB.BuscarInstanciaPorId(inst.ID)
	if err != nil {
		t.Fatalf("Erro ao buscar instância atualizada: %v", err)
	}
	if !instAtualizada.RejeitarChamadas || !instAtualizada.LeituraAutomatica || !instAtualizada.SempreOnline {
		t.Fatalf("Configurações anti-ban/atendimento não foram persistidas corretamente: %+v", instAtualizada)
	}
}

func TestIsolamentoTresNiveisMegaRevendaCliente(t *testing.T) {
	rotas := prepararAmbienteTeste(t)

	// 1. Criar Revendedor + Conta de acesso nível 'revendedor'
	rev, err := database.DB.CriarRevendedor("Revenda Alpha", "alpha", "alpha.com.br", "#10B981", 100)
	if err != nil {
		t.Fatalf("Erro ao criar revendedor: %v", err)
	}
	revID := rev.ID
	_ = database.DB.AtualizarInfraestruturaRevendedor(revID, "postgresql://user:pass@localhost:5432/alpha", "localhost:9000", "alpha_key", "alpha_secret", "alpha-bucket", "http://localhost:9000", false)
	_, err = database.DB.CriarUsuarioRevendedor(revID, "Revenda Alpha", "revenda@alpha.com.br", "SenhaRevenda!2026", 100)
	if err != nil {
		t.Fatalf("Erro ao criar usuário revendedor: %v", err)
	}

	// 2. Criar Cliente vinculado à Revenda Alpha e outro Cliente em outra revenda (Revenda Beta)
	revBeta, err := database.DB.CriarRevendedor("Revenda Beta", "beta", "beta.com.br", "#3B82F6", 50)
	if err != nil {
		t.Fatalf("Erro ao criar Revenda Beta: %v", err)
	}
	_ = database.DB.AtualizarInfraestruturaRevendedor(revBeta.ID, "postgresql://user:pass@localhost:5432/beta", "localhost:9000", "beta_key", "beta_secret", "beta-bucket", "http://localhost:9000", false)
	_, err = database.DB.CriarCliente(revID, "Cliente Alpha 1", "cli1@alpha.com.br", "SenhaCli1!2026", "111", 5)
	if err != nil {
		t.Fatalf("Erro ao criar Cliente Alpha 1: %v", err)
	}
	_, err = database.DB.CriarCliente(revBeta.ID, "Cliente Outra Revenda", "cli@outra.com.br", "SenhaCli2!2026", "222", 5)
	if err != nil {
		t.Fatalf("Erro ao criar Cliente Outra Revenda: %v", err)
	}

	// 3. Autenticar nos 3 níveis
	_, tokenAdmin, err := database.DB.AutenticarUsuario("admin@woohub.com.br", "SenhaForteAdmin!2026")
	if err != nil {
		t.Fatalf("Falha login superadmin: %v", err)
	}
	_, tokenRev, err := database.DB.AutenticarUsuario("revenda@alpha.com.br", "SenhaRevenda!2026")
	if err != nil {
		t.Fatalf("Falha login revendedor: %v", err)
	}
	_, tokenCli, err := database.DB.AutenticarUsuario("cli1@alpha.com.br", "SenhaCli1!2026")
	if err != nil {
		t.Fatalf("Falha login cliente: %v", err)
	}

	// 4. Nível Cliente Final NÃO pode listar revendedores nem clientes (403 Forbidden)
	for _, rotaProibida := range []string{"/api/v1/revendedor/listar", "/api/v1/cliente/listar"} {
		req := httptest.NewRequest(http.MethodGet, rotaProibida, nil)
		req.Header.Set("Authorization", "Bearer "+tokenCli)
		rec := httptest.NewRecorder()
		rotas.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("Cliente final deveria receber 403 em %s, mas recebeu %d", rotaProibida, rec.Code)
		}
	}

	// 5. Nível Revendedor NÃO pode listar revendedores (exclusivo Mega Admin -> 403),
	// mas PODE listar seus próprios clientes (e não vê a própria conta de revendedor nem clientes de outra revenda)
	reqRevList := httptest.NewRequest(http.MethodGet, "/api/v1/revendedor/listar", nil)
	reqRevList.Header.Set("Authorization", "Bearer "+tokenRev)
	recRevList := httptest.NewRecorder()
	rotas.ServeHTTP(recRevList, reqRevList)
	if recRevList.Code != http.StatusForbidden {
		t.Fatalf("Revendedor deveria receber 403 em /api/v1/revendedor/listar, mas recebeu %d", recRevList.Code)
	}

	reqCliList := httptest.NewRequest(http.MethodGet, "/api/v1/cliente/listar", nil)
	reqCliList.Header.Set("Authorization", "Bearer "+tokenRev)
	recCliList := httptest.NewRecorder()
	rotas.ServeHTTP(recCliList, reqCliList)
	if recCliList.Code != http.StatusOK {
		t.Fatalf("Revendedor deveria conseguir listar seus clientes (200), recebeu %d", recCliList.Code)
	}
	var respCli struct {
		Sucesso bool                     `json:"sucesso"`
		Dados   []map[string]interface{} `json:"dados"`
	}
	_ = json.Unmarshal(recCliList.Body.Bytes(), &respCli)
	if len(respCli.Dados) != 1 || respCli.Dados[0]["email"] != "cli1@alpha.com.br" {
		t.Fatalf("Revendedor deveria ver exatamente 1 cliente final próprio (cli1@alpha.com.br), mas recebeu: %+v", respCli.Dados)
	}

	// 6. Nível Mega Admin acessa /api/v1/revendedor/listar com sucesso (200 OK)
	reqAdminRev := httptest.NewRequest(http.MethodGet, "/api/v1/revendedor/listar", nil)
	reqAdminRev.Header.Set("Authorization", "Bearer "+tokenAdmin)
	recAdminRev := httptest.NewRecorder()
	rotas.ServeHTTP(recAdminRev, reqAdminRev)
	if recAdminRev.Code != http.StatusOK {
		t.Fatalf("Mega Admin deveria receber 200 em /api/v1/revendedor/listar, recebeu %d", recAdminRev.Code)
	}
}





func TestWhatsAppWebChatRoutes(t *testing.T) {
	rotas := ConfigurarRotas("../../web")

	// 1. Acesso a /chat e /webchat entrega chat.html com 200 OK
	rotasChat := []string{"/chat", "/webchat"}
	for _, rota := range rotasChat {
		req := httptest.NewRequest(http.MethodGet, rota, nil)
		rec := httptest.NewRecorder()
		rotas.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Esperava 200 OK para rota %s, mas recebeu %d", rota, rec.Code)
		}
		contentType := rec.Header().Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			t.Fatalf("Esperava Content-Type text/html para %s, mas recebeu %s", rota, contentType)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "WooAPI Web") || !strings.Contains(body, "chat.js") {
			t.Fatalf("Corpo retornado não parece ser o WhatsApp Web (chat.html) para %s", rota)
		}
	}

	// 2. Acesso via subdomínio web.wooapi.com.br
	reqSub := httptest.NewRequest(http.MethodGet, "/", nil)
	reqSub.Host = "web.wooapi.com.br"
	recSub := httptest.NewRecorder()
	rotas.ServeHTTP(recSub, reqSub)
	if recSub.Code != http.StatusOK {
		t.Fatalf("Esperava 200 OK para Host web.wooapi.com.br, mas recebeu %d", recSub.Code)
	}
	if !strings.Contains(recSub.Body.String(), "WooAPI Web") {
		t.Fatalf("Subdomínio web.wooapi.com.br deveria entregar o WhatsApp Web (chat.html)")
	}
}

func TestChatRecursosPlanoEAtalhos(t *testing.T) {
	rotas := prepararAmbienteTeste(t)

	// 1. Gera token autenticado
	_, token, err := database.DB.AutenticarUsuario("admin@woohub.com.br", "SenhaForteAdmin!2026")
	if err != nil {
		t.Fatalf("Falha ao autenticar usuario de teste: %v", err)
	}

	// 2. Testa endpoint /api/v1/chat/recursos
	reqRec := httptest.NewRequest(http.MethodGet, "/api/v1/chat/recursos", nil)
	reqRec.Header.Set("Authorization", "Bearer "+token)
	recRec := httptest.NewRecorder()
	rotas.ServeHTTP(recRec, reqRec)

	if recRec.Code != http.StatusOK {
		t.Fatalf("Esperava 200 em /api/v1/chat/recursos, recebeu %d", recRec.Code)
	}
	var respRec struct {
		Sucesso  bool              `json:"sucesso"`
		Recursos RecursosPlanoChat `json:"recursos"`
	}
	if err := json.Unmarshal(recRec.Body.Bytes(), &respRec); err != nil {
		t.Fatalf("Erro ao decodificar JSON de recursos: %v", err)
	}
	if !respRec.Recursos.RespostasRapidas {
		t.Fatalf("Esperava respostas rápidas ativas para o plano")
	}

	// 3. Testa CRUD de Respostas Rápidas (/api/v1/chat/respostas-rapidas)
	bodyCriar := `{"atalho": "/pix", "texto": "Chave PIX: financeiro@wooapi.com.br"}`
	reqCriar := httptest.NewRequest(http.MethodPost, "/api/v1/chat/respostas-rapidas", strings.NewReader(bodyCriar))
	reqCriar.Header.Set("Authorization", "Bearer "+token)
	reqCriar.Header.Set("Content-Type", "application/json")
	recCriar := httptest.NewRecorder()
	rotas.ServeHTTP(recCriar, reqCriar)

	if recCriar.Code != http.StatusOK {
		t.Fatalf("Esperava 200 ao cadastrar resposta rápida, recebeu %d: %s", recCriar.Code, recCriar.Body.String())
	}

	// Listar respostas rápidas
	reqList := httptest.NewRequest(http.MethodGet, "/api/v1/chat/respostas-rapidas", nil)
	reqList.Header.Set("Authorization", "Bearer "+token)
	recList := httptest.NewRecorder()
	rotas.ServeHTTP(recList, reqList)

	if recList.Code != http.StatusOK {
		t.Fatalf("Esperava 200 ao listar respostas rápidas, recebeu %d", recList.Code)
	}
	var respList struct {
		Sucesso bool                         `json:"sucesso"`
		Total   int                          `json:"total"`
		Atalhos []database.RespostaRapidaChat `json:"atalhos"`
	}
	_ = json.Unmarshal(recList.Body.Bytes(), &respList)
	if respList.Total == 0 {
		t.Fatalf("Esperava ao menos 1 resposta rápida cadastrada")
	}
}

func TestResendOAuthControleAcessoEDominios(t *testing.T) {
	rotas := prepararAmbienteTeste(t)

	// 1. Criar revendedor e usuario revendedor
	rev, err := database.DB.CriarRevendedor("Revenda OAuth Resend", "slug-resend", "resend.com.br", "#10B981", 50)
	if err != nil {
		t.Fatalf("Erro ao criar revenda: %v", err)
	}
	_, err = database.DB.CriarUsuarioRevendedor(rev.ID, "Admin Resend", "admin@resend-teste.com", "Senha!2026", 50)
	if err != nil {
		t.Fatalf("Erro ao criar usuario revendedor: %v", err)
	}

	// 2. Criar cliente final e autenticar
	_, err = database.DB.CriarCliente(rev.ID, "Cliente Resend", "cli@resend-teste.com", "SenhaCli!2026", "123", 5)
	if err != nil {
		t.Fatalf("Erro ao criar cliente: %v", err)
	}
	_, tokenCli, _ := database.DB.AutenticarUsuario("cli@resend-teste.com", "SenhaCli!2026")
	_, tokenRev, _ := database.DB.AutenticarUsuario("admin@resend-teste.com", "Senha!2026")

	// 3. Cliente final tenta iniciar OAuth do Resend -> 403 Forbidden
	reqCli := httptest.NewRequest(http.MethodGet, "/api/v1/revenda/email/oauth/iniciar", nil)
	reqCli.Header.Set("Authorization", "Bearer "+tokenCli)
	recCli := httptest.NewRecorder()
	rotas.ServeHTTP(recCli, reqCli)
	if recCli.Code != http.StatusForbidden {
		t.Errorf("Cliente final deveria receber 403, recebeu: %d (%s)", recCli.Code, recCli.Body.String())
	}

	// 4. Revendedor inicia OAuth do Resend -> 200 OK
	reqRev := httptest.NewRequest(http.MethodGet, "/api/v1/revenda/email/oauth/iniciar", nil)
	reqRev.Header.Set("Authorization", "Bearer "+tokenRev)
	recRev := httptest.NewRecorder()
	rotas.ServeHTTP(recRev, reqRev)
	if recRev.Code != http.StatusOK {
		t.Errorf("Revendedor deveria receber 200, recebeu: %d (%s)", recRev.Code, recRev.Body.String())
	}

	// 5. Revendedor tenta listar dominios antes de configurar Resend -> 400 Bad Request
	reqDom := httptest.NewRequest(http.MethodGet, "/api/v1/revenda/email/dominios", nil)
	reqDom.Header.Set("Authorization", "Bearer "+tokenRev)
	recDom := httptest.NewRecorder()
	rotas.ServeHTTP(recDom, reqDom)
	if recDom.Code != http.StatusBadRequest {
		t.Errorf("Deveria retornar 400 avisando para configurar Resend primeiro, recebeu: %d (%s)", recDom.Code, recDom.Body.String())
	}
}
