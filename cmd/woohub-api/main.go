package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"go.mau.fi/whatsmeow/internal/api"
	"go.mau.fi/whatsmeow/internal/config"
	"go.mau.fi/whatsmeow/internal/core"
	"go.mau.fi/whatsmeow/internal/database"
	"go.mau.fi/whatsmeow/internal/integracoes"
	"go.mau.fi/whatsmeow/internal/licenca"
	"go.mau.fi/whatsmeow/internal/storage"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
)

func init() {
	store.SetOSInfo("WooHub", [3]uint32{2, 3000, 1050728964})
}

func main() {
	zerolog.TimeFieldFormat = time.RFC3339
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"})

	fmt.Println("==================================================================")
	fmt.Println("🚀 WooHub API - Gateway Profissional WhatsApp (100% Brasil)")
	fmt.Println("⚡ Motor: WooHub Multidevice em Go puro")
	fmt.Println("💼 Modelo: B2B SaaS White-Label & E-revenda com Marca Própria")
	fmt.Println("==================================================================")

	// 1. Carregar Configurações (.env + variáveis de ambiente)
	cfg := config.CarregarConfiguracao()
	if err := cfg.ValidarProducao(); err != nil {
		log.Fatal().Err(err).Msg("Configuração de produção insegura ou incompleta")
	}
	log.Info().
		Str("ambiente", cfg.Ambiente).
		Str("porta", cfg.Porta).
		Str("banco", cfg.TipoBanco).
		Str("fuso", cfg.FusoHorarioPadrao).
		Msg("Configurações carregadas")

	if cfg.EhProducao() && cfg.AdminSenha == "admin123" {
		log.Warn().Msg("ATENÇÃO: ADMIN_SENHA está usando o valor padrão. Defina ADMIN_SENHA forte nas variáveis de ambiente!")
	}

	// 2. Conectar e Migrar Banco de Dados Multitenant (Supabase PostgreSQL ou SQLite)
	banco, err := database.InicializarBanco(cfg.TipoBanco, cfg.UriBanco, database.OpcoesSeed{
		AdminEmail:    cfg.AdminEmail,
		AdminSenha:    cfg.AdminSenha,
		SeedDadosDemo: cfg.SeedDadosDemo,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Falha ao inicializar o banco de dados")
	}
	log.Info().Str("dialeto", banco.ObterDialetoWhatsmeow()).Msg("Banco de dados pronto com migrações e índices aplicados")

	// 2.1. Gatekeeper de Licenciamento (Se a licença for inválida, suspensa ou ausente em modo Portainer, o container NÃO SOBE)
	if errLic := licenca.ValidarGatekeeperNoBoot(cfg, banco); errLic != nil {
		fmt.Println("==================================================================")
		fmt.Println("❌ CONTAINER BLOQUEADO POR LICENÇA - WOOHUB API")
		fmt.Printf("🚫 Motivo: %v\n", errLic)
		fmt.Println("==================================================================")
		os.Exit(1)
	}
	resumoLic := licenca.ObterResumoLicencaAtiva()
	log.Info().
		Interface("plano", resumoLic["plano"]).
		Interface("limite_instancias", resumoLic["limite_instancias"]).
		Str("storage_driver", cfg.StorageDriver).
		Msg("🔐 Licença criptográfica validada com sucesso")

	// 2.2. Se MinIO S3 global estiver configurado na Stack, valida/cria bucket inicial
	if cliMinio := storage.ObterClienteGlobal(); cliMinio != nil {
		ctxMinio, cancelMinio := context.WithTimeout(context.Background(), 10*time.Second)
		if errMinio := cliMinio.TestarECriarBucket(ctxMinio); errMinio != nil {
			log.Warn().Err(errMinio).Str("endpoint", cfg.MinioEndpoint).Msg("Aviso ao validar MinIO global no boot")
		} else {
			log.Info().Str("endpoint", cfg.MinioEndpoint).Str("bucket", cfg.MinioBucket).Msg("☁️ Storage MinIO S3 conectado e pronto")
		}
		cancelMinio()
	}

	// 3. Inicializar Container de Sessões Multidevice (sqlstore)
	container := sqlstore.NewWithDB(banco.ObterConexaoSQL(), banco.ObterDialetoWhatsmeow(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	if err := container.Upgrade(ctx); err != nil {
		log.Fatal().Err(err).Msg("Falha ao atualizar schema de sessões (sqlstore)")
	}
	cancel()
	log.Info().Msg("Armazenamento de chaves de sessão (sqlstore) inicializado")

	// 4. Inicializar Despachante Assíncrono de Webhooks
	webhookDispatcher := core.InicializarDespachanteWebhooks(1000, 5)
	log.Info().Msg("Fila de webhooks iniciada (5 workers)")

	// 5. Inicializar Gerenciador Central de Instâncias e Reconectar Sessões Salvas
	gerenciador := core.InicializarGerenciador(container, banco, webhookDispatcher)
	go gerenciador.ReconectarInstanciasSalvas(context.Background())
	go licenca.IniciarHeartbeatBackground(context.Background(), cfg, banco, gerenciador.ContarInstanciasConectadas)
	log.Info().Msg("Gerenciador de instâncias e Heartbeat de Licença iniciados em background")

	// 6. Configurar Rotas HTTP e Servidor Frontend
	// 5.1. Inicializar Hub Central de Integrações Nativas (Chatwoot, Typebot, n8n, Kommo CRM, RD Station e Webhooks)
	integracoes.InicializarGerenciadorIntegracoes(banco, gerenciador)
	log.Info().Msg("🔌 Hub Central de Integrações Nativas inicializado")

	caminhoFrontend := "./web"
	rotas := api.ConfigurarRotas(caminhoFrontend)

	servidor := &http.Server{
		Addr:              ":" + cfg.Porta,
		Handler:           rotas,
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       120 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// 7. Iniciar Servidor em Goroutine
	go func() {
		log.Info().Msgf("🟢 WooHub API rodando em http://localhost:%s", cfg.Porta)
		log.Info().Msgf("🖥️ Painel Web disponível em http://localhost:%s/", cfg.Porta)
		if err := servidor.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Erro no servidor HTTP")
		}
	}()

	// 8. Graceful Shutdown
	parada := make(chan os.Signal, 1)
	signal.Notify(parada, os.Interrupt, syscall.SIGTERM)
	<-parada

	log.Info().Msg("Encerrando WooHub API com segurança...")
	ctxShutdown, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()

	_ = servidor.Shutdown(ctxShutdown)
	_ = container.Close()
	log.Info().Msg("Serviço finalizado com sucesso.")
}
