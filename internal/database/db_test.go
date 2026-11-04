package database

import (
	"context"
	"os"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/internal/config"
	"go.mau.fi/whatsmeow/store/sqlstore"
)

func TestRebindPostgresPlaceholders(t *testing.T) {
	g := &GerenciadorDB{driver: "postgres"}

	entrada := `INSERT INTO clientes (id, revendedor_id, nome, email, ativo) VALUES (?, ?, ?, ?, 1)`
	esperado := `INSERT INTO clientes (id, revendedor_id, nome, email, ativo) VALUES ($1, $2, $3, $4, 1)`

	resultado := g.rebind(entrada)
	if resultado != esperado {
		t.Fatalf("Rebind Postgres incorreto:\nEsperado: %s\nObtido:   %s", esperado, resultado)
	}

	if g.ObterDialetoWhatsmeow() != "postgres" {
		t.Fatalf("Dialeto whatsmeow incorreto para postgres: %s", g.ObterDialetoWhatsmeow())
	}

	gSqlite := &GerenciadorDB{driver: "sqlite"}
	if gSqlite.rebind(entrada) != entrada {
		t.Fatalf("Rebind SQLite não deveria alterar a query")
	}
	if gSqlite.ObterDialetoWhatsmeow() != "sqlite3" {
		t.Fatalf("Dialeto whatsmeow incorreto para sqlite: %s", gSqlite.ObterDialetoWhatsmeow())
	}
}

func TestConexaoEMigracaoSupabase(t *testing.T) {
	_ = os.Chdir("../..")
	cfg := config.CarregarConfiguracao()
	if cfg.TipoBanco != "postgres" {
		t.Skip("TIPO_BANCO não é postgres")
	}

	db, err := InicializarBanco(cfg.TipoBanco, cfg.UriBanco, OpcoesSeed{
		AdminEmail:    cfg.AdminEmail,
		AdminSenha:    cfg.AdminSenha,
		SeedDadosDemo: cfg.SeedDadosDemo,
	})
	if err != nil {
		t.Fatalf("Falha ao inicializar banco Supabase: %v", err)
	}
	defer db.ObterConexaoSQL().Close()

	container := sqlstore.NewWithDB(db.ObterConexaoSQL(), db.ObterDialetoWhatsmeow(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := container.Upgrade(ctx); err != nil {
		t.Fatalf("Falha ao migrar tabelas whatsmeow (sqlstore) no Supabase: %v", err)
	}

	revs, err := db.ListarRevendedores()
	if err != nil {
		t.Fatalf("Erro ao listar revendedores no Supabase: %v", err)
	}
	clis, err := db.ListarClientes()
	if err != nil {
		t.Fatalf("Erro ao listar clientes no Supabase: %v", err)
	}
	insts, err := db.ListarInstancias()
	if err != nil {
		t.Fatalf("Erro ao listar instâncias no Supabase: %v", err)
	}

	t.Logf("SUPABASE OK -> Revendedores: %d | Clientes: %d | Instancias: %d", len(revs), len(clis), len(insts))
}


