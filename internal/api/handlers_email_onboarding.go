package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/internal/config"
	"go.mau.fi/whatsmeow/internal/database"
	"go.mau.fi/whatsmeow/internal/email"
)

type oauthStateItem struct {
	RevendedorID string
	Verifier     string
	RedirectURI  string
	CriadoEm     time.Time
}

var (
	resendOAuthMu     sync.Mutex
	resendOAuthStates = make(map[string]oauthStateItem)
)

func limparOAuthStatesExpirados() {
	resendOAuthMu.Lock()
	defer resendOAuthMu.Unlock()
	agora := time.Now()
	for k, v := range resendOAuthStates {
		if agora.Sub(v.CriadoEm) > 15*time.Minute {
			delete(resendOAuthStates, k)
		}
	}
}

func resolverRedirectURIResend(r *http.Request) string {
	cfg := config.ObterConfig()
	if strings.TrimSpace(cfg.ResendOAuthRedirectURI) != "" {
		return cfg.ResendOAuthRedirectURI
	}
	esquema := "https"
	if r.TLS == nil && (strings.HasPrefix(r.Host, "localhost") || strings.HasPrefix(r.Host, "127.0.0.1")) {
		esquema = "http"
	}
	return fmt.Sprintf("%s://%s/api/v1/revenda/email/oauth/callback", esquema, r.Host)
}

func alvoRevendaEmail(r *http.Request, cli *database.Cliente) (*database.Revendedor, error) {
	if cli.Papel == database.PapelRevendedor {
		return database.DB.BuscarRevendedorPorID(cli.RevendedorID)
	}
	return database.DB.BuscarRevendedorPorDominio(r.Host)
}

func HandlerEmailRevenda(w http.ResponseWriter, r *http.Request) {
	cli := obterUsuarioAutenticado(r)
	if cli == nil || cli.ViaChaveAPI || (cli.Papel != database.PapelRevendedor && cli.Papel != database.PapelSuperAdmin) {
		responderErro(w, 403, "Apenas revendedores podem configurar e-mail")
		return
	}
	rev, err := alvoRevendaEmail(r, cli)
	if err != nil || rev == nil {
		responderErro(w, 404, "Revenda não localizada")
		return
	}

	cfgApp := config.ObterConfig()
	oauthDisponivel := strings.TrimSpace(cfgApp.ResendOAuthClientID) != ""

	if r.Method == http.MethodGet {
		c, e := database.DB.BuscarConfiguracaoEmailRevenda(rev.ID)
		if e != nil {
			responderSucesso(w, "Resend ainda não configurado", map[string]interface{}{
				"configurado":      false,
				"oauth_disponivel": oauthDisponivel,
			})
			return
		}
		c.APIKey = "" // nunca vazar chave nem token
		responderSucesso(w, "Configuração carregada", map[string]interface{}{
			"configurado":      true,
			"configuracao":     c,
			"oauth_disponivel": oauthDisponivel,
		})
		return
	}

	var b struct {
		APIKey        string `json:"api_key"`
		Dominio       string `json:"dominio"`
		Remetente     string `json:"remetente"`
		NomeRemetente string `json:"nome_remetente"`
	}
	if decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &b) != nil || strings.TrimSpace(b.APIKey) == "" || strings.TrimSpace(b.Remetente) == "" {
		responderErro(w, 400, "API Key e remetente são obrigatórios")
		return
	}

	// Tentar buscar domínios na API do Resend para verificar se a chave é válida e trazer domínios
	var dominiosJSON string
	verificado := false
	if dominios, errDom := email.ListarDominios(b.APIKey); errDom == nil {
		raw, _ := json.Marshal(dominios)
		dominiosJSON = string(raw)
		for _, d := range dominios {
			if strings.EqualFold(d.Name, strings.TrimSpace(b.Dominio)) && d.Status == "verified" {
				verificado = true
				break
			}
		}
	}

	c := &database.ConfiguracaoEmailRevenda{
		RevendedorID:  rev.ID,
		APIKey:        b.APIKey,
		Dominio:       strings.TrimSpace(b.Dominio),
		Remetente:     strings.TrimSpace(b.Remetente),
		NomeRemetente: strings.TrimSpace(b.NomeRemetente),
		MetodoAuth:    "api_key",
		DominiosJSON:  dominiosJSON,
		Ativo:         true,
		Verificado:    verificado,
	}
	if err := database.DB.SalvarConfiguracaoEmailRevenda(c); err != nil {
		responderErro(w, 500, "Erro ao salvar Resend: "+err.Error())
		return
	}
	c.APIKey = ""
	responderSucesso(w, "Resend configurado para a revenda", c)
}

func HandlerEmailOAuthIniciar(w http.ResponseWriter, r *http.Request) {
	cli := obterUsuarioAutenticado(r)
	if cli == nil || cli.ViaChaveAPI || (cli.Papel != database.PapelRevendedor && cli.Papel != database.PapelSuperAdmin) {
		responderErro(w, 403, "Sem permissão para configurar e-mail")
		return
	}
	rev, err := alvoRevendaEmail(r, cli)
	if err != nil || rev == nil {
		responderErro(w, 404, "Revenda não localizada")
		return
	}

	cfgApp := config.ObterConfig()
	clientID := strings.TrimSpace(cfgApp.ResendOAuthClientID)
	if clientID == "" {
		responderSucesso(w, "Resend OAuth requer credenciais da plataforma", map[string]interface{}{
			"oauth_configurado": false,
			"mensagem":          "Configure RESEND_OAUTH_CLIENT_ID no arquivo .env da plataforma para ativar o login 1-clique.",
		})
		return
	}

	verifier, challenge, err := email.GerarPKCE()
	if err != nil {
		responderErro(w, 500, "Erro ao gerar PKCE: "+err.Error())
		return
	}

	bytesState := make([]byte, 24)
	_, _ = rand.Read(bytesState)
	state := hex.EncodeToString(bytesState)

	redirectURI := resolverRedirectURIResend(r)

	limparOAuthStatesExpirados()
	resendOAuthMu.Lock()
	resendOAuthStates[state] = oauthStateItem{
		RevendedorID: rev.ID,
		Verifier:     verifier,
		RedirectURI:  redirectURI,
		CriadoEm:     time.Now(),
	}
	resendOAuthMu.Unlock()

	urlAuth := email.ConstruirURLAutorizacao(clientID, redirectURI, state, challenge)
	responderSucesso(w, "URL de autorização gerada com sucesso", map[string]interface{}{
		"oauth_configurado": true,
		"url":               urlAuth,
		"state":             state,
	})
}

func HandlerEmailOAuthCallback(w http.ResponseWriter, r *http.Request) {
	erroParam := r.URL.Query().Get("error")
	if erroParam != "" {
		erroDesc := r.URL.Query().Get("error_description")
		renderizarRespostaOAuthHTML(w, false, fmt.Sprintf("Autorização cancelada no Resend: %s (%s)", erroParam, erroDesc))
		return
	}

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || state == "" {
		renderizarRespostaOAuthHTML(w, false, "Código de autorização ou state ausente.")
		return
	}

	resendOAuthMu.Lock()
	item, ok := resendOAuthStates[state]
	if ok {
		delete(resendOAuthStates, state)
	}
	resendOAuthMu.Unlock()

	if !ok || item.RevendedorID == "" {
		renderizarRespostaOAuthHTML(w, false, "Sessão de autorização inválida ou expirada. Tente novamente.")
		return
	}

	cfgApp := config.ObterConfig()
	token, err := email.TrocarCodigoOAuth(
		cfgApp.ResendOAuthClientID,
		cfgApp.ResendOAuthClientSecret,
		code,
		item.RedirectURI,
		item.Verifier,
	)
	if err != nil {
		renderizarRespostaOAuthHTML(w, false, "Falha na troca de credenciais com o Resend: "+err.Error())
		return
	}

	rev, errRev := database.DB.BuscarRevendedorPorID(item.RevendedorID)
	nomeEmpresa := "WooHub"
	if errRev == nil && rev != nil && rev.NomeEmpresa != "" {
		nomeEmpresa = rev.NomeEmpresa
	}

	dominios, _ := email.ListarDominios(token.AccessToken)
	var domEscolhido string
	verificado := false
	for _, d := range dominios {
		if d.Status == "verified" {
			domEscolhido = d.Name
			verificado = true
			break
		}
	}
	if domEscolhido == "" && len(dominios) > 0 {
		domEscolhido = dominios[0].Name
	}
	if domEscolhido == "" {
		domEscolhido = "mail." + strings.ToLower(strings.ReplaceAll(nomeEmpresa, " ", "")) + ".com.br"
	}

	rawDominios, _ := json.Marshal(dominios)
	remetente := "contato@" + domEscolhido

	c := &database.ConfiguracaoEmailRevenda{
		RevendedorID:  item.RevendedorID,
		APIKey:        token.AccessToken,
		Dominio:       domEscolhido,
		Remetente:     remetente,
		NomeRemetente: nomeEmpresa,
		MetodoAuth:    "oauth",
		TokenRefresh:  token.RefreshToken,
		DominiosJSON:  string(rawDominios),
		Ativo:         true,
		Verificado:    verificado,
	}

	if err := database.DB.SalvarConfiguracaoEmailRevenda(c); err != nil {
		renderizarRespostaOAuthHTML(w, false, "Erro ao gravar configuração no banco: "+err.Error())
		return
	}

	renderizarRespostaOAuthHTML(w, true, "Conta do Resend conectada com sucesso!")
}

func renderizarRespostaOAuthHTML(w http.ResponseWriter, sucesso bool, mensagem string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	corTitulo := "#10b981"
	icone := "⚡"
	if !sucesso {
		corTitulo = "#ef4444"
		icone = "⚠️"
	}

	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="pt-BR">
<head>
  <meta charset="utf-8">
  <title>WooHub - Integração Resend</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #0f172a; color: #f8fafc; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; }
    .card { background: #1e293b; border: 1px solid #334155; padding: 2.5rem; border-radius: 16px; max-width: 480px; text-align: center; box-shadow: 0 10px 25px rgba(0,0,0,0.5); }
    h2 { color: %s; margin-top: 0; font-size: 1.4rem; display: flex; align-items: center; justify-content: center; gap: 8px; }
    p { color: #94a3b8; font-size: 0.95rem; line-height: 1.5; margin: 1.2rem 0; }
    .btn { display: inline-block; background: #2563eb; color: white; padding: 10px 24px; border-radius: 8px; text-decoration: none; font-weight: 500; font-size: 0.9rem; }
  </style>
</head>
<body>
  <div class="card">
    <h2>%s %s</h2>
    <p>%s</p>
    <a href="/#email-resend" class="btn" onclick="fecharOuRedirecionar()">Voltar ao WooHub</a>
  </div>
  <script>
    function fecharOuRedirecionar() {
      if (window.opener) {
        window.opener.postMessage({ tipo: 'resend_oauth_concluido', sucesso: %t }, '*');
        window.close();
      } else {
        window.location.href = '/#email-resend?conectado=1';
      }
    }
    // Auto-notificar e fechar se for popup
    if (window.opener) {
      setTimeout(fecharOuRedirecionar, 1800);
    } else {
      setTimeout(fecharOuRedirecionar, 2500);
    }
  </script>
</body>
</html>`, corTitulo, icone, func() string {
		if sucesso {
			return "Conexão Realizada!"
		}
		return "Atenção na Conexão"
	}(), mensagem, sucesso)
}

func HandlerEmailListarDominios(w http.ResponseWriter, r *http.Request) {
	cli := obterUsuarioAutenticado(r)
	if cli == nil || cli.Papel == database.PapelCliente {
		responderErro(w, 403, "Sem permissão")
		return
	}
	rev, err := alvoRevendaEmail(r, cli)
	if err != nil || rev == nil {
		responderErro(w, 404, "Revenda não localizada")
		return
	}
	c, err := database.DB.BuscarConfiguracaoEmailRevenda(rev.ID)
	if err != nil || strings.TrimSpace(c.APIKey) == "" {
		responderErro(w, 400, "Configure o Resend primeiro")
		return
	}

	dominios, err := email.ListarDominios(c.APIKey)
	if err != nil {
		responderErro(w, 502, "Erro ao consultar domínios no Resend: "+err.Error())
		return
	}

	raw, _ := json.Marshal(dominios)
	c.DominiosJSON = string(raw)

	for _, d := range dominios {
		if strings.EqualFold(d.Name, c.Dominio) && d.Status == "verified" {
			c.Verificado = true
			break
		}
	}
	_ = database.DB.SalvarConfiguracaoEmailRevenda(c)

	responderSucesso(w, "Domínios sincronizados com sucesso", map[string]interface{}{
		"dominios":               dominios,
		"verificado":             c.Verificado,
		"cloudflare_connect_url": "https://resend.com/domains",
	})
}

func HandlerTestarEmailRevenda(w http.ResponseWriter, r *http.Request) {
	cli := obterUsuarioAutenticado(r)
	if cli == nil || cli.Papel == database.PapelCliente {
		responderErro(w, 403, "Sem permissão")
		return
	}
	rev, err := alvoRevendaEmail(r, cli)
	if err != nil || rev == nil {
		responderErro(w, 404, "Revenda não localizada")
		return
	}
	c, err := database.DB.BuscarConfiguracaoEmailRevenda(rev.ID)
	if err != nil {
		responderErro(w, 400, "Configure o Resend primeiro")
		return
	}
	var b struct {
		Destinatario string `json:"destinatario"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if strings.TrimSpace(b.Destinatario) == "" {
		b.Destinatario = cli.Email
	}
	if err := (email.Cliente{APIKey: c.APIKey, From: c.Remetente, FromName: c.NomeRemetente}).Enviar(b.Destinatario, "Teste de e-mail — "+rev.NomeEmpresa, "<p>O Resend foi configurado com sucesso.</p>"); err != nil {
		responderErro(w, 502, err.Error())
		return
	}
	c.Verificado = true
	_ = database.DB.SalvarConfiguracaoEmailRevenda(c)
	responderSucesso(w, "E-mail de teste enviado", nil)
}

func HandlerOnboarding(w http.ResponseWriter, r *http.Request) {
	cli := obterUsuarioAutenticado(r)
	if cli == nil || cli.ViaChaveAPI {
		responderErro(w, 401, "Sessão inválida")
		return
	}
	if r.Method == http.MethodGet {
		d, e := database.DB.BuscarOnboarding(cli.ID)
		if e != nil {
			d = map[string]interface{}{"usuario_id": cli.ID, "papel": cli.Papel, "etapa_atual": "conta", "concluido": false, "dados": "{}"}
		}
		responderSucesso(w, "Onboarding carregado", d)
		return
	}
	var b struct {
		Etapa     string                 `json:"etapa"`
		Dados     map[string]interface{} `json:"dados"`
		Concluido bool                   `json:"concluido"`
	}
	if decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &b) != nil || strings.TrimSpace(b.Etapa) == "" {
		responderErro(w, 400, "Etapa obrigatória")
		return
	}
	raw, _ := json.Marshal(b.Dados)
	if err := database.DB.SalvarEtapaOnboarding(cli.ID, cli.Papel, b.Etapa, string(raw), b.Concluido); err != nil {
		responderErro(w, 500, "Erro ao salvar onboarding")
		return
	}
	responderSucesso(w, "Onboarding atualizado", nil)
}
