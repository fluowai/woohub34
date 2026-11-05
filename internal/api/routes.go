package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"go.mau.fi/whatsmeow/internal/config"
	"go.mau.fi/whatsmeow/web"
)

// ConfigurarRotas mapeia todas as rotas da WooHub API 100% Brasil
func ConfigurarRotas(caminhoWeb string) http.Handler {
	mux := http.NewServeMux()

	// 1. Status e Metadados (Públicos)
	mux.HandleFunc("/api/v1/status", HandlerStatus)

	// 2. Autenticação e Gestão de Sessão (Login com Rate Limit / Logout)
	mux.HandleFunc("/api/v1/auth/login", limitarTaxaLogin(HandlerLogin))
	mux.HandleFunc("/api/v1/auth/logout", HandlerLogout)
	mux.HandleFunc("/api/v1/auth/me", HandlerUsuarioAtual)
	mux.HandleFunc("/api/v1/auth/alterar-senha", ExigirAutenticacao(HandlerAlterarSenha))

	// 3. White-Label, Revenda & Infraestrutura Isolada (BYO Supabase + BYO MinIO S3)
	mux.HandleFunc("/api/v1/revenda/marca", HandlerMarcaRevenda) // Público para carregar tema visual no login
	mux.HandleFunc("/api/v1/revenda/configurar", ExigirAutenticacao(HandlerConfigurarMarca))
	mux.HandleFunc("/api/v1/revenda/infra", ExigirAutenticacao(HandlerObterInfraRevenda))
	mux.HandleFunc("/api/v1/revenda/infra/configurar", ExigirAutenticacao(HandlerConfigurarInfraRevenda))
	mux.HandleFunc("/api/v1/revenda/infra/testar-supabase", ExigirAutenticacao(HandlerTestarSupabaseRevenda))
	mux.HandleFunc("/api/v1/revenda/infra/testar-minio", ExigirAutenticacao(HandlerTestarMinioRevenda))
	mux.HandleFunc("/api/v1/revendedor/listar", ExigirAutenticacao(HandlerListarRevendedores))
	mux.HandleFunc("/api/v1/revendedor/criar", ExigirAutenticacao(HandlerCriarRevendedor))
	mux.HandleFunc("/api/v1/revendedor/atualizar", ExigirAutenticacao(HandlerAtualizarRevendedor))
	mux.HandleFunc("/api/v1/revenda/email", ExigirAutenticacao(HandlerEmailRevenda))
	mux.HandleFunc("/api/v1/revenda/email/testar", ExigirAutenticacao(HandlerTestarEmailRevenda))
	mux.HandleFunc("/api/v1/onboarding", ExigirAutenticacao(HandlerOnboarding))

	// 3.1. Servidor Mestre de Licenças & Kill-Switch de Stacks Portainer
	mux.HandleFunc("/api/v1/licenca/validar", HandlerValidarLicencaBoot)
	mux.HandleFunc("/api/v1/licenca/heartbeat", HandlerHeartbeatLicenca)
	mux.HandleFunc("/api/v1/licenca/status-local", ExigirAutenticacao(HandlerStatusLicencaLocal))
	mux.HandleFunc("/api/v1/licenca/listar", ExigirAutenticacao(HandlerListarLicencas))
	mux.HandleFunc("/api/v1/licenca/criar", ExigirAutenticacao(HandlerCriarLicenca))
	mux.HandleFunc("/api/v1/licenca/status", ExigirAutenticacao(HandlerAlterarStatusLicenca))
	mux.HandleFunc("/api/v1/licenca/desvincular-ip", ExigirAutenticacao(HandlerDesvincularIPLicenca))

	// 4. Clientes da Revenda & Planos
	mux.HandleFunc("/api/v1/planos/listar", ExigirAutenticacao(HandlerListarPlanos))
	mux.HandleFunc("/api/v1/cliente/listar", ExigirAutenticacao(HandlerListarClientes))
	mux.HandleFunc("/api/v1/cliente/criar", ExigirAutenticacao(HandlerCriarCliente))
	mux.HandleFunc("/api/v1/cliente/apagar", ExigirAutenticacao(HandlerApagarCliente))

	// 5. Gestão de Instâncias (Protegidas)
	mux.HandleFunc("/api/v1/instancia/listar", ExigirAutenticacao(HandlerListarInstancias))
	mux.HandleFunc("/api/v1/instancia/criar", ExigirAutenticacao(HandlerCriarInstancia))

	// Rotas dinâmicas de instâncias (Protegidas com verificação RBAC de propriedade)
	mux.HandleFunc("/api/v1/instancia/", ExigirAutenticacao(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/conectar") {
			HandlerConectarInstancia(w, r)
		} else if strings.HasSuffix(path, "/qrcode/stream") {
			HandlerQRCodeStream(w, r)
		} else if strings.HasSuffix(path, "/qrcode") {
			HandlerObterQRCode(w, r)
		} else if strings.HasSuffix(path, "/codigo-pareamento") {
			HandlerCodigoPareamento(w, r)
		} else if strings.HasSuffix(path, "/desconectar") {
			HandlerDesconectar(w, r)
		} else if strings.HasSuffix(path, "/apagar") {
			HandlerApagarInstancia(w, r)
		} else if strings.HasSuffix(path, "/webhook") {
			HandlerConfigurarWebhook(w, r)
		} else if strings.HasSuffix(path, "/configuracoes") {
			HandlerConfigurarInstanciaAvancada(w, r)
		} else if strings.HasSuffix(path, "/status") {
			HandlerStatusInstancia(w, r)
		} else if strings.Contains(path, "/integracoes") {
			HandlerRoteamentoIntegracoes(w, r)
		} else if strings.HasSuffix(path, "/atribuicoes") {
			HandlerListarAtribuicoesInstancia(w, r)
		} else {
			responderErro(w, http.StatusNotFound, "Rota de instância não encontrada")
		}
	}))

	// 6. Pacote 1: Mensageria Completa & Interativa (Texto, Áudio PTT, Mídias, Vídeo, Doc, Sticker, Enquete, Localização, Contato, Reação, Editar, Revogar)
	mux.HandleFunc("/api/v1/mensagem/texto", ExigirAutenticacao(HandlerEnviarTexto))
	mux.HandleFunc("/api/v1/mensagem/audio", ExigirAutenticacao(HandlerEnviarAudio))
	mux.HandleFunc("/api/v1/mensagem/midia", ExigirAutenticacao(HandlerEnviarImagem))
	mux.HandleFunc("/api/v1/mensagem/imagem", ExigirAutenticacao(HandlerEnviarImagem))
	mux.HandleFunc("/api/v1/mensagem/video", ExigirAutenticacao(HandlerEnviarVideo))
	mux.HandleFunc("/api/v1/mensagem/documento", ExigirAutenticacao(HandlerEnviarDocumento))
	mux.HandleFunc("/api/v1/mensagem/sticker", ExigirAutenticacao(HandlerEnviarSticker))
		mux.HandleFunc("/api/v1/mensagem/enquete", ExigirAutenticacao(HandlerEnviarEnquete))
	mux.HandleFunc("/api/v1/mensagem/interativa", ExigirAutenticacao(HandlerEnviarMensagemInterativa))
	mux.HandleFunc("/api/v1/mensagem/botoes", ExigirAutenticacao(HandlerEnviarMensagemInterativa))
	mux.HandleFunc("/api/v1/mensagem/localizacao", ExigirAutenticacao(HandlerEnviarLocalizacao))
	mux.HandleFunc("/api/v1/mensagem/contato", ExigirAutenticacao(HandlerEnviarContato))
	mux.HandleFunc("/api/v1/mensagem/reacao", ExigirAutenticacao(HandlerEnviarReacao))
	mux.HandleFunc("/api/v1/mensagem/editar", ExigirAutenticacao(HandlerEditarMensagem))
	mux.HandleFunc("/api/v1/mensagem/revogar", ExigirAutenticacao(HandlerRevogarMensagem))
	mux.HandleFunc("/api/v1/mensagem/logs", ExigirAutenticacao(HandlerListarLogs))

	// 7. Pacote 2: Validador de Números BR, Perfil, Presença, Leitura e Etiquetas Business
	mux.HandleFunc("/api/v1/contato/verificar", ExigirAutenticacao(HandlerVerificarNumeros))
	mux.HandleFunc("/api/v1/contato/perfil", ExigirAutenticacao(HandlerObterPerfilContato))
	mux.HandleFunc("/api/v1/chat/presenca", ExigirAutenticacao(HandlerEnviarPresenca))
	mux.HandleFunc("/api/v1/chat/marcar-lido", ExigirAutenticacao(HandlerMarcarComoLido))
	mux.HandleFunc("/api/v1/chat/gerenciar", ExigirAutenticacao(HandlerGerenciarConversa))
	mux.HandleFunc("/api/v1/chat/estado", ExigirAutenticacao(HandlerGerenciarConversa))
	mux.HandleFunc("/api/v1/chat/etiqueta", ExigirAutenticacao(HandlerGerenciarEtiqueta))
	mux.HandleFunc("/api/v1/etiqueta/gerenciar", ExigirAutenticacao(HandlerGerenciarEtiqueta))

	// 8. Pacote 3: Gestão Completa de Grupos, Comunidades, Menção Fantasma e Status 24h
	mux.HandleFunc("/api/v1/grupo/listar", ExigirAutenticacao(HandlerListarGrupos))
	mux.HandleFunc("/api/v1/grupo/criar", ExigirAutenticacao(HandlerCriarGrupo))
	mux.HandleFunc("/api/v1/grupo/info", ExigirAutenticacao(HandlerInfoGrupo))
	mux.HandleFunc("/api/v1/grupo/participantes", ExigirAutenticacao(HandlerParticipantesGrupo))
	mux.HandleFunc("/api/v1/grupo/configurar", ExigirAutenticacao(HandlerConfigurarGrupo))
	mux.HandleFunc("/api/v1/grupo/convite", ExigirAutenticacao(HandlerLinkConviteGrupo))
	mux.HandleFunc("/api/v1/grupo/entrar", ExigirAutenticacao(HandlerEntrarGrupo))
	mux.HandleFunc("/api/v1/grupo/entrar-link", ExigirAutenticacao(HandlerEntrarGrupo))
	mux.HandleFunc("/api/v1/grupo/mencao-fantasma", ExigirAutenticacao(HandlerMencaoFantasmaGrupo))
	mux.HandleFunc("/api/v1/status/publicar", ExigirAutenticacao(HandlerPublicarStatus))

	// 8.1. Webhooks Receptores Nativos (Chatwoot, Kommo CRM, RD Station)
	mux.HandleFunc("/api/v1/integracoes/chatwoot/webhook", HandlerWebhookReversoChatwoot)
	mux.HandleFunc("/api/v1/integracoes/kommo/webhook", HandlerWebhookReversoKommo)
	mux.HandleFunc("/api/v1/integracoes/rdstation/webhook", HandlerWebhookReversoRDStation)

	// 9. Servidor de Arquivos Estáticos do Painel de Controle (Usa embed.FS no binário blindado ou disco local em dev)
	var fs http.Handler
	if info, err := os.Stat(filepath.Join(caminhoWeb, "index.html")); err == nil && !info.IsDir() {
		fs = http.FileServer(http.Dir(caminhoWeb))
	} else {
		fs = http.FileServer(http.FS(web.ArquivosEmbutidos))
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			responderErro(w, http.StatusNotFound, "Endpoint de API não encontrado")
			return
		}
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")

		if strings.HasSuffix(r.URL.Path, ".js") {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		} else if strings.HasSuffix(r.URL.Path, ".css") {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		} else if strings.HasSuffix(r.URL.Path, ".html") || r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}

		fs.ServeHTTP(w, r)
	})

	return middlewareSegurancaCORS(mux)
}

func middlewareSegurancaCORS(proximo http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Headers de Segurança HTTP (OWASP)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'self'; frame-ancestors 'self'; object-src 'none'; img-src 'self' data: https:; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; script-src 'self' 'unsafe-inline' 'unsafe-eval'; connect-src 'self' https: wss: ws:;")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")

		origemReq := r.Header.Get("Origin")
		origemPermitida := ""
		if config.AppConfig != nil && len(config.AppConfig.CorsOrigens) > 0 {
			if origemReq != "" {
				origemPermitida = ""
				for _, perm := range config.AppConfig.CorsOrigens {
					if strings.EqualFold(perm, origemReq) {
						origemPermitida = origemReq
						w.Header().Set("Vary", "Origin")
						break
					}
				}
			}
		}

		if origemPermitida != "" {
			w.Header().Set("Access-Control-Allow-Origin", origemPermitida)
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key, X-Auth-Token, X-Requested-With")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		proximo.ServeHTTP(w, r)
	})
}
