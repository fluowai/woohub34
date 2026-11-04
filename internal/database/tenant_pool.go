package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/store/sqlstore"
)

// PoolBancosRevenda gerencia conexões isoladas de Supabase para cada Revendedor (BYO-Supabase)
type PoolBancosRevenda struct {
	mu    sync.RWMutex
	pools map[string]*GerenciadorDB
}

var PoolTenants = &PoolBancosRevenda{
	pools: make(map[string]*GerenciadorDB),
}

// NormalizarURISupabase garante o uso da porta 5432 (Session Mode) no Supavisor e sslmode=require
func NormalizarURISupabase(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ""
	}
	if strings.Contains(uri, ".pooler.supabase.com:6543") {
		uri = strings.Replace(uri, ".pooler.supabase.com:6543", ".pooler.supabase.com:5432", 1)
	}
	if (strings.Contains(uri, ".supabase.co") || strings.Contains(uri, ".supabase.com")) && !strings.Contains(uri, "sslmode=") {
		if strings.Contains(uri, "?") {
			uri += "&sslmode=require"
		} else {
			uri += "?sslmode=require"
		}
	}
	return uri
}

// TestarEMigrarSupabaseIsolado conecta no Supabase informado pelo Revendedor, mede a latência e aplica todas as tabelas (WooHub + whatsmeow_device)
func TestarEMigrarSupabaseIsolado(ctx context.Context, supabaseURI string) (map[string]interface{}, error) {
	uriNormalizada := NormalizarURISupabase(supabaseURI)
	if uriNormalizada == "" {
		return nil, fmt.Errorf("URI do Supabase não informada")
	}
	if !strings.HasPrefix(uriNormalizada, "postgres://") && !strings.HasPrefix(uriNormalizada, "postgresql://") {
		return nil, fmt.Errorf("a URI do Supabase deve começar com postgresql:// ou postgres://")
	}

	inicio := time.Now()
	conexao, err := sql.Open("postgres", uriNormalizada)
	if err != nil {
		return nil, fmt.Errorf("erro ao abrir conexão PostgreSQL: %w", err)
	}
	defer conexao.Close()

	ctxPing, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	if err := conexao.PingContext(ctxPing); err != nil {
		return nil, fmt.Errorf("falha ao conectar no Supabase da revenda: %w", err)
	}
	latenciaMs := time.Since(inicio).Milliseconds()

	tempDB := &GerenciadorDB{
		db:     conexao,
		driver: "postgres",
	}

	// 1. Executa migrações das tabelas comerciais WooHub no Supabase isolado da revenda
	if err := tempDB.migrar(); err != nil {
		return nil, fmt.Errorf("conectou ao Supabase, mas falhou ao criar tabelas WooHub: %w", err)
	}

	// 2. Executa migrações criptográficas do whatsmeow (whatsmeow_device, prekeys, identity_keys, etc.)
	container := sqlstore.NewWithDB(conexao, "postgres", nil)
	ctxUpgrade, cancelUpgrade := context.WithTimeout(ctx, 30*time.Second)
	defer cancelUpgrade()
	if err := container.Upgrade(ctxUpgrade); err != nil {
		return nil, fmt.Errorf("tabelas WooHub criadas, mas falhou ao migrar tabelas whatsmeow: %w", err)
	}

	var totalTabelas int
	_ = conexao.QueryRowContext(ctxPing, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public'`).Scan(&totalTabelas)

	return map[string]interface{}{
		"conectado":           true,
		"latencia_ms":         latenciaMs,
		"tabelas_publicas":    totalTabelas,
		"pooler_session_mode": strings.Contains(uriNormalizada, ":5432"),
		"uri_normalizada":     mascararSenhaURI(uriNormalizada),
	}, nil
}

// ObterBancoDoRevendedor retorna o banco isolado da revenda (se configurado) ou o banco principal
func (p *PoolBancosRevenda) ObterBancoDoRevendedor(revendedorID string) *GerenciadorDB {
	if DB == nil || revendedorID == "" {
		return DB
	}

	p.mu.RLock()
	if dbIsolado, existe := p.pools[revendedorID]; existe && dbIsolado != nil {
		p.mu.RUnlock()
		return dbIsolado
	}
	p.mu.RUnlock()

	rev, err := DB.BuscarRevendedorPorID(revendedorID)
	if err != nil || rev == nil || strings.TrimSpace(rev.SupabaseURI) == "" {
		return DB
	}

	uriNorm := NormalizarURISupabase(rev.SupabaseURI)
	conexao, err := sql.Open("postgres", uriNorm)
	if err != nil {
		return DB
	}
	conexao.SetMaxOpenConns(15)
	conexao.SetMaxIdleConns(3)
	conexao.SetConnMaxLifetime(30 * time.Minute)

	if err := conexao.Ping(); err != nil {
		_ = conexao.Close()
		return DB
	}

	novoDB := &GerenciadorDB{
		db:     conexao,
		driver: "postgres",
	}
	_ = novoDB.migrar()

	p.mu.Lock()
	p.pools[revendedorID] = novoDB
	p.mu.Unlock()

	return novoDB
}

// InvalidarPoolRevendedor fecha e recria a conexão quando o revendedor altera sua URI do Supabase
func (p *PoolBancosRevenda) InvalidarPoolRevendedor(revendedorID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if antigo, ok := p.pools[revendedorID]; ok && antigo != nil {
		_ = antigo.db.Close()
		delete(p.pools, revendedorID)
	}
}

// MascararURIConexao remove a senha de uma URI antes de qualquer resposta de API
// ou log. Ela é exportada para que handlers não precisem duplicar essa lógica.
func MascararURIConexao(uri string) string {
	idxArroba := strings.LastIndex(uri, "@")
	idxDoisPontos := strings.Index(uri, "://")
	if idxArroba == -1 || idxDoisPontos == -1 {
		return uri
	}
	credenciais := uri[idxDoisPontos+3 : idxArroba]
	partes := strings.SplitN(credenciais, ":", 2)
	if len(partes) != 2 {
		return uri
	}
	return uri[:idxDoisPontos+3] + partes[0] + ":********" + uri[idxArroba:]
}

func mascararSenhaURI(uri string) string { return MascararURIConexao(uri) }

