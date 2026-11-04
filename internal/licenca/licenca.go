package licenca

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"go.mau.fi/whatsmeow/internal/config"
	"go.mau.fi/whatsmeow/internal/database"
)

// PayloadTokenEd25519 representa os dados criptograficamente assinados na licença
type PayloadTokenEd25519 struct {
	ChaveLicenca      string    `json:"chave"`
	ClienteNome       string    `json:"cliente"`
	DominioAutorizado string    `json:"dominio"`
	Plano             string    `json:"plano"`
	LimiteInstancias  int       `json:"limite_instancias"`
	ExpiraEm          time.Time `json:"expira_em"`
	EmitidoEm         time.Time `json:"emitido_em"`
}

// EstadoLicenca guarda o estado em memória da licença ativa do container
type EstadoLicenca struct {
	mu                sync.RWMutex
	Ativa             bool      `json:"ativa"`
	Chave             string    `json:"chave"`
	ClienteNome       string    `json:"cliente_nome"`
	Plano             string    `json:"plano"`
	LimiteInstancias  int       `json:"limite_instancias"`
	Status            string    `json:"status"`
	DominioAutorizado string    `json:"dominio_autorizado"`
	Fingerprint       string    `json:"fingerprint"`
	ExpiraEm          time.Time `json:"expira_em"`
	UltimoCheck       time.Time `json:"ultimo_check"`
	ModoMaster        bool      `json:"modo_master"`
}

var LicencaAtual = &EstadoLicenca{
	Ativa:            true,
	Chave:            "woo_lic_master_oficial_brasil",
	ClienteNome:      "Servidor Master WooHub (Matriz)",
	Plano:            "Master Unlimited",
	LimiteInstancias: 999,
	Status:           "ATIVA",
	ExpiraEm:         time.Now().AddDate(5, 0, 0),
	UltimoCheck:      time.Now(),
	ModoMaster:       true,
}

func obterParChavesEd25519() (ed25519.PublicKey, ed25519.PrivateKey) {
	sementeStr := "woohub_ed25519_master_license_authority_brasil_2026"
	if config.AppConfig != nil && config.AppConfig.ChaveMestreSuperAdmin != "" {
		sementeStr = config.AppConfig.ChaveMestreSuperAdmin + "::ed25519_woohub"
	}
	hash := sha256.Sum256([]byte(sementeStr))
	priv := ed25519.NewKeyFromSeed(hash[:])
	pub := priv.Public().(ed25519.PublicKey)
	return pub, priv
}

// GerarTokenAssinadoEd25519 gera um token inviolável assinado com Ed25519 contendo o limite de instâncias e validade
func GerarTokenAssinadoEd25519(chave, cliente, dominio, plano string, limite int, expiraEm time.Time) string {
	_, priv := obterParChavesEd25519()
	payload := PayloadTokenEd25519{
		ChaveLicenca:      chave,
		ClienteNome:       cliente,
		DominioAutorizado: dominio,
		Plano:             plano,
		LimiteInstancias:  limite,
		ExpiraEm:          expiraEm.UTC(),
		EmitidoEm:         time.Now().UTC(),
	}
	dadosJSON, _ := json.Marshal(payload)
	assinatura := ed25519.Sign(priv, dadosJSON)
	return fmt.Sprintf("WOO1.%s.%s",
		base64.RawURLEncoding.EncodeToString(dadosJSON),
		base64.RawURLEncoding.EncodeToString(assinatura),
	)
}

// VerificarTokenAssinadoEd25519 valida a assinatura Ed25519 de um token de licença
func VerificarTokenAssinadoEd25519(token string) (*PayloadTokenEd25519, error) {
	partes := strings.Split(strings.TrimSpace(token), ".")
	if len(partes) != 3 || partes[0] != "WOO1" {
		return nil, fmt.Errorf("formato de token Ed25519 inválido")
	}
	dadosJSON, err := base64.RawURLEncoding.DecodeString(partes[1])
	if err != nil {
		return nil, fmt.Errorf("payload do token inválido")
	}
	assinatura, err := base64.RawURLEncoding.DecodeString(partes[2])
	if err != nil {
		return nil, fmt.Errorf("assinatura do token inválida")
	}
	pub, _ := obterParChavesEd25519()
	if !ed25519.Verify(pub, dadosJSON, assinatura) {
		return nil, fmt.Errorf("assinatura criptográfica Ed25519 inválida (tentativa de adulteração detectada)")
	}
	var payload PayloadTokenEd25519
	if err := json.Unmarshal(dadosJSON, &payload); err != nil {
		return nil, err
	}
	if time.Now().After(payload.ExpiraEm) {
		return nil, fmt.Errorf("token de licença expirado em %s", payload.ExpiraEm.Format("02/01/2006"))
	}
	return &payload, nil
}

// ObterFingerprintContainer gera um identificador único do container/host onde a Stack está rodando
func ObterFingerprintContainer() string {
	hostname, _ := os.Hostname()
	raw := fmt.Sprintf("%s|%s|%s", hostname, runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("fp_%s_%x", hostname, sum[:6])
}

// ValidarGatekeeperNoBoot bloqueia a subida do container no Portainer se a licença não for válida
func ValidarGatekeeperNoBoot(cfg *config.Config, db *database.GerenciadorDB) error {
	fp := ObterFingerprintContainer()
	chave := strings.TrimSpace(cfg.LicencaChave)

	// 1. Se o container está configurado para validar em um Servidor Mestre Remoto (Modo Portainer Cliente)
	if cfg.LicencaServidorURL != "" {
		if chave == "" {
			return fmt.Errorf("WOOHUB_LICENCA_CHAVE não informada! Este container exige uma licença válida para subir em produção")
		}
		respValidacao, err := consultarServidorMestreRemoto(cfg.LicencaServidorURL, "/api/v1/licenca/validar", map[string]interface{}{
			"chave_licenca":         chave,
			"dominio":               cfg.DominioOficial,
			"fingerprint_container": fp,
			"versao":                "1.1.0-Brasil-Prod",
		})
		if err != nil {
			return fmt.Errorf("bloqueio de licença no boot: %w", err)
		}

		LicencaAtual.mu.Lock()
		LicencaAtual.Ativa = true
		LicencaAtual.Chave = chave
		LicencaAtual.ClienteNome = fmt.Sprintf("%v", respValidacao["cliente_nome"])
		LicencaAtual.Plano = fmt.Sprintf("%v", respValidacao["plano"])
		if lim, ok := respValidacao["limite_instancias"].(float64); ok && lim > 0 {
			LicencaAtual.LimiteInstancias = int(lim)
		}
		LicencaAtual.Status = "ATIVA"
		LicencaAtual.Fingerprint = fp
		LicencaAtual.UltimoCheck = time.Now()
		LicencaAtual.ModoMaster = false
		LicencaAtual.mu.Unlock()
		return nil
	}

	// 2. Se EXIGIR_LICENCA=true (imagem Docker para Portainer) e não informou chave, bloqueia subida!
	if cfg.ExigirLicencaRemota && chave == "" {
		return fmt.Errorf("container bloqueado: variável WOOHUB_LICENCA_CHAVE obrigatória não foi definida na Stack do Portainer")
	}

	// 3. Se informou uma chave local/Master, valida diretamente no banco de licenças
	if chave != "" && db != nil {
		licDB, err := db.BuscarLicencaPorChave(chave)
		if err != nil {
			return fmt.Errorf("chave de licença '%s' não encontrada ou inválida", chave)
		}
		if licDB.Status != "ATIVA" {
			return fmt.Errorf("licença '%s' está com status %s — container bloqueado", chave, licDB.Status)
		}
		if time.Now().After(licDB.ExpiraEm) {
			_ = db.AtualizarStatusLicenca(licDB.ID, "EXPIRADA", 0)
			return fmt.Errorf("licença '%s' expirou em %s — renove para subir o container", chave, licDB.ExpiraEm.Format("02/01/2006"))
		}
		_ = db.RegistrarHeartbeatLicenca(licDB.ChaveLicenca, "127.0.0.1", cfg.DominioOficial, fp, "1.1.0-Brasil-Prod", 0)

		LicencaAtual.mu.Lock()
		LicencaAtual.Ativa = true
		LicencaAtual.Chave = licDB.ChaveLicenca
		LicencaAtual.ClienteNome = licDB.ClienteNome
		LicencaAtual.Plano = licDB.Plano
		LicencaAtual.LimiteInstancias = licDB.LimiteInstancias
		LicencaAtual.Status = licDB.Status
		LicencaAtual.DominioAutorizado = licDB.DominioAutorizado
		LicencaAtual.Fingerprint = fp
		LicencaAtual.ExpiraEm = licDB.ExpiraEm
		LicencaAtual.UltimoCheck = time.Now()
		LicencaAtual.ModoMaster = cfg.ModoServidorLicencaMaster
		LicencaAtual.mu.Unlock()
		return nil
	}

	// 4. Modo Master Padrão (Servidor Matriz)
	LicencaAtual.mu.Lock()
	LicencaAtual.Fingerprint = fp
	LicencaAtual.UltimoCheck = time.Now()
	LicencaAtual.ModoMaster = true
	LicencaAtual.mu.Unlock()
	return nil
}

// IniciarHeartbeatBackground mantém o container validado a cada 15 min e executa Kill-Switch se a licença for suspensa
func IniciarHeartbeatBackground(ctx context.Context, cfg *config.Config, db *database.GerenciadorDB, contarInstancias func() int) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ativas := 0
			if contarInstancias != nil {
				ativas = contarInstancias()
			}
			fp := ObterFingerprintContainer()

			if cfg.LicencaServidorURL != "" && cfg.LicencaChave != "" {
				resp, err := consultarServidorMestreRemoto(cfg.LicencaServidorURL, "/api/v1/licenca/heartbeat", map[string]interface{}{
					"chave_licenca":         cfg.LicencaChave,
					"dominio":               cfg.DominioOficial,
					"fingerprint_container": fp,
					"versao":                "1.1.0-Brasil-Prod",
					"instancias_ativas":     ativas,
				})
				if err != nil {
					log.Error().Err(err).Msg("🚨 KILL-SWITCH: Licença revogada ou recusada pelo Servidor Mestre! Encerrando container...")
					os.Exit(1)
				}
				if acao, ok := resp["acao"].(string); ok && acao == "KILL" {
					log.Error().Msg("🚨 KILL-SWITCH ACIONADO: Container suspenso pelo administrador central!")
					os.Exit(1)
				}
			} else if db != nil {
				chave := cfg.LicencaChave
				if chave == "" {
					chave = "woo_lic_master_oficial_brasil"
				}
				lic, err := db.BuscarLicencaPorChave(chave)
				if err == nil && lic != nil {
					if cfg.ExigirLicencaRemota && (lic.Status != "ATIVA" || time.Now().After(lic.ExpiraEm)) {
						log.Error().Str("status", lic.Status).Msg("🚨 KILL-SWITCH: Licença local suspensa ou expirada! Encerrando container...")
						os.Exit(1)
					}
					_ = db.RegistrarHeartbeatLicenca(chave, "127.0.0.1", cfg.DominioOficial, fp, "1.1.0-Brasil-Prod", ativas)
				}
			}

			LicencaAtual.mu.Lock()
			LicencaAtual.UltimoCheck = time.Now()
			LicencaAtual.mu.Unlock()
		}
	}
}

// ObterResumoLicencaAtiva retorna os dados atuais da licença do container
func ObterResumoLicencaAtiva() map[string]interface{} {
	LicencaAtual.mu.RLock()
	defer LicencaAtual.mu.RUnlock()
	return map[string]interface{}{
		"ativa":             LicencaAtual.Ativa,
		"chave":             LicencaAtual.Chave,
		"cliente_nome":      LicencaAtual.ClienteNome,
		"plano":             LicencaAtual.Plano,
		"limite_instancias": LicencaAtual.LimiteInstancias,
		"status":            LicencaAtual.Status,
		"fingerprint":       LicencaAtual.Fingerprint,
		"expira_em":         LicencaAtual.ExpiraEm,
		"ultimo_check":      LicencaAtual.UltimoCheck,
		"modo_master":       LicencaAtual.ModoMaster,
	}
}

// ObterLimiteInstanciasLicenciado retorna o teto máximo de instâncias permitido pela licença criptográfica
func ObterLimiteInstanciasLicenciado() int {
	LicencaAtual.mu.RLock()
	defer LicencaAtual.mu.RUnlock()
	if LicencaAtual.LimiteInstancias <= 0 {
		return 50
	}
	return LicencaAtual.LimiteInstancias
}

func consultarServidorMestreRemoto(servidorBaseURL, rota string, payload map[string]interface{}) (map[string]interface{}, error) {
	corpo, _ := json.Marshal(payload)
	clienteHTTP := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(servidorBaseURL, "/")+rota, bytes.NewReader(corpo))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := clienteHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("não foi possível contatar o servidor de licenças (%s): %w", servidorBaseURL, err)
	}
	defer resp.Body.Close()

	var resultado struct {
		Sucesso  bool                   `json:"sucesso"`
		Mensagem string                 `json:"mensagem"`
		Dados    map[string]interface{} `json:"dados"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&resultado); err != nil {
		return nil, fmt.Errorf("resposta inválida do servidor de licenças (HTTP %d)", resp.StatusCode)
	}
	if !resultado.Sucesso || resp.StatusCode != http.StatusOK {
		return resultado.Dados, fmt.Errorf("%s", resultado.Mensagem)
	}
	return resultado.Dados, nil
}

