package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/internal/config"
)

// ConfigMinIO representa as credenciais de um servidor MinIO / S3 (Global ou por Revenda)
type ConfigMinIO struct {
	Endpoint  string `json:"minio_endpoint"`
	AccessKey string `json:"minio_access_key"`
	SecretKey string `json:"minio_secret_key"`
	Bucket    string `json:"minio_bucket"`
	Regiao    string `json:"minio_regiao"`
	UseSSL    bool   `json:"minio_use_ssl"`
	PublicURL string `json:"minio_public_url"`
}

// Configurado verifica se as credenciais mínimas do MinIO/S3 foram preenchidas
func (c ConfigMinIO) Configurado() bool {
	return strings.TrimSpace(c.Endpoint) != "" &&
		strings.TrimSpace(c.AccessKey) != "" &&
		strings.TrimSpace(c.SecretKey) != ""
}

// ClienteMinIO implementa o protocolo S3 (AWS Signature V4) nativo em Go puro para MinIO/S3
type ClienteMinIO struct {
	cfg        ConfigMinIO
	httpClient *http.Client
}

// NovoClienteMinIO cria uma instância do cliente S3/MinIO
func NovoClienteMinIO(cfg ConfigMinIO) *ClienteMinIO {
	if strings.TrimSpace(cfg.Bucket) == "" {
		cfg.Bucket = "woohub-midias"
	}
	if strings.TrimSpace(cfg.Regiao) == "" {
		cfg.Regiao = "us-east-1"
	}
	return &ClienteMinIO{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ObterClienteGlobal retorna o cliente MinIO configurado nas variáveis da Stack (se houver)
func ObterClienteGlobal() *ClienteMinIO {
	if config.AppConfig == nil {
		return nil
	}
	cfg := ConfigMinIO{
		Endpoint:  config.AppConfig.MinioEndpoint,
		AccessKey: config.AppConfig.MinioAccessKey,
		SecretKey: config.AppConfig.MinioSecretKey,
		Bucket:    config.AppConfig.MinioBucket,
		Regiao:    config.AppConfig.MinioRegiao,
		UseSSL:    config.AppConfig.MinioUseSSL,
		PublicURL: config.AppConfig.MinioPublicURL,
	}
	if !cfg.Configurado() {
		return nil
	}
	return NovoClienteMinIO(cfg)
}

func (m *ClienteMinIO) resolverEndpointBase() (string, string, error) {
	raw := strings.TrimSpace(m.cfg.Endpoint)
	if raw == "" {
		return "", "", fmt.Errorf("endpoint MinIO vazio")
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		if m.cfg.UseSSL {
			raw = "https://" + raw
		} else {
			raw = "http://" + raw
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("endpoint MinIO inválido: %w", err)
	}
	baseURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
	return baseURL, u.Host, nil
}

// TestarECriarBucket verifica a conectividade com o MinIO da revenda e cria o bucket caso não exista
func (m *ClienteMinIO) TestarECriarBucket(ctx context.Context) error {
	if !m.cfg.Configurado() {
		return fmt.Errorf("credenciais MinIO incompletas (informe endpoint, access_key e secret_key)")
	}

	// 1. Verifica se o bucket já existe (HEAD /{bucket})
	status, CorpoErro, err := m.executarRequisicaoS3(ctx, http.MethodHead, "/"+m.cfg.Bucket, nil, "")
	if err != nil {
		return fmt.Errorf("falha de conexão com MinIO (%s): %w", m.cfg.Endpoint, err)
	}

	if status == http.StatusOK || status == http.StatusNoContent {
		return nil
	}

	if status == http.StatusForbidden || status == http.StatusUnauthorized {
		return fmt.Errorf("acesso negado no MinIO (HTTP %d) — verifique Access Key e Secret Key", status)
	}

	// 2. Se retornar 404, tenta criar o bucket automaticamente (PUT /{bucket})
	if status == http.StatusNotFound {
		statusPut, erroPut, errPut := m.executarRequisicaoS3(ctx, http.MethodPut, "/"+m.cfg.Bucket, nil, "")
		if errPut != nil {
			return fmt.Errorf("falha ao criar bucket '%s': %w", m.cfg.Bucket, errPut)
		}
		if statusPut != http.StatusOK && statusPut != http.StatusCreated && statusPut != http.StatusConflict {
			return fmt.Errorf("MinIO recusou criação do bucket '%s' (HTTP %d): %s", m.cfg.Bucket, statusPut, erroPut)
		}
		return nil
	}

	return fmt.Errorf("resposta inesperada do MinIO (HTTP %d): %s", status, CorpoErro)
}

// UploadMidia envia um arquivo de mídia (áudio PTT, foto, vídeo, PDF) para o MinIO e retorna a URL de acesso
func (m *ClienteMinIO) UploadMidia(ctx context.Context, chaveObjeto string, dados []byte, contentType string) (string, error) {
	if !m.cfg.Configurado() {
		return "", fmt.Errorf("MinIO não configurado")
	}

	chaveLimpa := strings.TrimLeft(filepath.ToSlash(chaveObjeto), "/")
	caminhoS3 := fmt.Sprintf("/%s/%s", m.cfg.Bucket, chaveLimpa)

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	status, respCorpo, err := m.executarRequisicaoS3(ctx, http.MethodPut, caminhoS3, dados, contentType)
	if err != nil {
		return "", err
	}

	// Se o bucket ainda não existir, cria e tenta novamente
	if status == http.StatusNotFound {
		_ = m.TestarECriarBucket(ctx)
		status, respCorpo, err = m.executarRequisicaoS3(ctx, http.MethodPut, caminhoS3, dados, contentType)
		if err != nil {
			return "", err
		}
	}

	if status != http.StatusOK && status != http.StatusCreated && status != http.StatusNoContent {
		return "", fmt.Errorf("erro no upload MinIO (HTTP %d): %s", status, respCorpo)
	}

	// Se o revendedor configurou URL pública direta do bucket/CDN, retorna a URL limpa
	if m.cfg.PublicURL != "" {
		return fmt.Sprintf("%s/%s/%s", strings.TrimRight(m.cfg.PublicURL, "/"), m.cfg.Bucket, chaveLimpa), nil
	}

	// Caso contrário, gera uma URL pré-assinada válida por 7 dias (AWS4-HMAC-SHA256)
	return m.GerarURLPreAssinada(chaveLimpa, 7*24*time.Hour)
}

// GerarURLPreAssinada gera um link S3 autenticado via Query String (Presigned URL V4)
func (m *ClienteMinIO) GerarURLPreAssinada(chaveObjeto string, validade time.Duration) (string, error) {
	baseURL, host, err := m.resolverEndpointBase()
	if err != nil {
		return "", err
	}

	chaveLimpa := strings.TrimLeft(filepath.ToSlash(chaveObjeto), "/")
	caminhoURI := fmt.Sprintf("/%s/%s", m.cfg.Bucket, chaveLimpa)

	agora := time.Now().UTC()
	amzDate := agora.Format("20060102T150405Z")
	datestamp := agora.Format("20060102")
	expSegundos := int(validade.Seconds())
	if expSegundos <= 0 || expSegundos > 604800 {
		expSegundos = 604800 // Máximo S3 V4: 7 dias
	}

	escopoCredencial := fmt.Sprintf("%s/%s/s3/aws4_request", datestamp, m.cfg.Regiao)
	credencialCompleta := fmt.Sprintf("%s/%s", m.cfg.AccessKey, escopoCredencial)

	params := url.Values{}
	params.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	params.Set("X-Amz-Credential", credencialCompleta)
	params.Set("X-Amz-Date", amzDate)
	params.Set("X-Amz-Expires", fmt.Sprintf("%d", expSegundos))
	params.Set("X-Amz-SignedHeaders", "host")

	queryCanonica := codificarQueryCanonicaS3(params)
	headersCanonicos := fmt.Sprintf("host:%s\n", host)

	requisicaoCanonica := strings.Join([]string{
		http.MethodGet,
		caminhoURI,
		queryCanonica,
		headersCanonicos,
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")

	stringParaAssinar := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		escopoCredencial,
		hashSHA256Hex([]byte(requisicaoCanonica)),
	}, "\n")

	chaveAssinatura := derivarChaveAssinaturaS3(m.cfg.SecretKey, datestamp, m.cfg.Regiao, "s3")
	assinatura := hex.EncodeToString(hmacSHA256(chaveAssinatura, []byte(stringParaAssinar)))

	urlFinal := fmt.Sprintf("%s%s?%s&X-Amz-Signature=%s", baseURL, caminhoURI, queryCanonica, assinatura)
	return urlFinal, nil
}

func (m *ClienteMinIO) executarRequisicaoS3(ctx context.Context, metodo, caminhoURI string, payload []byte, contentType string) (int, string, error) {
	baseURL, host, err := m.resolverEndpointBase()
	if err != nil {
		return 0, "", err
	}

	agora := time.Now().UTC()
	amzDate := agora.Format("20060102T150405Z")
	datestamp := agora.Format("20060102")

	payloadHash := hashSHA256Hex(payload)

	req, err := http.NewRequestWithContext(ctx, metodo, baseURL+caminhoURI, bytes.NewReader(payload))
	if err != nil {
		return 0, "", err
	}

	req.Host = host
	req.Header.Set("Host", host)
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	headersCanonicos := fmt.Sprintf("host:%s\nx-amz-content-sha256:%s\nx-amz-date:%s\n", host, payloadHash, amzDate)
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	requisicaoCanonica := strings.Join([]string{
		metodo,
		caminhoURI,
		"",
		headersCanonicos,
		signedHeaders,
		payloadHash,
	}, "\n")

	escopoCredencial := fmt.Sprintf("%s/%s/s3/aws4_request", datestamp, m.cfg.Regiao)
	stringParaAssinar := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		escopoCredencial,
		hashSHA256Hex([]byte(requisicaoCanonica)),
	}, "\n")

	chaveAssinatura := derivarChaveAssinaturaS3(m.cfg.SecretKey, datestamp, m.cfg.Regiao, "s3")
	assinatura := hex.EncodeToString(hmacSHA256(chaveAssinatura, []byte(stringParaAssinar)))

	authHeader := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		m.cfg.AccessKey, escopoCredencial, signedHeaders, assinatura,
	)
	req.Header.Set("Authorization", authHeader)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	corpoBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, string(corpoBytes), nil
}

// SalvarMidiaHibrida salva a mídia no MinIO da revenda (ou MinIO global da Stack) ou faz fallback para disco local
func SalvarMidiaHibrida(ctx context.Context, cfgRevenda *ConfigMinIO, instanciaID, msgID, extensao, contentType string, dados []byte) (string, error) {
	ext := strings.TrimPrefix(extensao, ".")
	if ext == "" {
		ext = "bin"
	}
	chaveRelativa := fmt.Sprintf("%s/%s/%s.%s", instanciaID, time.Now().Format("2006-01"), msgID, ext)

	// 1. Prioridade: MinIO Isolado configurado pela própria Revenda
	if cfgRevenda != nil && cfgRevenda.Configurado() {
		cliRevenda := NovoClienteMinIO(*cfgRevenda)
		if urlRemota, err := cliRevenda.UploadMidia(ctx, chaveRelativa, dados, contentType); err == nil {
			return urlRemota, nil
		}
	}

	// 2. Segunda opção: MinIO Global configurado no .env da Stack Portainer
	if cliGlobal := ObterClienteGlobal(); cliGlobal != nil {
		if urlRemota, err := cliGlobal.UploadMidia(ctx, chaveRelativa, dados, contentType); err == nil {
			return urlRemota, nil
		}
	}

	// 3. Fallback: Armazenamento Local em disco
	dirBase := "./armazenamento_midia"
	urlBase := "http://localhost:8080"
	if config.AppConfig != nil {
		if config.AppConfig.DiretorioMidia != "" {
			dirBase = config.AppConfig.DiretorioMidia
		}
		if config.AppConfig.UrlPublicaPadrao != "" {
			urlBase = strings.TrimRight(config.AppConfig.UrlPublicaPadrao, "/")
		}
	}

	dirInstancia := filepath.Join(dirBase, instanciaID)
	_ = os.MkdirAll(dirInstancia, 0755)
	nomeArquivo := fmt.Sprintf("%s.%s", msgID, ext)
	caminhoCompleto := filepath.Join(dirInstancia, nomeArquivo)

	if err := os.WriteFile(caminhoCompleto, dados, 0644); err != nil {
		return "", err
	}

	return fmt.Sprintf("%s/api/v1/midia/%s/%s", urlBase, instanciaID, nomeArquivo), nil
}

func hmacSHA256(chave, dados []byte) []byte {
	h := hmac.New(sha256.New, chave)
	h.Write(dados)
	return h.Sum(nil)
}

func hashSHA256Hex(dados []byte) string {
	h := sha256.Sum256(dados)
	return hex.EncodeToString(h[:])
}

func derivarChaveAssinaturaS3(secretKey, dataYYYYMMDD, regiao, servico string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secretKey), []byte(dataYYYYMMDD))
	kRegion := hmacSHA256(kDate, []byte(regiao))
	kService := hmacSHA256(kRegion, []byte(servico))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	return kSigning
}

func codificarQueryCanonicaS3(v url.Values) string {
	chaves := make([]string, 0, len(v))
	for k := range v {
		chaves = append(chaves, k)
	}
	sort.Strings(chaves)
	var partes []string
	for _, k := range chaves {
		valores := v[k]
		sort.Strings(valores)
		for _, val := range valores {
			partes = append(partes, fmt.Sprintf("%s=%s",
				 strings.ReplaceAll(url.QueryEscape(k), "+", "%20"),
				 strings.ReplaceAll(url.QueryEscape(val), "+", "%20"),
			))
		}
	}
	return strings.Join(partes, "&")
}

