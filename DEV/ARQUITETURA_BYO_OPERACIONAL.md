# Arquitetura BYO operacional por revenda

## Decisão

O WooHub mantém o control plane central: licenças, planos, domínios, estado da assinatura e cadastro mínimo da revenda. Cada revenda deve fornecer e manter um Supabase/PostgreSQL e um MinIO/S3 próprios.

O cadastro da infraestrutura só é aceito após:

1. conexão e migração do Supabase, incluindo as tabelas de sessão do WhatsApp;
2. conexão, criação/validação do bucket e teste do MinIO;
3. persistência da configuração somente depois dos dois testes passarem.

Usuários que não sejam Super Admin recebem `412 Precondition Failed` nas rotas operacionais enquanto a infraestrutura BYO não estiver pronta. O sistema não deve fazer fallback silencioso para o banco central.

## Separação de responsabilidades

### Control plane central

- revendas, domínios e planos;
- licenças e heartbeat;
- status mínimo da infraestrutura;
- versão e limites contratados.

### Data plane da revenda

- clientes e usuários;
- instâncias e sessões WhatsApp;
- logs e auditoria operacional;
- webhooks e integrações;
- mídias e anexos.

## Estado de prontidão

`CRIADA → SUPABASE_PENDENTE → MINIO_PENDENTE → INFRAESTRUTURA_VALIDADA → OPERACIONAL`

Uma revenda só deve ser liberada para uso quando `InfraestruturaBYOPronta()` for verdadeiro.

## Próxima etapa obrigatória do runtime

O processo principal ainda inicializa um `sqlstore.Container` e um gerenciador de instâncias global. Para isolamento completo, o runtime deve manter um par `(GerenciadorDB, sqlstore.Container, GerenciadorInstancias)` por revenda e resolver o par pelo tenant autenticado/instância antes de cada operação. O `PoolTenants` existente é a base de conexões, mas não substitui essa separação de ciclo de vida.

