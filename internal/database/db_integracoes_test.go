package database

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIntegracoesEAtribuicoesDB(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_integracoes.db")
	defer os.Remove(dbPath)

	gdb, err := InicializarBanco("sqlite", dbPath, OpcoesSeed{SeedDadosDemo: true})
	if err != nil {
		t.Fatalf("Falha ao inicializar banco de teste: %v", err)
	}
	defer gdb.Fechar()

	instID := "inst_demo_brasil"

	// 1. Teste de Salvar e Obter Integração
	configChatwoot := `{"url":"https://app.chatwoot.com","account_id":"1","token":"test_token","inbox_id":"10"}`
	intCriada, err := gdb.SalvarIntegracaoInstancia(instID, "chatwoot", true, configChatwoot)
	if err != nil {
		t.Fatalf("Erro ao salvar integracao: %v", err)
	}
	if intCriada == nil || !intCriada.Ativo || intCriada.Tipo != "chatwoot" {
		t.Fatalf("Integracao retornada incorreta: %+v", intCriada)
	}

	lista, err := gdb.ObterIntegracoesInstancia(instID)
	if err != nil || len(lista) != 1 {
		t.Fatalf("Esperava 1 integracao na lista, obteve %d (err: %v)", len(lista), err)
	}

	// 2. Teste de Sessão Typebot
	jid := "5511999998888@s.whatsapp.net"
	err = gdb.SalvarSessaoTypebot(instID, jid, "sess_typebot_xyz", "ATIVO")
	if err != nil {
		t.Fatalf("Erro ao salvar sessao typebot: %v", err)
	}

	sess, err := gdb.ObterSessaoTypebot(instID, jid)
	if err != nil || sess == nil || sess.SessionIDTypebot != "sess_typebot_xyz" {
		t.Fatalf("Sessao typebot incorreta: %+v", sess)
	}

	// 3. Teste de Atribuição de Origem & Campanhas
	atrib := AtribuicaoContato{
		InstanciaID:      instID,
		JID:              jid,
		NumeroFormatado:  "+55 (11) 99999-8888",
		OrigemCanal:      "Google Ads",
		Campanha:         "Black Friday 2026",
		UtmSource:        "google",
		UtmMedium:        "cpc",
		UtmCampaign:      "Black Friday 2026",
		PrimeiraMensagem: "Quero saber o valor",
	}

	err = gdb.SalvarOuAtualizarAtribuicaoContato(atrib)
	if err != nil {
		t.Fatalf("Erro ao salvar atribuicao: %v", err)
	}

	atribSalva, err := gdb.ObterAtribuicaoContato(instID, jid)
	if err != nil || atribSalva == nil || atribSalva.OrigemCanal != "Google Ads" {
		t.Fatalf("Atribuicao salva incorreta: %+v", atribSalva)
	}

	listaAtrib, err := gdb.ListarAtribuicoesInstancia(instID, 10)
	if err != nil || len(listaAtrib) != 1 {
		t.Fatalf("Esperava 1 atribuicao listada, obteve %d", len(listaAtrib))
	}
}
