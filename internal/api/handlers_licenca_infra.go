package api

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/internal/config"
	"go.mau.fi/whatsmeow/internal/database"
	"go.mau.fi/whatsmeow/internal/licenca"
	"go.mau.fi/whatsmeow/internal/storage"
)

func extrairIPCliente(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		partes := strings.Split(xff, ",")
		return strings.TrimSpace(partes[0])
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

// ============================================================================
// MÓDULO 3: SERVIDOR MESTRE DE LICENÇAS & KILL-SWITCH PORTAINER
// ============================================================================

// HandlerValidarLicencaBoot é chamado pelos containers Portainer no momento do boot (main.go)
func HandlerValidarLicencaBoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}

	var body struct {
		ChaveLicenca         string `json:"chave_licenca"`
		Dominio              string `json:"dominio"`
		FingerprintContainer string `json:"fingerprint_container"`
		Versao               string `json:"versao"`
	}
	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil || strings.TrimSpace(body.ChaveLicenca) == "" {
		responderErro(w, http.StatusBadRequest, "Chave de licença obrigatória não fornecida")
		return
	}

	lic, err := database.DB.BuscarLicencaPorChave(body.ChaveLicenca)
	if err != nil || lic == nil {
		responderErro(w, http.StatusForbidden, "Licença inválida ou inexistente no Servidor Mestre")
		return
	}

	if lic.Status != "ATIVA" {
		responderErro(w, http.StatusForbidden, fmt.Sprintf("Licença bloqueada (Status: %s). Entre em contato com o suporte.", lic.Status))
		return
	}

	if time.Now().After(lic.ExpiraEm) {
		_ = database.DB.AtualizarStatusLicenca(lic.ID, "EXPIRADA", 0)
		responderErro(w, http.StatusForbidden, fmt.Sprintf("Licença expirada em %s. Renove sua assinatura para iniciar o container.", lic.ExpiraEm.Format("02/01/2006")))
		return
	}

	ipRemoto := extrairIPCliente(r)

	// Trava Anti-Clonagem de IP (se já houver IP vinculado diferente de localhost)
	if lic.IPAutorizado != "" && lic.IPAutorizado != "127.0.0.1" && lic.IPAutorizado != "::1" && lic.IPAutorizado != ipRemoto {
		responderErro(w, http.StatusForbidden, fmt.Sprintf("Violação de licença: esta chave já está vinculada ao IP %s (tentativa a partir de %s). Solicite desvínculo de IP no painel.", lic.IPAutorizado, ipRemoto))
		return
	}

	tokenEd25519 := lic.TokenAssinadoEd25519
	if tokenEd25519 == "" {
		tokenEd25519 = licenca.GerarTokenAssinadoEd25519(lic.ChaveLicenca, lic.ClienteNome, lic.DominioAutorizado, lic.Plano, lic.LimiteInstancias, lic.ExpiraEm)
	}

	_ = database.DB.RegistrarHeartbeatLicenca(lic.ChaveLicenca, ipRemoto, body.Dominio, body.FingerprintContainer, body.Versao, lic.InstanciasAtivas)

	responderSucesso(w, "Licença validada com sucesso! Container autorizado a iniciar.", map[string]interface{}{
		"id":                    lic.ID,
		"chave_licenca":         lic.ChaveLicenca,
		"cliente_nome":          lic.ClienteNome,
		"plano":                 lic.Plano,
		"limite_instancias":     lic.LimiteInstancias,
		"status":                "ATIVA",
		"expira_em":             lic.ExpiraEm,
		"ip_vinculado":          ipRemoto,
		"token_assinado_ed25519": tokenEd25519,
	})
}

// HandlerHeartbeatLicenca recebe telemetria periódica dos containers Portainer e aciona KILL se suspenso
func HandlerHeartbeatLicenca(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}

	var body struct {
		ChaveLicenca         string `json:"chave_licenca"`
		Dominio              string `json:"dominio"`
		FingerprintContainer string `json:"fingerprint_container"`
		Versao               string `json:"versao"`
		InstanciasAtivas     int    `json:"instancias_ativas"`
	}
	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil || strings.TrimSpace(body.ChaveLicenca) == "" {
		responderErro(w, http.StatusBadRequest, "Payload de heartbeat inválido")
		return
	}

	lic, err := database.DB.BuscarLicencaPorChave(body.ChaveLicenca)
	if err != nil || lic == nil {
		responderSucesso(w, "Licença revogada", map[string]interface{}{
			"acao":   "KILL",
			"motivo": "Chave de licença removida",
		})
		return
	}

	if lic.Status != "ATIVA" || time.Now().After(lic.ExpiraEm) {
		responderSucesso(w, "Licença suspensa ou expirada", map[string]interface{}{
			"acao":   "KILL",
			"status": lic.Status,
			"motivo": "Assinatura suspensa ou expirada",
		})
		return
	}

	ipRemoto := extrairIPCliente(r)
	_ = database.DB.RegistrarHeartbeatLicenca(lic.ChaveLicenca, ipRemoto, body.Dominio, body.FingerprintContainer, body.Versao, body.InstanciasAtivas)

	responderSucesso(w, "Heartbeat registrado", map[string]interface{}{
		"acao":              "OK",
		"status":            "ATIVA",
		"limite_instancias": lic.LimiteInstancias,
		"expira_em":         lic.ExpiraEm,
	})
}

// HandlerStatusLicencaLocal retorna os dados da licença ativa neste servidor/container
func HandlerStatusLicencaLocal(w http.ResponseWriter, r *http.Request) {
	responderSucesso(w, "Status da licença local", licenca.ObterResumoLicencaAtiva())
}

// HandlerListarLicencas lista todas as licenças emitidas para Stacks Portainer (Apenas SuperAdmin)
func HandlerListarLicencas(w http.ResponseWriter, r *http.Request) {
	cli := obterUsuarioAutenticado(r)
	if cli == nil || cli.Papel != database.PapelSuperAdmin {
		responderErro(w, http.StatusForbidden, "Apenas o Super Admin Master pode gerenciar licenças de distribuição")
		return
	}

	lista, err := database.DB.ListarLicencasInstalacoes()
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}
	responderSucesso(w, "Licenças listadas com sucesso", lista)
}

// HandlerCriarLicenca emite uma nova licença criptográfica Ed25519 e gera o template da Stack Portainer
func HandlerCriarLicenca(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}

	cli := obterUsuarioAutenticado(r)
	if cli == nil || cli.Papel != database.PapelSuperAdmin {
		responderErro(w, http.StatusForbidden, "Apenas o Super Admin Master pode emitir novas licenças")
		return
	}

	var body struct {
		RevendedorID      string `json:"revendedor_id"`
		ClienteNome       string `json:"cliente_nome"`
		EmailContato      string `json:"email_contato"`
		DominioAutorizado string `json:"dominio_autorizado"`
		Plano             string `json:"plano"`
		LimiteInstancias  int    `json:"limite_instancias"`
		DiasValidade      int    `json:"dias_validade"`
	}
	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil || strings.TrimSpace(body.ClienteNome) == "" {
		responderErro(w, http.StatusBadRequest, "Informe ao menos o 'cliente_nome' da revenda")
		return
	}

	if body.LimiteInstancias <= 0 {
		body.LimiteInstancias = 50
	}
	if body.DiasValidade <= 0 {
		body.DiasValidade = 30
	}
	if body.Plano == "" {
		body.Plano = "Stack Portainer Pro"
	}

	expiraEm := time.Now().AddDate(0, 0, body.DiasValidade)
	tokenProv := licenca.GerarTokenAssinadoEd25519("nova", body.ClienteNome, body.DominioAutorizado, body.Plano, body.LimiteInstancias, expiraEm)

	nova, err := database.DB.CriarLicencaInstalacao(
		body.RevendedorID,
		strings.TrimSpace(body.ClienteNome),
		strings.TrimSpace(body.EmailContato),
		strings.TrimSpace(body.DominioAutorizado),
		strings.TrimSpace(body.Plano),
		body.LimiteInstancias,
		body.DiasValidade,
		tokenProv,
	)
	if err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Gera o token Ed25519 definitivo com a chave gerada
	tokenFinal := licenca.GerarTokenAssinadoEd25519(nova.ChaveLicenca, nova.ClienteNome, nova.DominioAutorizado, nova.Plano, nova.LimiteInstancias, nova.ExpiraEm)
	nova.TokenAssinadoEd25519 = tokenFinal

	servidorLicenca := "https://licencas.woohub.com.br"
	if config.AppConfig != nil && config.AppConfig.UrlPublicaPadrao != "" {
		servidorLicenca = config.AppConfig.UrlPublicaPadrao
	}

	stackYAML := gerarStackPortainerYAML(nova.ChaveLicenca, servidorLicenca, nova.DominioAutorizado, nova.EmailContato)

	responderSucesso(w, "Licença emitida com sucesso!", map[string]interface{}{
		"licenca":              nova,
		"portainer_stack_yaml": stackYAML,
	})
}

// HandlerAlterarStatusLicenca permite suspender (KILL), bloquear, ativar ou renovar uma licença
func HandlerAlterarStatusLicenca(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}

	cli := obterUsuarioAutenticado(r)
	if cli == nil || cli.Papel != database.PapelSuperAdmin {
		responderErro(w, http.StatusForbidden, "Apenas o Super Admin Master pode alterar o status de licenças")
		return
	}

	var body struct {
		ID            string `json:"id"`
		Status        string `json:"status"` // ATIVA, SUSPENSA, BLOQUEADA
		DiasRenovacao int    `json:"dias_renovacao"`
	}
	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil || body.ID == "" || body.Status == "" {
		responderErro(w, http.StatusBadRequest, "Informe 'id' e 'status' (ATIVA, SUSPENSA, BLOQUEADA)")
		return
	}

	if err := database.DB.AtualizarStatusLicenca(body.ID, body.Status, body.DiasRenovacao); err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, fmt.Sprintf("Status da licença atualizado para %s!", strings.ToUpper(body.Status)), nil)
}

// HandlerDesvincularIPLicenca limpa o IP/Fingerprint vinculado à licença para permitir migração de VPS
func HandlerDesvincularIPLicenca(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}

	cli := obterUsuarioAutenticado(r)
	if cli == nil || cli.Papel != database.PapelSuperAdmin {
		responderErro(w, http.StatusForbidden, "Apenas o Super Admin Master pode desvincular IP de licenças")
		return
	}

	var body struct {
		ID string `json:"id"`
	}
	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil || body.ID == "" {
		responderErro(w, http.StatusBadRequest, "Informe o 'id' ou 'chave_licenca'")
		return
	}

	if err := database.DB.DesvincularIPLicenca(body.ID); err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	responderSucesso(w, "Trava de IP/Container resetada! O próximo container que subir assumirá o vínculo.", nil)
}

// ============================================================================
// MÓDULO 1 & 2: INFRAESTRUTURA ISOLADA POR REVENDA (BYO SUPABASE + BYO MINIO)
// ============================================================================

func obterRevendedorAlvoInfra(w http.ResponseWriter, r *http.Request, revendedorIDInformado string) (*database.Revendedor, bool) {
	cli := obterUsuarioAutenticado(r)
	if cli == nil {
		responderErro(w, http.StatusUnauthorized, "Não autorizado")
		return nil, false
	}
	if cli.Papel != database.PapelSuperAdmin && cli.Papel != database.PapelRevendedor {
		responderErro(w, http.StatusForbidden, "Apenas Revendedores e Administradores podem configurar infraestrutura isolada")
		return nil, false
	}

	revID := cli.RevendedorID
	if cli.Papel == database.PapelSuperAdmin && strings.TrimSpace(revendedorIDInformado) != "" {
		revID = strings.TrimSpace(revendedorIDInformado)
	}

	rev, err := database.DB.BuscarRevendedorPorID(revID)
	if err != nil || rev == nil {
		rev, err = database.DB.BuscarRevendedorPorDominio(r.Host)
		if err != nil || rev == nil {
			responderErro(w, http.StatusNotFound, "Revendedor não encontrado")
			return nil, false
		}
	}
	return rev, true
}

// HandlerObterInfraRevenda retorna as configurações atuais de Supabase e MinIO da revenda
func HandlerObterInfraRevenda(w http.ResponseWriter, r *http.Request) {
	revIDQuery := r.URL.Query().Get("revendedor_id")
	rev, ok := obterRevendedorAlvoInfra(w, r, revIDQuery)
	if !ok {
		return
	}

	secretMascarada := ""
	if rev.MinioSecretKey != "" {
		if len(rev.MinioSecretKey) > 6 {
			secretMascarada = rev.MinioSecretKey[:3] + "••••••••" + rev.MinioSecretKey[len(rev.MinioSecretKey)-3:]
		} else {
			secretMascarada = "••••••••"
		}
	}

	responderSucesso(w, "Configurações de infraestrutura isolada da revenda", map[string]interface{}{
		"revendedor_id":            rev.ID,
		"nome_empresa":             rev.NomeEmpresa,
		"supabase_uri_mascarada":   mascararURIInfra(rev.SupabaseURI),
		"supabase_isolado_ativo":   strings.TrimSpace(rev.SupabaseURI) != "",
		"minio_endpoint":           rev.MinioEndpoint,
		"minio_access_key":         rev.MinioAccessKey,
		"minio_secret_key_mascara": secretMascarada,
		"minio_bucket":             rev.MinioBucket,
		"minio_use_ssl":            rev.MinioUseSSL,
		"minio_public_url":         rev.MinioPublicURL,
		"minio_isolado_ativo":      strings.TrimSpace(rev.MinioEndpoint) != "",
	})
}

func mascararURIInfra(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ""
	}
	return database.MascararURIConexao(uri)
}

// HandlerConfigurarInfraRevenda salva as credenciais de Supabase isolado e MinIO S3 isolado da revenda
func HandlerConfigurarInfraRevenda(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}

	var body struct {
		RevendedorID   string `json:"revendedor_id"`
		SupabaseURI    string `json:"supabase_uri"`
		MinioEndpoint  string `json:"minio_endpoint"`
		MinioAccessKey string `json:"minio_access_key"`
		MinioSecretKey string `json:"minio_secret_key"`
		MinioBucket    string `json:"minio_bucket"`
		MinioUseSSL    bool   `json:"minio_use_ssl"`
		MinioPublicURL string `json:"minio_public_url"`
	}
	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	rev, ok := obterRevendedorAlvoInfra(w, r, body.RevendedorID)
	if !ok {
		return
	}

	supabaseURI := database.NormalizarURISupabase(body.SupabaseURI)
	secretFinal := strings.TrimSpace(body.MinioSecretKey)
	if secretFinal == "" || strings.Contains(secretFinal, "••••") {
		secretFinal = rev.MinioSecretKey
	}
	if supabaseURI == "" || strings.TrimSpace(body.MinioEndpoint) == "" ||
		strings.TrimSpace(body.MinioAccessKey) == "" || secretFinal == "" ||
		strings.TrimSpace(body.MinioBucket) == "" {
		responderErro(w, http.StatusBadRequest, "Supabase, endpoint MinIO, Access Key, Secret Key e bucket são obrigatórios")
		return
	}

	// A configuração só é persistida depois de validar os dois serviços. Isso
	// evita que uma revenda fique marcada como operacional com infraestrutura
	// incompleta ou com credenciais inválidas.
	if _, err := database.TestarEMigrarSupabaseIsolado(r.Context(), supabaseURI); err != nil {
		responderErro(w, http.StatusBadRequest, "Supabase da revenda inválido: "+err.Error())
		return
	}
	minioCfg := storage.ConfigMinIO{
		Endpoint:  strings.TrimSpace(body.MinioEndpoint),
		AccessKey: strings.TrimSpace(body.MinioAccessKey),
		SecretKey: secretFinal,
		Bucket:    strings.TrimSpace(body.MinioBucket),
		UseSSL:    body.MinioUseSSL,
	}
	if err := storage.NovoClienteMinIO(minioCfg).TestarECriarBucket(r.Context()); err != nil {
		responderErro(w, http.StatusBadRequest, "MinIO da revenda inválido: "+err.Error())
		return
	}

	if err := database.DB.AtualizarInfraestruturaRevendedor(
		rev.ID,
		supabaseURI,
		body.MinioEndpoint,
		body.MinioAccessKey,
		secretFinal,
		body.MinioBucket,
		body.MinioPublicURL,
		body.MinioUseSSL,
	); err != nil {
		responderErro(w, http.StatusInternalServerError, err.Error())
		return
	}

	database.PoolTenants.InvalidarPoolRevendedor(rev.ID)

	responderSucesso(w, "Infraestrutura isolada (Supabase + MinIO) salva com sucesso para a revenda!", map[string]interface{}{
		"revendedor_id":    rev.ID,
		"supabase_isolado": supabaseURI != "",
		"minio_isolado":    strings.TrimSpace(body.MinioEndpoint) != "",
	})
}

// HandlerTestarSupabaseRevenda testa a conexão com o Supabase isolado do revendedor e cria todas as tabelas automaticamente
func HandlerTestarSupabaseRevenda(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}

	var body struct {
		RevendedorID string `json:"revendedor_id"`
		SupabaseURI  string `json:"supabase_uri"`
	}
	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	rev, ok := obterRevendedorAlvoInfra(w, r, body.RevendedorID)
	if !ok {
		return
	}

	uriTeste := strings.TrimSpace(body.SupabaseURI)
	if uriTeste == "" {
		uriTeste = rev.SupabaseURI
	}

	res, err := database.TestarEMigrarSupabaseIsolado(r.Context(), uriTeste)
	if err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}

	responderSucesso(w, "Conexão com Supabase da revenda validada e todas as tabelas (WooHub + WhatsApp) migradas!", res)
}

// HandlerTestarMinioRevenda testa a conectividade S3 com o MinIO isolado do revendedor e cria o bucket se necessário
func HandlerTestarMinioRevenda(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responderErro(w, http.StatusMethodNotAllowed, "Método não permitido")
		return
	}

	var body struct {
		RevendedorID   string `json:"revendedor_id"`
		MinioEndpoint  string `json:"minio_endpoint"`
		MinioAccessKey string `json:"minio_access_key"`
		MinioSecretKey string `json:"minio_secret_key"`
		MinioBucket    string `json:"minio_bucket"`
		MinioUseSSL    bool   `json:"minio_use_ssl"`
	}
	if err := decodificarJSONLimitado(w, r, limiteBodyJSONPadrao, &body); err != nil {
		responderErro(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	rev, ok := obterRevendedorAlvoInfra(w, r, body.RevendedorID)
	if !ok {
		return
	}

	secret := strings.TrimSpace(body.MinioSecretKey)
	if secret == "" || strings.Contains(secret, "••••") {
		secret = rev.MinioSecretKey
	}

	cfgMinio := storage.ConfigMinIO{
		Endpoint:  strings.TrimSpace(body.MinioEndpoint),
		AccessKey: strings.TrimSpace(body.MinioAccessKey),
		SecretKey: secret,
		Bucket:    strings.TrimSpace(body.MinioBucket),
		UseSSL:    body.MinioUseSSL,
	}
	if cfgMinio.Endpoint == "" {
		cfgMinio.Endpoint = rev.MinioEndpoint
		cfgMinio.AccessKey = rev.MinioAccessKey
		cfgMinio.Bucket = rev.MinioBucket
		cfgMinio.UseSSL = rev.MinioUseSSL
	}

	cliMinio := storage.NovoClienteMinIO(cfgMinio)
	if err := cliMinio.TestarECriarBucket(r.Context()); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}

	responderSucesso(w, fmt.Sprintf("Conectado com sucesso ao MinIO (%s) e bucket '%s' pronto para receber mídias!", cfgMinio.Endpoint, cfgMinio.Bucket), map[string]interface{}{
		"endpoint": cfgMinio.Endpoint,
		"bucket":   cfgMinio.Bucket,
		"ssl":      cfgMinio.UseSSL,
	})
}

func gerarStackPortainerYAML(chaveLicenca, servidorLicenca, dominio, emailAdmin string) string {
	if dominio == "" {
		dominio = "api.suarevenda.com.br"
	}
	if emailAdmin == "" {
		emailAdmin = "admin@suarevenda.com.br"
	}
	return fmt.Sprintf(`version: "3.8"

# ==============================================================================
# STACK OFICIAL PORTAINER - WOOHUB API BRASIL (WHITE-LABEL LICENCIADA)
# Cada revenda utiliza seu próprio Supabase (PostgreSQL) e seu próprio MinIO S3
# ==============================================================================

services:
  woohub-api:
    image: ghcr.io/woohub-brasil/woohub-api:latest
    container_name: woohub_api_stack
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      - AMBIENTE=production
      - PORTA=8080
      - FUSO_HORARIO=America/Sao_Paulo

      # 1. LICENÇA OBRIGATÓRIA (Sem licença ativa o container não sobe)
      - EXIGIR_LICENCA=true
      - WOOHUB_LICENCA_CHAVE=%s
      - WOOHUB_SERVIDOR_LICENCA=%s
      - WOOHUB_DOMINIO_OFICIAL=%s

      # 2. SUPABASE ISOLADO DA REVENDA (Porta 5432 Session Mode recomendada)
      - TIPO_BANCO=postgres
      - URI_BANCO=postgresql://postgres.SEU_PROJETO:SUA_SENHA@aws-0-sa-east-1.pooler.supabase.com:5432/postgres?sslmode=require

      # 3. MINIO / S3 ISOLADO DA REVENDA (Armazenamento de Áudios PTT, Vídeos e PDFs)
      - STORAGE_DRIVER=s3
      - MINIO_ENDPOINT=https://s3.%s
      - MINIO_ACCESS_KEY=SUA_ACCESS_KEY_MINIO
      - MINIO_SECRET_KEY=SUA_SECRET_KEY_MINIO
      - MINIO_BUCKET=woohub-midias
      - MINIO_USE_SSL=true

      # 4. CREDENCIAIS INICIAIS DO PAINEL DA REVENDA
      - ADMIN_EMAIL=%s
      - ADMIN_SENHA=TroqueEstaSenhaForte123!
      - SEGREDO_JWT=gere_um_segredo_jwt_exclusivo_da_revenda_aqui

  # Atualizador Automático Licenciado (Mantém a Stack sempre na última versão)
  watchtower:
    image: containrrr/watchtower:latest
    container_name: woohub_auto_updater
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    command: --interval 3600 --cleanup woohub_api_stack
`, chaveLicenca, servidorLicenca, dominio, dominio, emailAdmin)
}

