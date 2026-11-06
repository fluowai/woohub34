package database

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq" // driver PostgreSQL / Supabase
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite" // driver SQLite embutido em puro Go
)

const (
	PapelSuperAdmin = "superadmin"
	PapelRevendedor = "revendedor"
	PapelCliente    = "cliente"
)

type Revendedor struct {
	ID                   string    `json:"id"`
	NomeEmpresa          string    `json:"nome_empresa"`
	Slug                 string    `json:"slug"`
	DominioPersonalizado string    `json:"dominio_personalizado"`
	LogoURL              string    `json:"logo_url"`
	FaviconURL           string    `json:"favicon_url"`
	CorPrimaria          string    `json:"cor_primaria"`
	CorSecundaria        string    `json:"cor_secundaria"`
	LimiteInstancias     int       `json:"limite_instancias"`
	Ativo                bool      `json:"ativo"`
	SupabaseURI          string    `json:"supabase_uri,omitempty"`
	MinioEndpoint        string    `json:"minio_endpoint,omitempty"`
	MinioAccessKey       string    `json:"minio_access_key,omitempty"`
	MinioSecretKey       string    `json:"minio_secret_key,omitempty"`
	MinioBucket          string    `json:"minio_bucket,omitempty"`
	MinioUseSSL          bool      `json:"minio_use_ssl"`
	MinioPublicURL       string    `json:"minio_public_url,omitempty"`
	CriadoEm             time.Time `json:"criado_em"`
}

// InfraestruturaBYOPronta indica se a revenda configurou os dois serviços
// obrigatórios do plano BYO. A aplicação nunca deve usar o banco central como
// fallback silencioso para dados operacionais de uma revenda.
func (r *Revendedor) InfraestruturaBYOPronta() bool {
	return r != nil && r.Ativo &&
		strings.TrimSpace(r.SupabaseURI) != "" &&
		strings.TrimSpace(r.MinioEndpoint) != "" &&
		strings.TrimSpace(r.MinioAccessKey) != "" &&
		strings.TrimSpace(r.MinioSecretKey) != "" &&
		strings.TrimSpace(r.MinioBucket) != ""
}

type ConfiguracaoEmailRevenda struct {
	RevendedorID string `json:"revendedor_id"`
	APIKey       string `json:"-"`
	Dominio      string `json:"dominio"`
	Remetente    string `json:"remetente"`
	NomeRemetente string `json:"nome_remetente"`
	MetodoAuth    string `json:"metodo_auth"`
	TokenRefresh  string `json:"-"`
	DominiosJSON  string `json:"dominios_json,omitempty"`
	Ativo        bool `json:"ativo"`
	Verificado   bool `json:"verificado"`
}

type LicencaInstalacao struct {
	ID                   string    `json:"id"`
	ChaveLicenca         string    `json:"chave_licenca"`
	RevendedorID         string    `json:"revendedor_id"`
	ClienteNome          string    `json:"cliente_nome"`
	EmailContato         string    `json:"email_contato"`
	DominioAutorizado    string    `json:"dominio_autorizado"`
	IPAutorizado         string    `json:"ip_autorizado"`
	FingerprintContainer string    `json:"fingerprint_container"`
	Plano                string    `json:"plano"`
	LimiteInstancias     int       `json:"limite_instancias"`
	Status               string    `json:"status"` // ATIVA, SUSPENSA, BLOQUEADA, EXPIRADA
	ExpiraEm             time.Time `json:"expira_em"`
	UltimoHeartbeat      time.Time `json:"ultimo_heartbeat"`
	UltimoIP             string    `json:"ultimo_ip"`
	VersaoInstalada      string    `json:"versao_instalada"`
	InstanciasAtivas     int       `json:"instancias_ativas"`
	TokenAssinadoEd25519 string    `json:"token_assinado_ed25519,omitempty"`
	CriadoEm             time.Time `json:"criado_em"`
}

type Cliente struct {
	ID               string    `json:"id"`
	RevendedorID     string    `json:"revendedor_id"`
	Nome             string    `json:"nome"`
	Email            string    `json:"email"`
	SenhaHash        string    `json:"-"`
	Documento        string    `json:"documento"`
	Papel            string    `json:"papel"` // "superadmin", "revendedor", "cliente"
	LimiteInstancias int       `json:"limite_instancias"`
	Ativo            bool      `json:"ativo"`
	CriadoEm         time.Time `json:"criado_em"`
	ViaChaveAPI      bool      `json:"-"` // Indica se autenticou via x-api-key restrita a uma instância
	InstanciaKeyID   string    `json:"-"` // ID da instância quando autenticado via x-api-key
}

type SessaoAuth struct {
	Token     string    `json:"token"`
	ClienteID string    `json:"cliente_id"`
	ExpiraEm  time.Time `json:"expira_em"`
	CriadoEm  time.Time `json:"criado_em"`
}

type Instancia struct {
	ID                      string    `json:"id"`
	ClienteID               string    `json:"cliente_id"`
	Nome                    string    `json:"nome"`
	ChaveAPI                string    `json:"chave_api"`
	JID                     string    `json:"jid,omitempty"`
	NumeroTelefone          string    `json:"numero_telefone,omitempty"`
	Status                  string    `json:"status"` // DESCONECTADO, GERANDO_QR, AGUARDANDO_LEITURA, CONECTADO
	WebhookURL              string    `json:"webhook_url,omitempty"`
	WebhookEventos          string    `json:"webhook_eventos,omitempty"` // separado por vírgula
	WebhookSegredo          string    `json:"webhook_segredo,omitempty"`
	IgnorarGrupos           bool      `json:"ignorar_grupos"`
	RejeitarChamadas        bool      `json:"rejeitar_chamadas"`
	MensagemChamadaRejeitar string    `json:"mensagem_chamada_rejeitada,omitempty"`
	ProxyURL                string    `json:"proxy_url,omitempty"`
	LeituraAutomatica       bool      `json:"leitura_automatica"`
	SempreOnline            bool      `json:"sempre_online"`
	CriadoEm                time.Time `json:"criado_em"`
	AtualizadoEm            time.Time `json:"atualizado_em"`
}

type LogMensagem struct {
	ID           string    `json:"id"`
	InstanciaID  string    `json:"instancia_id"`
	MensagemID   string    `json:"mensagem_id"`
	Tipo         string    `json:"tipo"`
	Destinatario string    `json:"destinatario"`
	Direcao      string    `json:"direcao"` // ENVIADA, RECEBIDA
	Status       string    `json:"status"`  // ENVIANDO, ENTREGUE, LIDA, ERRO
	DetalhesErro string    `json:"detalhes_erro,omitempty"`
	CriadoEm     time.Time `json:"criado_em"`
}

type IntegracaoInstancia struct {
	ID            string    `json:"id"`
	InstanciaID   string    `json:"instancia_id"`
	Tipo          string    `json:"tipo"` // 'chatwoot', 'typebot', 'n8n', 'kommo', 'rdstation', 'webhook'
	Ativo         bool      `json:"ativo"`
	Configuracoes string    `json:"configuracoes"` // JSON string com campos específicos
	CriadoEm      time.Time `json:"criado_em"`
	AtualizadoEm  time.Time `json:"atualizado_em"`
}

type SessaoTypebot struct {
	ID               string    `json:"id"`
	InstanciaID      string    `json:"instancia_id"`
	RemetenteJID     string    `json:"remetente_jid"`
	SessionIDTypebot string    `json:"session_id_typebot"`
	Estado           string    `json:"estado"` // 'ATIVO', 'PAUSADO_HUMANO'
	AtualizadoEm     time.Time `json:"atualizado_em"`
}

type AtribuicaoContato struct {
	ID               string    `json:"id"`
	InstanciaID      string    `json:"instancia_id"`
	JID              string    `json:"jid"`
	NumeroFormatado  string    `json:"numero_formatado"`
	OrigemCanal      string    `json:"origem_canal"` // "Meta Ads (Instagram)", "Google Ads", etc.
	Campanha         string    `json:"campanha"`
	UtmSource        string    `json:"utm_source"`
	UtmMedium        string    `json:"utm_medium"`
	UtmCampaign      string    `json:"utm_campaign"`
	UtmTerm          string    `json:"utm_term"`
	UtmContent       string    `json:"utm_content"`
	CtwaClid         string    `json:"ctwa_clid"`
	AdSourceID       string    `json:"ad_source_id"`
	AdSourceURL      string    `json:"ad_source_url"`
	Referencia       string    `json:"referencia"`
	PrimeiraMensagem string    `json:"primeira_mensagem"`
	CriadoEm         time.Time `json:"criado_em"`
	AtualizadoEm     time.Time `json:"atualizado_em"`
}

type GerenciadorDB struct {
	db     *sql.DB
	driver string // "postgres" ou "sqlite"
	mu     sync.RWMutex
}

var DB *GerenciadorDB

// OpcoesSeed configura o provisionamento inicial do banco
type OpcoesSeed struct {
	AdminEmail    string
	AdminSenha    string
	SeedDadosDemo bool
}

// InicializarBanco cria conexão PostgreSQL (Supabase) ou SQLite e executa migrações
func InicializarBanco(driver, uri string, opcoes ...OpcoesSeed) (*GerenciadorDB, error) {
	driver = strings.ToLower(strings.TrimSpace(driver))
	if strings.HasPrefix(uri, "postgres://") || strings.HasPrefix(uri, "postgresql://") || driver == "postgres" || driver == "postgresql" || driver == "supabase" {
		driver = "postgres"
		// Supavisor do Supabase na porta 6543 (Transaction Mode) quebra prepared statements do lib/pq e sqlstore;
		// converte automaticamente para porta 5432 (Session Mode) no mesmo pooler IPv4.
		if strings.Contains(uri, ".pooler.supabase.com:6543") {
			uri = strings.Replace(uri, ".pooler.supabase.com:6543", ".pooler.supabase.com:5432", 1)
		}
		// Se for host Supabase e não tiver sslmode explícito, adiciona sslmode=require
		if (strings.Contains(uri, ".supabase.co") || strings.Contains(uri, ".supabase.com")) && !strings.Contains(uri, "sslmode=") {
			if strings.Contains(uri, "?") {
				uri += "&sslmode=require"
			} else {
				uri += "?sslmode=require"
			}
		}
	} else {
		driver = "sqlite"
		if !strings.Contains(uri, "_pragma") && !strings.Contains(uri, "_foreign_keys") {
			if strings.Contains(uri, "?") {
				uri += "&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
			} else {
				uri += "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
			}
		}
	}

	conexao, err := sql.Open(driver, uri)
	if err != nil {
		return nil, fmt.Errorf("erro ao abrir conexão com banco (%s): %w", driver, err)
	}

	if driver == "postgres" {
		conexao.SetMaxOpenConns(25)
		conexao.SetMaxIdleConns(5)
		conexao.SetConnMaxLifetime(30 * time.Minute)
		conexao.SetConnMaxIdleTime(5 * time.Minute)
	} else {
		conexao.SetMaxOpenConns(1)
	}

	if err := conexao.Ping(); err != nil {
		_ = conexao.Close()
		return nil, fmt.Errorf("falha no ping ao banco de dados (%s): %w", driver, err)
	}

	g := &GerenciadorDB{
		db:     conexao,
		driver: driver,
	}

	if err := g.migrar(); err != nil {
		_ = conexao.Close()
		return nil, fmt.Errorf("erro ao executar migrações (%s): %w", driver, err)
	}

	seedCfg := OpcoesSeed{
		AdminEmail:    "admin@woohub.com.br",
		AdminSenha:    "admin123",
		SeedDadosDemo: true,
	}
	if len(opcoes) > 0 {
		if opcoes[0].AdminEmail != "" {
			seedCfg.AdminEmail = opcoes[0].AdminEmail
		}
		if opcoes[0].AdminSenha != "" {
			seedCfg.AdminSenha = opcoes[0].AdminSenha
		}
		seedCfg.SeedDadosDemo = opcoes[0].SeedDadosDemo
	}

	if err := g.criarDadosPadrao(seedCfg); err != nil {
		_ = conexao.Close()
		return nil, fmt.Errorf("erro ao criar dados padrão: %w", err)
	}

	// Inicia rotina periódica de limpeza de sessões expiradas
	go g.iniciarLimpezaPeriodicaSessoes()

	DB = g
	return g, nil
}

func (g *GerenciadorDB) ObterConexaoSQL() *sql.DB {
	return g.db
}

// ObterDialetoWhatsmeow retorna o dialeto compatível com sqlstore.NewWithDB ("postgres" ou "sqlite3")
func (g *GerenciadorDB) ObterDialetoWhatsmeow() string {
	if g.driver == "postgres" {
		return "postgres"
	}
	return "sqlite3"
}

// rebind converte placeholders '?' para '$1, $2, ...' automaticamente quando o driver é PostgreSQL (Supabase)
func (g *GerenciadorDB) rebind(query string) string {
	if g.driver != "postgres" {
		return query
	}
	var sb strings.Builder
	sb.Grow(len(query) + 16)
	paramIdx := 1
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			sb.WriteByte('$')
			sb.WriteString(strconv.Itoa(paramIdx))
			paramIdx++
		} else {
			sb.WriteByte(query[i])
		}
	}
	return sb.String()
}

func (g *GerenciadorDB) exec(query string, args ...interface{}) (sql.Result, error) {
	return g.db.Exec(g.rebind(query), args...)
}

func (g *GerenciadorDB) query(query string, args ...interface{}) (*sql.Rows, error) {
	return g.db.Query(g.rebind(query), args...)
}

func (g *GerenciadorDB) queryRow(query string, args ...interface{}) *sql.Row {
	return g.db.QueryRow(g.rebind(query), args...)
}

func gerarChaveAleatoria(prefixo string, bytesLen int) string {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		return prefixo + strings.ReplaceAll(uuid.New().String(), "-", "")
	}
	return prefixo + hex.EncodeToString(b)
}

func (g *GerenciadorDB) migrar() error {
	tipoDataHora := "DATETIME"
	if g.driver == "postgres" {
		tipoDataHora = "TIMESTAMPTZ"
	}

	queries := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS revendedores (
			id TEXT PRIMARY KEY,
			nome_empresa TEXT NOT NULL,
			slug TEXT UNIQUE NOT NULL,
			dominio_personalizado TEXT UNIQUE,
			logo_url TEXT DEFAULT '',
			favicon_url TEXT DEFAULT '',
			cor_primaria TEXT DEFAULT '#00C853',
			cor_secundaria TEXT DEFAULT '#1E293B',
			limite_instancias INTEGER DEFAULT 15,
			ativo INTEGER DEFAULT 1,
			criado_em %s DEFAULT CURRENT_TIMESTAMP
		);`, tipoDataHora),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS clientes (
			id TEXT PRIMARY KEY,
			revendedor_id TEXT NOT NULL,
			nome TEXT NOT NULL,
			email TEXT UNIQUE NOT NULL,
			senha_hash TEXT NOT NULL,
			documento TEXT DEFAULT '',
			papel TEXT NOT NULL DEFAULT 'cliente',
			limite_instancias INTEGER DEFAULT 5,
			ativo INTEGER DEFAULT 1,
			criado_em %s DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (revendedor_id) REFERENCES revendedores(id) ON DELETE CASCADE
		);`, tipoDataHora),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS instancias (
			id TEXT PRIMARY KEY,
			cliente_id TEXT NOT NULL,
			nome TEXT NOT NULL,
			chave_api TEXT UNIQUE NOT NULL,
			jid TEXT DEFAULT '',
			numero_telefone TEXT DEFAULT '',
			status TEXT DEFAULT 'DESCONECTADO',
			webhook_url TEXT DEFAULT '',
			webhook_eventos TEXT DEFAULT 'mensagem_recebida,status_envio,conexao',
			webhook_segredo TEXT DEFAULT '',
			ignorar_grupos INTEGER DEFAULT 0,
			rejeitar_chamadas INTEGER DEFAULT 0,
			mensagem_chamada_rejeitada TEXT DEFAULT '',
			proxy_url TEXT DEFAULT '',
			leitura_automatica INTEGER DEFAULT 0,
			sempre_online INTEGER DEFAULT 0,
			criado_em %s DEFAULT CURRENT_TIMESTAMP,
			atualizado_em %s DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (cliente_id) REFERENCES clientes(id) ON DELETE CASCADE
		);`, tipoDataHora, tipoDataHora),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS logs_mensagens (
			id TEXT PRIMARY KEY,
			instancia_id TEXT NOT NULL,
			mensagem_id TEXT DEFAULT '',
			tipo TEXT NOT NULL,
			destinatario TEXT NOT NULL,
			direcao TEXT NOT NULL,
			status TEXT DEFAULT 'ENVIANDO',
			detalhes_erro TEXT DEFAULT '',
			criado_em %s DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (instancia_id) REFERENCES instancias(id) ON DELETE CASCADE
		);`, tipoDataHora),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS sessoes_auth (
			token TEXT PRIMARY KEY,
			cliente_id TEXT NOT NULL,
			expira_em %s NOT NULL,
			criado_em %s DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (cliente_id) REFERENCES clientes(id) ON DELETE CASCADE
		);`, tipoDataHora, tipoDataHora),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS licencas_instalacoes (
			id TEXT PRIMARY KEY,
			chave_licenca TEXT UNIQUE NOT NULL,
			revendedor_id TEXT DEFAULT '',
			cliente_nome TEXT NOT NULL,
			email_contato TEXT DEFAULT '',
			dominio_autorizado TEXT DEFAULT '',
			ip_autorizado TEXT DEFAULT '',
			fingerprint_container TEXT DEFAULT '',
			plano TEXT DEFAULT 'Enterprise Stack',
			limite_instancias INTEGER DEFAULT 50,
			status TEXT DEFAULT 'ATIVA',
			expira_em %s NOT NULL,
			ultimo_heartbeat %s DEFAULT CURRENT_TIMESTAMP,
			ultimo_ip TEXT DEFAULT '',
			versao_instalada TEXT DEFAULT '1.1.0-Brasil-Prod',
			instancias_ativas INTEGER DEFAULT 0,
			token_assinado_ed25519 TEXT DEFAULT '',
			criado_em %s DEFAULT CURRENT_TIMESTAMP
		);`, tipoDataHora, tipoDataHora, tipoDataHora),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS integracoes_instancia (
			id TEXT PRIMARY KEY,
			instancia_id TEXT NOT NULL,
			tipo TEXT NOT NULL,
			ativo INTEGER DEFAULT 0,
			configuracoes TEXT NOT NULL,
			criado_em %s DEFAULT CURRENT_TIMESTAMP,
			atualizado_em %s DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (instancia_id) REFERENCES instancias(id) ON DELETE CASCADE,
			UNIQUE (instancia_id, tipo)
		);`, tipoDataHora, tipoDataHora),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS sessoes_typebot (
			id TEXT PRIMARY KEY,
			instancia_id TEXT NOT NULL,
			remetente_jid TEXT NOT NULL,
			session_id_typebot TEXT NOT NULL,
			estado TEXT DEFAULT 'ATIVO',
			atualizado_em %s DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (instancia_id) REFERENCES instancias(id) ON DELETE CASCADE,
			UNIQUE (instancia_id, remetente_jid)
		);`, tipoDataHora),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS atribuicoes_contato (
			id TEXT PRIMARY KEY,
			instancia_id TEXT NOT NULL,
			jid TEXT NOT NULL,
			numero_formatado TEXT DEFAULT '',
			origem_canal TEXT DEFAULT '',
			campanha TEXT DEFAULT '',
			utm_source TEXT DEFAULT '',
			utm_medium TEXT DEFAULT '',
			utm_campaign TEXT DEFAULT '',
			utm_term TEXT DEFAULT '',
			utm_content TEXT DEFAULT '',
			ctwa_clid TEXT DEFAULT '',
			ad_source_id TEXT DEFAULT '',
			ad_source_url TEXT DEFAULT '',
			referencia TEXT DEFAULT '',
			primeira_mensagem TEXT DEFAULT '',
			criado_em %s DEFAULT CURRENT_TIMESTAMP,
			atualizado_em %s DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (instancia_id) REFERENCES instancias(id) ON DELETE CASCADE,
			UNIQUE (instancia_id, jid)
		);`, tipoDataHora, tipoDataHora),
		`CREATE INDEX IF NOT EXISTS idx_clientes_revendedor_id ON clientes(revendedor_id);`,
		`CREATE INDEX IF NOT EXISTS idx_instancias_cliente_id ON instancias(cliente_id);`,
		`CREATE INDEX IF NOT EXISTS idx_instancias_chave_api ON instancias(chave_api);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_instancia_criado ON logs_mensagens(instancia_id, criado_em);`,
		`CREATE INDEX IF NOT EXISTS idx_sessoes_auth_expira_em ON sessoes_auth(expira_em);`,
		`CREATE INDEX IF NOT EXISTS idx_licencas_chave ON licencas_instalacoes(chave_licenca);`,
		`CREATE INDEX IF NOT EXISTS idx_integracoes_inst_tipo ON integracoes_instancia(instancia_id, tipo);`,
		`CREATE INDEX IF NOT EXISTS idx_sessoes_typebot_busca ON sessoes_typebot(instancia_id, remetente_jid);`,
		`CREATE INDEX IF NOT EXISTS idx_atribuicoes_contato_busca ON atribuicoes_contato(instancia_id, jid);`,
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS configuracoes_email_revenda (
			revendedor_id TEXT PRIMARY KEY, api_key TEXT NOT NULL DEFAULT '', dominio TEXT DEFAULT '',
			remetente TEXT DEFAULT '', nome_remetente TEXT DEFAULT '', ativo INTEGER DEFAULT 1,
			verificado INTEGER DEFAULT 0, atualizado_em %s DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (revendedor_id) REFERENCES revendedores(id) ON DELETE CASCADE
		);`, tipoDataHora),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS onboarding_contas (
			usuario_id TEXT PRIMARY KEY, papel TEXT NOT NULL, etapa_atual TEXT NOT NULL DEFAULT 'conta',
			concluido INTEGER DEFAULT 0, dados TEXT DEFAULT '{}', atualizado_em %s DEFAULT CURRENT_TIMESTAMP
		);`, tipoDataHora),
	}

	for _, q := range queries {
		if _, err := g.db.Exec(q); err != nil {
			return err
		}
	}

	// Migração suave para bancos existentes que ainda não possuíam a coluna 'papel' ou novas colunas de instância/revenda
	_, _ = g.db.Exec(`ALTER TABLE clientes ADD COLUMN papel TEXT NOT NULL DEFAULT 'cliente'`)
	_, _ = g.db.Exec(`ALTER TABLE instancias ADD COLUMN proxy_url TEXT DEFAULT ''`)
	_, _ = g.db.Exec(`ALTER TABLE instancias ADD COLUMN leitura_automatica INTEGER DEFAULT 0`)
	_, _ = g.db.Exec(`ALTER TABLE instancias ADD COLUMN sempre_online INTEGER DEFAULT 0`)
	_, _ = g.db.Exec(`ALTER TABLE revendedores ADD COLUMN supabase_uri TEXT DEFAULT ''`)
	_, _ = g.db.Exec(`ALTER TABLE revendedores ADD COLUMN minio_endpoint TEXT DEFAULT ''`)
	_, _ = g.db.Exec(`ALTER TABLE revendedores ADD COLUMN minio_access_key TEXT DEFAULT ''`)
	_, _ = g.db.Exec(`ALTER TABLE revendedores ADD COLUMN minio_secret_key TEXT DEFAULT ''`)
	_, _ = g.db.Exec(`ALTER TABLE revendedores ADD COLUMN minio_bucket TEXT DEFAULT 'woohub-midias'`)
	_, _ = g.db.Exec(`ALTER TABLE revendedores ADD COLUMN minio_use_ssl INTEGER DEFAULT 1`)
	_, _ = g.db.Exec(`ALTER TABLE revendedores ADD COLUMN minio_public_url TEXT DEFAULT ''`)
	_, _ = g.db.Exec(`ALTER TABLE configuracoes_email_revenda ADD COLUMN metodo_auth TEXT DEFAULT 'api_key'`)
	_, _ = g.db.Exec(`ALTER TABLE configuracoes_email_revenda ADD COLUMN token_refresh TEXT DEFAULT ''`)
	_, _ = g.db.Exec(`ALTER TABLE configuracoes_email_revenda ADD COLUMN dominios_json TEXT DEFAULT ''`)
	_, _ = g.exec(`UPDATE clientes SET papel = ? WHERE id = 'cli_demo_admin' OR LOWER(email) = 'admin@woohub.com.br'`, PapelSuperAdmin)
	_ = g.CriarTabelasChat()

	return nil
}

func (g *GerenciadorDB) BuscarConfiguracaoEmailRevenda(id string) (*ConfiguracaoEmailRevenda, error) {
	var c ConfiguracaoEmailRevenda; var ativo, verificado int
	err := g.queryRow(`SELECT revendedor_id, api_key, COALESCE(dominio,''), COALESCE(remetente,''), COALESCE(nome_remetente,''), COALESCE(metodo_auth,'api_key'), COALESCE(token_refresh,''), COALESCE(dominios_json,''), ativo, verificado FROM configuracoes_email_revenda WHERE revendedor_id = ?`, id).Scan(&c.RevendedorID, &c.APIKey, &c.Dominio, &c.Remetente, &c.NomeRemetente, &c.MetodoAuth, &c.TokenRefresh, &c.DominiosJSON, &ativo, &verificado)
	if err != nil { return nil, err }
	c.Ativo, c.Verificado = ativo == 1, verificado == 1
	return &c, nil
}

func (g *GerenciadorDB) SalvarConfiguracaoEmailRevenda(c *ConfiguracaoEmailRevenda) error {
	ativo, verificado := 0, 0; if c.Ativo { ativo = 1 }; if c.Verificado { verificado = 1 }
	if c.MetodoAuth == "" { c.MetodoAuth = "api_key" }
	_, err := g.exec(`INSERT INTO configuracoes_email_revenda (revendedor_id, api_key, dominio, remetente, nome_remetente, metodo_auth, token_refresh, dominios_json, ativo, verificado, atualizado_em) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP) ON CONFLICT (revendedor_id) DO UPDATE SET api_key=excluded.api_key, dominio=excluded.dominio, remetente=excluded.remetente, nome_remetente=excluded.nome_remetente, metodo_auth=excluded.metodo_auth, token_refresh=excluded.token_refresh, dominios_json=excluded.dominios_json, ativo=excluded.ativo, verificado=excluded.verificado, atualizado_em=CURRENT_TIMESTAMP`, c.RevendedorID, c.APIKey, c.Dominio, c.Remetente, c.NomeRemetente, c.MetodoAuth, c.TokenRefresh, c.DominiosJSON, ativo, verificado)
	return err
}

func (g *GerenciadorDB) SalvarEtapaOnboarding(usuarioID, papel, etapa, dados string, concluido bool) error {
	v := 0; if concluido { v = 1 }
	_, err := g.exec(`INSERT INTO onboarding_contas (usuario_id, papel, etapa_atual, concluido, dados, atualizado_em) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP) ON CONFLICT (usuario_id) DO UPDATE SET etapa_atual=excluded.etapa_atual, concluido=excluded.concluido, dados=excluded.dados, atualizado_em=CURRENT_TIMESTAMP`, usuarioID, papel, etapa, v, dados)
	return err
}

func (g *GerenciadorDB) BuscarOnboarding(usuarioID string) (map[string]interface{}, error) {
	var papel, etapa, dados string; var concluido int
	err := g.queryRow(`SELECT papel, etapa_atual, concluido, COALESCE(dados,'{}') FROM onboarding_contas WHERE usuario_id = ?`, usuarioID).Scan(&papel, &etapa, &concluido, &dados)
	if err != nil { return nil, err }
	return map[string]interface{}{"usuario_id": usuarioID, "papel": papel, "etapa_atual": etapa, "concluido": concluido == 1, "dados": dados}, nil
}

// Funções Utilitárias de Senha (bcrypt estrito)
func HashSenha(senha string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
	return string(bytes), err
}

func VerificarSenha(senhaDigitada, hashArmazenado string) bool {
	if strings.HasPrefix(hashArmazenado, "$2a$") || strings.HasPrefix(hashArmazenado, "$2b$") || strings.HasPrefix(hashArmazenado, "$2y$") {
		err := bcrypt.CompareHashAndPassword([]byte(hashArmazenado), []byte(senhaDigitada))
		return err == nil
	}
	return false
}

func (g *GerenciadorDB) criarDadosPadrao(cfg OpcoesSeed) error {
	var count int
	err := g.queryRow("SELECT COUNT(*) FROM revendedores").Scan(&count)
	if err != nil {
		return err
	}

	hashAdmin, err := HashSenha(cfg.AdminSenha)
	if err != nil {
		return err
	}

	adminEmail := strings.TrimSpace(strings.ToLower(cfg.AdminEmail))
	if adminEmail == "" {
		adminEmail = "admin@woohub.com.br"
	}

	if count == 0 {
		revId := "rev_woohub_oficial"
		cliId := "cli_demo_admin"
		instId := "inst_demo_brasil"
		chaveApiDemo := gerarChaveAleatoria("woo_br_", 16)

		_, err = g.exec(`
			INSERT INTO revendedores (id, nome_empresa, slug, dominio_personalizado, logo_url, favicon_url, cor_primaria, cor_secundaria, limite_instancias, ativo)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
		`, revId, "WooHub API Brasil", "woohub", "localhost", "", "", "#00C853", "#0F172A", 100)
		if err != nil {
			return err
		}

		_, err = g.exec(`
			INSERT INTO clientes (id, revendedor_id, nome, email, senha_hash, documento, papel, limite_instancias, ativo)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)
		`, cliId, revId, "Administrador WooHub", adminEmail, hashAdmin, "00.000.000/0001-91", PapelSuperAdmin, 100)
		if err != nil {
			return err
		}

		_, err = g.exec(`
			INSERT INTO instancias (id, cliente_id, nome, chave_api, status)
			VALUES (?, ?, ?, ?, 'DESCONECTADO')
		`, instId, cliId, "Instância Principal", chaveApiDemo)
		if err != nil {
			return err
		}
	} else {
		var cliCount int
		_ = g.queryRow("SELECT COUNT(*) FROM clientes WHERE LOWER(email) = ?", adminEmail).Scan(&cliCount)
		if cliCount == 0 {
			var revId string
			_ = g.queryRow("SELECT id FROM revendedores ORDER BY criado_em ASC LIMIT 1").Scan(&revId)
			if revId != "" {
				_, _ = g.exec(`
					INSERT INTO clientes (id, revendedor_id, nome, email, senha_hash, documento, papel, limite_instancias, ativo)
					VALUES ('cli_demo_admin', ?, 'Administrador WooHub', ?, ?, '00.000.000/0001-91', ?, 100, 1)
				`, revId, adminEmail, hashAdmin, PapelSuperAdmin)
			}
		} else {
			_, _ = g.exec(`UPDATE clientes SET papel = ? WHERE LOWER(email) = ?`, PapelSuperAdmin, adminEmail)
		}
	}

	// Garante que exista ao menos a licença Master oficial cadastrada para monitoramento no Mega Admin
	var countLic int
	_ = g.queryRow("SELECT COUNT(*) FROM licencas_instalacoes").Scan(&countLic)
	if countLic == 0 {
		expMaster := time.Now().AddDate(5, 0, 0)
		_, _ = g.exec(`
			INSERT INTO licencas_instalacoes (id, chave_licenca, revendedor_id, cliente_nome, email_contato, dominio_autorizado, ip_autorizado, plano, limite_instancias, status, expira_em, versao_instalada, instancias_ativas)
			VALUES ('lic_master_oficial', 'woo_lic_master_oficial_brasil', 'rev_woohub_oficial', 'Servidor Master WooHub (Matriz)', ?, 'localhost', '127.0.0.1', 'Master Unlimited', 999, 'ATIVA', ?, '1.1.0-Brasil-Prod', 1)
		`, adminEmail, expMaster)
	}

	if !cfg.SeedDadosDemo {
		return nil
	}

	// Dados demonstrativos apenas quando SeedDadosDemo está habilitado
	var countVertice int
	_ = g.queryRow("SELECT COUNT(*) FROM revendedores WHERE id = 'rev_vertice' OR slug = 'vertice'").Scan(&countVertice)
	if countVertice == 0 {
		_, _ = g.exec(`
			INSERT INTO revendedores (id, nome_empresa, slug, dominio_personalizado, logo_url, favicon_url, cor_primaria, cor_secundaria, limite_instancias, ativo)
			VALUES ('rev_vertice', 'Agência Vértice', 'vertice', 'api.vertice.com.br', '', '', '#10B981', '#041C14', 150, 1)
		`)
	}

	var countCliVertice int
	_ = g.queryRow("SELECT COUNT(*) FROM clientes WHERE revendedor_id = 'rev_vertice'").Scan(&countCliVertice)
	if countCliVertice == 0 {
		clientesSeed := []struct {
			ID, Nome, Email, Doc, Papel string
			Limite                      int
		}{
			{"cli_rev_vertice_admin", "Agência Vértice", "revenda@vertice.com.br", "10.000.000/0001-10", PapelRevendedor, 150},
			{"cli_clinica_essencia", "Clínica Essência", "contato@clinicaessencia.com.br", "12.345.678/0001-90", PapelCliente, 20},
			{"cli_casa_verde", "Casa Verde", "atendimento@casaverde.com.br", "98.765.432/0001-11", PapelCliente, 10},
			{"cli_imobiliaria_prime", "Imobiliária Prime", "vendas@imobiliariaprime.com.br", "45.678.901/0001-22", PapelCliente, 30},
			{"cli_studio_aurora", "Studio Aurora", "contato@studioaurora.com.br", "23.456.789/0001-33", PapelCliente, 10},
		}
		for _, cs := range clientesSeed {
			_, _ = g.exec(`
				INSERT INTO clientes (id, revendedor_id, nome, email, senha_hash, documento, papel, limite_instancias, ativo)
				VALUES (?, 'rev_vertice', ?, ?, ?, ?, ?, ?, 1)
			`, cs.ID, cs.Nome, cs.Email, hashAdmin, cs.Doc, cs.Papel, cs.Limite)
		}

		_, _ = g.exec(`
			INSERT INTO instancias (id, cliente_id, nome, chave_api, status, numero_telefone)
			VALUES ('inst_atend', 'cli_clinica_essencia', 'Atendimento', ?, 'DESCONECTADO', '')
		`, gerarChaveAleatoria("woo_key_", 16))
		_, _ = g.exec(`
			INSERT INTO instancias (id, cliente_id, nome, chave_api, status, numero_telefone)
			VALUES ('inst_comercial', 'cli_clinica_essencia', 'Comercial', ?, 'DESCONECTADO', '')
		`, gerarChaveAleatoria("woo_key_", 16))
		_, _ = g.exec(`
			INSERT INTO instancias (id, cliente_id, nome, chave_api, status)
			VALUES ('inst_homolog', 'cli_clinica_essencia', 'Homologação', ?, 'DESCONECTADO')
		`, gerarChaveAleatoria("woo_key_", 16))
	} else {
		var countRevAdmin int
		_ = g.queryRow("SELECT COUNT(*) FROM clientes WHERE revendedor_id = 'rev_vertice' AND papel = ?", PapelRevendedor).Scan(&countRevAdmin)
		if countRevAdmin == 0 {
			_, _ = g.exec(`
				INSERT INTO clientes (id, revendedor_id, nome, email, senha_hash, documento, papel, limite_instancias, ativo)
				VALUES ('cli_rev_vertice_admin', 'rev_vertice', 'Agência Vértice', 'revenda@vertice.com.br', ?, '10.000.000/0001-10', ?, 150, 1)
			`, hashAdmin, PapelRevendedor)
		}
	}

	return nil
}

func (g *GerenciadorDB) iniciarLimpezaPeriodicaSessoes() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		_, _ = g.exec(`DELETE FROM sessoes_auth WHERE expira_em < ?`, time.Now())
	}
}

// Métodos de Revendedores (White-Label + Infraestrutura Isolada BYO Supabase & MinIO)
func (g *GerenciadorDB) BuscarRevendedorPorDominio(dominio string) (*Revendedor, error) {
	if idx := strings.Index(dominio, ":"); idx != -1 {
		dominio = dominio[:idx]
	}

	var r Revendedor
	var ativoInt, sslInt int
	query := `SELECT id, nome_empresa, slug, COALESCE(dominio_personalizado, ''), COALESCE(logo_url, ''), COALESCE(favicon_url, ''), cor_primaria, cor_secundaria, limite_instancias, ativo,
			  COALESCE(supabase_uri, ''), COALESCE(minio_endpoint, ''), COALESCE(minio_access_key, ''), COALESCE(minio_secret_key, ''), COALESCE(minio_bucket, 'woohub-midias'), COALESCE(minio_use_ssl, 1), COALESCE(minio_public_url, ''), criado_em 
			  FROM revendedores WHERE dominio_personalizado = ? OR slug = ? LIMIT 1`
	err := g.queryRow(query, dominio, dominio).Scan(
		&r.ID, &r.NomeEmpresa, &r.Slug, &r.DominioPersonalizado, &r.LogoURL, &r.FaviconURL, &r.CorPrimaria, &r.CorSecundaria, &r.LimiteInstancias, &ativoInt,
		&r.SupabaseURI, &r.MinioEndpoint, &r.MinioAccessKey, &r.MinioSecretKey, &r.MinioBucket, &sslInt, &r.MinioPublicURL, &r.CriadoEm,
	)
	if err != nil {
		queryPadrao := `SELECT id, nome_empresa, slug, COALESCE(dominio_personalizado, ''), COALESCE(logo_url, ''), COALESCE(favicon_url, ''), cor_primaria, cor_secundaria, limite_instancias, ativo,
						COALESCE(supabase_uri, ''), COALESCE(minio_endpoint, ''), COALESCE(minio_access_key, ''), COALESCE(minio_secret_key, ''), COALESCE(minio_bucket, 'woohub-midias'), COALESCE(minio_use_ssl, 1), COALESCE(minio_public_url, ''), criado_em 
						FROM revendedores ORDER BY criado_em ASC LIMIT 1`
		errPadrao := g.queryRow(queryPadrao).Scan(
			&r.ID, &r.NomeEmpresa, &r.Slug, &r.DominioPersonalizado, &r.LogoURL, &r.FaviconURL, &r.CorPrimaria, &r.CorSecundaria, &r.LimiteInstancias, &ativoInt,
			&r.SupabaseURI, &r.MinioEndpoint, &r.MinioAccessKey, &r.MinioSecretKey, &r.MinioBucket, &sslInt, &r.MinioPublicURL, &r.CriadoEm,
		)
		if errPadrao != nil {
			return nil, err
		}
	}
	r.Ativo = ativoInt == 1
	r.MinioUseSSL = sslInt == 1
	return &r, nil
}

func (g *GerenciadorDB) BuscarRevendedorPorID(id string) (*Revendedor, error) {
	var r Revendedor
	var ativoInt, sslInt int
	query := `SELECT id, nome_empresa, slug, COALESCE(dominio_personalizado, ''), COALESCE(logo_url, ''), COALESCE(favicon_url, ''), cor_primaria, cor_secundaria, limite_instancias, ativo,
			  COALESCE(supabase_uri, ''), COALESCE(minio_endpoint, ''), COALESCE(minio_access_key, ''), COALESCE(minio_secret_key, ''), COALESCE(minio_bucket, 'woohub-midias'), COALESCE(minio_use_ssl, 1), COALESCE(minio_public_url, ''), criado_em 
			  FROM revendedores WHERE id = ? LIMIT 1`
	err := g.queryRow(query, id).Scan(
		&r.ID, &r.NomeEmpresa, &r.Slug, &r.DominioPersonalizado, &r.LogoURL, &r.FaviconURL, &r.CorPrimaria, &r.CorSecundaria, &r.LimiteInstancias, &ativoInt,
		&r.SupabaseURI, &r.MinioEndpoint, &r.MinioAccessKey, &r.MinioSecretKey, &r.MinioBucket, &sslInt, &r.MinioPublicURL, &r.CriadoEm,
	)
	if err != nil {
		return nil, err
	}
	r.Ativo = ativoInt == 1
	r.MinioUseSSL = sslInt == 1
	return &r, nil
}

func (g *GerenciadorDB) AtualizarRevendedor(r *Revendedor) error {
	query := `UPDATE revendedores SET nome_empresa = ?, dominio_personalizado = ?, logo_url = ?, favicon_url = ?, cor_primaria = ?, cor_secundaria = ? WHERE id = ?`
	_, err := g.exec(query, r.NomeEmpresa, r.DominioPersonalizado, r.LogoURL, r.FaviconURL, r.CorPrimaria, r.CorSecundaria, r.ID)
	return err
}

func (g *GerenciadorDB) AtualizarRevendedorAdministrativo(id, nome, dominio, cor string, limite int) error {
	if limite <= 0 { limite = 50 }
	_, err := g.exec(`UPDATE revendedores SET nome_empresa = ?, dominio_personalizado = ?, cor_primaria = ?, limite_instancias = ? WHERE id = ?`, strings.TrimSpace(nome), strings.TrimSpace(dominio), strings.TrimSpace(cor), limite, id)
	return err
}

func (g *GerenciadorDB) AtualizarInfraestruturaRevendedor(id, supabaseURI, minioEndpoint, minioAccessKey, minioSecretKey, minioBucket, minioPublicURL string, minioUseSSL bool) error {
	sslInt := 0
	if minioUseSSL {
		sslInt = 1
	}
	if strings.TrimSpace(minioBucket) == "" {
		minioBucket = "woohub-midias"
	}
	query := `UPDATE revendedores SET supabase_uri = ?, minio_endpoint = ?, minio_access_key = ?, minio_secret_key = ?, minio_bucket = ?, minio_use_ssl = ?, minio_public_url = ? WHERE id = ?`
	_, err := g.exec(query, strings.TrimSpace(supabaseURI), strings.TrimSpace(minioEndpoint), strings.TrimSpace(minioAccessKey), strings.TrimSpace(minioSecretKey), strings.TrimSpace(minioBucket), sslInt, strings.TrimSpace(minioPublicURL), id)
	return err
}

func (g *GerenciadorDB) ListarRevendedores() ([]map[string]interface{}, error) {
	query := `SELECT r.id, r.nome_empresa, r.slug, COALESCE(r.dominio_personalizado, ''), 
	          r.cor_primaria, r.cor_secundaria, r.limite_instancias, r.ativo,
	          COALESCE(r.supabase_uri, ''), COALESCE(r.minio_endpoint, ''), COALESCE(r.minio_bucket, 'woohub-midias'),
	          r.criado_em,
	          (SELECT COUNT(*) FROM clientes c WHERE c.revendedor_id = r.id AND COALESCE(c.papel, 'cliente') = 'cliente') as total_clientes,
	          (SELECT COUNT(*) FROM instancias i JOIN clientes c ON i.cliente_id = c.id WHERE c.revendedor_id = r.id) as total_instancias
	          FROM revendedores r ORDER BY r.criado_em ASC`
	rows, err := g.query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lista []map[string]interface{}
	for rows.Next() {
		var id, nome, slug, dominio, corP, corS, supaURI, minioEnd, minioBkt string
		var limite, ativo, totalCli, totalInst int
		var criadoEm time.Time
		if err := rows.Scan(&id, &nome, &slug, &dominio, &corP, &corS, &limite, &ativo, &supaURI, &minioEnd, &minioBkt, &criadoEm, &totalCli, &totalInst); err == nil {
			lista = append(lista, map[string]interface{}{
				"id":                    id,
				"nome_empresa":          nome,
				"slug":                  slug,
				"dominio_personalizado": dominio,
				"cor_primaria":          corP,
				"cor_secundaria":        corS,
				"limite_instancias":     limite,
				"ativo":                 ativo == 1,
				"supabase_isolado":      supaURI != "",
				"minio_isolado":         minioEnd != "",
				"minio_endpoint":        minioEnd,
				"minio_bucket":          minioBkt,
				"criado_em":             criadoEm,
				"total_clientes":        totalCli,
				"total_instancias":      totalInst,
			})
		}
	}
	return lista, nil
}

func (g *GerenciadorDB) CriarRevendedor(nome, slug, dominio, corPrimaria string, limite int) (*Revendedor, error) {
	if limite <= 0 {
		limite = 50
	}
	if slug == "" {
		slug = strings.ToLower(strings.ReplaceAll(nome, " ", "-"))
	}
	if corPrimaria == "" {
		corPrimaria = "#10B981"
	}
	id := "rev_" + uuid.New().String()[:12]
	_, err := g.exec(`
		INSERT INTO revendedores (id, nome_empresa, slug, dominio_personalizado, cor_primaria, cor_secundaria, limite_instancias, ativo)
		VALUES (?, ?, ?, ?, ?, '#041C14', ?, 1)
	`, id, nome, slug, dominio, corPrimaria, limite)
	if err != nil {
		return nil, err
	}
	return g.BuscarRevendedorPorID(id)
}

// CriarUsuarioRevendedor cria a conta de acesso (papel = "revendedor") vinculada a um Revendedor
func (g *GerenciadorDB) CriarUsuarioRevendedor(revendedorId, nome, email, senha string, limite int) (map[string]interface{}, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, fmt.Errorf("e-mail de acesso do revendedor é obrigatório")
	}
	if limite <= 0 {
		limite = 150
	}
	senhaGerada := false
	if strings.TrimSpace(senha) == "" {
		senha = gerarChaveAleatoria("Rev@", 6)
		senhaGerada = true
	}
	hash, err := HashSenha(senha)
	if err != nil {
		return nil, err
	}

	id := "cli_rev_" + uuid.New().String()[:10]
	_, err = g.exec(`
		INSERT INTO clientes (id, revendedor_id, nome, email, senha_hash, documento, papel, limite_instancias, ativo)
		VALUES (?, ?, ?, ?, ?, '', ?, ?, 1)
	`, id, revendedorId, nome, email, hash, PapelRevendedor, limite)
	if err != nil {
		return nil, err
	}

	resp := map[string]interface{}{
		"id":                id,
		"revendedor_id":     revendedorId,
		"nome":              nome,
		"email":             email,
		"papel":             PapelRevendedor,
		"limite_instancias": limite,
		"ativo":             true,
	}
	if senhaGerada {
		resp["senha_temporaria"] = senha
	}
	return resp, nil
}

// Métodos de Clientes
func (g *GerenciadorDB) ListarClientes(revendedorId ...string) ([]map[string]interface{}, error) {
	var query string
	var rows *sql.Rows
	var err error

	if len(revendedorId) > 0 && revendedorId[0] != "" {
		query = `SELECT c.id, c.revendedor_id, c.nome, c.email, COALESCE(c.documento, ''), COALESCE(c.papel, 'cliente'), c.limite_instancias, c.ativo, c.criado_em,
		         (SELECT COUNT(*) FROM instancias i WHERE i.cliente_id = c.id) as total_instancias,
		         (SELECT COUNT(*) FROM instancias i WHERE i.cliente_id = c.id AND i.status = 'CONECTADO') as instancias_conectadas
		         FROM clientes c WHERE c.revendedor_id = ? AND COALESCE(c.papel, 'cliente') = 'cliente' ORDER BY c.criado_em DESC`
		rows, err = g.query(query, revendedorId[0])
	} else {
		query = `SELECT c.id, c.revendedor_id, c.nome, c.email, COALESCE(c.documento, ''), COALESCE(c.papel, 'cliente'), c.limite_instancias, c.ativo, c.criado_em,
		         (SELECT COUNT(*) FROM instancias i WHERE i.cliente_id = c.id) as total_instancias,
		         (SELECT COUNT(*) FROM instancias i WHERE i.cliente_id = c.id AND i.status = 'CONECTADO') as instancias_conectadas
		         FROM clientes c WHERE COALESCE(c.papel, 'cliente') = 'cliente' ORDER BY c.criado_em DESC`
		rows, err = g.query(query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lista []map[string]interface{}
	for rows.Next() {
		var id, revId, nome, email, doc, papel string
		var limite, ativo, totalInst, instConectadas int
		var criadoEm time.Time
		if err := rows.Scan(&id, &revId, &nome, &email, &doc, &papel, &limite, &ativo, &criadoEm, &totalInst, &instConectadas); err == nil {
			plano := "Básico"
			mensalidade := "R$ 197,00"
			if limite >= 20 {
				plano = "Profissional"
				mensalidade = "R$ 297,00"
			}
			if limite >= 30 {
				plano = "Enterprise"
				mensalidade = "R$ 497,00"
			}

			lista = append(lista, map[string]interface{}{
				"id":                    id,
				"revendedor_id":         revId,
				"nome":                  nome,
				"email":                 email,
				"documento":             doc,
				"papel":                 papel,
				"limite_instancias":     limite,
				"ativo":                 ativo == 1,
				"criado_em":             criadoEm,
				"total_instancias":      totalInst,
				"instancias_conectadas": instConectadas,
				"plano":                 plano,
				"mensalidade":           mensalidade,
			})
		}
	}
	return lista, nil
}

func (g *GerenciadorDB) CriarCliente(revendedorId, nome, email, senha, documento string, limite int) (map[string]interface{}, error) {
	if revendedorId == "" {
		var firstRev string
		_ = g.queryRow("SELECT id FROM revendedores ORDER BY criado_em ASC LIMIT 1").Scan(&firstRev)
		if firstRev != "" {
			revendedorId = firstRev
		} else {
			revendedorId = "rev_woohub_oficial"
		}
	}
	if limite <= 0 {
		limite = 5
	}
	senhaGerada := false
	if strings.TrimSpace(senha) == "" {
		senha = gerarChaveAleatoria("Woo@", 6)
		senhaGerada = true
	}
	hash, err := HashSenha(senha)
	if err != nil {
		return nil, err
	}

	id := "cli_" + uuid.New().String()[:12]
	_, err = g.exec(`
		INSERT INTO clientes (id, revendedor_id, nome, email, senha_hash, documento, papel, limite_instancias, ativo)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)
	`, id, revendedorId, nome, strings.ToLower(strings.TrimSpace(email)), hash, documento, PapelCliente, limite)
	if err != nil {
		return nil, err
	}

	// Cria uma instância inicial padrão para o novo cliente com chave criptograficamente segura
	instId := "inst_" + uuid.New().String()[:12]
	chave := gerarChaveAleatoria("woo_key_", 18)
	_, _ = g.exec(`
		INSERT INTO instancias (id, cliente_id, nome, chave_api, status)
		VALUES (?, ?, 'Principal', ?, 'DESCONECTADO')
	`, instId, id, chave)

	resp := map[string]interface{}{
		"id":                id,
		"revendedor_id":     revendedorId,
		"nome":              nome,
		"email":             email,
		"documento":         documento,
		"papel":             PapelCliente,
		"limite_instancias": limite,
		"ativo":             true,
	}
	if senhaGerada {
		resp["senha_temporaria"] = senha
	}
	return resp, nil
}

func (g *GerenciadorDB) ApagarCliente(id string) error {
	_, err := g.exec(`DELETE FROM clientes WHERE id = ?`, id)
	return err
}

// Métodos de Instâncias
func (g *GerenciadorDB) CriarInstancia(clienteId, nome string) (*Instancia, error) {
	id := "inst_" + uuid.New().String()[:12]
	chave := gerarChaveAleatoria("woo_key_", 18)

	query := `INSERT INTO instancias (id, cliente_id, nome, chave_api, status) VALUES (?, ?, ?, ?, 'DESCONECTADO')`
	_, err := g.exec(query, id, clienteId, nome, chave)
	if err != nil {
		return nil, err
	}

	return g.BuscarInstanciaPorId(id)
}

func (g *GerenciadorDB) BuscarInstanciaPorId(id string) (*Instancia, error) {
	var i Instancia
	var ignGrupos, rejChamadas, leitAuto, sempreOn int
	query := `SELECT id, cliente_id, nome, chave_api, COALESCE(jid, ''), COALESCE(numero_telefone, ''), status, 
			  COALESCE(webhook_url, ''), COALESCE(webhook_eventos, ''), COALESCE(webhook_segredo, ''), 
			  ignorar_grupos, rejeitar_chamadas, COALESCE(mensagem_chamada_rejeitada, ''), COALESCE(proxy_url, ''),
			  COALESCE(leitura_automatica, 0), COALESCE(sempre_online, 0), criado_em, atualizado_em 
			  FROM instancias WHERE id = ?`
	err := g.queryRow(query, id).Scan(
		&i.ID, &i.ClienteID, &i.Nome, &i.ChaveAPI, &i.JID, &i.NumeroTelefone, &i.Status,
		&i.WebhookURL, &i.WebhookEventos, &i.WebhookSegredo,
		&ignGrupos, &rejChamadas, &i.MensagemChamadaRejeitar, &i.ProxyURL,
		&leitAuto, &sempreOn, &i.CriadoEm, &i.AtualizadoEm,
	)
	if err != nil {
		return nil, err
	}
	i.IgnorarGrupos = ignGrupos == 1
	i.RejeitarChamadas = rejChamadas == 1
	i.LeituraAutomatica = leitAuto == 1
	i.SempreOnline = sempreOn == 1
	return &i, nil
}

func (g *GerenciadorDB) BuscarInstanciaPorChave(chave string) (*Instancia, error) {
	var i Instancia
	var ignGrupos, rejChamadas, leitAuto, sempreOn int
	query := `SELECT id, cliente_id, nome, chave_api, COALESCE(jid, ''), COALESCE(numero_telefone, ''), status, 
			  COALESCE(webhook_url, ''), COALESCE(webhook_eventos, ''), COALESCE(webhook_segredo, ''), 
			  ignorar_grupos, rejeitar_chamadas, COALESCE(mensagem_chamada_rejeitada, ''), COALESCE(proxy_url, ''),
			  COALESCE(leitura_automatica, 0), COALESCE(sempre_online, 0), criado_em, atualizado_em 
			  FROM instancias WHERE chave_api = ?`
	err := g.queryRow(query, chave).Scan(
		&i.ID, &i.ClienteID, &i.Nome, &i.ChaveAPI, &i.JID, &i.NumeroTelefone, &i.Status,
		&i.WebhookURL, &i.WebhookEventos, &i.WebhookSegredo,
		&ignGrupos, &rejChamadas, &i.MensagemChamadaRejeitar, &i.ProxyURL,
		&leitAuto, &sempreOn, &i.CriadoEm, &i.AtualizadoEm,
	)
	if err != nil {
		return nil, err
	}
	i.IgnorarGrupos = ignGrupos == 1
	i.RejeitarChamadas = rejChamadas == 1
	i.LeituraAutomatica = leitAuto == 1
	i.SempreOnline = sempreOn == 1
	return &i, nil
}

func (g *GerenciadorDB) ListarInstancias(clienteId ...string) ([]Instancia, error) {
	var query string
	var rows *sql.Rows
	var err error

	if len(clienteId) > 0 && clienteId[0] != "" {
		query = `SELECT id, cliente_id, nome, chave_api, COALESCE(jid, ''), COALESCE(numero_telefone, ''), status, 
				  COALESCE(webhook_url, ''), COALESCE(webhook_eventos, ''), COALESCE(webhook_segredo, ''), 
				  ignorar_grupos, rejeitar_chamadas, COALESCE(mensagem_chamada_rejeitada, ''), COALESCE(proxy_url, ''),
				  COALESCE(leitura_automatica, 0), COALESCE(sempre_online, 0), criado_em, atualizado_em 
				  FROM instancias WHERE cliente_id = ? ORDER BY criado_em DESC`
		rows, err = g.query(query, clienteId[0])
	} else {
		query = `SELECT id, cliente_id, nome, chave_api, COALESCE(jid, ''), COALESCE(numero_telefone, ''), status, 
				  COALESCE(webhook_url, ''), COALESCE(webhook_eventos, ''), COALESCE(webhook_segredo, ''), 
				  ignorar_grupos, rejeitar_chamadas, COALESCE(mensagem_chamada_rejeitada, ''), COALESCE(proxy_url, ''),
				  COALESCE(leitura_automatica, 0), COALESCE(sempre_online, 0), criado_em, atualizado_em 
				  FROM instancias ORDER BY criado_em DESC`
		rows, err = g.query(query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lista []Instancia
	for rows.Next() {
		var i Instancia
		var ignGrupos, rejChamadas, leitAuto, sempreOn int
		err := rows.Scan(
			&i.ID, &i.ClienteID, &i.Nome, &i.ChaveAPI, &i.JID, &i.NumeroTelefone, &i.Status,
			&i.WebhookURL, &i.WebhookEventos, &i.WebhookSegredo,
			&ignGrupos, &rejChamadas, &i.MensagemChamadaRejeitar, &i.ProxyURL,
			&leitAuto, &sempreOn, &i.CriadoEm, &i.AtualizadoEm,
		)
		if err != nil {
			continue
		}
		i.IgnorarGrupos = ignGrupos == 1
		i.RejeitarChamadas = rejChamadas == 1
		i.LeituraAutomatica = leitAuto == 1
		i.SempreOnline = sempreOn == 1
		lista = append(lista, i)
	}
	return lista, nil
}

func (g *GerenciadorDB) ListarInstanciasPorRevendedor(revendedorId string) ([]Instancia, error) {
	query := `SELECT i.id, i.cliente_id, i.nome, i.chave_api, COALESCE(i.jid, ''), COALESCE(i.numero_telefone, ''), i.status, 
			  COALESCE(i.webhook_url, ''), COALESCE(i.webhook_eventos, ''), COALESCE(i.webhook_segredo, ''), 
			  i.ignorar_grupos, i.rejeitar_chamadas, COALESCE(i.mensagem_chamada_rejeitada, ''), COALESCE(i.proxy_url, ''),
			  COALESCE(i.leitura_automatica, 0), COALESCE(i.sempre_online, 0), i.criado_em, i.atualizado_em 
			  FROM instancias i
			  JOIN clientes c ON i.cliente_id = c.id
			  WHERE c.revendedor_id = ?
			  ORDER BY i.criado_em DESC`
	rows, err := g.query(query, revendedorId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lista []Instancia
	for rows.Next() {
		var i Instancia
		var ignGrupos, rejChamadas, leitAuto, sempreOn int
		err := rows.Scan(
			&i.ID, &i.ClienteID, &i.Nome, &i.ChaveAPI, &i.JID, &i.NumeroTelefone, &i.Status,
			&i.WebhookURL, &i.WebhookEventos, &i.WebhookSegredo,
			&ignGrupos, &rejChamadas, &i.MensagemChamadaRejeitar, &i.ProxyURL,
			&leitAuto, &sempreOn, &i.CriadoEm, &i.AtualizadoEm,
		)
		if err != nil {
			continue
		}
		i.IgnorarGrupos = ignGrupos == 1
		i.RejeitarChamadas = rejChamadas == 1
		i.LeituraAutomatica = leitAuto == 1
		i.SempreOnline = sempreOn == 1
		lista = append(lista, i)
	}
	return lista, nil
}

func (g *GerenciadorDB) AtualizarConfiguracoesInstancia(id string, ignorarGrupos, rejeitarChamadas, leituraAutomatica, sempreOnline bool, msgChamadaRejeitada, proxyURL string) error {
	ignInt := 0
	if ignorarGrupos {
		ignInt = 1
	}
	rejInt := 0
	if rejeitarChamadas {
		rejInt = 1
	}
	leitInt := 0
	if leituraAutomatica {
		leitInt = 1
	}
	onInt := 0
	if sempreOnline {
		onInt = 1
	}
	query := `UPDATE instancias SET ignorar_grupos = ?, rejeitar_chamadas = ?, mensagem_chamada_rejeitada = ?, proxy_url = ?, leitura_automatica = ?, sempre_online = ?, atualizado_em = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := g.exec(query, ignInt, rejInt, msgChamadaRejeitada, proxyURL, leitInt, onInt, id)
	return err
}

func (g *GerenciadorDB) AtualizarStatusInstancia(id, status, jid, numero string) error {
	query := `UPDATE instancias SET status = ?, jid = ?, numero_telefone = ?, atualizado_em = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := g.exec(query, status, jid, numero, id)
	return err
}

func (g *GerenciadorDB) AtualizarWebhookInstancia(id, url, eventos, segredo string) error {
	query := `UPDATE instancias SET webhook_url = ?, webhook_eventos = ?, webhook_segredo = ?, atualizado_em = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := g.exec(query, url, eventos, segredo, id)
	return err
}

func (g *GerenciadorDB) ApagarInstancia(id string) error {
	query := `DELETE FROM instancias WHERE id = ?`
	_, err := g.exec(query, id)
	return err
}

// Logs de Mensagens
func (g *GerenciadorDB) GravarLogMensagem(instanciaId, msgId, tipo, dest, direcao, status, detalheErro string) error {
	id := "log_" + uuid.New().String()[:12]
	query := `INSERT INTO logs_mensagens (id, instancia_id, mensagem_id, tipo, destinatario, direcao, status, detalhes_erro)
			  VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := g.exec(query, id, instanciaId, msgId, tipo, dest, direcao, status, detalheErro)
	return err
}

func (g *GerenciadorDB) ListarLogsRecentes(instanciaId string, limite int) ([]LogMensagem, error) {
	if limite <= 0 || limite > 500 {
		limite = 50
	}
	var rows *sql.Rows
	var err error
	if instanciaId != "" {
		rows, err = g.query(`SELECT id, instancia_id, COALESCE(mensagem_id, ''), tipo, destinatario, direcao, status, COALESCE(detalhes_erro, ''), criado_em 
								FROM logs_mensagens WHERE instancia_id = ? ORDER BY criado_em DESC LIMIT ?`, instanciaId, limite)
	} else {
		rows, err = g.query(`SELECT id, instancia_id, COALESCE(mensagem_id, ''), tipo, destinatario, direcao, status, COALESCE(detalhes_erro, ''), criado_em 
								FROM logs_mensagens ORDER BY criado_em DESC LIMIT ?`, limite)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []LogMensagem
	for rows.Next() {
		var l LogMensagem
		if err := rows.Scan(&l.ID, &l.InstanciaID, &l.MensagemID, &l.Tipo, &l.Destinatario, &l.Direcao, &l.Status, &l.DetalhesErro, &l.CriadoEm); err == nil {
			logs = append(logs, l)
		}
	}
	return logs, nil
}

func (g *GerenciadorDB) ListarLogsPorCliente(clienteId string, limite int) ([]LogMensagem, error) {
	if limite <= 0 || limite > 500 {
		limite = 50
	}
	rows, err := g.query(`SELECT l.id, l.instancia_id, COALESCE(l.mensagem_id, ''), l.tipo, l.destinatario, l.direcao, l.status, COALESCE(l.detalhes_erro, ''), l.criado_em 
						  FROM logs_mensagens l
						  JOIN instancias i ON l.instancia_id = i.id
						  WHERE i.cliente_id = ?
						  ORDER BY l.criado_em DESC LIMIT ?`, clienteId, limite)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []LogMensagem
	for rows.Next() {
		var l LogMensagem
		if err := rows.Scan(&l.ID, &l.InstanciaID, &l.MensagemID, &l.Tipo, &l.Destinatario, &l.Direcao, &l.Status, &l.DetalhesErro, &l.CriadoEm); err == nil {
			logs = append(logs, l)
		}
	}
	return logs, nil
}

func (g *GerenciadorDB) ListarLogsPorRevendedor(revendedorId string, limite int) ([]LogMensagem, error) {
	if limite <= 0 || limite > 500 {
		limite = 50
	}
	rows, err := g.query(`SELECT l.id, l.instancia_id, COALESCE(l.mensagem_id, ''), l.tipo, l.destinatario, l.direcao, l.status, COALESCE(l.detalhes_erro, ''), l.criado_em 
						  FROM logs_mensagens l
						  JOIN instancias i ON l.instancia_id = i.id
						  JOIN clientes c ON i.cliente_id = c.id
						  WHERE c.revendedor_id = ?
						  ORDER BY l.criado_em DESC LIMIT ?`, revendedorId, limite)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []LogMensagem
	for rows.Next() {
		var l LogMensagem
		if err := rows.Scan(&l.ID, &l.InstanciaID, &l.MensagemID, &l.Tipo, &l.Destinatario, &l.Direcao, &l.Status, &l.DetalhesErro, &l.CriadoEm); err == nil {
			logs = append(logs, l)
		}
	}
	return logs, nil
}

// ==========================================
// MÉTODOS DE AUTENTICAÇÃO E SESSÃO (LOGIN)
// ==========================================

func (g *GerenciadorDB) AutenticarUsuario(email, senha string) (*Cliente, string, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	senha = strings.TrimSpace(senha)

	if email == "" || senha == "" {
		return nil, "", fmt.Errorf("e-mail e senha são obrigatórios")
	}

	var c Cliente
	var ativoInt int
	query := `SELECT id, revendedor_id, nome, email, senha_hash, COALESCE(documento, ''), COALESCE(papel, 'cliente'), limite_instancias, ativo, criado_em 
			  FROM clientes WHERE LOWER(email) = ? LIMIT 1`
	err := g.queryRow(query, email).Scan(
		&c.ID, &c.RevendedorID, &c.Nome, &c.Email, &c.SenhaHash, &c.Documento, &c.Papel, &c.LimiteInstancias, &ativoInt, &c.CriadoEm,
	)
	if err != nil {
		return nil, "", fmt.Errorf("credenciais inválidas ou usuário inexistente")
	}

	c.Ativo = ativoInt == 1
	if !c.Ativo {
		return nil, "", fmt.Errorf("esta conta de usuário está desativada")
	}

	if !VerificarSenha(senha, c.SenhaHash) {
		return nil, "", fmt.Errorf("credenciais inválidas")
	}

	token := gerarChaveAleatoria("woo_sess_", 32)
	expiraEm := time.Now().Add(7 * 24 * time.Hour)

	_, err = g.exec(`INSERT INTO sessoes_auth (token, cliente_id, expira_em) VALUES (?, ?, ?)`, token, c.ID, expiraEm)
	if err != nil {
		return nil, "", fmt.Errorf("falha ao salvar sessão de login: %w", err)
	}

	return &c, token, nil
}

func (g *GerenciadorDB) ValidarSessao(token string) (*Cliente, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("token de autenticação não fornecido")
	}

	var c Cliente
	var ativoInt int
	var expiraEm time.Time

	query := `SELECT c.id, c.revendedor_id, c.nome, c.email, c.senha_hash, COALESCE(c.documento, ''), COALESCE(c.papel, 'cliente'), c.limite_instancias, c.ativo, c.criado_em, s.expira_em 
			  FROM sessoes_auth s
			  JOIN clientes c ON s.cliente_id = c.id
			  WHERE s.token = ? LIMIT 1`

	err := g.queryRow(query, token).Scan(
		&c.ID, &c.RevendedorID, &c.Nome, &c.Email, &c.SenhaHash, &c.Documento, &c.Papel, &c.LimiteInstancias, &ativoInt, &c.CriadoEm, &expiraEm,
	)
	if err != nil {
		return nil, fmt.Errorf("sessão inválida ou não encontrada")
	}

	if time.Now().After(expiraEm) {
		_ = g.EncerrarSessao(token)
		return nil, fmt.Errorf("sessão expirada. Faça login novamente")
	}

	c.Ativo = ativoInt == 1
	if !c.Ativo {
		return nil, fmt.Errorf("usuário desativado")
	}

	return &c, nil
}

func (g *GerenciadorDB) EncerrarSessao(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	_, err := g.exec(`DELETE FROM sessoes_auth WHERE token = ?`, token)
	return err
}

func (g *GerenciadorDB) BuscarClientePorID(id string) (*Cliente, error) {
	var c Cliente
	var ativoInt int
	query := `SELECT id, revendedor_id, nome, email, senha_hash, COALESCE(documento, ''), COALESCE(papel, 'cliente'), limite_instancias, ativo, criado_em 
			  FROM clientes WHERE id = ? LIMIT 1`
	err := g.queryRow(query, id).Scan(
		&c.ID, &c.RevendedorID, &c.Nome, &c.Email, &c.SenhaHash, &c.Documento, &c.Papel, &c.LimiteInstancias, &ativoInt, &c.CriadoEm,
	)
	if err != nil {
		return nil, err
	}
	c.Ativo = ativoInt == 1
	return &c, nil
}

func (g *GerenciadorDB) AtualizarSenhaCliente(clienteId, novaSenha string) error {
	if len(strings.TrimSpace(novaSenha)) < 6 {
		return fmt.Errorf("a nova senha deve ter no mínimo 6 caracteres")
	}
	hash, err := HashSenha(novaSenha)
	if err != nil {
		return fmt.Errorf("erro ao criptografar senha: %w", err)
	}
	_, err = g.exec(`UPDATE clientes SET senha_hash = ? WHERE id = ?`, hash, clienteId)
	return err
}

// ObterRevendedorDaInstancia retorna o Revendedor dono da instância (para resolver MinIO/Supabase isolado da revenda)
func (g *GerenciadorDB) ObterRevendedorDaInstancia(instanciaID string) (*Revendedor, error) {
	var revID string
	err := g.queryRow(`
		SELECT c.revendedor_id
		FROM instancias i
		JOIN clientes c ON i.cliente_id = c.id
		WHERE i.id = ? LIMIT 1
	`, instanciaID).Scan(&revID)
	if err != nil || revID == "" {
		return nil, err
	}
	return g.BuscarRevendedorPorID(revID)
}

// ============================================================================
// MÉTODOS DE LICENCIAMENTO E CONTROLE DE CONTAINERS PORTAINER (KILL-SWITCH)
// ============================================================================

func (g *GerenciadorDB) CriarLicencaInstalacao(revendedorID, clienteNome, emailContato, dominioAutorizado, plano string, limiteInstancias, diasValidade int, tokenEd25519 string) (*LicencaInstalacao, error) {
	if limiteInstancias <= 0 {
		limiteInstancias = 50
	}
	if diasValidade <= 0 {
		diasValidade = 30
	}
	if plano == "" {
		plano = "Portainer Enterprise Stack"
	}
	id := "lic_" + uuid.New().String()[:12]
	chave := gerarChaveAleatoria("woo_lic_", 16)
	expiraEm := time.Now().AddDate(0, 0, diasValidade)

	_, err := g.exec(`
		INSERT INTO licencas_instalacoes (
			id, chave_licenca, revendedor_id, cliente_nome, email_contato,
			dominio_autorizado, ip_autorizado, plano, limite_instancias,
			status, expira_em, token_assinado_ed25519
		) VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, 'ATIVA', ?, ?)
	`, id, chave, revendedorID, clienteNome, emailContato, strings.TrimSpace(dominioAutorizado), plano, limiteInstancias, expiraEm, tokenEd25519)
	if err != nil {
		return nil, err
	}

	return g.BuscarLicencaPorChave(chave)
}

func (g *GerenciadorDB) BuscarLicencaPorChave(chave string) (*LicencaInstalacao, error) {
	var l LicencaInstalacao
	query := `SELECT id, chave_licenca, COALESCE(revendedor_id, ''), cliente_nome, COALESCE(email_contato, ''),
			  COALESCE(dominio_autorizado, ''), COALESCE(ip_autorizado, ''), COALESCE(fingerprint_container, ''),
			  COALESCE(plano, 'Stack'), limite_instancias, status, expira_em, ultimo_heartbeat,
			  COALESCE(ultimo_ip, ''), COALESCE(versao_instalada, ''), COALESCE(instancias_ativas, 0),
			  COALESCE(token_assinado_ed25519, ''), criado_em
			  FROM licencas_instalacoes WHERE chave_licenca = ? OR id = ? LIMIT 1`
	err := g.queryRow(query, strings.TrimSpace(chave), strings.TrimSpace(chave)).Scan(
		&l.ID, &l.ChaveLicenca, &l.RevendedorID, &l.ClienteNome, &l.EmailContato,
		&l.DominioAutorizado, &l.IPAutorizado, &l.FingerprintContainer,
		&l.Plano, &l.LimiteInstancias, &l.Status, &l.ExpiraEm, &l.UltimoHeartbeat,
		&l.UltimoIP, &l.VersaoInstalada, &l.InstanciasAtivas,
		&l.TokenAssinadoEd25519, &l.CriadoEm,
	)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (g *GerenciadorDB) ListarLicencasInstalacoes() ([]LicencaInstalacao, error) {
	query := `SELECT id, chave_licenca, COALESCE(revendedor_id, ''), cliente_nome, COALESCE(email_contato, ''),
			  COALESCE(dominio_autorizado, ''), COALESCE(ip_autorizado, ''), COALESCE(fingerprint_container, ''),
			  COALESCE(plano, 'Stack'), limite_instancias, status, expira_em, ultimo_heartbeat,
			  COALESCE(ultimo_ip, ''), COALESCE(versao_instalada, ''), COALESCE(instancias_ativas, 0),
			  COALESCE(token_assinado_ed25519, ''), criado_em
			  FROM licencas_instalacoes ORDER BY criado_em DESC`
	rows, err := g.query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lista []LicencaInstalacao
	for rows.Next() {
		var l LicencaInstalacao
		if err := rows.Scan(
			&l.ID, &l.ChaveLicenca, &l.RevendedorID, &l.ClienteNome, &l.EmailContato,
			&l.DominioAutorizado, &l.IPAutorizado, &l.FingerprintContainer,
			&l.Plano, &l.LimiteInstancias, &l.Status, &l.ExpiraEm, &l.UltimoHeartbeat,
			&l.UltimoIP, &l.VersaoInstalada, &l.InstanciasAtivas,
			&l.TokenAssinadoEd25519, &l.CriadoEm,
		); err == nil {
			if l.Status == "ATIVA" && time.Now().After(l.ExpiraEm) {
				l.Status = "EXPIRADA"
			}
			lista = append(lista, l)
		}
	}
	return lista, nil
}

func (g *GerenciadorDB) AtualizarStatusLicenca(idOuChave, novoStatus string, diasRenovacao int) error {
	novoStatus = strings.ToUpper(strings.TrimSpace(novoStatus))
	if diasRenovacao > 0 {
		novaExp := time.Now().AddDate(0, 0, diasRenovacao)
		_, err := g.exec(`UPDATE licencas_instalacoes SET status = ?, expira_em = ? WHERE id = ? OR chave_licenca = ?`, novoStatus, novaExp, idOuChave, idOuChave)
		return err
	}
	_, err := g.exec(`UPDATE licencas_instalacoes SET status = ? WHERE id = ? OR chave_licenca = ?`, novoStatus, idOuChave, idOuChave)
	return err
}

func (g *GerenciadorDB) DesvincularIPLicenca(idOuChave string) error {
	_, err := g.exec(`UPDATE licencas_instalacoes SET ip_autorizado = '', fingerprint_container = '' WHERE id = ? OR chave_licenca = ?`, idOuChave, idOuChave)
	return err
}

func (g *GerenciadorDB) RegistrarHeartbeatLicenca(chave, ipRemoto, dominio, fingerprint, versao string, instanciasAtivas int) error {
	_, err := g.exec(`
		UPDATE licencas_instalacoes
		SET ultimo_heartbeat = CURRENT_TIMESTAMP,
		    ultimo_ip = ?,
		    ip_autorizado = CASE WHEN ip_autorizado = '' THEN ? ELSE ip_autorizado END,
		    dominio_autorizado = CASE WHEN dominio_autorizado = '' THEN ? ELSE dominio_autorizado END,
		    fingerprint_container = CASE WHEN fingerprint_container = '' THEN ? ELSE fingerprint_container END,
		    versao_instalada = ?,
		    instancias_ativas = ?
		WHERE chave_licenca = ?
	`, ipRemoto, ipRemoto, dominio, fingerprint, versao, instanciasAtivas, chave)
	return err
}

// --- Métodos de Integrações Nativas ---

func (g *GerenciadorDB) Fechar() error {
	if g != nil && g.db != nil {
		return g.db.Close()
	}
	return nil
}

func (g *GerenciadorDB) ObterIntegracoesInstancia(instanciaID string) ([]IntegracaoInstancia, error) {
	rows, err := g.query(`
		SELECT id, instancia_id, tipo, ativo, configuracoes, criado_em, atualizado_em
		FROM integracoes_instancia
		WHERE instancia_id = ?
		ORDER BY tipo ASC
	`, instanciaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lista []IntegracaoInstancia
	for rows.Next() {
		var it IntegracaoInstancia
		var ativoInt int
		if err := rows.Scan(&it.ID, &it.InstanciaID, &it.Tipo, &ativoInt, &it.Configuracoes, &it.CriadoEm, &it.AtualizadoEm); err == nil {
			it.Ativo = ativoInt == 1
			lista = append(lista, it)
		}
	}
	return lista, nil
}

func (g *GerenciadorDB) ObterIntegracaoPorTipo(instanciaID, tipo string) (*IntegracaoInstancia, error) {
	row := g.queryRow(`
		SELECT id, instancia_id, tipo, ativo, configuracoes, criado_em, atualizado_em
		FROM integracoes_instancia
		WHERE instancia_id = ? AND tipo = ?
	`, instanciaID, strings.ToLower(strings.TrimSpace(tipo)))

	var it IntegracaoInstancia
	var ativoInt int
	if err := row.Scan(&it.ID, &it.InstanciaID, &it.Tipo, &ativoInt, &it.Configuracoes, &it.CriadoEm, &it.AtualizadoEm); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	it.Ativo = ativoInt == 1
	return &it, nil
}

func (g *GerenciadorDB) SalvarIntegracaoInstancia(instanciaID, tipo string, ativo bool, configJSON string) (*IntegracaoInstancia, error) {
	tipo = strings.ToLower(strings.TrimSpace(tipo))
	ativoInt := 0
	if ativo {
		ativoInt = 1
	}

	existente, _ := g.ObterIntegracaoPorTipo(instanciaID, tipo)
	if existente != nil {
		_, err := g.exec(`
			UPDATE integracoes_instancia
			SET ativo = ?, configuracoes = ?, atualizado_em = CURRENT_TIMESTAMP
			WHERE instancia_id = ? AND tipo = ?
		`, ativoInt, configJSON, instanciaID, tipo)
		if err != nil {
			return nil, err
		}
		return g.ObterIntegracaoPorTipo(instanciaID, tipo)
	}

	novoID := "int_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]
	_, err := g.exec(`
		INSERT INTO integracoes_instancia (id, instancia_id, tipo, ativo, configuracoes, criado_em, atualizado_em)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, novoID, instanciaID, tipo, ativoInt, configJSON)
	if err != nil {
		return nil, err
	}
	return g.ObterIntegracaoPorTipo(instanciaID, tipo)
}

// --- Métodos de Sessões Typebot ---

func (g *GerenciadorDB) ObterSessaoTypebot(instanciaID, jid string) (*SessaoTypebot, error) {
	row := g.queryRow(`
		SELECT id, instancia_id, remetente_jid, session_id_typebot, estado, atualizado_em
		FROM sessoes_typebot
		WHERE instancia_id = ? AND remetente_jid = ?
	`, instanciaID, jid)

	var s SessaoTypebot
	if err := row.Scan(&s.ID, &s.InstanciaID, &s.RemetenteJID, &s.SessionIDTypebot, &s.Estado, &s.AtualizadoEm); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

func (g *GerenciadorDB) SalvarSessaoTypebot(instanciaID, jid, sessionID, estado string) error {
	existente, _ := g.ObterSessaoTypebot(instanciaID, jid)
	if existente != nil {
		_, err := g.exec(`
			UPDATE sessoes_typebot
			SET session_id_typebot = ?, estado = ?, atualizado_em = CURRENT_TIMESTAMP
			WHERE instancia_id = ? AND remetente_jid = ?
		`, sessionID, estado, instanciaID, jid)
		return err
	}

	novoID := "typ_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]
	_, err := g.exec(`
		INSERT INTO sessoes_typebot (id, instancia_id, remetente_jid, session_id_typebot, estado, atualizado_em)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, novoID, instanciaID, jid, sessionID, estado)
	return err
}

func (g *GerenciadorDB) ExcluirSessaoTypebot(instanciaID, jid string) error {
	_, err := g.exec(`DELETE FROM sessoes_typebot WHERE instancia_id = ? AND remetente_jid = ?`, instanciaID, jid)
	return err
}

// --- Métodos de Atribuição de Origem & Campanhas ---

func (g *GerenciadorDB) SalvarOuAtualizarAtribuicaoContato(atrib AtribuicaoContato) error {
	existente, _ := g.ObterAtribuicaoContato(atrib.InstanciaID, atrib.JID)
	if existente != nil {
		canal := existente.OrigemCanal
		if canal == "" || canal == "Orgânico / Direto" {
			canal = atrib.OrigemCanal
		}
		campanha := existente.Campanha
		if campanha == "" {
			campanha = atrib.Campanha
		}
		utmSource := existente.UtmSource
		if utmSource == "" {
			utmSource = atrib.UtmSource
		}
		utmCampaign := existente.UtmCampaign
		if utmCampaign == "" {
			utmCampaign = atrib.UtmCampaign
		}

		_, err := g.exec(`
			UPDATE atribuicoes_contato
			SET numero_formatado = CASE WHEN ? != '' THEN ? ELSE numero_formatado END,
			    origem_canal = ?,
			    campanha = ?,
			    utm_source = ?,
			    utm_medium = CASE WHEN ? != '' THEN ? ELSE utm_medium END,
			    utm_campaign = ?,
			    utm_term = CASE WHEN ? != '' THEN ? ELSE utm_term END,
			    utm_content = CASE WHEN ? != '' THEN ? ELSE utm_content END,
			    ctwa_clid = CASE WHEN ? != '' THEN ? ELSE ctwa_clid END,
			    ad_source_id = CASE WHEN ? != '' THEN ? ELSE ad_source_id END,
			    ad_source_url = CASE WHEN ? != '' THEN ? ELSE ad_source_url END,
			    referencia = CASE WHEN ? != '' THEN ? ELSE referencia END,
			    atualizado_em = CURRENT_TIMESTAMP
			WHERE instancia_id = ? AND jid = ?
		`, atrib.NumeroFormatado, atrib.NumeroFormatado, canal, campanha, utmSource,
			atrib.UtmMedium, atrib.UtmMedium, utmCampaign,
			atrib.UtmTerm, atrib.UtmTerm, atrib.UtmContent, atrib.UtmContent,
			atrib.CtwaClid, atrib.CtwaClid, atrib.AdSourceID, atrib.AdSourceID,
			atrib.AdSourceURL, atrib.AdSourceURL, atrib.Referencia, atrib.Referencia,
			atrib.InstanciaID, atrib.JID)
		return err
	}

	novoID := "atr_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]
	_, err := g.exec(`
		INSERT INTO atribuicoes_contato (
			id, instancia_id, jid, numero_formatado, origem_canal, campanha,
			utm_source, utm_medium, utm_campaign, utm_term, utm_content,
			ctwa_clid, ad_source_id, ad_source_url, referencia, primeira_mensagem,
			criado_em, atualizado_em
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, novoID, atrib.InstanciaID, atrib.JID, atrib.NumeroFormatado, atrib.OrigemCanal, atrib.Campanha,
		atrib.UtmSource, atrib.UtmMedium, atrib.UtmCampaign, atrib.UtmTerm, atrib.UtmContent,
		atrib.CtwaClid, atrib.AdSourceID, atrib.AdSourceURL, atrib.Referencia, atrib.PrimeiraMensagem)
	return err
}

func (g *GerenciadorDB) ObterAtribuicaoContato(instanciaID, jid string) (*AtribuicaoContato, error) {
	row := g.queryRow(`
		SELECT id, instancia_id, jid, numero_formatado, origem_canal, campanha,
		       utm_source, utm_medium, utm_campaign, utm_term, utm_content,
		       ctwa_clid, ad_source_id, ad_source_url, referencia, primeira_mensagem,
		       criado_em, atualizado_em
		FROM atribuicoes_contato
		WHERE instancia_id = ? AND jid = ?
	`, instanciaID, jid)

	var a AtribuicaoContato
	if err := row.Scan(
		&a.ID, &a.InstanciaID, &a.JID, &a.NumeroFormatado, &a.OrigemCanal, &a.Campanha,
		&a.UtmSource, &a.UtmMedium, &a.UtmCampaign, &a.UtmTerm, &a.UtmContent,
		&a.CtwaClid, &a.AdSourceID, &a.AdSourceURL, &a.Referencia, &a.PrimeiraMensagem,
		&a.CriadoEm, &a.AtualizadoEm,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

func (g *GerenciadorDB) ListarAtribuicoesInstancia(instanciaID string, limite int) ([]AtribuicaoContato, error) {
	if limite <= 0 || limite > 200 {
		limite = 50
	}
	rows, err := g.query(`
		SELECT id, instancia_id, jid, numero_formatado, origem_canal, campanha,
		       utm_source, utm_medium, utm_campaign, utm_term, utm_content,
		       ctwa_clid, ad_source_id, ad_source_url, referencia, primeira_mensagem,
		       criado_em, atualizado_em
		FROM atribuicoes_contato
		WHERE instancia_id = ?
		ORDER BY atualizado_em DESC
		LIMIT ?
	`, instanciaID, limite)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lista []AtribuicaoContato
	for rows.Next() {
		var a AtribuicaoContato
		if err := rows.Scan(
			&a.ID, &a.InstanciaID, &a.JID, &a.NumeroFormatado, &a.OrigemCanal, &a.Campanha,
			&a.UtmSource, &a.UtmMedium, &a.UtmCampaign, &a.UtmTerm, &a.UtmContent,
			&a.CtwaClid, &a.AdSourceID, &a.AdSourceURL, &a.Referencia, &a.PrimeiraMensagem,
			&a.CriadoEm, &a.AtualizadoEm,
		); err == nil {
			lista = append(lista, a)
		}
	}
	return lista, nil
}
