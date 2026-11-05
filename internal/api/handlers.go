package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.mau.fi/whatsmeow/internal/config"
	"go.mau.fi/whatsmeow/internal/core"
	"go.mau.fi/whatsmeow/internal/database"
	"go.mau.fi/whatsmeow/internal/licenca"
)

type chaveContexto string

const contextoUsuarioKey chaveContexto = "usuario_autenticado"

const (
	limiteBodyJSONPadrao = 1 << 20  // 1 MB
	limiteBodyMidia      = 25 << 20 // 25 MB
)

type RespostaPadrao struct {
	Sucesso  bool        `json:"sucesso"`
	Mensagem string      `json:"mensagem,omitempty"`
	Dados    interface{} `json:"dados,omitempty"`
	Erro     string      `json:"erro,omitempty"`
}

func responderJSON(w http.ResponseWriter, status int, dados interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(dados)
}

func responderErro(w http.ResponseWriter, status int, mensagem string) {
	responderJSON(w, status, RespostaPadrao{
		Sucesso: false,
		Erro:    mensagem,
	})
}

func responderSucesso(w http.ResponseWriter, mensagem string, dados interface{}) {
	responderJSON(w, http.StatusOK, RespostaPadrao{
		Sucesso:  true,
		Mensagem: mensagem,
		Dados:    dados,
	})
}

func decodificarJSONLimitado(w http.ResponseWriter, r *http.Request, limite int64, destino interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, limite)
	dec := json.NewDecoder(r.Body)
	return dec.Decode(destino)
}

// ============================================================================
// RATE LIMITER PARA LOGIN (PROTEÇÃO CONTRA FORÇA BRUTA)
// ============================================================================

type registroTentativas struct {
	contagem  int
	janelaFim time.Time
}

var (
	muLoginRate         sync.Mutex
	tentativasLoginIP   = make(map[string]*registroTentativas)
	maxTentativasMinuto = 12
)

func obterIPCliente(r *http.Request) string {
	// Não confiar em X-Forwarded-For recebido diretamente do cliente. O proxy
	// reverso deve remover/substituir esse cabeçalho antes de encaminhar a requisição.
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func limitarTaxaLogin(proximo http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := obterIPCliente(r)
		agora := time.Now()

		muLoginRate.Lock()
		reg, existe := tentativasLoginIP[ip]
		if !existe || agora.After(reg.janelaFim) {
			reg = &registroTentativas{contagem: 0, janelaFim: agora.Add(1 * time.Minute)}
			tentativasLoginIP[ip] = reg
		}
		reg.contagem++
		atual := reg.contagem
		muLoginRate.Unlock()

		if atual > maxTentativasMinuto {
			responderErro(w, http.StatusTooManyRequests, "Muitas tentativas de login. Aguarde 1 minuto antes de tentar novamente.")
			return
		}

		proximo(w, r)
	}
}

// ============================================================================
// AUTENTICAÇÃO E CONTROLE DE ACESSO MULTITENANT (RBAC)
// ============================================================================

func extrairToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth != "" {
		partes := strings.SplitN(auth, " ", 2)
		if len(partes) == 2 && strings.EqualFold(partes[0], "Bearer") {
			return strings.TrimSpace(partes[1])
		}
		return strings.TrimSpace(auth)
	}

	if token := r.Header.Get("X-Auth-Token"); token != "" {
		return strings.TrimSpace(token)
	}

	if cookie, err := r.Cookie("woohub_token"); err == nil && cookie.Value != "" {
		return strings.TrimSpace(cookie.Value)
	}

	if token := r.URL.Query().Get("token"); token != "" {
		return strings.TrimSpace(token)
	}

	return ""
}

func validarAutenticacao(r *http.Request) (*database.Cliente, error) {
	// 1. Validação por Token de Sessão (Painel Web ou Bearer API)
	token := extrairToken(r)
	if token != "" {
		cli, err := database.DB.ValidarSessao(token)
		if err == nil && cli != nil {
			return cli, nil
		}
		// Suporte a Chave de API de Instância via Bearer Token (n8n / Postman / cURL)
		inst, errInst := database.DB.BuscarInstanciaPorChave(token)
		if errInst == nil && inst != nil {
			cliInst, errCli := database.DB.BuscarClientePorID(inst.ClienteID)
			if errCli == nil && cliInst != nil && cliInst.Ativo {
				cliCopia := *cliInst
				cliCopia.ViaChaveAPI = true
				cliCopia.InstanciaKeyID = inst.ID
				return &cliCopia, nil
			}
		}
	}

	// 2. Validação por x-api-key de Instância (escopo estrito à instância da chave)
	apiKey := r.Header.Get("x-api-key")
	if apiKey == "" {
		apiKey = r.Header.Get("X-Api-Key")
	}
	if apiKey != "" {
		inst, err := database.DB.BuscarInstanciaPorChave(apiKey)
		if err == nil && inst != nil {
			cli, err := database.DB.BuscarClientePorID(inst.ClienteID)
			if err == nil && cli != nil && cli.Ativo {
				cliCopia := *cli
				cliCopia.ViaChaveAPI = true
				cliCopia.InstanciaKeyID = inst.ID
				return &cliCopia, nil
			}
		}
	}

	return nil, fmt.Errorf("não autorizado. Por favor faça login")
}

func ExigirAutenticacao(proximo http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cli, err := validarAutenticacao(r)
		if err != nil {
			responderErro(w, http.StatusUnauthorized, "Sessão expirada ou não autenticada. Faça login para continuar.")
			return
		}
		// Regras estritas de RBAC têm precedência sobre status de infraestrutura (403 Forbidden antes de 412)
		if strings.HasPrefix(r.URL.Path, "/api/v1/revendedor/") && cli.Papel != database.PapelSuperAdmin {
			responderErro(w, http.StatusForbidden, "Apenas o Mega Admin tem permissão para acessar recursos de revendedores")
			return
		}
		if (r.URL.Path == "/api/v1/cliente/listar" || r.URL.Path == "/api/v1/cliente/criar") && cli.Papel == database.PapelCliente {
			responderErro(w, http.StatusForbidden, "Clientes finais não possuem permissão para gerenciar a carteira de clientes")
			return
		}

		// O control plane continua acessível ao Super Admin para configurar a
		// infraestrutura. Todos os demais usuários só podem operar quando a
		// revenda possui Supabase e MinIO próprios validados.
		if cli.Papel != database.PapelSuperAdmin && !rotaConfiguraInfraestrutura(r.URL.Path) {
			rev, revErr := database.DB.BuscarRevendedorPorID(cli.RevendedorID)
			if revErr != nil || (rev.ID != "rev_woohub_oficial" && !rev.InfraestruturaBYOPronta()) {
				responderErro(w, http.StatusPreconditionFailed, "A infraestrutura da revenda ainda não está configurada. Cadastre e valide o Supabase e o MinIO próprios para liberar o painel.")
				return
			}
		}
		ctx := context.WithValue(r.Context(), contextoUsuarioKey, cli)
		proximo(w, r.WithContext(ctx))
	}
}

func rotaConfiguraInfraestrutura(path string) bool {
	return strings.HasPrefix(path, "/api/v1/revenda/infra") ||
		path == "/api/v1/revenda/marca" ||
		path == "/api/v1/auth/logout" ||
		path == "/api/v1/auth/me" ||
		path == "/api/v1/auth/alterar-senha" ||
		path == "/api/v1/licenca/status-local"
}

func obterUsuarioAutenticado(r *http.Request) *database.Cliente {
	if v := r.Context().Value(contextoUsuarioKey); v != nil {
		if cli, ok := v.(*database.Cliente); ok {
			return cli
		}
	}
	cli, _ := validarAutenticacao(r)
	return cli
}

// verificarAcessoInstancia garante que um cliente ou x-api-key jamais acesse instâncias de terceiros (Anti-IDOR)
func verificarAcessoInstancia(cli *database.Cliente, inst *database.Instancia) error {
	if cli == nil || inst == nil {
		return fmt.Errorf("acesso negado à instância")
	}

	// Chave de API de instância só pode operar na própria instância a que pertence
	if cli.ViaChaveAPI {
		if cli.InstanciaKeyID != inst.ID {
			return fmt.Errorf("esta x-api-key pertence a outra instância e não tem permissão para acessar %s", inst.ID)
		}
		return nil
	}

	if cli.Papel == database.PapelSuperAdmin {
		return nil
	}

	if cli.Papel == database.PapelRevendedor {
		if inst.ClienteID == cli.ID {
			return nil
		}
		dono, err := database.DB.BuscarClientePorID(inst.ClienteID)
		if err == nil && dono != nil && dono.RevendedorID == cli.RevendedorID {
			return nil
		}
		return fmt.Errorf("esta instância não pertence à sua carteira de revenda")
	}

	if inst.ClienteID != cli.ID {
		return fmt.Errorf("você não tem permissão para acessar ou modificar esta instância")
	}

	return nil
}

// ============================================================================
// HANDLERS DE AUTENTICAÇÃO
// ============================================================================

func HandlerLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}

	var body struct {
		Email string `json:"email"`
		Senha string `json:"senha"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo da requisição inválido")
		return
	}

	cli, token, err := database.DB.AutenticarUsuario(body.Email, body.Senha)
	if err != nil {
		responderErro(w, http.StatusUnauthorized, err.Error())
		return
	}

	var rev *database.Revendedor
	if cli.RevendedorID != "" {
		rev, _ = database.DB.BuscarRevendedorPorID(cli.RevendedorID)
	}
	if rev == nil {
		rev, _ = database.DB.BuscarRevendedorPorDominio(r.Host)
	}

	cookieSeguro := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") || (config.AppConfig != nil && config.AppConfig.EhProducao())

	http.SetCookie(w, &http.Cookie{
		Name:     "woohub_token",
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(7 * 24 * time.Hour),
		HttpOnly: true,
		Secure:   cookieSeguro,
		SameSite: http.SameSiteLaxMode,
	})

	responderSucesso(w, "Login realizado com sucesso!", map[string]interface{}{
		"token": token,
		"usuario": map[string]interface{}{
			"id":                cli.ID,
			"revendedor_id":     cli.RevendedorID,
			"nome":              cli.Nome,
			"email":             cli.Email,
			"documento":         cli.Documento,
			"papel":             cli.Papel,
			"limite_instancias": cli.LimiteInstancias,
		},
		"revendedor": rev,
	})
}

func HandlerLogout(w http.ResponseWriter, r *http.Request) {
	token := extrairToken(r)
	if token != "" {
		_ = database.DB.EncerrarSessao(token)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "woohub_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	responderSucesso(w, "Sessão encerrada com sucesso!", nil)
}

func HandlerUsuarioAtual(w http.ResponseWriter, r *http.Request) {
	cli, err := validarAutenticacao(r)
	if err != nil || cli == nil || cli.ViaChaveAPI {
		responderJSON(w, http.StatusOK, RespostaPadrao{
			Sucesso: false,
			Erro:    "Sessão inválida ou expirada",
		})
		return
	}

	var rev *database.Revendedor
	if cli.RevendedorID != "" {
		rev, _ = database.DB.BuscarRevendedorPorID(cli.RevendedorID)
	}
	if rev == nil {
		rev, _ = database.DB.BuscarRevendedorPorDominio(r.Host)
	}

	responderSucesso(w, "Usuário autenticado", map[string]interface{}{
		"usuario": map[string]interface{}{
			"id":                cli.ID,
			"revendedor_id":     cli.RevendedorID,
			"nome":              cli.Nome,
			"email":             cli.Email,
			"documento":         cli.Documento,
			"papel":             cli.Papel,
			"limite_instancias": cli.LimiteInstancias,
		},
		"revendedor": rev,
	})
}

func HandlerListarPlanos(w http.ResponseWriter, r *http.Request) {
	cli, _ := validarAutenticacao(r)
	limiteUsuario := 5
	papelUsuario := "cliente"
	if cli != nil {
		if cli.LimiteInstancias > 0 {
			limiteUsuario = cli.LimiteInstancias
		}
		if cli.Papel != "" {
			papelUsuario = cli.Papel
		}
	}

	planos := []map[string]interface{}{
		{
			"id":                "basico",
			"nome":              "Starter / Básico",
			"preco_mensal":      "R$ 197,00",
			"limite_instancias": 5,
			"franquia_mensagens": "30.000 disparos/mês",
			"destaque":          false,
			"recursos": []string{
				"Até 5 Instâncias WhatsApp Simultâneas",
				"Pacote 1: Texto, Áudio PTT Microfone Verde, Imagem, Vídeo, PDF e Enquetes",
				"Pacote 2: Validador 9º Dígito BR + Download Automático de Mídias no Webhook",
				"Webhooks em Tempo Real com Assinatura HMAC SHA-256",
			},
		},
		{
			"id":                "profissional",
			"nome":              "Business Pro",
			"preco_mensal":      "R$ 297,00",
			"limite_instancias": 20,
			"franquia_mensagens": "100.000 disparos/mês",
			"destaque":          true,
			"recursos": []string{
				"Até 20 Instâncias WhatsApp Simultâneas",
				"Todos os recursos do Pacote 1 e Pacote 2 liberados",
				"Pacote 3: Automação de Grupos, Menção Fantasma (@todos) e Status 24h",
				"Pacote 4: Blindagem Anti-Ban, Proxy SOCKS5/HTTP e Rejeição de Chamadas VoIP",
				"Etiquetas WhatsApp Business e Simulação Humana (Digitando / Gravando Áudio)",
			},
		},
		{
			"id":                "enterprise",
			"nome":              "Enterprise White-Label & Stack",
			"preco_mensal":      "R$ 497,00",
			"limite_instancias": 150,
			"franquia_mensagens": "Disparos Ilimitados",
			"destaque":          false,
			"recursos": []string{
				"Até 150 Instâncias WhatsApp + Subcontas de Clientes",
				"Painel 100% White-Label com Domínio Próprio (CNAME) e Cores da Agência",
				"Infraestrutura Isolada Opcional (BYO Supabase PostgreSQL + MinIO S3)",
				"Deploy Dedicado via Stack Portainer com SLA 99,98% e Suporte Prioritário",
			},
		},
	}

	responderSucesso(w, "Planos listados com sucesso", map[string]interface{}{
		"planos":            planos,
		"limite_contratado": limiteUsuario,
		"papel_atual":       papelUsuario,
	})
}

func HandlerAlterarSenha(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}

	cli := obterUsuarioAutenticado(r)
	if cli == nil || cli.ViaChaveAPI {
		responderErro(w, http.StatusForbidden, "Operação permitida apenas via sessão de usuário")
		return
	}

	var body struct {
		SenhaAtual string `json:"senha_atual"`
		NovaSenha  string `json:"nova_senha"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Dados inválidos")
		return
	}

	if !database.VerificarSenha(body.SenhaAtual, cli.SenhaHash) {
		responderErro(w, http.StatusBadRequest, "Senha atual incorreta")
		return
	}

	if err := database.DB.AtualizarSenhaCliente(cli.ID, body.NovaSenha); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}

	responderSucesso(w, "Senha alterada com sucesso!", nil)
}

// ============================================================================
// STATUS & WHITE-LABEL / REVENDEDORES
// ============================================================================

func HandlerStatus(w http.ResponseWriter, r *http.Request) {
	revendedor, _ := database.DB.BuscarRevendedorPorDominio(r.Host)
	nomeMarca := "WooHub API Brasil"
	if revendedor != nil && revendedor.NomeEmpresa != "" {
		nomeMarca = revendedor.NomeEmpresa
	}

	bancoDialeto := "sqlite"
	if database.DB != nil && database.DB.ObterDialetoWhatsmeow() == "postgres" {
		bancoDialeto = "postgresql (supabase)"
	}

	responderSucesso(w, "WooHub API operacional", map[string]interface{}{
		"status":           "ONLINE",
		"motor":            "WooHub Native Engine (Go Multidevice)",
		"banco_dados":      bancoDialeto,
		"versao":           "1.2.0-Brasil-Prod",
		"marca":            nomeMarca,
		"fuso_horario":     "America/Sao_Paulo",
		"hora_atual":       time.Now().Format("2006-01-02 15:04:05"),
		"nono_digito_br":   true,
		"audio_ptt_nativo": true,
	})
}

func HandlerMarcaRevenda(w http.ResponseWriter, r *http.Request) {
	revendedor, err := database.DB.BuscarRevendedorPorDominio(r.Host)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, "Erro ao carregar dados de marca")
		return
	}

	responderSucesso(w, "Dados da marca carregados com sucesso", revendedor)
}

func HandlerConfigurarMarca(w http.ResponseWriter, r *http.Request) {
	cli := obterUsuarioAutenticado(r)
	if cli == nil || cli.ViaChaveAPI || (cli.Papel != database.PapelSuperAdmin && cli.Papel != database.PapelRevendedor) {
		responderErro(w, http.StatusForbidden, "Apenas administradores e revendedores podem configurar a marca White-Label")
		return
	}

	var body struct {
		NomeEmpresa          string `json:"nome_empresa"`
		DominioPersonalizado string `json:"dominio_personalizado"`
		LogoURL              string `json:"logo_url"`
		FaviconURL           string `json:"favicon_url"`
		CorPrimaria          string `json:"cor_primaria"`
		CorSecundaria        string `json:"cor_secundaria"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo da requisição inválido")
		return
	}

	var rev *database.Revendedor
	var err error
	if cli.Papel == database.PapelRevendedor {
		rev, err = database.DB.BuscarRevendedorPorID(cli.RevendedorID)
	} else {
		rev, err = database.DB.BuscarRevendedorPorDominio(r.Host)
	}
	if err != nil || rev == nil {
		responderErro(w, http.StatusNotFound, "Revendedor não localizado")
		return
	}

	if body.NomeEmpresa != "" {
		rev.NomeEmpresa = strings.TrimSpace(body.NomeEmpresa)
	}
	if body.DominioPersonalizado != "" {
		rev.DominioPersonalizado = strings.TrimSpace(body.DominioPersonalizado)
	}
	if body.LogoURL != "" {
		rev.LogoURL = strings.TrimSpace(body.LogoURL)
	}
	if body.FaviconURL != "" {
		rev.FaviconURL = strings.TrimSpace(body.FaviconURL)
	}
	if body.CorPrimaria != "" {
		rev.CorPrimaria = strings.TrimSpace(body.CorPrimaria)
	}
	if body.CorSecundaria != "" {
		rev.CorSecundaria = strings.TrimSpace(body.CorSecundaria)
	}

	if err := database.DB.AtualizarRevendedor(rev); err != nil {
		responderErro(w, http.StatusInternalServerError, "Erro ao atualizar configurações de marca: "+err.Error())
		return
	}

	responderSucesso(w, "Marca própria configurada com sucesso!", rev)
}

func HandlerListarRevendedores(w http.ResponseWriter, r *http.Request) {
	cli := obterUsuarioAutenticado(r)
	if cli == nil || cli.ViaChaveAPI || cli.Papel != database.PapelSuperAdmin {
		responderErro(w, http.StatusForbidden, "Apenas o Mega Admin tem permissão para listar revendedores da plataforma")
		return
	}

	revendedores, err := database.DB.ListarRevendedores()
	if err != nil {
		responderErro(w, http.StatusInternalServerError, "Erro ao listar revendedores: "+err.Error())
		return
	}

	responderSucesso(w, "Revendedores listados com sucesso", revendedores)
}

func HandlerCriarRevendedor(w http.ResponseWriter, r *http.Request) {
	cli := obterUsuarioAutenticado(r)
	if cli == nil || cli.ViaChaveAPI || cli.Papel != database.PapelSuperAdmin {
		responderErro(w, http.StatusForbidden, "Apenas o Super Admin pode cadastrar novos revendedores White-Label")
		return
	}

	var body struct {
		NomeEmpresa          string `json:"nome_empresa"`
		Slug                 string `json:"slug"`
		DominioPersonalizado string `json:"dominio_personalizado"`
		CorPrimaria          string `json:"cor_primaria"`
		LimiteInstancias     int    `json:"limite_instancias"`
		EmailAcesso          string `json:"email_acesso"`
		SenhaAcesso          string `json:"senha_acesso"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil || strings.TrimSpace(body.NomeEmpresa) == "" {
		responderErro(w, http.StatusBadRequest, "Nome da empresa parceira é obrigatório")
		return
	}

	novoRev, err := database.DB.CriarRevendedor(
		strings.TrimSpace(body.NomeEmpresa),
		strings.TrimSpace(body.Slug),
		strings.TrimSpace(body.DominioPersonalizado),
		strings.TrimSpace(body.CorPrimaria),
		body.LimiteInstancias,
	)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, "Erro ao cadastrar revendedor: "+err.Error())
		return
	}

	var usuarioCriado map[string]interface{}
	if strings.TrimSpace(body.EmailAcesso) != "" {
		usuarioCriado, _ = database.DB.CriarUsuarioRevendedor(
			novoRev.ID,
			novoRev.NomeEmpresa,
			strings.TrimSpace(body.EmailAcesso),
			body.SenhaAcesso,
			novoRev.LimiteInstancias,
		)
	}

	responderSucesso(w, "Revendedor White-Label cadastrado com sucesso!", map[string]interface{}{
		"revendedor":     novoRev,
		"usuario_acesso": usuarioCriado,
	})
}

func HandlerAtualizarRevendedor(w http.ResponseWriter, r *http.Request) {
	cli := obterUsuarioAutenticado(r)
	if cli == nil || cli.ViaChaveAPI || cli.Papel != database.PapelSuperAdmin { responderErro(w, http.StatusForbidden, "Apenas o Mega Admin pode editar revendedores"); return }
	var body struct { ID string `json:"id"`; NomeEmpresa string `json:"nome_empresa"`; DominioPersonalizado string `json:"dominio_personalizado"`; CorPrimaria string `json:"cor_primaria"`; LimiteInstancias int `json:"limite_instancias"` }
	if decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body) != nil || strings.TrimSpace(body.ID) == "" || strings.TrimSpace(body.NomeEmpresa) == "" { responderErro(w, http.StatusBadRequest, "ID e nome da empresa são obrigatórios"); return }
	if err := database.DB.AtualizarRevendedorAdministrativo(body.ID, body.NomeEmpresa, body.DominioPersonalizado, body.CorPrimaria, body.LimiteInstancias); err != nil { responderErro(w, http.StatusInternalServerError, "Erro ao atualizar revendedor: "+err.Error()); return }
	rev, err := database.DB.BuscarRevendedorPorID(body.ID); if err != nil { responderErro(w, http.StatusInternalServerError, "Revendedor atualizado, mas não foi possível recarregar os dados"); return }
	responderSucesso(w, "Revendedor atualizado com sucesso", rev)
}

// ============================================================================
// GESTÃO DE CLIENTES (ISOLADO POR REVENDEDOR / SUPERADMIN)
// ============================================================================

func HandlerListarClientes(w http.ResponseWriter, r *http.Request) {
	cli := obterUsuarioAutenticado(r)
	if cli == nil || cli.ViaChaveAPI || (cli.Papel != database.PapelSuperAdmin && cli.Papel != database.PapelRevendedor) {
		responderErro(w, http.StatusForbidden, "Apenas administradores e revendedores podem listar a carteira de clientes")
		return
	}

	revId := r.URL.Query().Get("revendedor_id")
	if cli.Papel == database.PapelRevendedor {
		revId = cli.RevendedorID
	}

	clientes, err := database.DB.ListarClientes(revId)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, "Erro ao listar clientes: "+err.Error())
		return
	}
	responderSucesso(w, "Clientes listados com sucesso", clientes)
}

func HandlerCriarCliente(w http.ResponseWriter, r *http.Request) {
	cli := obterUsuarioAutenticado(r)
	if cli == nil || cli.ViaChaveAPI || (cli.Papel != database.PapelSuperAdmin && cli.Papel != database.PapelRevendedor) {
		responderErro(w, http.StatusForbidden, "Sem permissão para cadastrar clientes")
		return
	}

	var body struct {
		RevendedorID     string `json:"revendedor_id"`
		Nome             string `json:"nome"`
		Email            string `json:"email"`
		Senha            string `json:"senha"`
		Documento        string `json:"documento"`
		LimiteInstancias int    `json:"limite_instancias"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil || strings.TrimSpace(body.Nome) == "" || strings.TrimSpace(body.Email) == "" {
		responderErro(w, http.StatusBadRequest, "Nome e e-mail do cliente são obrigatórios")
		return
	}

	revAlvo := body.RevendedorID
	if cli.Papel == database.PapelRevendedor {
		revAlvo = cli.RevendedorID
	}

	novoCli, err := database.DB.CriarCliente(
		revAlvo,
		strings.TrimSpace(body.Nome),
		strings.TrimSpace(body.Email),
		body.Senha,
		strings.TrimSpace(body.Documento),
		body.LimiteInstancias,
	)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, "Erro ao cadastrar cliente: "+err.Error())
		return
	}

	responderSucesso(w, "Cliente cadastrado com sucesso!", novoCli)
}

func HandlerApagarCliente(w http.ResponseWriter, r *http.Request) {
	cli := obterUsuarioAutenticado(r)
	if cli == nil || cli.ViaChaveAPI || (cli.Papel != database.PapelSuperAdmin && cli.Papel != database.PapelRevendedor) {
		responderErro(w, http.StatusForbidden, "Sem permissão para excluir clientes")
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		var body struct {
			ID string `json:"id"`
		}
		_ = decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body)
		id = body.ID
	}

	if id == "" {
		responderErro(w, http.StatusBadRequest, "ID do cliente é obrigatório")
		return
	}

	if id == cli.ID {
		responderErro(w, http.StatusBadRequest, "Você não pode excluir sua própria conta ativa")
		return
	}

	alvo, err := database.DB.BuscarClientePorID(id)
	if err != nil || alvo == nil {
		responderErro(w, http.StatusNotFound, "Cliente não encontrado")
		return
	}

	if cli.Papel == database.PapelRevendedor && alvo.RevendedorID != cli.RevendedorID {
		responderErro(w, http.StatusForbidden, "Você não tem permissão para excluir clientes de outra revenda")
		return
	}

	// Desconecta e remove da memória todas as instâncias pertencentes ao cliente
	instsCliente, _ := database.DB.ListarInstancias(id)
	for _, ic := range instsCliente {
		core.Gerenciador.RemoverInstancia(r.Context(), ic.ID)
	}

	if err := database.DB.ApagarCliente(id); err != nil {
		responderErro(w, http.StatusInternalServerError, "Erro ao excluir cliente: "+err.Error())
		return
	}

	responderSucesso(w, "Cliente e conexões excluídos com sucesso!", nil)
}

// ============================================================================
// GESTÃO DE INSTÂNCIAS (COM PROTEÇÃO RBAC / ISOLAMENTO POR TENANT)
// ============================================================================

func HandlerListarInstancias(w http.ResponseWriter, r *http.Request) {
	cli := obterUsuarioAutenticado(r)
	if cli == nil {
		responderErro(w, http.StatusUnauthorized, "Não autorizado")
		return
	}

	if cli.ViaChaveAPI {
		inst, err := database.DB.BuscarInstanciaPorId(cli.InstanciaKeyID)
		if err != nil || inst == nil {
			responderSucesso(w, "Instâncias recuperadas com sucesso", []database.Instancia{})
			return
		}
		responderSucesso(w, "Instâncias recuperadas com sucesso", []database.Instancia{*inst})
		return
	}

	clienteIdFiltro := r.URL.Query().Get("cliente_id")

	var instancias []database.Instancia
	var err error

	if cli.Papel == database.PapelSuperAdmin {
		instancias, err = database.DB.ListarInstancias(clienteIdFiltro)
	} else if cli.Papel == database.PapelRevendedor {
		if clienteIdFiltro != "" {
			alvo, errAlvo := database.DB.BuscarClientePorID(clienteIdFiltro)
			if errAlvo != nil || alvo == nil || alvo.RevendedorID != cli.RevendedorID {
				responderErro(w, http.StatusForbidden, "Cliente filtrado não pertence à sua revenda")
				return
			}
			instancias, err = database.DB.ListarInstancias(clienteIdFiltro)
		} else {
			instancias, err = database.DB.ListarInstanciasPorRevendedor(cli.RevendedorID)
		}
	} else {
		instancias, err = database.DB.ListarInstancias(cli.ID)
	}

	if err != nil {
		responderErro(w, http.StatusInternalServerError, "Erro ao listar instâncias: "+err.Error())
		return
	}

	if core.Gerenciador != nil {
		instancias = core.Gerenciador.SincronizarStatusInstancias(instancias)
	}

	responderSucesso(w, "Instâncias recuperadas com sucesso", instancias)
}

func HandlerCriarInstancia(w http.ResponseWriter, r *http.Request) {
	cli := obterUsuarioAutenticado(r)
	if cli == nil || cli.ViaChaveAPI {
		responderErro(w, http.StatusForbidden, "Operação não permitida com chave de instância única")
		return
	}

	var body struct {
		Nome                     string `json:"nome"`
		ClienteID                string `json:"cliente_id"`
		WebhookURL               string `json:"webhook_url"`
		ProxyURL                 string `json:"proxy_url"`
		RejeitarChamadas         bool   `json:"rejeitar_chamadas"`
		MensagemChamadaRejeitada string `json:"mensagem_chamada_rejeitada"`
		IgnorarGrupos            bool   `json:"ignorar_grupos"`
		LeituraAutomatica        bool   `json:"leitura_automatica"`
		SempreOnline             bool   `json:"sempre_online"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil || strings.TrimSpace(body.Nome) == "" {
		responderErro(w, http.StatusBadRequest, "Nome da instância é obrigatório")
		return
	}

	whURL := strings.TrimSpace(body.WebhookURL)
	if whURL != "" {
		if err := core.ValidarURLWebhook(whURL); err != nil {
			responderErro(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	clienteAlvoID := strings.TrimSpace(body.ClienteID)
	if cli.Papel == database.PapelCliente || clienteAlvoID == "" {
		clienteAlvoID = cli.ID
	} else if cli.Papel == database.PapelRevendedor && clienteAlvoID != cli.ID {
		alvo, err := database.DB.BuscarClientePorID(clienteAlvoID)
		if err != nil || alvo == nil || alvo.RevendedorID != cli.RevendedorID {
			responderErro(w, http.StatusForbidden, "Você só pode criar instâncias para clientes da sua revenda")
			return
		}
	}

	// 1. Verifica o teto criptográfico da Licença do Container (impede burlar limite editando o próprio Supabase)
	todasInsts, _ := database.DB.ListarInstancias()
	tetoLicenca := licenca.ObterLimiteInstanciasLicenciado()
	if tetoLicenca > 0 && len(todasInsts) >= tetoLicenca {
		responderErro(w, http.StatusForbidden, fmt.Sprintf("Limite global da Licença WooHub atingido (%d de %d instâncias permitidas na sua licença). Faça upgrade da sua licença.", len(todasInsts), tetoLicenca))
		return
	}

	// 2. Verifica limite de instâncias do cliente alvo
	if cli.Papel != database.PapelSuperAdmin {
		existentes, _ := database.DB.ListarInstancias(clienteAlvoID)
		if cli.LimiteInstancias > 0 && len(existentes) >= cli.LimiteInstancias {
			responderErro(w, http.StatusForbidden, fmt.Sprintf("Limite de instâncias atingido (%d de %d)", len(existentes), cli.LimiteInstancias))
			return
		}
	}

	novaInst, err := database.DB.CriarInstancia(clienteAlvoID, strings.TrimSpace(body.Nome))
	if err != nil {
		responderErro(w, http.StatusInternalServerError, "Erro ao criar instância: "+err.Error())
		return
	}

	if whURL != "" {
		_ = database.DB.AtualizarWebhookInstancia(novaInst.ID, whURL, "mensagem_recebida,status_envio,conexao,chamada_recebida,voto_enquete,reacao_mensagem", "")
		novaInst.WebhookURL = whURL
	}

	if body.ProxyURL != "" || body.RejeitarChamadas || body.IgnorarGrupos || body.LeituraAutomatica || body.SempreOnline || body.MensagemChamadaRejeitada != "" {
		_ = core.Gerenciador.ConfigurarInstanciaAvancada(r.Context(), novaInst.ID, core.ConfiguracaoInstanciaReq{
			IgnorarGrupos:            body.IgnorarGrupos,
			RejeitarChamadas:         body.RejeitarChamadas,
			MensagemChamadaRejeitada: body.MensagemChamadaRejeitada,
			ProxyURL:                 body.ProxyURL,
			LeituraAutomatica:        body.LeituraAutomatica,
			SempreOnline:             body.SempreOnline,
		})
		if atualizada, errAt := database.DB.BuscarInstanciaPorId(novaInst.ID); errAt == nil && atualizada != nil {
			novaInst = atualizada
		}
	}

	responderSucesso(w, "Instância criada com sucesso!", novaInst)
}

func validarPropriedadeInstanciaPorID(w http.ResponseWriter, r *http.Request, id string) (*database.Instancia, bool) {
	if id == "" {
		responderErro(w, http.StatusBadRequest, "ID da instância não informado")
		return nil, false
	}

	instDB, err := database.DB.BuscarInstanciaPorId(id)
	if err != nil || instDB == nil {
		responderErro(w, http.StatusNotFound, "Instância não encontrada")
		return nil, false
	}

	cli := obterUsuarioAutenticado(r)
	if err := verificarAcessoInstancia(cli, instDB); err != nil {
		responderErro(w, http.StatusForbidden, err.Error())
		return nil, false
	}

	return instDB, true
}

func HandlerStatusInstancia(w http.ResponseWriter, r *http.Request) {
	id := extrairIdDaRota(r.URL.Path, "/api/v1/instancia/", "/status")
	instDB, ok := validarPropriedadeInstanciaPorID(w, r, id)
	if !ok {
		return
	}

	status := instDB.Status
	num := instDB.NumeroTelefone
	if instAtiva, err := core.Gerenciador.ObterInstanciaAtiva(id); err == nil && instAtiva != nil {
		st, _, n := instAtiva.ObterEstado()
		if st != "" {
			status = st
		}
		if n != "" {
			num = n
		}
	}

	responderSucesso(w, "Status da instância obtido com sucesso", map[string]interface{}{
		"id":                         instDB.ID,
		"nome":                       instDB.Nome,
		"status":                     status,
		"conectado":                  status == "CONECTADO",
		"numero_telefone":            num,
		"webhook_url":                instDB.WebhookURL,
		"proxy_url":                  instDB.ProxyURL,
		"rejeitar_chamadas":          instDB.RejeitarChamadas,
		"mensagem_chamada_rejeitada": instDB.MensagemChamadaRejeitar,
		"ignorar_grupos":             instDB.IgnorarGrupos,
		"leitura_automatica":         instDB.LeituraAutomatica,
		"sempre_online":              instDB.SempreOnline,
	})
}

func HandlerConectarInstancia(w http.ResponseWriter, r *http.Request) {
	id := extrairIdDaRota(r.URL.Path, "/api/v1/instancia/", "/conectar")
	if _, ok := validarPropriedadeInstanciaPorID(w, r, id); !ok {
		return
	}

	_, err := core.Gerenciador.Conectar(r.Context(), id)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	inst, _ := database.DB.BuscarInstanciaPorId(id)
	responderSucesso(w, "Processo de conexão iniciado. Leia o QR Code ou solicite o código de pareamento.", inst)
}

func HandlerObterQRCode(w http.ResponseWriter, r *http.Request) {
	id := extrairIdDaRota(r.URL.Path, "/api/v1/instancia/", "/qrcode")
	if _, ok := validarPropriedadeInstanciaPorID(w, r, id); !ok {
		return
	}

	inst, err := core.Gerenciador.ObterInstanciaAtiva(id)
	if err != nil {
		responderErro(w, http.StatusNotFound, err.Error())
		return
	}

	status, ultimoQR, numero := inst.ObterEstado()
	if status == "CONECTADO" {
		responderSucesso(w, "Instância já está conectada ao WhatsApp", map[string]interface{}{
			"conectado": true,
			"numero":    numero,
		})
		return
	}

	if ultimoQR == "" {
		_, _ = core.Gerenciador.Conectar(r.Context(), id)
		status, ultimoQR, _ = inst.ObterEstado()
	}

	responderSucesso(w, "QR Code obtido com sucesso", map[string]interface{}{
		"conectado":     false,
		"status":        status,
		"qrcode_base64": ultimoQR,
	})
}

func HandlerQRCodeStream(w http.ResponseWriter, r *http.Request) {
	id := extrairIdDaRota(r.URL.Path, "/api/v1/instancia/", "/qrcode/stream")
	if _, ok := validarPropriedadeInstanciaPorID(w, r, id); !ok {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming SSE não suportado pelo cliente", http.StatusInternalServerError)
		return
	}

	// Remove o WriteTimeout global de 120s para esta conexão SSE de longa duração
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	inst, err := core.Gerenciador.Conectar(r.Context(), id)
	if err != nil {
		fmt.Fprintf(w, "event: erro\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return
	}

	canalQR, cancelarAssinatura := inst.AssinarQR()
	defer cancelarAssinatura()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case qrCode, aberto := <-canalQR:
			if !aberto {
				return
			}
			if qrCode == "CONECTADO" {
				fmt.Fprintf(w, "event: conectado\ndata: true\n\n")
				flusher.Flush()
				return
			}
			fmt.Fprintf(w, "event: qrcode\ndata: %s\n\n", qrCode)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func HandlerCodigoPareamento(w http.ResponseWriter, r *http.Request) {
	id := extrairIdDaRota(r.URL.Path, "/api/v1/instancia/", "/codigo-pareamento")
	if _, ok := validarPropriedadeInstanciaPorID(w, r, id); !ok {
		return
	}

	var body struct {
		NumeroTelefone string `json:"numero_telefone"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil || body.NumeroTelefone == "" {
		responderErro(w, http.StatusBadRequest, "Informe o 'numero_telefone' no formato brasileiro (ex: 11987654321)")
		return
	}

	codigo, err := core.Gerenciador.GerarCodigoPareamento(r.Context(), id, body.NumeroTelefone)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Código de pareamento gerado com sucesso!", map[string]string{
		"codigo_pareamento": codigo,
		"numero_solicitado": core.FormatarNumeroBrasilVisual(body.NumeroTelefone),
		"instrucao":         "Abra o WhatsApp no celular -> Aparelhos Conectados -> Conectar com número de telefone -> Digite este código",
	})
}

func HandlerDesconectar(w http.ResponseWriter, r *http.Request) {
	id := extrairIdDaRota(r.URL.Path, "/api/v1/instancia/", "/desconectar")
	if _, ok := validarPropriedadeInstanciaPorID(w, r, id); !ok {
		return
	}

	if err := core.Gerenciador.Desconectar(id); err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Instância desconectada com sucesso!", nil)
}

func HandlerApagarInstancia(w http.ResponseWriter, r *http.Request) {
	id := extrairIdDaRota(r.URL.Path, "/api/v1/instancia/", "/apagar")
	if _, ok := validarPropriedadeInstanciaPorID(w, r, id); !ok {
		return
	}

	core.Gerenciador.RemoverInstancia(r.Context(), id)
	if err := database.DB.ApagarInstancia(id); err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Instância excluída com sucesso!", nil)
}

func HandlerConfigurarWebhook(w http.ResponseWriter, r *http.Request) {
	id := extrairIdDaRota(r.URL.Path, "/api/v1/instancia/", "/webhook")
	if _, ok := validarPropriedadeInstanciaPorID(w, r, id); !ok {
		return
	}

	var body struct {
		WebhookURL     string `json:"webhook_url"`
		WebhookEventos string `json:"webhook_eventos"`
		WebhookSegredo string `json:"webhook_segredo"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	whURL := strings.TrimSpace(body.WebhookURL)
	if whURL != "" {
		if err := core.ValidarURLWebhook(whURL); err != nil {
			responderErro(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if err := database.DB.AtualizarWebhookInstancia(id, whURL, strings.TrimSpace(body.WebhookEventos), strings.TrimSpace(body.WebhookSegredo)); err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Configurações de Webhook atualizadas!", nil)
}

// ============================================================================
// DISPARO DE MENSAGENS E LOGS (COM VERIFICAÇÃO DE PROPRIEDADE DA INSTÂNCIA)
// ============================================================================

func resolverEValidarInstanciaEnvio(w http.ResponseWriter, r *http.Request, instanciaIDInformada string) (*database.Instancia, bool) {
	cli := obterUsuarioAutenticado(r)
	if cli == nil {
		responderErro(w, http.StatusUnauthorized, "Não autorizado")
		return nil, false
	}

	instID := strings.TrimSpace(instanciaIDInformada)
	if instID == "" && cli.ViaChaveAPI {
		instID = cli.InstanciaKeyID
	}

	if instID == "" {
		responderErro(w, http.StatusBadRequest, "Instância não identificada")
		return nil, false
	}

	return validarPropriedadeInstanciaPorID(w, r, instID)
}

func HandlerEnviarTexto(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID          string   `json:"instancia_id"`
		Destinatario         string   `json:"destinatario"`
		Numero               string   `json:"numero"`
		Mensagem             string   `json:"mensagem"`
		Simulacao            bool     `json:"simulacao"`
		ResponderMensagemID  string   `json:"responder_mensagem_id"`
		ResponderRemetente   string   `json:"responder_remetente"`
		TextoCitado          string   `json:"texto_citado"`
		Mencionar            []string `json:"mencionar"`
		MencionarTodos       bool     `json:"mencionar_todos"`
		DelayDigitandoMs     int      `json:"delay_digitando_ms"`
		Encaminhada          bool     `json:"encaminhada"`
		EncaminhadaFrequente bool     `json:"encaminhada_frequente"`
		ExpiracaoSegundos    uint32   `json:"expiracao_segundos"`
		TituloLink           string   `json:"titulo_link"`
		DescricaoLink        string   `json:"descricao_link"`
		UrlLink              string   `json:"url_link"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo da requisição JSON inválido ou excede limite de 1 MB")
		return
	}

	if body.Destinatario == "" && body.Numero != "" {
		body.Destinatario = body.Numero
	}
	body.Destinatario = strings.TrimSpace(body.Destinatario)
	body.Mensagem = strings.TrimSpace(body.Mensagem)

	if body.Destinatario == "" || body.Mensagem == "" {
		responderErro(w, http.StatusBadRequest, "Campos 'instancia_id', 'destinatario' (ou 'numero') e 'mensagem' são obrigatórios")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		simMsgID := fmt.Sprintf("sim_%s", uuid.New().String()[:12])
		_ = database.DB.GravarLogMensagem(instDB.ID, simMsgID, "texto", body.Destinatario, "ENVIADA", "ENTREGUE (SIMULADO)", "")

		if instDB.WebhookURL != "" {
			core.Despachante.Enviar(instDB.WebhookURL, instDB.WebhookSegredo, core.PayloadWebhook{
				InstanciaID: instDB.ID,
				Nome:        instDB.Nome,
				Evento:      core.EventoStatusEnvio,
				Dados: map[string]interface{}{
					"id_mensagem":  simMsgID,
					"destinatario": core.FormatarNumeroBrasilVisual(body.Destinatario),
					"status":       "ENTREGUE (SIMULADO)",
					"mensagem":     body.Mensagem,
					"modo":         "simulacao",
				},
			})
		}

		responderSucesso(w, "Mensagem de teste simulada com sucesso! Log registrado no banco de dados.", map[string]string{
			"id_mensagem":  simMsgID,
			"destinatario": core.FormatarNumeroBrasilVisual(body.Destinatario),
			"status":       "ENTREGUE (SIMULADO)",
			"modo":         "simulacao",
		})
		return
	}

	opcoes := &core.OpcoesEnvio{
		ResponderMensagemID:  body.ResponderMensagemID,
		ResponderRemetente:   body.ResponderRemetente,
		TextoCitado:          body.TextoCitado,
		Mencionar:            body.Mencionar,
		MencionarTodos:       body.MencionarTodos,
		DelayDigitandoMs:     body.DelayDigitandoMs,
		Encaminhada:          body.Encaminhada,
		EncaminhadaFrequente: body.EncaminhadaFrequente,
		ExpiracaoSegundos:    body.ExpiracaoSegundos,
		TituloLink:           body.TituloLink,
		DescricaoLink:        body.DescricaoLink,
		UrlLink:              body.UrlLink,
	}

	msgID, err := core.Gerenciador.EnviarMensagemTextoComOpcoes(r.Context(), instDB.ID, body.Destinatario, body.Mensagem, opcoes)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Mensagem enviada com sucesso!", map[string]string{
		"id_mensagem":  msgID,
		"destinatario": core.FormatarNumeroBrasilVisual(body.Destinatario),
		"status":       "ENTREGUE",
		"modo":         "whatsapp_real",
	})
}

func HandlerEnviarAudio(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID         string `json:"instancia_id"`
		Destinatario        string `json:"destinatario"`
		Numero              string `json:"numero"`
		AudioBase64         string `json:"audio_base64"`
		GravarNaHora        bool   `json:"gravar_na_hora"`
		VisualizacaoUnica   bool   `json:"visualizacao_unica"`
		DelayDigitandoMs    int    `json:"delay_digitando_ms"`
		ResponderMensagemID string `json:"responder_mensagem_id"`
		Simulacao           bool   `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyMidia, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo da requisição JSON inválido ou excede limite de 25 MB")
		return
	}

	if body.Destinatario == "" && body.Numero != "" {
		body.Destinatario = body.Numero
	}
	body.Destinatario = strings.TrimSpace(body.Destinatario)

	if body.Destinatario == "" {
		responderErro(w, http.StatusBadRequest, "Destinatário ('destinatario' ou 'numero') é obrigatório")
		return
	}

	instDB, ok := resolverEValidarInstanciaEnvio(w, r, body.InstanciaID)
	if !ok {
		return
	}

	if body.Simulacao {
		simMsgID := fmt.Sprintf("sim_ptt_%s", uuid.New().String()[:12])
		_ = database.DB.GravarLogMensagem(instDB.ID, simMsgID, "audio_ptt", body.Destinatario, "ENVIADA", "ENTREGUE (SIMULADO)", "")

		if instDB.WebhookURL != "" {
			core.Despachante.Enviar(instDB.WebhookURL, instDB.WebhookSegredo, core.PayloadWebhook{
				InstanciaID: instDB.ID,
				Nome:        instDB.Nome,
				Evento:      core.EventoStatusEnvio,
				Dados: map[string]interface{}{
					"id_mensagem":  simMsgID,
					"destinatario": core.FormatarNumeroBrasilVisual(body.Destinatario),
					"status":       "ENTREGUE (SIMULADO)",
					"tipo":         "audio_ptt",
					"modo":         "simulacao",
				},
			})
		}

		responderSucesso(w, "Áudio PTT de teste simulado com sucesso! Log registrado no banco de dados.", map[string]string{
			"id_mensagem":  simMsgID,
			"destinatario": core.FormatarNumeroBrasilVisual(body.Destinatario),
			"tipo":         "audio_ptt",
			"status":       "ENTREGUE (SIMULADO)",
			"modo":         "simulacao",
		})
		return
	}

	if body.AudioBase64 == "" {
		responderErro(w, http.StatusBadRequest, "audio_base64 é obrigatório")
		return
	}

	dadosAudio, err := base64.StdEncoding.DecodeString(limparHeaderBase64(body.AudioBase64))
	if err != nil {
		responderErro(w, http.StatusBadRequest, "Áudio Base64 inválido")
		return
	}

	opcoes := &core.OpcoesEnvio{
		VisualizacaoUnica:   body.VisualizacaoUnica,
		DelayDigitandoMs:    body.DelayDigitandoMs,
		ResponderMensagemID: body.ResponderMensagemID,
	}

	msgID, err := core.Gerenciador.EnviarMensagemAudioComOpcoes(r.Context(), instDB.ID, body.Destinatario, dadosAudio, body.GravarNaHora, opcoes)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Áudio enviado com sucesso!", map[string]string{
		"id_mensagem": msgID,
		"tipo":        "audio_ptt",
		"status":      "ENTREGUE",
		"modo":        "whatsapp_real",
	})
}

func HandlerEnviarImagem(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanciaID         string   `json:"instancia_id"`
		Destinatario        string   `json:"destinatario"`
		Numero              string   `json:"numero"`
		ImagemBase64        string   `json:"imagem_base64"`
		Legenda             string   `json:"legenda"`
		VisualizacaoUnica   bool     `json:"visualizacao_unica"`
		ResponderMensagemID string   `json:"responder_mensagem_id"`
		Mencionar           []string `json:"mencionar"`
		MencionarTodos      bool     `json:"mencionar_todos"`
		DelayDigitandoMs    int      `json:"delay_digitando_ms"`
		Simulacao           bool     `json:"simulacao"`
	}

	if err := decodificarJSONLimitado(w, r, limiteBodyMidia, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "Corpo da requisição inválido ou excede 25 MB")
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
		simMsgID := fmt.Sprintf("sim_img_%s", uuid.New().String()[:12])
		_ = database.DB.GravarLogMensagem(instDB.ID, simMsgID, "imagem", body.Destinatario, "ENVIADA", "ENTREGUE (SIMULADO)", "")
		responderSucesso(w, "Imagem simulada com sucesso!", map[string]string{
			"id_mensagem":  simMsgID,
			"destinatario": core.FormatarNumeroBrasilVisual(body.Destinatario),
			"tipo":         "imagem",
			"status":       "ENTREGUE (SIMULADO)",
			"modo":         "simulacao",
		})
		return
	}

	if body.ImagemBase64 == "" {
		responderErro(w, http.StatusBadRequest, "imagem_base64 é obrigatório")
		return
	}

	dadosImagem, err := base64.StdEncoding.DecodeString(limparHeaderBase64(body.ImagemBase64))
	if err != nil {
		responderErro(w, http.StatusBadRequest, "Imagem Base64 inválida")
		return
	}

	opcoes := &core.OpcoesEnvio{
		VisualizacaoUnica:   body.VisualizacaoUnica,
		ResponderMensagemID: body.ResponderMensagemID,
		Mencionar:           body.Mencionar,
		MencionarTodos:      body.MencionarTodos,
		DelayDigitandoMs:    body.DelayDigitandoMs,
	}

	msgID, err := core.Gerenciador.EnviarMensagemImagemComOpcoes(r.Context(), instDB.ID, body.Destinatario, dadosImagem, body.Legenda, opcoes)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Imagem enviada com sucesso!", map[string]string{
		"id_mensagem": msgID,
		"tipo":        "imagem",
		"status":      "ENTREGUE",
		"modo":        "whatsapp_real",
	})
}

func HandlerListarLogs(w http.ResponseWriter, r *http.Request) {
	cli := obterUsuarioAutenticado(r)
	if cli == nil {
		responderErro(w, http.StatusUnauthorized, "Não autorizado")
		return
	}

	instId := strings.TrimSpace(r.URL.Query().Get("instancia_id"))

	if cli.ViaChaveAPI {
		instId = cli.InstanciaKeyID
	}

	if instId != "" {
		if _, ok := validarPropriedadeInstanciaPorID(w, r, instId); !ok {
			return
		}
		logs, err := database.DB.ListarLogsRecentes(instId, 50)
		if err != nil {
			responderErro(w, http.StatusInternalServerError, err.Error())
			return
		}
		responderSucesso(w, "Logs recuperados com sucesso", logs)
		return
	}

	if cli.Papel == database.PapelSuperAdmin {
		logs, err := database.DB.ListarLogsRecentes("", 50)
		if err != nil {
			responderErro(w, http.StatusInternalServerError, err.Error())
			return
		}
		responderSucesso(w, "Logs recuperados com sucesso", logs)
		return
	}

	if cli.Papel == database.PapelRevendedor {
		logs, err := database.DB.ListarLogsPorRevendedor(cli.RevendedorID, 50)
		if err != nil {
			responderErro(w, http.StatusInternalServerError, err.Error())
			return
		}
		responderSucesso(w, "Logs recuperados com sucesso", logs)
		return
	}

	logs, err := database.DB.ListarLogsPorCliente(cli.ID, 50)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}
	responderSucesso(w, "Logs recuperados com sucesso", logs)
}

func extrairIdDaRota(caminho, prefixo, sufixo string) string {
	if !strings.HasPrefix(caminho, prefixo) {
		return ""
	}
	resto := caminho[len(prefixo):]
	if sufixo != "" {
		if idx := strings.Index(resto, sufixo); idx != -1 {
			return resto[:idx]
		}
	}
	return strings.Trim(resto, "/")
}

func limparHeaderBase64(s string) string {
	if idx := strings.Index(s, ","); idx != -1 && strings.Contains(s[:idx], "base64") {
		return s[idx+1:]
	}
	return s
}
