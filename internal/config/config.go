package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config guarda as configurações gerais da WooHub API
type Config struct {
	Ambiente               string // "production" ou "development"
	Porta                  string
	TipoBanco              string // "postgres" (Supabase) ou "sqlite"
	UriBanco               string
	SupabaseURL            string
	SupabasePublishableKey string
	SupabaseSecretKey      string
	SegredoJWT             string
	ChaveMestreSuperAdmin  string
	AdminEmail             string
	AdminSenha             string
	SeedDadosDemo          bool
	CorsOrigens            []string
	FusoHorarioPadrao      string // "America/Sao_Paulo"
	LimitePadraoInstancias int
	DiretorioMidia         string
	UrlPublicaPadrao       string
	ResendAPIKey            string
	ResendFrom              string
	ResendFromName          string
	ResendOAuthClientID     string
	ResendOAuthClientSecret string
	ResendOAuthRedirectURI  string

	// Licenciamento & Proteção de Stack (Portainer / BYOI)
	LicencaChave              string
	LicencaServidorURL        string
	DominioOficial            string
	ExigirLicencaRemota       bool
	ModoServidorLicencaMaster bool

	// Storage Global S3 / MinIO (para Stack Portainer ou Padrão)
	StorageDriver  string // "s3" ou "local"
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioBucket    string
	MinioRegiao    string
	MinioUseSSL    bool
	MinioPublicURL string
}

var AppConfig *Config

// ObterConfig retorna a configuração atual ou carrega caso ainda não tenha sido inicializada
func ObterConfig() *Config {
	if AppConfig == nil {
		return CarregarConfiguracao()
	}
	return AppConfig
}

// CarregarConfiguracao carrega arquivo .env (se existir) e inicializa as variáveis de ambiente
func CarregarConfiguracao() *Config {
	carregarDotEnv(".env")

	ambiente := strings.ToLower(obterEnv("AMBIENTE", obterEnv("GO_ENV", "development")))
	porta := obterEnv("PORTA", obterEnv("PORT", "8080"))

	// Suporte nativo a SUPABASE_DB_URL / DATABASE_URL / URI_BANCO
	uriBanco := obterEnv("URI_BANCO", obterEnv("SUPABASE_DB_URL", obterEnv("DATABASE_URL", "")))
	tipoBanco := strings.ToLower(obterEnv("TIPO_BANCO", ""))

	// Autodetecção inteligente para Supabase / PostgreSQL
	if strings.HasPrefix(uriBanco, "postgres://") || strings.HasPrefix(uriBanco, "postgresql://") {
		tipoBanco = "postgres"
	} else if tipoBanco == "supabase" || tipoBanco == "postgresql" || tipoBanco == "pgx" {
		tipoBanco = "postgres"
	} else if tipoBanco == "" {
		tipoBanco = "sqlite"
	}

	if uriBanco == "" {
		if tipoBanco == "postgres" {
			uriBanco = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
		} else {
			uriBanco = "woohub.db"
		}
	}

	supabaseURL := obterEnv("SUPABASE_URL", "")
	supabasePubKey := obterEnv("SUPABASE_PUBLISHABLE_KEY", "")
	supabaseSecKey := obterEnv("SUPABASE_SECRET_KEY", "")
	segredoJWT := obterEnv("SEGREDO_JWT", "woohub-chave-secreta-super-segura-brasil-2026")
	chaveMestre := obterEnv("CHAVE_MESTRE", "woohub_master_key_brasil_prod")
	adminEmail := obterEnv("ADMIN_EMAIL", "admin@woohub.com.br")
	adminSenha := obterEnv("ADMIN_SENHA", "admin123")
	resendAPIKey := obterEnv("RESEND_API_KEY", "")
	resendFrom := obterEnv("RESEND_FROM", "noreply@seudominio.com.br")
	resendFromName := obterEnv("RESEND_FROM_NAME", "WooHub")
	resendOAuthClientID := obterEnv("RESEND_OAUTH_CLIENT_ID", "")
	resendOAuthClientSecret := obterEnv("RESEND_OAUTH_CLIENT_SECRET", "")
	resendOAuthRedirectURI := obterEnv("RESEND_OAUTH_REDIRECT_URI", "")

	seedPadrao := "true"
	if ambiente == "production" || ambiente == "prod" {
		seedPadrao = "false"
	}
	seedDemoStr := strings.ToLower(obterEnv("SEED_DADOS_DEMO", seedPadrao))
	seedDadosDemo := seedDemoStr == "true" || seedDemoStr == "1" || seedDemoStr == "sim"

	corsStr := obterEnv("CORS_ORIGENS", "*")
	var corsOrigens []string
	for _, o := range strings.Split(corsStr, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			corsOrigens = append(corsOrigens, o)
		}
	}

	fuso := obterEnv("FUSO_HORARIO", "America/Sao_Paulo")
	diretorioMidia := obterEnv("DIRETORIO_MIDIA", "./armazenamento_midia")
	urlPublica := obterEnv("URL_PUBLICA", "http://localhost:"+porta)

	limiteStr := obterEnv("LIMITE_PADRAO_INSTANCIAS", "15")
	limite, err := strconv.Atoi(limiteStr)
	if err != nil || limite <= 0 {
		limite = 15
	}

	// Garante que o diretório de mídias exista
	_ = os.MkdirAll(diretorioMidia, 0755)

	licencaChave := obterEnv("WOOHUB_LICENCA_CHAVE", obterEnv("LICENCA_CHAVE", ""))
	licencaServidorURL := strings.TrimRight(obterEnv("WOOHUB_SERVIDOR_LICENCA", ""), "/")
	dominioOficial := obterEnv("WOOHUB_DOMINIO_OFICIAL", urlPublica)
	exigirLicStr := strings.ToLower(obterEnv("EXIGIR_LICENCA", "false"))
	exigirLicencaRemota := exigirLicStr == "true" || exigirLicStr == "1" || exigirLicStr == "sim"
	modoMasterStr := strings.ToLower(obterEnv("MODO_SERVIDOR_MASTER", "true"))
	modoServidorMaster := modoMasterStr == "true" || modoMasterStr == "1" || licencaServidorURL == ""

	storageDriver := strings.ToLower(obterEnv("STORAGE_DRIVER", "local"))
	minioEndpoint := obterEnv("MINIO_ENDPOINT", "")
	minioAccessKey := obterEnv("MINIO_ACCESS_KEY", "")
	minioSecretKey := obterEnv("MINIO_SECRET_KEY", "")
	minioBucket := obterEnv("MINIO_BUCKET", "woohub-midias")
	minioRegiao := obterEnv("MINIO_REGIAO", "us-east-1")
	minioSSLStr := strings.ToLower(obterEnv("MINIO_USE_SSL", "true"))
	minioUseSSL := minioSSLStr == "true" || minioSSLStr == "1" || minioSSLStr == "sim"
	minioPublicURL := strings.TrimRight(obterEnv("MINIO_PUBLIC_URL", ""), "/")
	if minioEndpoint != "" && minioAccessKey != "" && minioSecretKey != "" {
		storageDriver = "s3"
	}

	AppConfig = &Config{
		Ambiente:                  ambiente,
		Porta:                     porta,
		TipoBanco:                 tipoBanco,
		UriBanco:                  uriBanco,
		SupabaseURL:               supabaseURL,
		SupabasePublishableKey:    supabasePubKey,
		SupabaseSecretKey:         supabaseSecKey,
		SegredoJWT:                segredoJWT,
		ChaveMestreSuperAdmin:     chaveMestre,
		AdminEmail:                adminEmail,
		AdminSenha:                adminSenha,
		SeedDadosDemo:             seedDadosDemo,
		CorsOrigens:               corsOrigens,
		FusoHorarioPadrao:         fuso,
		LimitePadraoInstancias:    limite,
		DiretorioMidia:            diretorioMidia,
		UrlPublicaPadrao:          urlPublica,
		ResendAPIKey:              resendAPIKey,
		ResendFrom:                resendFrom,
		ResendFromName:            resendFromName,
		ResendOAuthClientID:     resendOAuthClientID,
		ResendOAuthClientSecret: resendOAuthClientSecret,
		ResendOAuthRedirectURI:  resendOAuthRedirectURI,
		LicencaChave:              licencaChave,
		LicencaServidorURL:        licencaServidorURL,
		DominioOficial:            dominioOficial,
		ExigirLicencaRemota:       exigirLicencaRemota,
		ModoServidorLicencaMaster: modoServidorMaster,
		StorageDriver:             storageDriver,
		MinioEndpoint:             minioEndpoint,
		MinioAccessKey:            minioAccessKey,
		MinioSecretKey:            minioSecretKey,
		MinioBucket:               minioBucket,
		MinioRegiao:               minioRegiao,
		MinioUseSSL:               minioUseSSL,
		MinioPublicURL:            minioPublicURL,
	}

	return AppConfig
}

func (c *Config) EhProducao() bool {
	return c.Ambiente == "production" || c.Ambiente == "prod"
}

// ValidarProducao impede que uma configuração de desenvolvimento seja
// promovida silenciosamente para produção.
func (c *Config) ValidarProducao() error {
	if !c.EhProducao() {
		return nil
	}
	if c.AdminSenha == "" || c.AdminSenha == "admin123" || len(c.AdminSenha) < 12 {
		return fmt.Errorf("ADMIN_SENHA deve existir e ter pelo menos 12 caracteres")
	}
	if c.SegredoJWT == "" || strings.Contains(strings.ToLower(c.SegredoJWT), "woohub-chave-secreta") || len(c.SegredoJWT) < 32 {
		return fmt.Errorf("SEGREDO_JWT deve existir e ter pelo menos 32 caracteres aleatórios")
	}
	if c.ChaveMestreSuperAdmin == "" || strings.Contains(strings.ToLower(c.ChaveMestreSuperAdmin), "woohub_master_key") || len(c.ChaveMestreSuperAdmin) < 32 {
		return fmt.Errorf("CHAVE_MESTRE deve existir e ter pelo menos 32 caracteres aleatórios")
	}
	if len(c.CorsOrigens) == 0 || (len(c.CorsOrigens) == 1 && c.CorsOrigens[0] == "*") {
		return fmt.Errorf("CORS_ORIGENS deve listar origens explícitas; '*' não é permitido em produção")
	}
	if c.TipoBanco != "postgres" {
		return fmt.Errorf("produção exige TIPO_BANCO=postgres")
	}
	if c.UriBanco == "" {
		return fmt.Errorf("URI_BANCO é obrigatório em produção")
	}
	return nil
}

func obterEnv(chave, padrao string) string {
	valor := strings.TrimSpace(os.Getenv(chave))
	if valor == "" {
		return padrao
	}
	return valor
}

// carregarDotEnv lê variáveis de um arquivo .env local sem sobrescrever variáveis já definidas no SO
func carregarDotEnv(caminho string) {
	f, err := os.Open(caminho)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		linha := strings.TrimSpace(scanner.Text())
		if linha == "" || strings.HasPrefix(linha, "#") {
			continue
		}
		partes := strings.SplitN(linha, "=", 2)
		if len(partes) != 2 {
			continue
		}
		chave := strings.TrimSpace(partes[0])
		valor := strings.TrimSpace(partes[1])
		valor = strings.Trim(valor, `"'`)
		if chave != "" && os.Getenv(chave) == "" {
			_ = os.Setenv(chave, valor)
		}
	}
}
