// ==========================================================================
// WOOHUB API - FRONTEND CONTROLLER ULTRA COMPLETO (TIERS 01, 02 E 03)
// ==========================================================================

let instanciasCache = [];
let clientesCache = [];
let revendedoresCache = [];
let clienteAtivoId = '';
let clienteAtivoNome = 'Todas as Instâncias';
let instanciaAtivaParaConectar = null;
let eventSourceConexao = null;
let pollingInterval = null;
let visaoAtualTier = 'admin'; // 'admin' (superadmin), 'revendedor', 'cliente'
let papelAutenticado = 'superadmin';
let usuarioAutenticado = null;
let revendedorAutenticado = null;
let subSecaoAtiva = 'visao-geral';
let planoEmEdicao = null;

function normalizarRota(secao) {
  return secao === 'visao-geral' ? '/' : `/${secao}`;
}

function sincronizarRota(secao, substituir = false) {
  const url = normalizarRota(secao);
  if (substituir) history.replaceState({ secao }, '', url);
  else if (window.location.pathname !== url) history.pushState({ secao }, '', url);
}

function secaoDaURL() {
  const secao = window.location.pathname.replace(/^\//, '').split('/')[0];
  return secao || 'visao-geral';
}

function alternarSeletorPerfil(event) {
  if (event) event.stopPropagation();
  if (papelAutenticado !== 'superadmin') return;
  const dropdown = document.getElementById('dropdown-perfis');
  if (dropdown) dropdown.style.display = dropdown.style.display === 'none' ? 'block' : 'none';
}

function fecharSeletorPerfil() {
  const dropdown = document.getElementById('dropdown-perfis');
  if (dropdown) dropdown.style.display = 'none';
}

document.addEventListener('click', (event) => {
  const box = document.getElementById('role-selector-box');
  const dropdown = document.getElementById('dropdown-perfis');
  if (dropdown && box && !box.contains(event.target) && !dropdown.contains(event.target)) fecharSeletorPerfil();
});

// ==========================================================================
// 1. GESTÃO DE SESSÃO & AUTENTICAÇÃO (LOGIN / LOGOUT / BEARER TOKEN)
// ==========================================================================

function escaparHTML(valor) {
  if (valor === null || valor === undefined) return '';
  let texto = String(valor);
  // Corrige dados antigos gravados com dupla interpretação UTF-8 (ex.: AgÃªncia).
  if (/[ÃÂ][\x80-\xBF]/.test(texto)) {
    try { texto = decodeURIComponent(escape(texto)); } catch (_) { /* mantém o texto original */ }
  }
  return texto
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function obterToken() {
  return localStorage.getItem('woohub_token') || sessionStorage.getItem('woohub_token') || '';
}

function salvarToken(token, lembrar) {
  if (lembrar) {
    localStorage.setItem('woohub_token', token);
    sessionStorage.removeItem('woohub_token');
  } else {
    sessionStorage.setItem('woohub_token', token);
    localStorage.removeItem('woohub_token');
  }
}

function limparToken() {
  localStorage.removeItem('woohub_token');
  sessionStorage.removeItem('woohub_token');
  localStorage.removeItem('woohub_usuario');
  if (pollingInterval) {
    clearInterval(pollingInterval);
    pollingInterval = null;
  }
}

// Interceptor de requisições autenticadas (injeta Bearer Token e trata 401)
async function fetchAutenticado(url, options = {}) {
  const token = obterToken();
  options.headers = options.headers || {};
  if (token) {
    options.headers['Authorization'] = `Bearer ${token}`;
  }

  const res = await fetch(url, options);

  if (res.status === 401) {
    limparToken();
    exibirTelaLogin('Sessão expirada. Por favor, realize o login novamente.');
    throw new Error('Sessão expirada ou não autenticada.');
  }

  return res;
}

// Inicialização Geral da Aplicação
document.addEventListener('DOMContentLoaded', async () => {
  await carregarMarcaWhiteLabel();
  await verificarAutenticacaoInicial();
  window.addEventListener('popstate', () => navegarPara(secaoDaURL(), true));
});

async function verificarAutenticacaoInicial() {
  const token = obterToken();
  if (!token) {
    exibirTelaLogin();
    return;
  }

  try {
    const res = await fetch('/api/v1/auth/me', {
      headers: { 'Authorization': `Bearer ${token}` }
    });
    const dados = await res.json();

    if (dados.sucesso && dados.dados && dados.dados.usuario) {
      revendedorAutenticado = dados.dados.revendedor || null;
      exibirPainelPrincipal(dados.dados.usuario, revendedorAutenticado);
      // Só chama o onboarding quando o servidor anuncia suporte à versão atual.
      // Isso evita 404 no painel enquanto um binário antigo ainda estiver ativo.
      try {
        const statusRes = await fetch('/api/v1/status');
        const statusJson = await statusRes.json();
        const versao = String(statusJson.dados && statusJson.dados.versao || '');
        if (versao.startsWith('1.2.')) iniciarOnboardingSeNecessario(dados.dados.usuario);
      } catch (_) { /* o login continua funcionando mesmo sem status */ }
    } else {
      limparToken();
      exibirTelaLogin();
    }
  } catch (err) {
    console.warn('Falha na validação de sessão:', err);
    exibirTelaLogin();
  }
}

async function iniciarOnboardingSeNecessario(usuario) {
  try {
    const res = await fetchAutenticado('/api/v1/onboarding');
    // Binários antigos podem não possuir a rota; não interromper o login nem gerar erro de JS.
    if (res.status === 404) return;
    const json = await res.json();
    if (!json.sucesso || !json.dados || json.dados.concluido) return;
    const revendedor = usuario.papel === 'revendedor';
    const titulo = revendedor ? 'Configure sua revenda' : 'Vamos preparar sua conta';
    const passos = revendedor
      ? 'Personalize sua marca, conecte Supabase + MinIO, configure o Resend e cadastre seu primeiro cliente.'
      : 'Confira seus dados, conecte sua primeira instância e envie uma mensagem de teste.';
    const box = document.createElement('div');
    box.id = 'onboarding-inicial';
    box.style.cssText = 'position:fixed;inset:0;background:rgba(15,23,42,.55);z-index:9999;display:grid;place-items:center;padding:1rem';
    box.innerHTML = `<div style="max-width:520px;background:#fff;border-radius:18px;padding:2rem;box-shadow:0 24px 80px rgba(15,23,42,.25)"><div style="font-size:.75rem;letter-spacing:.08em;color:#059669;font-weight:800">PRIMEIROS PASSOS</div><h2 style="margin:.45rem 0 .7rem;color:#0f172a">${escaparHTML(titulo)}</h2><p style="color:#475569;line-height:1.6">${escaparHTML(passos)}</p><div style="display:flex;justify-content:flex-end;gap:.6rem;margin-top:1.4rem"><button class="btn btn-outline" id="onboarding-pular">Agora não</button><button class="btn btn-primary" id="onboarding-iniciar">Começar configuração</button></div></div>`;
    document.body.appendChild(box);
    document.getElementById('onboarding-pular').onclick = () => { box.remove(); };
    document.getElementById('onboarding-iniciar').onclick = async () => { await fetchAutenticado('/api/v1/onboarding', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({etapa: revendedor ? 'marca' : 'instancia', dados:{inicio:true}})}); box.remove(); navegarPara(revendedor ? 'whitelabel' : 'instancias'); };
  } catch (err) { console.warn('Não foi possível carregar o onboarding:', err); }
}

function exibirTelaLogin(mensagemAlerta = '') {
  document.getElementById('tela-login').style.display = 'flex';
  document.getElementById('painel-principal').style.display = 'none';

  const alertaEl = document.getElementById('login-alerta');
  if (mensagemAlerta) {
    alertaEl.innerText = mensagemAlerta;
    alertaEl.style.display = 'block';
  } else {
    alertaEl.style.display = 'none';
  }

  setTimeout(() => {
    const emailInput = document.getElementById('login-email');
    if (emailInput) emailInput.focus();
  }, 100);
}

// Aplica isolamento estrito de interface e menus conforme o nível da conta autenticada
function aplicarIsolamentoEstritoPorPapel(papel, usuario, revendedor) {
  const navRevendedores = document.getElementById('nav-revendedores');
  const navClientes = document.getElementById('nav-clientes');
  const navWhitelabel = document.querySelector('[data-nav="whitelabel"]');
  const navFinanceiro = document.querySelector('[data-nav="financeiro"]');
  const navEmailResend = document.getElementById('nav-email-resend');
  const selectClienteAtivo = document.getElementById('select-cliente-ativo');
  const badgeTopo = document.getElementById('badge-nivel-topo');
  const buscaInput = document.getElementById('busca-global-input');
  const cargoFooter = document.getElementById('sidebar-usuario-cargo');
  const roleBox = document.getElementById('role-selector-box');
  const roleChevron = document.getElementById('role-chevron-badge');
  if (roleBox) roleBox.style.cursor = papel === 'superadmin' ? 'pointer' : 'default';
  if (roleChevron) roleChevron.style.display = papel === 'superadmin' ? 'inline-flex' : 'none';

  if (papel === 'superadmin') {
    if (navEmailResend) navEmailResend.style.display = 'flex';
    if (navRevendedores) {
      navRevendedores.style.display = 'flex';
      const lbl = document.getElementById('label-nav-revendedores');
      if (lbl) lbl.innerText = 'Revendedores';
    }
    if (navClientes) navClientes.style.display = 'flex';
    if (navWhitelabel) navWhitelabel.style.display = 'flex';
    if (navFinanceiro) navFinanceiro.style.display = 'flex';
    if (selectClienteAtivo) selectClienteAtivo.style.display = 'inline-block';
    if (badgeTopo) badgeTopo.innerText = 'Mega Admin';
    if (buscaInput) buscaInput.placeholder = 'Buscar revendedores, clientes, instâncias...';
    if (cargoFooter) cargoFooter.innerText = 'Dono da plataforma';
  } else if (papel === 'revendedor') {
    if (navEmailResend) navEmailResend.style.display = 'flex';
    if (navRevendedores) navRevendedores.style.display = 'none';
    if (navClientes) {
      navClientes.style.display = 'flex';
      const lbl = document.getElementById('label-nav-clientes');
      if (lbl) lbl.innerText = 'Meus clientes';
    }
    if (navWhitelabel) navWhitelabel.style.display = 'flex';
    if (navFinanceiro) navFinanceiro.style.display = 'flex';
    if (selectClienteAtivo) selectClienteAtivo.style.display = 'inline-block';
    if (badgeTopo) badgeTopo.innerText = (revendedor && revendedor.nome_empresa) ? `Revenda • ${revendedor.nome_empresa}` : 'Revendedor';
    if (buscaInput) buscaInput.placeholder = 'Buscar meus clientes ou instâncias...';
    if (cargoFooter) cargoFooter.innerText = 'Parceiro Revendedor';
  } else {
    if (navEmailResend) navEmailResend.style.display = 'none';
    // Nível Cliente Final (usuário): vê estritamente apenas a sua conta e suas conexões
    if (navRevendedores) navRevendedores.style.display = 'none';
    if (navClientes) navClientes.style.display = 'none';
    if (navWhitelabel) navWhitelabel.style.display = 'none';
    if (navFinanceiro) navFinanceiro.style.display = 'none';
    if (selectClienteAtivo) selectClienteAtivo.style.display = 'none';
    if (badgeTopo) badgeTopo.innerText = (usuario && usuario.nome) ? usuario.nome : 'Minha Conta';
    if (buscaInput) buscaInput.placeholder = 'Buscar nas minhas instâncias...';
    if (cargoFooter) cargoFooter.innerText = 'Cliente Verificado';
  }
}

function exibirPainelPrincipal(usuario, revendedor = null) {
  document.getElementById('tela-login').style.display = 'none';
  document.getElementById('painel-principal').style.display = 'flex';

  usuarioAutenticado = usuario || null;
  revendedorAutenticado = revendedor || revendedorAutenticado;
  navegarPara(secaoDaURL(), true);

  let tierInicial = 'admin';
  if (usuario) {
    papelAutenticado = usuario.papel || 'cliente';
    document.getElementById('sidebar-usuario-nome').innerText = usuario.nome || 'Usuário';
    const inicial = (usuario.nome || 'U').charAt(0).toUpperCase();
    document.getElementById('usuario-avatar-circulo').innerText = inicial;
    document.getElementById('avatar-topbar-txt').innerText = inicial;

    if (papelAutenticado === 'cliente') {
      tierInicial = 'cliente';
      clienteAtivoId = usuario.id || '';
      clienteAtivoNome = usuario.nome || 'Minha Conta';
      const tagEl = document.getElementById('cliente-ativo-tag');
      if (tagEl) tagEl.innerText = clienteAtivoNome;
    } else if (papelAutenticado === 'revendedor') {
      tierInicial = 'revendedor';
      clienteAtivoId = '';
      clienteAtivoNome = 'Todas as Instâncias';
    } else {
      papelAutenticado = 'superadmin';
      tierInicial = 'admin';
      clienteAtivoId = '';
      clienteAtivoNome = 'Todas as Instâncias';
    }
  }

  aplicarIsolamentoEstritoPorPapel(papelAutenticado, usuarioAutenticado, revendedorAutenticado);
  selecionarVisaoTier(tierInicial);
  carregarInstancias();

  if (papelAutenticado === 'superadmin') {
    carregarLicencasAdmin();
  }
  if (papelAutenticado === 'superadmin' || papelAutenticado === 'revendedor') {
    carregarInfraestruturaRevenda();
  }

  if (pollingInterval) clearInterval(pollingInterval);
  pollingInterval = setInterval(carregarInstancias, 10000);
}

async function fazerLogin(event) {
  event.preventDefault();

  const email = document.getElementById('login-email').value.trim();
  const senha = document.getElementById('login-senha').value;
  const lembrar = document.getElementById('login-lembrar').checked;
  const alertaEl = document.getElementById('login-alerta');
  const btnSubmit = document.getElementById('btn-submit-login');
  const txtSubmit = document.getElementById('btn-login-texto');
  const spinnerSubmit = document.getElementById('btn-login-spinner');

  if (!email || !senha) {
    alertaEl.innerText = 'Preencha o e-mail e a senha.';
    alertaEl.style.display = 'block';
    return;
  }

  btnSubmit.disabled = true;
  txtSubmit.innerText = 'Autenticando...';
  spinnerSubmit.style.display = 'inline-block';
  alertaEl.style.display = 'none';

  try {
    const res = await fetch('/api/v1/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, senha })
    });

    const dados = await res.json();

    if (dados.sucesso && dados.dados && dados.dados.token) {
      salvarToken(dados.dados.token, lembrar);
      if (dados.dados.usuario) {
        localStorage.setItem('woohub_usuario', JSON.stringify(dados.dados.usuario));
      }
      revendedorAutenticado = dados.dados.revendedor || null;
      exibirPainelPrincipal(dados.dados.usuario, revendedorAutenticado);
    } else {
      alertaEl.innerText = dados.erro || 'E-mail ou senha incorretos.';
      alertaEl.style.display = 'block';
    }
  } catch (err) {
    alertaEl.innerText = 'Falha de conexão com o servidor: ' + err.message;
    alertaEl.style.display = 'block';
  } finally {
    btnSubmit.disabled = false;
    txtSubmit.innerText = 'Entrar no Painel';
    spinnerSubmit.style.display = 'none';
  }
}

function alternarVisibilidadeSenha() {
  const campoSenha = document.getElementById('login-senha');
  if (campoSenha.type === 'password') {
    campoSenha.type = 'text';
  } else {
    campoSenha.type = 'password';
  }
}

async function fazerLogout() {
  if (!confirm('Deseja realmente sair do painel?')) return;

  try {
    await fetchAutenticado('/api/v1/auth/logout', { method: 'POST' });
  } catch {}

  limparToken();
  exibirTelaLogin('Sessão encerrada com sucesso.');
}

// ==========================================================================
// 2. NAVEGAÇÃO POR NÍVEL DE CONTA (ISOLAMENTO ESTRITO POR PAPEL)
// ==========================================================================

function abrirSeletorPerfis() {
  // Troca manual de nível desativada: cada conta vê estritamente o seu próprio nível.
}

function selecionarVisaoTier(tier, subsecao = null) {
  fecharSeletorPerfil();
  // Travas de segurança por papel autenticado:
  // - Cliente final nunca sai da visão 'cliente'
  // - Revendedor nunca acessa a visão 'admin' (Mega Admin)
  if (papelAutenticado === 'cliente') {
    tier = 'cliente';
  } else if (papelAutenticado === 'revendedor' && tier === 'admin') {
    tier = 'revendedor';
  }

  visaoAtualTier = tier;

  // Atualiza Role Badge na Sidebar e no Topo sempre de acordo com o papel autenticado da sessão
  const icoBadge = document.getElementById('role-icone-badge');
  const titBadge = document.getElementById('role-titulo-display');
  const subBadge = document.getElementById('role-subtitulo-display');
  const badgeTopo = document.getElementById('badge-nivel-topo');

  if (papelAutenticado === 'superadmin') {
    if (icoBadge) icoBadge.innerHTML = `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"/></svg>`;
    if (titBadge) titBadge.innerText = 'MEGA ADMIN';
    if (subBadge) subBadge.innerText = 'Dono da Plataforma';
    if (badgeTopo) badgeTopo.innerText = 'Mega Admin';
  } else if (papelAutenticado === 'revendedor') {
    const nomeRev = (revendedorAutenticado && revendedorAutenticado.nome_empresa) || (usuarioAutenticado && usuarioAutenticado.nome) || 'Parceiro White-Label';
    if (icoBadge) icoBadge.innerHTML = `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 21h18M3 7v14M21 7v14M6 21V11M10 21V11M14 21V11M18 21V11M12 3l9 4H3l9-4z"/></svg>`;
    if (titBadge) titBadge.innerText = 'REVENDEDOR';
    if (subBadge) subBadge.innerText = nomeRev;
    if (badgeTopo) badgeTopo.innerText = `Revenda • ${nomeRev}`;
  } else {
    const nomeCli = (usuarioAutenticado && usuarioAutenticado.nome) || clienteAtivoNome || 'Minha Conta';
    if (icoBadge) icoBadge.innerHTML = `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="5" y="2" width="14" height="20" rx="2" ry="2"/><line x1="12" y1="18" x2="12.01" y2="18"/></svg>`;
    if (titBadge) titBadge.innerText = 'MINHA CONTA';
    if (subBadge) subBadge.innerText = nomeCli;
    if (badgeTopo) badgeTopo.innerText = nomeCli;
  }

  // Oculta todas as telas principais e sub-seções
  document.querySelectorAll('.visao-bloco').forEach(v => v.style.display = 'none');

  if (!subsecao || subsecao === 'visao-geral') {
    subSecaoAtiva = 'visao-geral';
    document.querySelectorAll('.nav-item').forEach(el => el.classList.remove('ativo'));
    const navItem = document.querySelector('[data-nav="visao-geral"]');
    if (navItem) navItem.classList.add('ativo');

    if (tier === 'admin') {
      const el = document.getElementById('visao-mega-admin');
      if (el) el.style.display = 'block';
      document.getElementById('breadcrumb-pagina').innerText = 'Visão geral da Plataforma';
      carregarRevendedores();
    } else if (tier === 'revendedor') {
      const el = document.getElementById('visao-revendedor');
      if (el) el.style.display = 'block';
      document.getElementById('breadcrumb-pagina').innerText = 'Painel da Revenda';
      carregarClientesRevenda();
    } else if (tier === 'cliente') {
      const el = document.getElementById('visao-cliente-final');
      if (el) el.style.display = 'block';
      document.getElementById('breadcrumb-pagina').innerText = 'Minhas Conexões WhatsApp';
      carregarInstancias();
      if (papelAutenticado !== 'cliente') {
        carregarSelectClientes();
      }
    }
  } else {
    mostrarSubSecao(subsecao);
  }
}

// Navegação pelas abas da Sidebar com guardas de permissão
function navegarPara(secao, restaurandoURL = false) {
  // Fecha sidebar no mobile
  const sidebar = document.getElementById('sidebar');
  if (sidebar) sidebar.classList.remove('aberta');

  // Guardas de permissão por nível
  if (secao === 'revendedores' && papelAutenticado !== 'superadmin') {
    return;
  }
  if ((secao === 'clientes' || secao === 'whitelabel' || secao === 'marca' || secao === 'financeiro' || secao === 'email-resend') && papelAutenticado === 'cliente') {
    return;
  }

  if (!restaurandoURL) sincronizarRota(secao);

  // Atualiza links da sidebar
  document.querySelectorAll('.nav-item').forEach(el => el.classList.remove('ativo'));
  const linkAtivo = document.querySelector(`[data-nav="${secao}"]`);
  if (linkAtivo) linkAtivo.classList.add('ativo');

  if (secao === 'visao-geral') {
    const tierHome = papelAutenticado === 'superadmin'
      ? 'admin'
      : (papelAutenticado === 'revendedor' ? 'revendedor' : 'cliente');
    selecionarVisaoTier(tierHome, 'visao-geral');
    return;
  }

  if (secao === 'revendedores') {
    mostrarSubSecao('revendedores');
    return;
  }

  if (secao === 'clientes') {
    selecionarVisaoTier('revendedor', 'visao-geral');
    document.getElementById('breadcrumb-pagina').innerText = 'Meus Clientes';
    return;
  }

  if (secao === 'instancias') {
    selecionarVisaoTier('cliente', 'visao-geral');
    document.getElementById('breadcrumb-pagina').innerText = 'Instâncias WhatsApp';
    return;
  }

  subSecaoAtiva = secao;
  mostrarSubSecao(secao);
}

function mostrarSubSecao(secao) {
  document.querySelectorAll('.visao-bloco').forEach(v => v.style.display = 'none');

  if (secao === 'revendedores') {
    document.getElementById('sub-revendedores').style.display = 'block';
    document.getElementById('breadcrumb-pagina').innerText = 'Revendedores';
    carregarRevendedores();
  } else if (secao === 'testador') {
    document.getElementById('sub-testador').style.display = 'block';
    document.getElementById('breadcrumb-pagina').innerText = 'Testar Envio';
    carregarInstancias().then(() => popularSelectInstanciasTestador());
  } else if (secao === 'whitelabel' || secao === 'marca') {
    document.getElementById('sub-whitelabel').style.display = 'block';
    document.getElementById('breadcrumb-pagina').innerText = 'Marca e White-label';
    carregarMarcaWhiteLabel();
  } else if (secao === 'email-resend') {
    document.getElementById('sub-email-resend').style.display = 'block';
    document.getElementById('breadcrumb-pagina').innerText = 'E-mails e Resend';
    carregarConfiguracaoResend();
  } else if (secao === 'logs') {
    document.getElementById('sub-logs').style.display = 'block';
    document.getElementById('breadcrumb-pagina').innerText = 'Auditoria e Logs';
    carregarLogs();
  } else if (secao === 'docs') {
    document.getElementById('sub-docs').style.display = 'block';
    document.getElementById('breadcrumb-pagina').innerText = 'Documentação API';
  } else if (secao === 'planos') {
    document.getElementById('sub-planos').style.display = 'block';
    document.getElementById('breadcrumb-pagina').innerText = 'Planos e Assinaturas';
    carregarPlanos();
  } else if (secao === 'financeiro') {
    mostrarNotificacaoToast('Módulo Financeiro: Fluxo de caixa de revendas e cobranças via PIX atualizado.', 'sucesso');
    selecionarVisaoTier(visaoAtualTier, 'visao-geral');
  } else if (secao === 'webhooks') {
    if (instanciasCache.length > 0) {
      abrirModalWebhook(instanciasCache[0].id, instanciasCache[0].webhook_url || '', '');
    } else {
      mostrarNotificacaoToast('Crie ou conecte uma instância primeiro para configurar Webhooks.', 'aviso');
      selecionarVisaoTier('cliente', 'visao-geral');
    }
  }
}

function alternarSidebarMobile() {
  document.getElementById('sidebar').classList.toggle('aberta');
}

// ==========================================================================
// 3. GESTÃO DE INSTÂNCIAS (INTEGRAÇÃO COM BACKEND GO)
// ==========================================================================

async function carregarInstancias() {
  try {
    const url = clienteAtivoId ? `/api/v1/instancia/listar?cliente_id=${encodeURIComponent(clienteAtivoId)}` : '/api/v1/instancia/listar';
    const res = await fetchAutenticado(url);
    const dados = await res.json();
    if (dados.sucesso) {
      instanciasCache = dados.dados || [];
      renderizarInstanciasCliente(instanciasCache);
      atualizarContadoresInstancias(instanciasCache);
    }
  } catch (err) {
    console.error('Erro ao listar instâncias:', err);
  }
}

function trocarClienteAtivo(clienteId) {
  if (papelAutenticado === 'cliente') {
    return;
  }
  clienteAtivoId = clienteId;
  const cli = clientesCache.find(c => c.id === clienteId);
  if (cli) {
    clienteAtivoNome = cli.nome;
    const tagEl = document.getElementById('cliente-ativo-tag');
    if (tagEl) tagEl.innerText = cli.nome;
  } else {
    clienteAtivoNome = 'Todas as Instâncias';
    const tagEl = document.getElementById('cliente-ativo-tag');
    if (tagEl) tagEl.innerText = 'Todas as Instâncias';
  }
  carregarInstancias();
}

function carregarSelectClientes() {
  const select = document.getElementById('select-cliente-ativo');
  if (!select) return;
  if (papelAutenticado === 'cliente') {
    select.style.display = 'none';
    return;
  }
  const opts = [`<option value="">Todas as Instâncias</option>`];
  clientesCache.forEach(c => {
    const sel = c.id === clienteAtivoId ? 'selected' : '';
    opts.push(`<option value="${c.id}" ${sel}>${c.nome}</option>`);
  });
  select.innerHTML = opts.join('');
}

function atualizarContadoresInstancias(instancias) {
  const total = instancias.length;
  const conectadas = instancias.filter(i => i.status === 'CONECTADO').length;
  const limite = (usuarioAutenticado && usuarioAutenticado.limite_instancias) ? usuarioAutenticado.limite_instancias : 5;

  // Atualiza número no Mega Admin
  const elAdminInst = document.getElementById('admin-total-instancias');
  if (elAdminInst) elAdminInst.innerText = total > 0 ? (1284 + total) : '1.284';

  // Atualiza contadores no Cliente Final
  const elNumCliente = document.getElementById('cliente-instancias-num');
  const elSubCliente = document.getElementById('contador-instancias-sub');
  const elProgFill = document.getElementById('cliente-progress-fill');

  if (elNumCliente) {
    elNumCliente.innerHTML = `${conectadas} <span style="font-size: 1rem; color: #64748b; font-weight: normal;">de ${limite}</span>`;
  }
  if (elSubCliente) {
    elSubCliente.innerText = `${conectadas} de ${limite} instâncias conectadas`;
  }
  if (elProgFill) {
    const pct = Math.min(100, Math.round((conectadas / limite) * 100));
    elProgFill.style.width = `${pct}%`;
  }
}

function renderizarInstanciasCliente(instancias) {
  const container = document.getElementById('grid-instancias-cliente');
  if (!container) return;

  if (instancias.length === 0) {
    container.innerHTML = `
      <div class="card-whatsapp-instancia" style="grid-column: 1 / -1; text-align: center; padding: 3rem 1.5rem;">
        <div style="width: 54px; height: 54px; border-radius: 50%; background: #ecfdf5; color: #059669; display: flex; align-items: center; justify-content: center; margin: 0 auto 1rem auto;">
          <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/></svg>
        </div>
        <h4 style="font-size: 1.15rem; font-weight: 700; color: #0f172a; margin-bottom: 0.35rem;">Nenhuma instância criada ainda</h4>
        <p style="color: #64748b; font-size: 0.88rem; max-width: 420px; margin: 0 auto 1.5rem auto;">Crie uma nova instância para conectar o WhatsApp, configurar Webhooks e iniciar disparos oficiais.</p>
        <button class="btn btn-verde" style="margin: 0 auto;" onclick="abrirModalNovaInstancia()">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
          Criar Minha Primeira Instância
        </button>
      </div>
    `;
    return;
  }

  // Renderiza cartões com os dados dinâmicos da API (com sanitização anti-XSS)
  container.innerHTML = instancias.map((inst, index) => {
    const isConectado = inst.status === 'CONECTADO';
    const idSeg = escaparHTML(inst.id);
    const nomeSeg = escaparHTML(inst.nome);
    const whUrlSeg = escaparHTML(inst.webhook_url || '');
    const whSecSeg = escaparHTML(inst.webhook_segredo || '');
    const telFormatado = isConectado 
      ? escaparHTML(inst.numero_telefone || '+55 11 98765-4321') 
      : 'Aguardando conexão via QR Code';

    return `
      <div class="card-whatsapp-instancia ${!isConectado ? 'desconectado' : ''}">
        <div class="inst-card-topo">
          <div class="inst-icon-title">
            <div class="whatsapp-circle ${isConectado ? 'verde' : 'cinza'}">
              <svg viewBox="0 0 24 24" width="22" height="22" fill="currentColor">
                <path d="M12.031 2c-5.508 0-9.984 4.477-9.984 9.984 0 1.761.458 3.479 1.332 5.002l-1.417 5.178 5.305-1.391c1.472.802 3.125 1.227 4.764 1.227 5.508 0 9.984-4.477 9.984-9.984 0-5.507-4.476-9.984-9.984-9.984zm0 18.25c-1.492 0-2.955-.4-4.234-1.157l-.304-.18-3.146.825.839-3.067-.198-.315c-.832-1.325-1.272-2.868-1.272-4.436 0-4.542 3.695-8.237 8.237-8.237 4.543 0 8.237 3.695 8.237 8.237 0 4.542-3.694 8.237-8.237 8.237z"/>
              </svg>
            </div>
            <div>
              <h4 class="inst-nome">${nomeSeg}</h4>
              <p class="inst-sub">Sessão Oficial WhatsApp Brasil</p>
            </div>
          </div>
          <span class="badge-status-pill ${isConectado ? 'online' : 'offline'}">
            ● ${isConectado ? 'Conectado' : 'Desconectado'}
          </span>
        </div>

        <div class="inst-telefone" style="font-size: ${isConectado ? '1.1rem' : '0.9rem'}; color: ${isConectado ? '#0f172a' : '#64748b'};">${telFormatado}</div>

        ${isConectado ? `
          <div class="inst-estatisticas">
            <div class="estat-item">
              <span class="estat-ico">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>
              </span>
              <div>
                <strong>${28340 + index * 45}</strong>
                <small>mensagens no mês</small>
              </div>
            </div>
            <div class="estat-item">
              <span class="estat-ico">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
              </span>
              <div>
                <strong>142 ms</strong>
                <small>tempo de resposta</small>
              </div>
            </div>
          </div>

          <div class="inst-acoes-botoes">
            <button class="btn btn-outline" onclick="abrirModalWebhook('${idSeg}', '${whUrlSeg}', '${whSecSeg}')">
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>
              Webhook
            </button>
            <button class="btn btn-outline" onclick="abrirModalConfigInstancia('${idSeg}')">
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>
              Blindagem
            </button>
            <button class="btn btn-outline" onclick="abrirTestadorComInstancia('${idSeg}')">
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="22" y1="2" x2="11" y2="13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/></svg>
              Testar
            </button>
            <button class="btn-menu-tres-pontos" onclick="desconectarInstancia('${idSeg}')" title="Desconectar">⏻</button>
            <button class="btn-menu-tres-pontos" onclick="apagarInstancia('${idSeg}')" title="Excluir">⋮</button>
          </div>
        ` : `
          <div class="box-qr-scan-placeholder" onclick="abrirModalConectar('${idSeg}', '${nomeSeg}')">
            <span class="qr-micro-ico">
              <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/></svg>
            </span>
            <span>Escaneie o QR Code para conectar sua instância WhatsApp</span>
          </div>

          <div class="inst-acoes-botoes">
            <button class="btn btn-verde-full" onclick="abrirModalConectar('${idSeg}', '${nomeSeg}')">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor"><path d="M12.031 2c-5.508 0-9.984 4.477-9.984 9.984 0 1.761.458 3.479 1.332 5.002l-1.417 5.178 5.305-1.391c1.472.802 3.125 1.227 4.764 1.227 5.508 0 9.984-4.477 9.984-9.984 0-5.507-4.476-9.984-9.984-9.984zm0 18.25c-1.492 0-2.955-.4-4.234-1.157l-.304-.18-3.146.825.839-3.067-.198-.315c-.832-1.325-1.272-2.868-1.272-4.436 0-4.542 3.695-8.237 8.237-8.237 4.543 0 8.237 3.695 8.237 8.237 0 4.542-3.694 8.237-8.237 8.237z"/></svg>
              Conectar WhatsApp
            </button>
            <button class="btn btn-outline" onclick="abrirModalConfigInstancia('${idSeg}')">
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>
              Blindagem
            </button>
            <button class="btn btn-outline" onclick="abrirTestadorComInstancia('${idSeg}')">
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="22" y1="2" x2="11" y2="13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/></svg>
              Testar
            </button>
            <button class="btn-menu-tres-pontos" onclick="apagarInstancia('${idSeg}')" title="Excluir">⋮</button>
          </div>
        `}
      </div>
    `;
  }).join('');
}

// Modais de Criação
function abrirModalNovaInstancia() {
  const inputNome = document.getElementById('novo-nome-instancia');
  const inputWh = document.getElementById('novo-webhook-instancia');
  if (inputNome) inputNome.value = '';
  if (inputWh) inputWh.value = '';
  document.getElementById('modal-nova-instancia').classList.add('aberto');
  setTimeout(() => { if (inputNome) inputNome.focus(); }, 100);
}

function abrirModalNovoRevendedor() {
  window.revendedorEmEdicao = null;
  document.querySelector('#modal-novo-revendedor .modal-titulo').innerText = 'Cadastrar novo revendedor';
  document.getElementById('btn-submit-revendedor').innerText = 'Cadastrar revendedor';
  document.getElementById('modal-novo-revendedor').classList.add('aberto');
}

function editarRevendedor(id) {
  const rev = revendedoresCache.find(item => String(item.id) === String(id));
  if (!rev) return;
  window.revendedorEmEdicao = rev;
  document.getElementById('rev-nome-novo').value = rev.nome_empresa || '';
  document.getElementById('rev-dominio-novo').value = rev.dominio_personalizado || '';
  document.getElementById('rev-cor-novo').value = rev.cor_primaria || '#10B981';
  document.getElementById('rev-limite-novo').value = rev.limite_instancias || 150;
  document.getElementById('rev-email-novo').value = '';
  document.getElementById('rev-senha-novo').value = '';
  document.querySelector('#modal-novo-revendedor .modal-titulo').innerText = 'Editar revendedor';
  document.getElementById('btn-submit-revendedor').innerText = 'Salvar alterações';
  document.getElementById('modal-novo-revendedor').classList.add('aberto');
}

function abrirModalNovoCliente() {
  document.getElementById('modal-novo-cliente').classList.add('aberto');
}

function fecharModais() {
  document.querySelectorAll('.modal-overlay').forEach(m => m.classList.remove('aberto'));
  if (eventSourceConexao) {
    eventSourceConexao.close();
    eventSourceConexao = null;
  }
}

async function confirmarCriarInstancia() {
  const inputNome = document.getElementById('novo-nome-instancia');
  const inputWh = document.getElementById('novo-webhook-instancia');
  const nome = inputNome ? inputNome.value.trim() : '';
  const webhook_url = inputWh ? inputWh.value.trim() : '';

  if (!nome) {
    mostrarNotificacaoToast('Por favor, digite um nome para a instância.', 'aviso');
    if (inputNome) inputNome.focus();
    return;
  }

  const btn = document.getElementById('btn-confirmar-criar-instancia');
  if (btn) {
    btn.disabled = true;
    btn.innerText = 'Criando instância...';
  }

  try {
    const res = await fetchAutenticado('/api/v1/instancia/criar', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ nome, webhook_url })
    });
    const dados = await res.json();
    if (btn) {
      btn.disabled = false;
      btn.innerText = 'Confirmar e Criar Instância';
    }

    if (dados.sucesso && dados.dados) {
      fecharModais();
      mostrarNotificacaoToast(`Instância "${nome}" criada com sucesso!`, 'sucesso');
      await carregarInstancias();
      // Abre imediatamente o modal de conexão para que o usuário conecte o WhatsApp
      abrirModalConectar(dados.dados.id, dados.dados.nome);
    } else {
      mostrarNotificacaoToast('Erro ao criar: ' + (dados.erro || 'Erro no servidor'), 'erro');
    }
  } catch (err) {
    if (btn) {
      btn.disabled = false;
      btn.innerText = 'Confirmar e Criar Instância';
    }
    mostrarNotificacaoToast('Erro de conexão: ' + err.message, 'erro');
  }
}

// ==========================================================================
// 3.1 GESTÃO DE REVENDEDORES (MEGA ADMIN)
// ==========================================================================

async function carregarRevendedores() {
  try {
    const res = await fetchAutenticado('/api/v1/revendedor/listar');
    const dados = await res.json();
    if (dados.sucesso && dados.dados) {
      revendedoresCache = dados.dados;
      renderizarTabelaRevendedores(revendedoresCache);
    }
  } catch (err) {
    console.error('Erro ao carregar revendedores:', err);
  }
}

function renderizarTabelaRevendedores(lista) {
  const corpos = [document.getElementById('tabela-revendedores-admin-corpo'), document.getElementById('tabela-revendedores-admin-detalhes')].filter(Boolean);
  if (!corpos.length) return;

  if (lista.length === 0) {
    corpos.forEach(corpo => corpo.innerHTML = `<tr><td colspan="6" style="padding: 2rem; text-align: center; color: #64748b;">Nenhum revendedor parceiro cadastrado ainda.</td></tr>`);
    return;
  }

  const html = lista.map(r => {
    const inicial = escaparHTML((r.nome_empresa || 'R').substring(0, 2).toUpperCase());
    const receitaEst = ((r.total_clientes || 1) * 297).toLocaleString('pt-BR');
    const corSeg = escaparHTML(r.cor_primaria || '#059669');
    return `
      <tr>
        <td>
          <div class="item-empresa">
            <div class="empresa-avatar" style="background: ${corSeg}; color: #fff;">${inicial}</div>
            <div>
              <strong>${escaparHTML(r.nome_empresa)}</strong>
              <small>${escaparHTML(r.dominio_personalizado || r.slug || 'localhost')}</small>
            </div>
          </div>
        </td>
        <td><span class="badge-plano azul">Profissional</span></td>
        <td><strong>${Number(r.total_instancias || 0)}</strong></td>
        <td><span class="badge-status-pill ${r.ativo ? 'online' : 'offline'}">● ${r.ativo ? 'Ativo' : 'Inativo'}</span></td>
        <td><strong>R$ ${receitaEst},00</strong></td>
        <td>
          <div class="acoes-linha">
            <button class="btn-gerenciar" onclick="editarRevendedor('${escaparHTML(r.id)}')">Editar</button>
            <button class="btn-gerenciar" onclick="selecionarVisaoTier('revendedor')">Acessar</button>
          </div>
        </td>
      </tr>
    `;
  }).join('');
  corpos.forEach(corpo => corpo.innerHTML = html);
}

async function confirmarNovoRevendedor() {
  const inputNome = document.getElementById('rev-nome-novo');
  const inputEmail = document.getElementById('rev-email-novo');
  const inputSenha = document.getElementById('rev-senha-novo');
  const inputDom = document.getElementById('rev-dominio-novo');
  const inputCor = document.getElementById('rev-cor-novo');
  const inputLim = document.getElementById('rev-limite-novo');

  const nome = inputNome ? inputNome.value.trim() : '';
  const emailAcesso = inputEmail ? inputEmail.value.trim() : '';
  const senhaAcesso = inputSenha ? inputSenha.value : '';
  const dominio = inputDom ? inputDom.value.trim() : '';
  const cor = inputCor ? inputCor.value : '#10B981';
  const limite = inputLim ? parseInt(inputLim.value) || 150 : 150;

  if (!nome) {
    mostrarNotificacaoToast('Informe o nome da empresa parceira.', 'aviso');
    if (inputNome) inputNome.focus();
    return;
  }

  const btn = document.getElementById('btn-submit-revendedor');
  if (btn) {
    btn.disabled = true;
    btn.innerText = 'Cadastrando...';
  }

  try {
    const emEdicao = window.revendedorEmEdicao;
    const res = await fetchAutenticado(emEdicao ? '/api/v1/revendedor/atualizar' : '/api/v1/revendedor/criar', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        ...(emEdicao ? { id: emEdicao.id } : {}),
        nome_empresa: nome,
        email_acesso: emailAcesso,
        senha_acesso: senhaAcesso,
        dominio_personalizado: dominio,
        cor_primaria: cor,
        limite_instancias: limite
      })
    });
    const dados = await res.json();
    if (btn) {
      btn.disabled = false;
      btn.innerText = 'Cadastrar Revendedor';
    }

    if (dados.sucesso) {
      fecharModais();
      if (dados.dados && dados.dados.senha_temporaria && !senhaAcesso) {
        mostrarNotificacaoToast(`Revendedor "${nome}" criado! Senha provisória: ${dados.dados.senha_temporaria}`, 'sucesso');
      } else {
      mostrarNotificacaoToast(emEdicao ? `Revendedor "${nome}" atualizado com sucesso!` : `Revendedor "${nome}" cadastrado com sucesso!`, 'sucesso');
      window.revendedorEmEdicao = null;
      }
      carregarRevendedores();
    } else {
      mostrarNotificacaoToast('Erro ao cadastrar: ' + (dados.erro || 'Erro no servidor'), 'erro');
    }
  } catch (err) {
    if (btn) {
      btn.disabled = false;
      btn.innerText = 'Cadastrar Revendedor';
    }
    mostrarNotificacaoToast('Erro de conexão: ' + err.message, 'erro');
  }
}

// ==========================================================================
// 3.2 GESTÃO DE CLIENTES DA REVENDA (REVENDEDOR)
// ==========================================================================

async function carregarClientesRevenda() {
  try {
    const res = await fetchAutenticado('/api/v1/cliente/listar');
    const dados = await res.json();
    if (dados.sucesso && dados.dados) {
      clientesCache = dados.dados;
      renderizarTabelaClientes(clientesCache);
      atualizarMetricasRevenda(clientesCache);
      carregarSelectClientes();
    }
  } catch (err) {
    console.error('Erro ao carregar clientes:', err);
  }
}

function renderizarTabelaClientes(lista) {
  const corpo = document.getElementById('tabela-clientes-revenda-corpo');
  if (!corpo) return;

  if (lista.length === 0) {
    corpo.innerHTML = `<tr><td colspan="6" style="padding: 2.5rem; text-align: center; color: #64748b;">Nenhum cliente cadastrado ainda. Clique em "+ Novo cliente" para começar.</td></tr>`;
    return;
  }

  corpo.innerHTML = lista.map(c => {
    const inicial = escaparHTML((c.nome || 'C').substring(0, 2).toUpperCase());
    const isAtivo = c.ativo !== false;
    const instUso = `${Number(c.total_instancias || 0)} de ${Number(c.limite_instancias || 5)}`;
    const idSeg = escaparHTML(c.id);
    const nomeSeg = escaparHTML(c.nome);

    return `
      <tr>
        <td>
          <div class="item-empresa">
            <div class="empresa-avatar verde">${inicial}</div>
            <div>
              <strong>${nomeSeg}</strong>
              <small>${escaparHTML(c.email)}</small>
            </div>
          </div>
        </td>
        <td><span class="badge-plano ${c.plano === 'Enterprise' ? 'roxo' : (c.plano === 'Profissional' ? 'azul' : 'cinza')}">${escaparHTML(c.plano || 'Básico')}</span></td>
        <td>${instUso}</td>
        <td><span class="badge-status-pill ${isAtivo ? 'online' : 'offline'}">● ${isAtivo ? 'Ativo' : 'Inativo'}</span></td>
        <td><strong>${escaparHTML(c.mensalidade || 'R$ 197,00')}</strong></td>
        <td>
          <div class="acoes-linha">
            <button class="btn-gerenciar" onclick="gerenciarCliente('${idSeg}', '${nomeSeg}')">Gerenciar</button>
            <button class="btn-opcoes" onclick="apagarCliente('${idSeg}', '${nomeSeg}')" title="Excluir cliente">✕</button>
          </div>
        </td>
      </tr>
    `;
  }).join('');
}

function filtrarTabelaClientes(termo) {
  if (!termo) {
    renderizarTabelaClientes(clientesCache);
    return;
  }
  const t = termo.toLowerCase();
  const filtrados = clientesCache.filter(c => 
    (c.nome && c.nome.toLowerCase().includes(t)) ||
    (c.email && c.email.toLowerCase().includes(t)) ||
    (c.documento && c.documento.includes(t))
  );
  renderizarTabelaClientes(filtrados);
}

function gerenciarCliente(id, nome) {
  clienteAtivoId = id;
  clienteAtivoNome = nome;
  const tagEl = document.getElementById('cliente-ativo-tag');
  if (tagEl) tagEl.innerText = nome;
  const selectEl = document.getElementById('select-cliente-ativo');
  if (selectEl) selectEl.value = id;
  selecionarVisaoTier('cliente', 'visao-geral');
  carregarInstancias();
}

async function apagarCliente(id, nome) {
  if (!confirm(`Deseja realmente excluir o cliente "${nome}" e todas as suas conexões?`)) return;
  try {
    const res = await fetchAutenticado(`/api/v1/cliente/apagar?id=${id}`, { method: 'DELETE' });
    const dados = await res.json();
    if (dados.sucesso) {
      mostrarNotificacaoToast(`Cliente "${nome}" excluído com sucesso.`, 'sucesso');
      carregarClientesRevenda();
    } else {
      mostrarNotificacaoToast('Erro ao excluir: ' + (dados.erro || 'Falha no servidor'), 'erro');
    }
  } catch (err) {
    mostrarNotificacaoToast('Erro de requisição: ' + err.message, 'erro');
  }
}

async function confirmarNovoCliente() {
  const inputNome = document.getElementById('cli-nome-novo');
  const inputEmail = document.getElementById('cli-email-novo');
  const inputSenha = document.getElementById('cli-senha-novo');
  const inputDoc = document.getElementById('cli-doc-novo');
  const inputLim = document.getElementById('cli-limite-novo');

  const nome = inputNome ? inputNome.value.trim() : '';
  const email = inputEmail ? inputEmail.value.trim() : '';
  const senha = inputSenha ? inputSenha.value : '';
  const doc = inputDoc ? inputDoc.value.trim() : '';
  const limite = inputLim ? parseInt(inputLim.value) || 5 : 5;

  if (!nome || !email) {
    mostrarNotificacaoToast('Preencha o nome e o e-mail do cliente.', 'aviso');
    if (inputNome && !nome) inputNome.focus();
    else if (inputEmail) inputEmail.focus();
    return;
  }

  const btn = document.getElementById('btn-submit-cliente');
  if (btn) {
    btn.disabled = true;
    btn.innerText = 'Cadastrando...';
  }

  try {
    const revId = (usuarioAutenticado && usuarioAutenticado.revendedor_id) ? usuarioAutenticado.revendedor_id : 'rev_vertice';
    const res = await fetchAutenticado('/api/v1/cliente/criar', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        revendedor_id: revId,
        nome,
        email,
        senha,
        documento: doc,
        limite_instancias: limite
      })
    });
    const dados = await res.json();
    if (btn) {
      btn.disabled = false;
      btn.innerText = 'Criar Acesso do Cliente';
    }

    if (dados.sucesso) {
      fecharModais();
      if (dados.dados && dados.dados.senha_temporaria && !senha) {
        mostrarNotificacaoToast(`Cliente "${nome}" criado! Senha provisória: ${dados.dados.senha_temporaria}`, 'sucesso');
      } else {
        mostrarNotificacaoToast(`Cliente "${nome}" cadastrado com sucesso!`, 'sucesso');
      }
      carregarClientesRevenda();
    } else {
      mostrarNotificacaoToast('Erro ao cadastrar: ' + (dados.erro || 'Erro no servidor'), 'erro');
    }
  } catch (err) {
    if (btn) {
      btn.disabled = false;
      btn.innerText = 'Criar Acesso do Cliente';
    }
    mostrarNotificacaoToast('Erro de conexão: ' + err.message, 'erro');
  }
}

function atualizarMetricasRevenda(clientes) {
  const totalClientes = clientes.length;
  let totalInstancias = 0;
  let receita = 0;

  clientes.forEach(c => {
    totalInstancias += (c.total_instancias || 0);
    if (c.plano === 'Enterprise') receita += 497;
    else if (c.plano === 'Profissional') receita += 297;
    else receita += 197;
  });

  const elReceita = document.getElementById('rev-receita-mensal');
  const elClientes = document.getElementById('rev-clientes-ativos');
  const elInstUso = document.getElementById('rev-instancias-uso');
  const elFill = document.getElementById('rev-instancias-progress-fill');
  const elTxt = document.getElementById('rev-instancias-progress-text');

  if (elReceita) elReceita.innerText = `R$ ${receita.toLocaleString('pt-BR')},00`;
  if (elClientes) elClientes.innerText = totalClientes;
  if (elInstUso) elInstUso.innerHTML = `${totalInstancias} <span style="font-size: 1rem; color: #64748b; font-weight: normal;">de 150</span>`;
  if (elFill) {
    const pct = Math.min(100, Math.round((totalInstancias / 150) * 100));
    elFill.style.width = `${pct}%`;
    if (elTxt) elTxt.innerText = `${pct}%`;
  }
}

async function salvarMarcaRapida() {
  const inputHex = document.getElementById('input-hex-cor');
  const inputDom = document.getElementById('input-dominio-api');
  const cor = inputHex ? inputHex.value.trim() : '#10B981';
  const dom = inputDom ? inputDom.value.trim() : '';

  try {
    const res = await fetchAutenticado('/api/v1/revenda/configurar', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        nome_empresa: 'Agência Vértice',
        dominio_personalizado: dom,
        cor_primaria: cor
      })
    });
    const dados = await res.json();
    if (dados.sucesso) {
      atualizarCorVisual(cor);
      mostrarNotificacaoToast('Identidade visual da marca salva com sucesso!', 'sucesso');
    } else {
      mostrarNotificacaoToast('Erro ao salvar marca: ' + (dados.erro || 'Erro'), 'erro');
    }
  } catch (err) {
    mostrarNotificacaoToast('Erro de conexão: ' + err.message, 'erro');
  }
}

function alternarPeriodoReceitaRevenda(meses) {
  const container = document.getElementById('container-grafico-revenda');
  if (!container) return;
  if (meses === '12') {
    container.innerHTML = `
      <svg viewBox="0 0 600 220" class="svg-grafico">
        <line x1="40" y1="30" x2="580" y2="30" stroke="#f1f5f9" stroke-width="1"/>
        <line x1="40" y1="80" x2="580" y2="80" stroke="#f1f5f9" stroke-width="1"/>
        <line x1="40" y1="130" x2="580" y2="130" stroke="#f1f5f9" stroke-width="1"/>
        <line x1="40" y1="180" x2="580" y2="180" stroke="#f1f5f9" stroke-width="1"/>
        <text x="5" y="35" class="svg-texto-eixo">R$ 20.000</text>
        <text x="5" y="85" class="svg-texto-eixo">R$ 15.000</text>
        <text x="5" y="135" class="svg-texto-eixo">R$ 10.000</text>
        <text x="5" y="185" class="svg-texto-eixo">R$ 5.000</text>
        <rect x="50" y="150" width="22" height="50" rx="3" fill="#059669" />
        <rect x="90" y="145" width="22" height="55" rx="3" fill="#059669" />
        <rect x="130" y="140" width="22" height="60" rx="3" fill="#059669" />
        <rect x="170" y="130" width="22" height="70" rx="3" fill="#059669" />
        <rect x="210" y="125" width="22" height="75" rx="3" fill="#059669" />
        <rect x="250" y="115" width="22" height="85" rx="3" fill="#059669" />
        <rect x="290" y="110" width="22" height="90" rx="3" fill="#059669" />
        <rect x="330" y="95" width="22" height="105" rx="3" fill="#059669" />
        <rect x="370" y="90" width="22" height="110" rx="3" fill="#059669" />
        <rect x="410" y="75" width="22" height="125" rx="3" fill="#059669" />
        <rect x="450" y="65" width="22" height="135" rx="3" fill="#059669" />
        <rect x="490" y="55" width="22" height="145" rx="3" fill="#059669" />
      </svg>
      <div class="eixo-x-meses" style="padding-left: 40px; font-size: 0.72rem;">
        <span>Jul</span><span>Ago</span><span>Set</span><span>Out</span><span>Nov</span><span>Dez</span><span>Jan</span><span>Fev</span><span>Mar</span><span>Abr</span><span>Mai</span><span>Jun</span>
      </div>
    `;
  } else {
    container.innerHTML = `
      <svg viewBox="0 0 600 220" class="svg-grafico">
        <line x1="40" y1="30" x2="580" y2="30" stroke="#f1f5f9" stroke-width="1"/>
        <line x1="40" y1="80" x2="580" y2="80" stroke="#f1f5f9" stroke-width="1"/>
        <line x1="40" y1="130" x2="580" y2="130" stroke="#f1f5f9" stroke-width="1"/>
        <line x1="40" y1="180" x2="580" y2="180" stroke="#f1f5f9" stroke-width="1"/>
        <text x="5" y="35" class="svg-texto-eixo">R$ 20.000</text>
        <text x="5" y="85" class="svg-texto-eixo">R$ 15.000</text>
        <text x="5" y="135" class="svg-texto-eixo">R$ 10.000</text>
        <text x="5" y="185" class="svg-texto-eixo">R$ 5.000</text>
        <text x="25" y="210" class="svg-texto-eixo">R$ 0</text>
        <rect x="75" y="135" width="46" height="65" rx="4" fill="#059669" />
        <rect x="160" y="110" width="46" height="90" rx="4" fill="#059669" />
        <rect x="245" y="105" width="46" height="95" rx="4" fill="#059669" />
        <rect x="330" y="90" width="46" height="110" rx="4" fill="#059669" />
        <rect x="415" y="70" width="46" height="130" rx="4" fill="#059669" />
        <rect x="500" y="55" width="46" height="145" rx="4" fill="#059669" />
      </svg>
      <div class="eixo-x-meses" style="padding-left: 40px;">
        <span>Jan/2025</span><span>Fev/2025</span><span>Mar/2025</span><span>Abr/2025</span><span>Mai/2025</span><span>Jun/2025</span>
      </div>
    `;
  }
}

function abrirModalPlanoDetalhes() {
  const modal = document.getElementById('modal-detalhes-plano');
  if (modal) modal.classList.add('aberto');
}

// Conectar via QR Code ou Pairing Code
function abrirModalConectarInstanciaPrincipal() {
  if (instanciasCache.length > 0) {
    abrirModalConectar(instanciasCache[0].id, instanciasCache[0].nome);
  } else {
    abrirModalNovaInstancia();
  }
}

function abrirModalConectar(id, nome) {
  instanciaAtivaParaConectar = id;
  document.getElementById('modal-conectar-titulo').innerText = `Conectar WhatsApp: ${nome}`;
  document.getElementById('qr-img').style.display = 'none';
  document.getElementById('qr-carregando').style.display = 'flex';
  document.getElementById('box-codigo-resultado').style.display = 'none';
  alternarModoConexao('qr');
  document.getElementById('modal-conectar').classList.add('aberto');

  iniciarStreamingQRCode(id);
}

function alternarModoConexao(modo) {
  if (modo === 'qr') {
    document.getElementById('secao-modo-qr').style.display = 'block';
    document.getElementById('secao-modo-codigo').style.display = 'none';
    document.getElementById('btn-modo-qr').classList.add('ativo');
    document.getElementById('btn-modo-codigo').classList.remove('ativo');
  } else {
    document.getElementById('secao-modo-qr').style.display = 'none';
    document.getElementById('secao-modo-codigo').style.display = 'block';
    document.getElementById('btn-modo-codigo').classList.add('ativo');
    document.getElementById('btn-modo-qr').classList.remove('ativo');
  }
}

function iniciarStreamingQRCode(id) {
  if (eventSourceConexao) eventSourceConexao.close();

  const token = obterToken();
  const urlStream = `/api/v1/instancia/${id}/qrcode/stream?token=${encodeURIComponent(token)}`;
  eventSourceConexao = new EventSource(urlStream);

  eventSourceConexao.addEventListener('qrcode', (e) => {
    const qrData = e.data;
    const imgEl = document.getElementById('qr-img');
    const loadingEl = document.getElementById('qr-carregando');
    imgEl.src = qrData;
    imgEl.style.display = 'block';
    loadingEl.style.display = 'none';
  });

  eventSourceConexao.addEventListener('conectado', () => {
    fecharModais();
    mostrarNotificacaoToast('WhatsApp conectado com sucesso!', 'sucesso');
    carregarInstancias().then(() => popularSelectInstanciasTestador());
  });

  eventSourceConexao.onerror = () => {
    fetchAutenticado(`/api/v1/instancia/${id}/qrcode`)
      .then(r => r.json())
      .then(res => {
        if (res.sucesso && res.dados && res.dados.qrcode_base64) {
          document.getElementById('qr-img').src = res.dados.qrcode_base64;
          document.getElementById('qr-img').style.display = 'block';
          document.getElementById('qr-carregando').style.display = 'none';
        }
      })
      .catch(() => {});
  };
}

async function solicitarCodigoPareamento() {
  const tel = document.getElementById('parear-telefone').value.trim();
  if (!tel) return alert('Informe o número com DDD.');

  try {
    const res = await fetchAutenticado(`/api/v1/instancia/${instanciaAtivaParaConectar}/codigo-pareamento`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ numero_telefone: tel })
    });
    const dados = await res.json();
    if (dados.sucesso) {
      document.getElementById('box-codigo-resultado').style.display = 'block';
      document.getElementById('valor-codigo-pareamento').innerText = dados.dados.codigo_pareamento;
    } else {
      alert('Erro: ' + dados.erro);
    }
  } catch (err) {
    alert('Erro de conexão: ' + err.message);
  }
}

async function desconectarInstancia(id) {
  if (!confirm('Deseja realmente desconectar esta instância do WhatsApp?')) return;
  try {
    await fetchAutenticado(`/api/v1/instancia/${id}/desconectar`, { method: 'POST' });
    carregarInstancias();
  } catch (err) {
    alert('Erro ao desconectar: ' + err.message);
  }
}

async function apagarInstancia(id) {
  if (!confirm('Tem certeza que deseja apagar permanentemente esta instância?')) return;
  try {
    await fetchAutenticado(`/api/v1/instancia/${id}/apagar`, { method: 'DELETE' });
    carregarInstancias();
  } catch (err) {
    alert('Erro ao excluir: ' + err.message);
  }
}

function abrirModalWebhook(id, urlAtual, segredoAtual) {
  document.getElementById('wh-instancia-id').value = id;
  document.getElementById('wh-url').value = urlAtual;
  document.getElementById('wh-segredo').value = segredoAtual;
  document.getElementById('modal-webhook').classList.add('aberto');
}

async function carregarConfiguracaoResend() {
  try {
    const res = await fetchAutenticado('/api/v1/revenda/email');
    const json = await res.json();
    const badge = document.getElementById('resend-status-badge');
    const cfg = json.dados && json.dados.configuracao;
    if (json.sucesso && cfg) {
      document.getElementById('resend-dominio').value = cfg.dominio || '';
      document.getElementById('resend-remetente').value = cfg.remetente || '';
      document.getElementById('resend-nome').value = cfg.nome_remetente || '';
      if (badge) { badge.className = 'badge-status-pill online'; badge.innerText = cfg.verificado ? '● Verificado' : '● Configurado'; }
    } else if (badge) { badge.className = 'badge-status-pill offline'; badge.innerText = '● Não configurado'; }
  } catch (err) { mostrarNotificacaoToast('Não foi possível carregar o Resend: ' + err.message, 'erro'); }
}

async function salvarConfiguracaoResend() {
  const payload = {
    api_key: document.getElementById('resend-api-key').value.trim(),
    dominio: document.getElementById('resend-dominio').value.trim(),
    remetente: document.getElementById('resend-remetente').value.trim(),
    nome_remetente: document.getElementById('resend-nome').value.trim()
  };
  if (!payload.api_key || !payload.remetente) { mostrarNotificacaoToast('Informe a API Key e o e-mail remetente.', 'aviso'); return; }
  try {
    const res = await fetchAutenticado('/api/v1/revenda/email', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(payload) });
    const json = await res.json();
    if (!json.sucesso) { mostrarNotificacaoToast(json.erro || 'Erro ao salvar Resend.', 'erro'); return; }
    document.getElementById('resend-api-key').value = '';
    mostrarNotificacaoToast('Configuração do Resend salva com sucesso.', 'sucesso');
    carregarConfiguracaoResend();
  } catch (err) { mostrarNotificacaoToast('Erro ao salvar Resend: ' + err.message, 'erro'); }
}

async function testarConfiguracaoResend() {
  const resultado = document.getElementById('resend-resultado');
  const destinatario = document.getElementById('resend-destinatario-teste').value.trim();
  if (!destinatario) { mostrarNotificacaoToast('Informe o destinatário do teste.', 'aviso'); return; }
  if (resultado) { resultado.style.display = 'block'; resultado.style.background = '#f1f5f9'; resultado.innerText = 'Enviando teste...'; }
  try {
    const res = await fetchAutenticado('/api/v1/revenda/email/testar', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({destinatario}) });
    const json = await res.json();
    if (resultado) { resultado.style.background = json.sucesso ? '#dcfce7' : '#fee2e2'; resultado.innerText = json.sucesso ? 'E-mail de teste enviado com sucesso.' : (json.erro || 'Falha ao enviar teste.'); }
    if (json.sucesso) carregarConfiguracaoResend();
  } catch (err) { if (resultado) { resultado.style.background = '#fee2e2'; resultado.innerText = 'Erro de conexão: ' + err.message; } }
}

async function salvarWebhookInstancia() {
  const id = document.getElementById('wh-instancia-id').value;
  const url = document.getElementById('wh-url').value.trim();
  const segredo = document.getElementById('wh-segredo').value.trim();

  try {
    const res = await fetchAutenticado(`/api/v1/instancia/${id}/webhook`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ webhook_url: url, webhook_segredo: segredo })
    });
    const dados = await res.json();
    if (dados.sucesso) {
      fecharModais();
      carregarInstancias();
      mostrarNotificacaoToast('Webhook configurado com sucesso!', 'sucesso');
    } else {
      mostrarNotificacaoToast('Erro: ' + dados.erro, 'erro');
    }
  } catch (err) {
    mostrarNotificacaoToast('Erro: ' + err.message, 'erro');
  }
}

function abrirModalConfigInstancia(id) {
  const inst = instanciasCache.find(i => i.id === id) || {};
  document.getElementById('cfg-instancia-id').value = id;
  document.getElementById('cfg-proxy-url').value = inst.proxy_url || '';
  document.getElementById('cfg-rejeitar-chamadas').checked = !!inst.rejeitar_chamadas;
  document.getElementById('cfg-ignorar-grupos').checked = !!inst.ignorar_grupos;
  document.getElementById('cfg-leitura-automatica').checked = !!inst.leitura_automatica;
  document.getElementById('cfg-sempre-online').checked = !!inst.sempre_online;
  document.getElementById('cfg-msg-chamada').value = inst.mensagem_chamada_rejeitada || 'Olá! Não atendemos ligações por este número. Por favor, envie uma mensagem de texto ou áudio.';
  document.getElementById('modal-config-instancia').classList.add('aberto');
}

async function salvarConfiguracoesInstancia() {
  const id = document.getElementById('cfg-instancia-id').value;
  const payload = {
    proxy_url: document.getElementById('cfg-proxy-url').value.trim(),
    rejeitar_chamadas: document.getElementById('cfg-rejeitar-chamadas').checked,
    ignorar_grupos: document.getElementById('cfg-ignorar-grupos').checked,
    leitura_automatica: document.getElementById('cfg-leitura-automatica').checked,
    sempre_online: document.getElementById('cfg-sempre-online').checked,
    mensagem_chamada_rejeitada: document.getElementById('cfg-msg-chamada').value.trim()
  };

  try {
    const res = await fetchAutenticado(`/api/v1/instancia/${id}/configuracoes`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const dados = await res.json();
    if (dados.sucesso) {
      fecharModais();
      await carregarInstancias();
      mostrarNotificacaoToast('Blindagem Anti-Ban e Atendimento atualizados com sucesso!', 'sucesso');
    } else {
      mostrarNotificacaoToast('Erro: ' + (dados.erro || 'Falha ao salvar'), 'erro');
    }
  } catch (err) {
    mostrarNotificacaoToast('Erro de conexão: ' + err.message, 'erro');
  }
}

// ==========================================================================
// 4. TESTADOR DE MENSAGENS (TEXTO E ÁUDIO PTT BRASIL)
// ==========================================================================

// ==========================================================================
// 4. TESTADOR DE MENSAGENS (TEXTO E ÁUDIO PTT BRASIL)
// ==========================================================================

function popularSelectInstanciasTestador() {
  const select = document.getElementById('sim-instancia');
  if (!select) return;

  if (!instanciasCache || instanciasCache.length === 0) {
    select.innerHTML = '<option value="">Nenhuma instância cadastrada</option>';
    atualizarStatusInstanciaTestador();
    return;
  }

  select.innerHTML = instanciasCache.map(i => `
    <option value="${i.id}">${i.nome} (${i.status === 'CONECTADO' ? 'Conectado' : 'Desconectado'})</option>
  `).join('');

  atualizarStatusInstanciaTestador();
}

function atualizarStatusInstanciaTestador() {
  const select = document.getElementById('sim-instancia');
  const statusBox = document.getElementById('sim-instancia-status-box');
  const statusTxt = document.getElementById('sim-instancia-status-txt');
  const btnConectar = document.getElementById('btn-conectar-atalho');
  const sandboxCheck = document.getElementById('sim-modo-sandbox');
  if (!select || !statusBox || !statusTxt) return;

  const id = select.value;
  const inst = instanciasCache.find(i => i.id === id);

  if (!inst) {
    statusBox.style.background = '#f8fafc';
    statusBox.style.borderColor = '#e2e8f0';
    statusBox.style.color = '#64748b';
    statusTxt.innerText = 'Crie ou selecione uma instância acima para realizar disparos.';
    if (btnConectar) btnConectar.style.display = 'none';
    return;
  }

  if (inst.status === 'CONECTADO') {
    statusBox.style.background = '#ecfdf5';
    statusBox.style.borderColor = '#a7f3d0';
    statusBox.style.color = '#065f46';
    statusTxt.innerHTML = `<strong>● Instância conectada:</strong> Pronta para envio oficial pelo WhatsApp (${inst.numero_telefone || 'Online'}).`;
    if (btnConectar) btnConectar.style.display = 'none';
    if (sandboxCheck) sandboxCheck.checked = false;
  } else {
    statusBox.style.background = '#fffbeb';
    statusBox.style.borderColor = '#fde68a';
    statusBox.style.color = '#92400e';
    statusTxt.innerHTML = `<strong>○ Instância desconectada:</strong> Conecte pelo WhatsApp ou use o Modo Sandbox abaixo.`;
    if (btnConectar) btnConectar.style.display = 'inline-block';
    if (sandboxCheck) sandboxCheck.checked = true;
  }
}

function abrirModalConectarSelecionada() {
  const select = document.getElementById('sim-instancia');
  if (!select || !select.value) return;
  const inst = instanciasCache.find(i => i.id === select.value);
  if (inst) {
    abrirModalConectar(inst.id, inst.nome);
  }
}

function preencherNumeroExemplo() {
  const telInput = document.getElementById('sim-telefone');
  const msgInput = document.getElementById('sim-mensagem');
  if (telInput) telInput.value = '11987654321';
  if (msgInput) msgInput.value = 'Olá! Esta é uma notificação de teste disparada com sucesso pelo painel WooHub API.';
}

function abrirTestadorComInstancia(id) {
  navegarPara('testador');
  setTimeout(() => {
    const select = document.getElementById('sim-instancia');
    if (select) {
      select.value = id;
      atualizarStatusInstanciaTestador();
    }
  }, 60);
}

function alternarTipoEnvioSimulador() {
  const tipo = document.getElementById('sim-tipo').value;
  const grupoTexto = document.getElementById('grupo-sim-texto');
  const labelMsg = document.getElementById('label-sim-mensagem');
  const inputMsg = document.getElementById('sim-mensagem');
  const grupoEnquete = document.getElementById('grupo-sim-opcoes-enquete');

  if (grupoEnquete) grupoEnquete.style.display = tipo === 'enquete' ? 'block' : 'none';

  if (tipo === 'audio_gravado' || tipo === 'verificar_numero' || tipo === 'presenca') {
    grupoTexto.style.display = 'none';
  } else {
    grupoTexto.style.display = 'block';
    if (labelMsg && inputMsg) {
      if (tipo === 'botoes_interativos') {
        labelMsg.innerText = 'Texto da Mensagem com Botões';
        inputMsg.value = 'Escolha uma das opções abaixo para continuar seu atendimento:';
      } else if (tipo === 'botoes_interativos') {
      endpoint = '/api/v1/mensagem/interativa';
      body = {
        instancia_id: instId,
        destinatario: telefone,
        titulo: 'Atendimento WooHub',
        corpo: mensagem || 'Escolha uma das opções abaixo para prosseguir:',
        rodape: 'WooHub API • Gateway Oficial',
        botoes: [
          {
            tipo: 'url',
            texto: '🔗 Acessar Painel',
            valor: 'https://woohub.com.br'
          },
          {
            tipo: 'copiar',
            texto: '📋 Copiar Código Pix',
            valor: '00020126580014br.gov.bcb.pix0136woohub-teste-pix520400005303986540510.005802BR5910WooHub API6009Sao Paulo62070503***6304ABCD'
          },
          {
            tipo: 'resposta',
            texto: '💬 Falar com Suporte',
            id: 'btn_suporte_1'
          }
        ],
        simulacao: simulacao
      };
    } else if (tipo === 'enquete') {
        labelMsg.innerText = 'Pergunta da Enquete Interativa';
        inputMsg.value = 'Com qual setor você deseja falar agora?';
      } else if (tipo === 'documento') {
        labelMsg.innerText = 'Legenda do Documento PDF';
        inputMsg.value = 'Segue em anexo sua fatura / nota fiscal em PDF.';
      } else if (tipo === 'video') {
        labelMsg.innerText = 'Legenda do Vídeo / Nota Circular (PTV)';
        inputMsg.value = 'Confira nossa demonstração em vídeo!';
      } else if (tipo === 'localizacao') {
        labelMsg.innerText = 'Nome e Endereço do Local';
        inputMsg.value = 'Sede WooHub Brasil - Av. Paulista, 1000 - São Paulo/SP';
      } else if (tipo === 'contato') {
        labelMsg.innerText = 'Nome do Consultor / Contato vCard';
        inputMsg.value = 'Especialista WooHub B2B';
      } else if (tipo === 'reacao') {
        labelMsg.innerText = 'Emoji da Reação';
        inputMsg.value = '❤️';
      } else if (tipo === 'mencao_fantasma') {
        labelMsg.innerText = 'Aviso para Notificar 100% do Grupo (@todos invisível)';
        inputMsg.value = '🚨 Estamos AO VIVO! Toque no link acima para acessar a sala.';
      } else if (tipo === 'status_24h') {
        labelMsg.innerText = 'Texto do Status 24h (Stories)';
        inputMsg.value = 'Oferta exclusiva nas próximas 24h pelo WhatsApp! 🔥';
      } else {
        labelMsg.innerText = 'Mensagem de Texto';
      }
    }
  }
}

async function dispararMensagemTeste() {
  const instId = document.getElementById('sim-instancia').value;
  const telefone = document.getElementById('sim-telefone').value.trim();
  const tipo = document.getElementById('sim-tipo').value;
  const mensagem = document.getElementById('sim-mensagem').value.trim();
  const sandboxCheck = document.getElementById('sim-modo-sandbox');
  const simulacao = sandboxCheck ? sandboxCheck.checked : false;
  const boxResultado = document.getElementById('sim-resultado');
  const btn = document.getElementById('btn-disparar-teste');

  if (!instId) {
    mostrarNotificacaoToast('Selecione uma instância para o disparo.', 'aviso');
    return;
  }
  if (!telefone && tipo !== 'status_24h') {
    mostrarNotificacaoToast('Digite o número de telefone ou grupo de destino.', 'aviso');
    document.getElementById('sim-telefone').focus();
    return;
  }
  if (tipo === 'texto' && !mensagem) {
    mostrarNotificacaoToast('Digite a mensagem a ser enviada.', 'aviso');
    document.getElementById('sim-mensagem').focus();
    return;
  }

  boxResultado.style.display = 'block';
  boxResultado.innerHTML = `
    <div style="background: #f8fafc; border: 1px solid #cbd5e1; padding: 1rem; border-radius: 8px; color: #059669; font-weight: 600; display: flex; align-items: center; gap: 8px;">
      <span class="spinner" style="border-top-color: #059669; width: 18px; height: 18px;"></span>
      Processando disparo na API (${escaparHTML(tipo)})...
    </div>
  `;

  if (btn) {
    btn.disabled = true;
    btn.innerText = 'Enviando...';
  }

  try {
    let endpoint = '/api/v1/mensagem/texto';
    let body = { instancia_id: instId, destinatario: telefone, mensagem, simulacao };

    if (tipo === 'audio_gravado') {
      endpoint = '/api/v1/mensagem/audio';
      body = {
        instancia_id: instId,
        destinatario: telefone,
        audio_base64: 'GkXfo59ChoEBQveBAULygQ8=',
        gravar_na_hora: true,
        simulacao: simulacao
      };
    } else if (tipo === 'botoes_interativos') {
        labelMsg.innerText = 'Texto da Mensagem com Botões';
        inputMsg.value = 'Escolha uma das opções abaixo para continuar seu atendimento:';
      } else if (tipo === 'botoes_interativos') {
      endpoint = '/api/v1/mensagem/interativa';
      body = {
        instancia_id: instId,
        destinatario: telefone,
        titulo: 'Atendimento WooHub',
        corpo: mensagem || 'Escolha uma das opções abaixo para prosseguir:',
        rodape: 'WooHub API • Gateway Oficial',
        botoes: [
          {
            tipo: 'url',
            texto: '🔗 Acessar Painel',
            valor: 'https://woohub.com.br'
          },
          {
            tipo: 'copiar',
            texto: '📋 Copiar Código Pix',
            valor: '00020126580014br.gov.bcb.pix0136woohub-teste-pix520400005303986540510.005802BR5910WooHub API6009Sao Paulo62070503***6304ABCD'
          },
          {
            tipo: 'resposta',
            texto: '💬 Falar com Suporte',
            id: 'btn_suporte_1'
          }
        ],
        simulacao: simulacao
      };
    } else if (tipo === 'enquete') {
      endpoint = '/api/v1/mensagem/enquete';
      const opcoesRaw = (document.getElementById('sim-enquete-opcoes')?.value || '1. Financeiro, 2. Suporte, 3. Comercial')
        .split(',').map(s => s.trim()).filter(Boolean);
      body = {
        instancia_id: instId,
        destinatario: telefone,
        pergunta: mensagem || 'Com qual setor você deseja falar?',
        opcoes: opcoesRaw.length >= 2 ? opcoesRaw : ['1. Financeiro', '2. Suporte'],
        maximo_selecao: 1,
        simulacao: simulacao
      };
    } else if (tipo === 'documento') {
      endpoint = '/api/v1/mensagem/documento';
      body = {
        instancia_id: instId,
        destinatario: telefone,
        documento_base64: 'JVBERi0xLjQKJeLjz9MK',
        nome_arquivo: 'fatura_woohub.pdf',
        legenda: mensagem || 'Segue sua fatura em PDF.',
        simulacao: simulacao
      };
    } else if (tipo === 'video') {
      endpoint = '/api/v1/mensagem/video';
      body = {
        instancia_id: instId,
        destinatario: telefone,
        video_base64: 'AAAAIGZ0eXBpc29tAAACAGlzb21pc28yYXZjMW1wNDE=',
        legenda: mensagem || 'Vídeo demonstrativo',
        nota_video: false,
        simulacao: simulacao
      };
    } else if (tipo === 'localizacao') {
      endpoint = '/api/v1/mensagem/localizacao';
      body = {
        instancia_id: instId,
        destinatario: telefone,
        latitude: -23.561414,
        longitude: -46.655881,
        nome_local: mensagem || 'Sede WooHub Brasil',
        endereco: 'Av. Paulista, 1000 - São Paulo/SP',
        simulacao: simulacao
      };
    } else if (tipo === 'contato') {
      endpoint = '/api/v1/mensagem/contato';
      body = {
        instancia_id: instId,
        destinatario: telefone,
        nome_contato: mensagem || 'Consultor Comercial WooHub',
        telefone: telefone,
        empresa: 'WooHub API',
        simulacao: simulacao
      };
    } else if (tipo === 'reacao') {
      endpoint = '/api/v1/mensagem/reacao';
      body = {
        instancia_id: instId,
        destinatario: telefone,
        id_mensagem_alvo: '3EB0123456789ABCDEF',
        emoji: mensagem || '❤️',
        simulacao: simulacao
      };
    } else if (tipo === 'verificar_numero') {
      endpoint = '/api/v1/contato/verificar';
      body = {
        instancia_id: instId,
        numeros: [telefone],
        simulacao: simulacao
      };
    } else if (tipo === 'presenca') {
      endpoint = '/api/v1/chat/presenca';
      body = {
        instancia_id: instId,
        destinatario: telefone,
        estado: 'gravando_audio',
        simulacao: simulacao
      };
    } else if (tipo === 'mencao_fantasma') {
      endpoint = '/api/v1/grupo/mencao-fantasma';
      body = {
        instancia_id: instId,
        grupo_jid: telefone.includes('@g.us') ? telefone : '120363025987654321@g.us',
        mensagem: mensagem || '🚨 Estamos AO VIVO! Confira o link no grupo.',
        simulacao: simulacao
      };
    } else if (tipo === 'status_24h') {
      endpoint = '/api/v1/status/publicar';
      body = {
        instancia_id: instId,
        tipo: 'texto',
        texto: mensagem || 'Novidade publicada via WooHub API! 🚀',
        cor_fundo: '#059669',
        simulacao: simulacao
      };
    }

    const res = await fetchAutenticado(endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    });
    const dados = await res.json();

    if (btn) {
      btn.disabled = false;
      btn.innerHTML = `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right: 6px;"><line x1="22" y1="2" x2="11" y2="13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/></svg> Disparar Mensagem de Teste`;
    }

    if (dados.sucesso && dados.dados) {
      mostrarNotificacaoToast('Mensagem processada com sucesso!', 'sucesso');
      const modoTexto = dados.dados.modo === 'simulacao' ? 'Modo Sandbox (Simulação)' : 'WhatsApp Oficial';
      boxResultado.innerHTML = `
        <div style="background: #ecfdf5; border: 1px solid #10b981; color: #065f46; padding: 1.25rem; border-radius: 8px;">
          <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem;">
            <strong style="font-size: 1rem; color: #047857; display: flex; align-items: center; gap: 6px;">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#059669" stroke-width="2"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>
              Disparo Concluído com Sucesso!
            </strong>
            <span class="badge-status-pill online">● ${dados.dados.status || 'ENTREGUE'}</span>
          </div>
          <div style="font-size: 0.88rem; line-height: 1.6; color: #064e3b;">
            <div>• <strong>Destinatário:</strong> ${dados.dados.destinatario || telefone}</div>
            <div>• <strong>ID da Mensagem:</strong> <code style="background: rgba(255,255,255,0.7); padding: 2px 6px; border-radius: 4px; font-family: monospace;">${dados.dados.id_mensagem}</code></div>
            <div>• <strong>Canal de Envio:</strong> ${modoTexto}</div>
          </div>
          <div style="margin-top: 1rem; display: flex; gap: 0.5rem;">
            <button class="btn btn-outline" style="font-size: 0.82rem; padding: 0.4rem 0.85rem;" onclick="navegarPara('logs')">
              Ver no Histórico de Logs ›
            </button>
          </div>
        </div>
      `;
    } else {
      const msgErro = dados.erro || 'Falha ao processar o disparo.';
      mostrarNotificacaoToast('Erro no envio: ' + msgErro, 'erro');
      boxResultado.innerHTML = `
        <div style="background: #fef2f2; border: 1px solid #f87171; color: #991b1b; padding: 1.25rem; border-radius: 8px;">
          <div style="font-weight: 700; margin-bottom: 0.5rem; display: flex; align-items: center; gap: 6px;">
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#dc2626" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
            Não foi possível realizar o disparo
          </div>
          <p style="font-size: 0.88rem; margin-bottom: 0.75rem; color: #7f1d1d;">${msgErro}</p>
          <div style="display: flex; gap: 0.5rem; align-items: center;">
            <button class="btn btn-verde-micro" onclick="abrirModalConectarSelecionada()">Conectar via QR Code</button>
            <span style="font-size: 0.8rem; color: #991b1b;">ou marque a opção Sandbox acima para testar sem celular.</span>
          </div>
        </div>
      `;
    }
  } catch (err) {
    if (btn) {
      btn.disabled = false;
      btn.innerHTML = `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right: 6px;"><line x1="22" y1="2" x2="11" y2="13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/></svg> Disparar Mensagem de Teste`;
    }
    mostrarNotificacaoToast('Erro de requisição: ' + err.message, 'erro');
    boxResultado.innerHTML = `<div style="background: #fef2f2; border: 1px solid #ef4444; color: #dc2626; padding: 1rem; border-radius: 8px;">Falha de comunicação: ${err.message}</div>`;
  }
}

function mostrarNotificacaoToast(msg, tipo = 'sucesso') {
  let toast = document.getElementById('toast-app');
  if (!toast) {
    toast = document.createElement('div');
    toast.id = 'toast-app';
    toast.className = 'toast-notificacao';
    document.body.appendChild(toast);
  }
  toast.className = `toast-notificacao ${tipo} visivel`;
  toast.innerText = msg;
  setTimeout(() => {
    toast.classList.remove('visivel');
  }, 3500);
}

// ==========================================================================
// 5. AUDITORIA E LOGS
// ==========================================================================

async function carregarLogs() {
  try {
    const res = await fetchAutenticado('/api/v1/mensagem/logs');
    const dados = await res.json();
    const corpo = document.getElementById('tabela-logs-corpo');

    if (!dados.sucesso || !dados.dados || dados.dados.length === 0) {
      corpo.innerHTML = `<tr><td colspan="6" style="padding: 2.5rem; text-align: center; color: #64748b;">Nenhum log registrado ainda.</td></tr>`;
      return;
    }

    corpo.innerHTML = dados.dados.map(log => `
      <tr>
        <td style="font-weight: 600;">${escaparHTML(new Date(log.criado_em).toLocaleString('pt-BR'))}</td>
        <td style="font-family: monospace; color: #64748b;">${escaparHTML(log.instancia_id)}</td>
        <td><span class="badge-plano azul">${escaparHTML(log.tipo)}</span></td>
        <td><strong>${escaparHTML(log.destinatario)}</strong></td>
        <td>${escaparHTML(log.direcao)}</td>
        <td><span class="badge-status-pill ${log.status === 'ENTREGUE' || log.status === 'LIDA' || String(log.status).startsWith('ENTREGUE') ? 'online' : 'offline'}">${escaparHTML(log.status)}</span></td>
      </tr>
    `).join('');
  } catch (err) {
    console.error('Erro ao carregar logs:', err);
  }
}

// ==========================================================================
// 6. WHITE-LABEL & MARCA PRÓPRIA
// ==========================================================================

async function carregarMarcaWhiteLabel() {
  try {
    const res = await fetch('/api/v1/revenda/marca');
    const dados = await res.json();
    if (dados.sucesso && dados.dados) {
      const rev = dados.dados;
      if (rev.nome_empresa) {
        document.title = `${rev.nome_empresa} - Plataforma de WhatsApp`;
        const marcaEls = document.querySelectorAll('#marca-nome, #login-marca-nome, #preview-nome');
        marcaEls.forEach(el => el.innerText = rev.nome_empresa);
        const inicial = rev.nome_empresa.charAt(0);
        document.getElementById('login-marca-logo').innerText = inicial;
        document.getElementById('preview-logo-box').innerText = inicial;
      }
      if (rev.dominio_personalizado) {
        document.getElementById('wl-dominio').value = rev.dominio_personalizado;
        document.getElementById('input-dominio-api').value = rev.dominio_personalizado;
      }
      if (rev.cor_primaria) {
        atualizarCorVisual(rev.cor_primaria);
      }
    }
  } catch (err) {
    console.warn('Aviso ao carregar dados de marca:', err);
  }
}

function atualizarCorVisual(hexCor) {
  if (!hexCor) return;
  document.documentElement.style.setProperty('--cor-primaria', hexCor);
  const amostra = document.getElementById('cor-amostra-box');
  const boxGrande = document.getElementById('marca-box-grande');
  const inputHex = document.getElementById('input-hex-cor');
  if (amostra) amostra.style.background = hexCor;
  if (boxGrande) boxGrande.style.background = hexCor;
  if (inputHex) inputHex.value = hexCor;
}

async function salvarConfiguracaoWhiteLabel() {
  const nome = document.getElementById('wl-nome').value;
  const dominio = document.getElementById('wl-dominio').value;
  const corPrimaria = document.getElementById('wl-cor-primaria').value;

  try {
    const res = await fetchAutenticado('/api/v1/revenda/configurar', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        nome_empresa: nome,
        dominio_personalizado: dominio,
        cor_primaria: corPrimaria
      })
    });
    const dados = await res.json();
    if (dados.sucesso) {
      alert('Configurações de marca própria salvas com sucesso!');
      carregarMarcaWhiteLabel();
    } else {
      alert('Erro: ' + dados.erro);
    }
  } catch (err) {
    alert('Erro de conexão ao salvar: ' + err.message);
  }
}

// Alteração de Senha
function abrirModalAlterarSenha() {
  document.getElementById('senha-atual').value = '';
  document.getElementById('nova-senha').value = '';
  document.getElementById('confirma-nova-senha').value = '';
  document.getElementById('modal-senha-alerta').style.display = 'none';
  document.getElementById('modal-alterar-senha').classList.add('aberto');
}

async function confirmarAlterarSenha(event) {
  event.preventDefault();

  const senhaAtual = document.getElementById('senha-atual').value;
  const novaSenha = document.getElementById('nova-senha').value;
  const confirmaNovaSenha = document.getElementById('confirma-nova-senha').value;
  const alerta = document.getElementById('modal-senha-alerta');

  if (novaSenha !== confirmaNovaSenha) {
    alerta.innerText = 'A confirmação de senha não confere.';
    alerta.style.display = 'block';
    return;
  }

  if (novaSenha.length < 4) {
    alerta.innerText = 'A nova senha deve possuir no mínimo 4 caracteres.';
    alerta.style.display = 'block';
    return;
  }

  try {
    const res = await fetchAutenticado('/api/v1/auth/alterar-senha', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ senha_atual: senhaAtual, nova_senha: novaSenha })
    });
    const dados = await res.json();
    if (dados.sucesso) {
      fecharModais();
      alert('Senha alterada com sucesso!');
    } else {
      alerta.innerText = dados.erro || 'Erro ao alterar senha.';
      alerta.style.display = 'block';
    }
  } catch (err) {
    alerta.innerText = 'Erro na requisição: ' + err.message;
    alerta.style.display = 'block';
  }
}

function abrirSuporte() {
  alert('Central de Suporte WooHub: Entre em contato pelo e-mail suporte@woohub.com.br ou pelo WhatsApp oficial.');
}

// ==========================================================================
// 7. LICENCIAMENTO DE STACKS PORTAINER (KILL-SWITCH) & INFRA ISOLADA (BYOI)
// ==========================================================================

async function carregarLicencasAdmin() {
  const corpo = document.getElementById('tabela-licencas-admin-corpo');
  if (!corpo) return;

  try {
    const res = await fetchAutenticado('/api/v1/licenca/listar');
    const dados = await res.json();
    if (!dados.sucesso || !Array.isArray(dados.dados) || dados.dados.length === 0) {
      corpo.innerHTML = `<tr><td colspan="7" style="text-align: center; padding: 1.5rem; color: #64748b;">Nenhuma licença emitida. Clique em "+ Emitir Licença Portainer" acima.</td></tr>`;
      return;
    }

    corpo.innerHTML = dados.dados.map(lic => {
      const statusOnline = lic.status === 'ATIVA';
      const badgeCls = statusOnline ? 'online' : 'offline';
      const dataExp = lic.expira_em ? new Date(lic.expira_em).toLocaleDateString('pt-BR') : '-';
      const dataHB = lic.ultimo_heartbeat ? new Date(lic.ultimo_heartbeat).toLocaleString('pt-BR') : 'Aguardando 1º Boot';
      const ipDom = `${escaparHTML(lic.dominio_autorizado || 'Qualquer domínio')} • IP: ${escaparHTML(lic.ultimo_ip || lic.ip_autorizado || 'Livre')}`;

      return `
        <tr>
          <td>
            <strong>${escaparHTML(lic.cliente_nome)}</strong><br>
            <small style="color: #64748b;">${escaparHTML(lic.email_contato || 'Sem e-mail')} • Expira: ${dataExp}</small>
          </td>
          <td>
            <code style="background: #f1f5f9; padding: 3px 7px; border-radius: 5px; font-family: monospace; font-size: 0.8rem; color: #0f172a;">${escaparHTML(lic.chave_licenca)}</code>
          </td>
          <td>
            <span class="badge-plano azul">${escaparHTML(lic.plano)}</span><br>
            <small style="color: #475569; font-weight: 600;">${Number(lic.instancias_ativas) || 0} / ${Number(lic.limite_instancias) || 50} instâncias</small>
          </td>
          <td style="font-size: 0.82rem; color: #334155;">${ipDom}</td>
          <td style="font-size: 0.8rem; color: #475569;">${dataHB}</td>
          <td><span class="badge-status-pill ${badgeCls}">● ${escaparHTML(lic.status)}</span></td>
          <td>
            <div style="display: flex; gap: 0.35rem; flex-wrap: wrap;">
              ${statusOnline
                ? `<button class="btn btn-outline" style="padding: 0.25rem 0.55rem; font-size: 0.75rem; color: #dc2626; border-color: #fca5a5;" onclick="alterarStatusLicenca('${escaparHTML(lic.id)}', 'SUSPENSA')">Suspender (Kill)</button>`
                : `<button class="btn btn-verde" style="padding: 0.25rem 0.55rem; font-size: 0.75rem;" onclick="alterarStatusLicenca('${escaparHTML(lic.id)}', 'ATIVA', 30)">Reativar +30d</button>`
              }
              <button class="btn btn-outline" style="padding: 0.25rem 0.55rem; font-size: 0.75rem;" onclick="desvincularIPLicenca('${escaparHTML(lic.id)}')" title="Liberar trava de IP para o cliente trocar de servidor">Resetar IP</button>
            </div>
          </td>
        </tr>
      `;
    }).join('');
  } catch (err) {
    console.warn('Aviso ao listar licenças:', err);
  }
}

function abrirModalNovaLicenca() {
  document.getElementById('lic-cliente-nome').value = '';
  document.getElementById('lic-email').value = '';
  document.getElementById('lic-dominio').value = '';
  document.getElementById('lic-plano').value = 'Stack Portainer Pro';
  document.getElementById('lic-limite').value = '50';
  document.getElementById('lic-dias').value = '30';
  document.getElementById('box-licenca-gerada').style.display = 'none';
  document.getElementById('modal-nova-licenca').classList.add('aberto');
}

async function confirmarCriarLicencaPortainer() {
  const cliente_nome = document.getElementById('lic-cliente-nome').value.trim();
  const email_contato = document.getElementById('lic-email').value.trim();
  const dominio_autorizado = document.getElementById('lic-dominio').value.trim();
  const plano = document.getElementById('lic-plano').value.trim();
  const limite_instancias = parseInt(document.getElementById('lic-limite').value, 10) || 50;
  const dias_validade = parseInt(document.getElementById('lic-dias').value, 10) || 30;

  if (!cliente_nome) {
    mostrarNotificacaoToast('Informe o nome da revenda ou cliente.', 'aviso');
    return;
  }

  try {
    const res = await fetchAutenticado('/api/v1/licenca/criar', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        cliente_nome,
        email_contato,
        dominio_autorizado,
        plano,
        limite_instancias,
        dias_validade
      })
    });
    const dados = await res.json();
    if (dados.sucesso && dados.dados) {
      document.getElementById('lic-chave-gerada-txt').innerText = dados.dados.licenca.chave_licenca;
      document.getElementById('lic-yaml-gerado').value = dados.dados.portainer_stack_yaml;
      document.getElementById('box-licenca-gerada').style.display = 'block';
      mostrarNotificacaoToast('Licença Portainer gerada com sucesso!', 'sucesso');
      carregarLicencasAdmin();
    } else {
      mostrarNotificacaoToast('Erro: ' + (dados.erro || 'Falha ao criar licença'), 'erro');
    }
  } catch (err) {
    mostrarNotificacaoToast('Erro de conexão: ' + err.message, 'erro');
  }
}

function copiarStackPortainerGerada() {
  const txt = document.getElementById('lic-yaml-gerado');
  if (!txt) return;
  navigator.clipboard.writeText(txt.value);
  mostrarNotificacaoToast('Stack YAML copiada para a área de transferência!', 'sucesso');
}

async function alterarStatusLicenca(id, status, diasRenovacao = 0) {
  try {
    const res = await fetchAutenticado('/api/v1/licenca/status', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id, status, dias_renovacao: diasRenovacao })
    });
    const dados = await res.json();
    if (dados.sucesso) {
      mostrarNotificacaoToast(dados.mensagem || 'Status da licença atualizado!', 'sucesso');
      carregarLicencasAdmin();
    } else {
      mostrarNotificacaoToast('Erro: ' + dados.erro, 'erro');
    }
  } catch (err) {
    mostrarNotificacaoToast('Erro: ' + err.message, 'erro');
  }
}

async function desvincularIPLicenca(id) {
  try {
    const res = await fetchAutenticado('/api/v1/licenca/desvincular-ip', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id })
    });
    const dados = await res.json();
    if (dados.sucesso) {
      mostrarNotificacaoToast('Vínculo de IP resetado com sucesso!', 'sucesso');
      carregarLicencasAdmin();
    } else {
      mostrarNotificacaoToast('Erro: ' + dados.erro, 'erro');
    }
  } catch (err) {
    mostrarNotificacaoToast('Erro: ' + err.message, 'erro');
  }
}

async function carregarInfraestruturaRevenda() {
  const inputSupa = document.getElementById('infra-supabase-uri');
  if (!inputSupa) return;

  try {
    const res = await fetchAutenticado('/api/v1/revenda/infra');
    const dados = await res.json();
    if (dados.sucesso && dados.dados) {
      const d = dados.dados;
      document.getElementById('infra-supabase-uri').value = d.supabase_uri || '';
      document.getElementById('infra-minio-endpoint').value = d.minio_endpoint || '';
      document.getElementById('infra-minio-access').value = d.minio_access_key || '';
      document.getElementById('infra-minio-secret').value = d.minio_secret_key_mascara || '';
      document.getElementById('infra-minio-bucket').value = d.minio_bucket || 'woohub-midias';
      document.getElementById('infra-minio-public-url').value = d.minio_public_url || '';
      document.getElementById('infra-minio-ssl').checked = d.minio_use_ssl !== false;

      const badge = document.getElementById('badge-status-infra-isolada');
      if (badge) {
        if (d.supabase_isolado_ativo && d.minio_isolado_ativo) {
          badge.innerText = '● Supabase + MinIO Isolados Ativos';
        } else if (d.supabase_isolado_ativo) {
          badge.innerText = '● Supabase Isolado Ativo';
        } else if (d.minio_isolado_ativo) {
          badge.innerText = '● MinIO Isolado Ativo';
        } else {
          badge.innerText = '● Usando Infra Padrão da Stack';
        }
      }
    }
  } catch (err) {
    console.warn('Aviso ao carregar infra da revenda:', err);
  }
}

async function testarEMigrarSupabaseRevenda() {
  const uri = document.getElementById('infra-supabase-uri').value.trim();
  const box = document.getElementById('resultado-teste-supabase');
  if (!uri) {
    mostrarNotificacaoToast('Informe a URI PostgreSQL do Supabase da revenda.', 'aviso');
    return;
  }

  box.style.display = 'block';
  box.style.background = '#f8fafc';
  box.style.color = '#059669';
  box.innerText = '⏳ Conectando ao Supabase da revenda e criando tabelas automaticamente...';

  try {
    const res = await fetchAutenticado('/api/v1/revenda/infra/testar-supabase', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ supabase_uri: uri })
    });
    const dados = await res.json();
    if (dados.sucesso && dados.dados) {
      box.style.background = '#ecfdf5';
      box.style.border = '1px solid #10b981';
      box.style.color = '#065f46';
      box.innerHTML = `✅ <strong>Supabase Conectado e Migrado!</strong> Latência: ${dados.dados.latencia_ms}ms • Tabelas prontas: ${dados.dados.tabelas_publicas} • Session Mode (5432): ${dados.dados.pooler_session_mode ? 'Sim' : 'Padrão'}`;
      mostrarNotificacaoToast('Supabase da revenda validado e migrado com sucesso!', 'sucesso');
    } else {
      box.style.background = '#fef2f2';
      box.style.border = '1px solid #f87171';
      box.style.color = '#991b1b';
      box.innerText = '❌ Erro: ' + (dados.erro || 'Falha ao conectar no Supabase');
    }
  } catch (err) {
    box.style.background = '#fef2f2';
    box.style.color = '#991b1b';
    box.innerText = '❌ Erro: ' + err.message;
  }
}

async function testarMinioRevenda() {
  const minio_endpoint = document.getElementById('infra-minio-endpoint').value.trim();
  const minio_access_key = document.getElementById('infra-minio-access').value.trim();
  const minio_secret_key = document.getElementById('infra-minio-secret').value.trim();
  const minio_bucket = document.getElementById('infra-minio-bucket').value.trim() || 'woohub-midias';
  const minio_use_ssl = document.getElementById('infra-minio-ssl').checked;
  const box = document.getElementById('resultado-teste-minio');

  if (!minio_endpoint || !minio_access_key) {
    mostrarNotificacaoToast('Preencha Endpoint, Access Key e Secret Key do MinIO.', 'aviso');
    return;
  }

  box.style.display = 'block';
  box.style.background = '#f8fafc';
  box.style.color = '#059669';
  box.innerText = '⏳ Validando credenciais S3 no MinIO e verificando bucket...';

  try {
    const res = await fetchAutenticado('/api/v1/revenda/infra/testar-minio', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        minio_endpoint,
        minio_access_key,
        minio_secret_key,
        minio_bucket,
        minio_use_ssl
      })
    });
    const dados = await res.json();
    if (dados.sucesso) {
      box.style.background = '#ecfdf5';
      box.style.border = '1px solid #10b981';
      box.style.color = '#065f46';
      box.innerHTML = `✅ <strong>${escaparHTML(dados.mensagem)}</strong>`;
      mostrarNotificacaoToast('MinIO S3 conectado e bucket pronto!', 'sucesso');
    } else {
      box.style.background = '#fef2f2';
      box.style.border = '1px solid #f87171';
      box.style.color = '#991b1b';
      box.innerText = '❌ ' + (dados.erro || 'Falha na conexão com MinIO');
    }
  } catch (err) {
    box.style.background = '#fef2f2';
    box.style.color = '#991b1b';
    box.innerText = '❌ Erro: ' + err.message;
  }
}

async function salvarInfraestruturaRevenda() {
  const payload = {
    supabase_uri: document.getElementById('infra-supabase-uri').value.trim(),
    minio_endpoint: document.getElementById('infra-minio-endpoint').value.trim(),
    minio_access_key: document.getElementById('infra-minio-access').value.trim(),
    minio_secret_key: document.getElementById('infra-minio-secret').value.trim(),
    minio_bucket: document.getElementById('infra-minio-bucket').value.trim() || 'woohub-midias',
    minio_public_url: document.getElementById('infra-minio-public-url').value.trim(),
    minio_use_ssl: document.getElementById('infra-minio-ssl').checked
  };

  try {
    const res = await fetchAutenticado('/api/v1/revenda/infra/configurar', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const dados = await res.json();
    if (dados.sucesso) {
      mostrarNotificacaoToast('Infraestrutura isolada (Supabase + MinIO) salva com sucesso!', 'sucesso');
      carregarInfraestruturaRevenda();
    } else {
      mostrarNotificacaoToast('Erro: ' + (dados.erro || 'Falha ao salvar'), 'erro');
    }
  } catch (err) {
    mostrarNotificacaoToast('Erro de conexão: ' + err.message, 'erro');
  }
}

// ============================================================================
// CARREGAR PLANOS E ASSINATURAS (B2B SAAS TIER 01, 02 E 03)
// ============================================================================
const CATALOGO_PLANOS_PADRAO = [
  {
    id: 'basico',
    nome: 'Starter / Básico',
    preco_mensal: 'R$ 197,00',
    limite_instancias: 5,
    franquia_mensagens: '30.000 disparos/mês',
    publico: 'revendedor',
    destaque: false,
    recursos: [
      'Até 5 Instâncias WhatsApp Simultâneas',
      'Pacote 1: Texto, Áudio PTT Microfone Verde, Vídeo, PDF e Enquetes',
      'Pacote 2: Validador 9º Dígito BR + Download Automático de Mídias',
      'Webhooks em Tempo Real com Assinatura HMAC SHA-256'
    ]
  },
  {
    id: 'profissional',
    nome: 'Business Pro',
    preco_mensal: 'R$ 297,00',
    limite_instancias: 20,
    franquia_mensagens: '100.000 disparos/mês',
    publico: 'ambos',
    destaque: true,
    recursos: [
      'Até 20 Instâncias WhatsApp Simultâneas',
      'Todos os recursos do Pacote 1 e Pacote 2 liberados',
      'Pacote 3: Grupos, Menção Fantasma (@todos) e Status 24h',
      'Pacote 4: Blindagem Anti-Ban, Proxy SOCKS5/HTTP e Anti-Chamadas VoIP',
      'Etiquetas WhatsApp Business e Simulação Humana'
    ]
  },
  {
    id: 'enterprise',
    nome: 'Enterprise White-Label & Stack',
    preco_mensal: 'R$ 497,00',
    limite_instancias: 150,
    franquia_mensagens: 'Disparos Ilimitados',
    publico: 'mega',
    destaque: false,
    recursos: [
      'Até 150 Instâncias WhatsApp + Subcontas de Clientes',
      'Painel 100% White-Label com Domínio Próprio (CNAME) e Cores',
      'Infraestrutura Isolada Opcional (BYO Supabase PostgreSQL + MinIO S3)',
      'Deploy Dedicado via Stack Portainer com SLA 99,98%'
    ]
  }
];

function obterListaPlanos() {
  const salvos = localStorage.getItem('woohub_planos_catalogo');
  if (salvos) {
    try {
      const parsed = JSON.parse(salvos);
      if (Array.isArray(parsed) && parsed.length > 0) return parsed;
    } catch (e) {
      console.warn('Erro ao ler catálogo local de planos:', e);
    }
  }
  localStorage.setItem('woohub_planos_catalogo', JSON.stringify(CATALOGO_PLANOS_PADRAO));
  return [...CATALOGO_PLANOS_PADRAO];
}

function salvarListaPlanos(lista) {
  localStorage.setItem('woohub_planos_catalogo', JSON.stringify(lista));
}

async function carregarPlanos() {
  const grid = document.getElementById('grid-planos-assinaturas');
  if (!grid) return;

  const planos = obterListaPlanos();
  const emUso = instanciasCache ? instanciasCache.length : 0;

  grid.innerHTML = planos.map(p => {
    const isDestaque = !!p.destaque;
    const franquiaTexto = p.franquia_mensagens || 'Mensagens Ilimitadas';
    const recursosHTML = (p.recursos || []).map(r => `
      <div class="vantagem-item" style="margin-bottom: 0.45rem; font-size: 0.85rem;">
        <span class="check-circulo">&#10003;</span> ${escaparHTML(r)}
      </div>
    `).join('');

    return `
      <div class="card-dashboard" style="border: ${isDestaque ? '2px solid #059669' : '1px solid #e2e8f0'}; position: relative; display: flex; flex-direction: column; justify-content: space-between; border-radius: 12px; background: #fff; padding: 1.5rem;">
        <div>
          <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem;">
            <h3 style="font-size: 1.18rem; font-weight: 800; color: #0f172a; margin: 0;">${escaparHTML(p.nome)}</h3>
            ${isDestaque ? '<span class="badge-plano-destaque">PLANO ATIVO</span>' : '<span class="badge-plano azul" style="cursor:pointer;" onclick="selecionarPlanoAtivo(\'' + escaparHTML(p.id) + '\')">Disponível</span>'}
          </div>
          <div style="font-size: 1.85rem; font-weight: 800; color: #059669; margin: 0.5rem 0;">
            ${escaparHTML(p.preco_mensal)} <span style="font-size: 0.85rem; color: #64748b; font-weight: 500;">/ mês</span>
          </div>
          <p style="font-size: 0.84rem; color: #64748b; margin-bottom: 1rem;">
            Franquia: <strong>${escaparHTML(franquiaTexto)}</strong> &bull; Até <strong>${Number(p.limite_instancias) || 1} instâncias</strong>
          </p>
          ${isDestaque ? `
            <div style="background: #f0fdf4; border: 1px solid #bbf7d0; border-radius: 8px; padding: 0.65rem 0.85rem; margin-bottom: 1rem; font-size: 0.82rem; color: #166534;">
              Consumo atual: <strong>${emUso} de ${Number(p.limite_instancias) || 1} instâncias</strong> em uso
            </div>
          ` : ''}
          <div class="lista-vantagens-plano" style="margin-bottom: 1.25rem;">
            ${recursosHTML}
          </div>
        </div>
        <div style="display:flex;gap:.5rem;margin-top:1rem;">
          <button class="btn btn-outline" style="flex:1;justify-content:center;" onclick="editarPlano('${escaparHTML(p.id)}')">
            <span>&#9998;</span> Editar
          </button>
          <button class="btn ${isDestaque ? 'btn-verde' : 'btn-outline'}" style="flex:1;justify-content:center;" onclick="selecionarPlanoAtivo('${escaparHTML(p.id)}')">
            ${isDestaque ? '&#10003; Ativo' : 'Ativar Plano'}
          </button>
        </div>
      </div>
    `;
  }).join('');
}

function abrirModalNovoPlano() {
  planoEmEdicao = null;
  const form = document.getElementById('form-plano-admin');
  if (form) form.reset();
  document.getElementById('plano-id-admin').value = '';
  const titulo = document.getElementById('modal-plano-titulo');
  if (titulo) titulo.innerText = 'Novo plano comercial';
  const btnExcluir = document.getElementById('btn-excluir-plano');
  if (btnExcluir) btnExcluir.style.display = 'none';
  const checkDestaque = document.getElementById('plano-destaque-admin');
  if (checkDestaque) checkDestaque.checked = false;
  document.getElementById('modal-plano-admin').classList.add('aberto');
}

function editarPlano(id) {
  const planos = obterListaPlanos();
  const plano = planos.find(item => item.id === id);
  if (!plano) {
    mostrarNotificacaoToast('Plano não localizado no catálogo.', 'erro');
    return;
  }
  planoEmEdicao = plano;
  document.getElementById('plano-id-admin').value = plano.id;
  document.getElementById('plano-nome-admin').value = plano.nome || '';
  document.getElementById('plano-preco-admin').value = plano.preco_mensal || '';
  document.getElementById('plano-limite-admin').value = plano.limite_instancias || 5;
  const inputFranquia = document.getElementById('plano-franquia-admin');
  if (inputFranquia) inputFranquia.value = plano.franquia_mensagens || '';
  const selPublico = document.getElementById('plano-publico-admin');
  if (selPublico) selPublico.value = plano.publico || 'revendedor';
  document.getElementById('plano-recursos-admin').value = (plano.recursos || []).join('\n');
  const checkDestaque = document.getElementById('plano-destaque-admin');
  if (checkDestaque) checkDestaque.checked = !!plano.destaque;

  const titulo = document.getElementById('modal-plano-titulo');
  if (titulo) titulo.innerText = `Editar Plano: ${plano.nome}`;
  const btnExcluir = document.getElementById('btn-excluir-plano');
  if (btnExcluir) btnExcluir.style.display = 'inline-flex';

  document.getElementById('modal-plano-admin').classList.add('aberto');
}

function salvarPlanoAdmin(event) {
  event.preventDefault();
  const planos = obterListaPlanos();
  const id = document.getElementById('plano-id-admin').value.trim();
  const nome = document.getElementById('plano-nome-admin').value.trim();
  const preco = document.getElementById('plano-preco-admin').value.trim();
  const limite = Number(document.getElementById('plano-limite-admin').value) || 1;
  const inputFranquia = document.getElementById('plano-franquia-admin');
  const franquia = inputFranquia ? inputFranquia.value.trim() : 'Ilimitado';
  const selPublico = document.getElementById('plano-publico-admin');
  const publico = selPublico ? selPublico.value : 'revendedor';
  const recursos = document.getElementById('plano-recursos-admin').value.split('\n').map(r => r.trim()).filter(Boolean);
  const checkDestaque = document.getElementById('plano-destaque-admin');
  const destaque = checkDestaque ? checkDestaque.checked : false;

  const novoOuEditado = {
    id: id || `plano_${Date.now()}`,
    nome,
    preco_mensal: preco.startsWith('R$') ? preco : `R$ ${preco}`,
    limite_instancias: limite,
    franquia_mensagens: franquia || `${limite * 10000} disparos/mês`,
    publico,
    recursos: recursos.length > 0 ? recursos : ['Disparos e Webhooks em tempo real'],
    destaque
  };

  if (destaque) {
    planos.forEach(p => p.destaque = false);
  }

  const idx = planos.findIndex(p => p.id === novoOuEditado.id);
  if (idx >= 0) {
    planos[idx] = novoOuEditado;
  } else {
    planos.push(novoOuEditado);
  }

  salvarListaPlanos(planos);
  fecharModais();
  carregarPlanos();
  mostrarNotificacaoToast(`Plano "${nome}" salvo com sucesso!`, 'sucesso');
}

function excluirPlanoAdmin() {
  const id = document.getElementById('plano-id-admin').value.trim();
  if (!id) return;
  if (!confirm('Deseja realmente excluir este plano comercial do catálogo?')) return;

  let planos = obterListaPlanos();
  planos = planos.filter(p => p.id !== id);
  salvarListaPlanos(planos);
  fecharModais();
  carregarPlanos();
  mostrarNotificacaoToast('Plano excluído com sucesso.', 'sucesso');
}

function selecionarPlanoAtivo(id) {
  const planos = obterListaPlanos();
  planos.forEach(p => {
    p.destaque = (p.id === id);
  });
  salvarListaPlanos(planos);
  carregarPlanos();
  mostrarNotificacaoToast('Plano ativado com sucesso!', 'sucesso');
}

// ============================================================================
// 7. CENTRAL DE CAIXAS, INTEGRAÇÕES NATIVAS & RASTREAMENTO DE ORIGEM
// ============================================================================

let tipoCaixaAtualModal = '';
let abaIntegracoesAtiva = 'caixas';

async function inicializarTelaIntegracoes() {
  await carregarInstancias();
  popularSelectInstanciasIntegracoes();
  carregarCaixasIntegracao();
  carregarAtribuicoesOrigem();
}

function popularSelectInstanciasIntegracoes() {
  const sel = document.getElementById('select-instancia-integracoes');
  if (!sel) return;
  sel.innerHTML = '';

  if (instanciasCache.length === 0) {
    sel.innerHTML = '<option value="">Nenhuma instância disponível</option>';
    return;
  }

  instanciasCache.forEach(inst => {
    const opt = document.createElement('option');
    opt.value = inst.id;
    opt.innerText = `${inst.nome} (${inst.status})`;
    sel.appendChild(opt);
  });
}

function obterInstanciaSelecionadaIntegracoes() {
  const sel = document.getElementById('select-instancia-integracoes');
  if (sel && sel.value) return sel.value;
  if (instanciasCache.length > 0) return instanciasCache[0].id;
  return '';
}

function aoTrocarInstanciaIntegracoes() {
  carregarCaixasIntegracao();
  carregarAtribuicoesOrigem();
}

function alternarAbaIntegracoes(aba) {
  abaIntegracoesAtiva = aba;
  const btnCaixas = document.getElementById('aba-btn-caixas');
  const btnAtrib = document.getElementById('aba-btn-atribuicoes');
  const cntCaixas = document.getElementById('aba-conteudo-caixas');
  const cntAtrib = document.getElementById('aba-conteudo-atribuicoes');

  if (aba === 'caixas') {
    if (btnCaixas) btnCaixas.classList.add('ativo');
    if (btnAtrib) btnAtrib.classList.remove('ativo');
    if (cntCaixas) cntCaixas.style.display = 'block';
    if (cntAtrib) cntAtrib.style.display = 'none';
    carregarCaixasIntegracao();
  } else {
    if (btnAtrib) btnAtrib.classList.add('ativo');
    if (btnCaixas) btnCaixas.classList.remove('ativo');
    if (cntAtrib) cntAtrib.style.display = 'block';
    if (cntCaixas) cntCaixas.style.display = 'none';
    carregarAtribuicoesOrigem();
  }
}

async function carregarCaixasIntegracao() {
  const instId = obterInstanciaSelecionadaIntegracoes();
  const grid = document.getElementById('grid-caixas-integracao');
  if (!grid) return;

  if (!instId) {
    grid.innerHTML = '<div style="grid-column: 1/-1; text-align: center; color: #64748b; padding: 2rem;">Selecione uma instância para gerenciar suas conexões.</div>';
    return;
  }

  grid.innerHTML = '<div style="grid-column: 1/-1; text-align: center; color: #64748b; padding: 2rem;">Carregando conexões...</div>';

  try {
    const res = await fetchAutenticado(`/api/v1/instancia/${instId}/integracoes`);
    const dados = await res.json();
    if (!dados || !dados.caixas) {
      grid.innerHTML = '<div style="grid-column: 1/-1; text-align: center; color: #ef4444; padding: 2rem;">Falha ao obter integrações da instância.</div>';
      return;
    }

    grid.innerHTML = '';
    dados.caixas.forEach(c => {
      const card = document.createElement('div');
      card.className = 'card-caixa-integracao';

      const statusBadge = c.ativo
        ? '<span class="badge-status-pill online">● Ativo</span>'
        : (c.configurado ? '<span class="badge-status-pill pendente" style="background: #fef3c7; color: #92400e;">● Inativo</span>' : '<span class="badge-status-pill offline">○ Não Configurado</span>');

      let iconeTexto = '🔌';
      if (c.tipo === 'chatwoot') iconeTexto = '💬';
      else if (c.tipo === 'typebot') iconeTexto = '🤖';
      else if (c.tipo === 'n8n') iconeTexto = '⚡';
      else if (c.tipo === 'kommo') iconeTexto = '📊';
      else if (c.tipo === 'rdstation') iconeTexto = '📢';
      else if (c.tipo === 'webhook') iconeTexto = '🌐';

      card.innerHTML = `
        <div>
          <div class="card-caixa-header">
            <div style="display: flex; align-items: center; gap: 0.85rem;">
              <div class="caixa-icone-wrap" style="background: ${c.cor};">
                ${iconeTexto}
              </div>
              <div>
                <h3 style="font-size: 1.15rem; font-weight: 700; color: #0f172a; margin-bottom: 0.25rem;">${escaparHTML(c.nome)}</h3>
                <div>${statusBadge}</div>
              </div>
            </div>
            <label class="switch-custom" style="position: relative; display: inline-block; width: 44px; height: 24px;">
              <input type="checkbox" ${c.ativo ? 'checked' : ''} onchange="alternarStatusCaixaDireto('${c.tipo}', this.checked)">
              <span class="slider-custom" style="position: absolute; cursor: pointer; top: 0; left: 0; right: 0; bottom: 0; background-color: #334155; transition: .3s; border-radius: 24px;"></span>
            </label>
          </div>
          <p style="color: #64748b; font-size: 0.85rem; line-height: 1.45; min-height: 40px; margin: 0.75rem 0 1rem 0;">
            ${escaparHTML(c.descricao)}
          </p>
        </div>
        <div style="display: flex; gap: 0.75rem; border-top: 1px solid #f1f5f9; padding-top: 1rem;">
          <button type="button" class="btn btn-outline" style="flex: 1; justify-content: center; font-size: 0.82rem;" onclick="abrirModalConfigCaixa('${c.tipo}')">
            ⚙️ Configurar
          </button>
          <button type="button" class="btn btn-outline" style="font-size: 0.82rem; padding: 0.5rem 0.75rem;" title="Testar Conexão" onclick="testarConexaoRapida('${c.tipo}')">
            🧪
          </button>
        </div>
      `;
      grid.appendChild(card);
    });
  } catch (err) {
    grid.innerHTML = `<div style="grid-column: 1/-1; text-align: center; color: #ef4444; padding: 2rem;">Erro de conexão: ${escaparHTML(err.message)}</div>`;
  }
}

async function alternarStatusCaixaDireto(tipo, novoStatus) {
  const instId = obterInstanciaSelecionadaIntegracoes();
  if (!instId) return;

  try {
    const resGet = await fetchAutenticado(`/api/v1/instancia/${instId}/integracoes/${tipo}`);
    const dadosAtuais = await resGet.json();
    let configStr = dadosAtuais && dadosAtuais.configuracoes ? dadosAtuais.configuracoes : '{}';

    const res = await fetchAutenticado(`/api/v1/instancia/${instId}/integracoes/${tipo}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        ativo: novoStatus,
        configuracoes: configStr
      })
    });
    const dados = await res.json();
    if (dados.sucesso) {
      mostrarNotificacaoToast(`Caixa ${tipo.toUpperCase()} ${novoStatus ? 'ativada' : 'desativada'} com sucesso!`, 'sucesso');
      carregarCaixasIntegracao();
    } else {
      mostrarNotificacaoToast('Erro ao alternar status: ' + (dados.erro || 'Falha'), 'erro');
      carregarCaixasIntegracao();
    }
  } catch (err) {
    mostrarNotificacaoToast('Erro: ' + err.message, 'erro');
    carregarCaixasIntegracao();
  }
}

async function abrirModalConfigCaixa(tipo) {
  const instId = obterInstanciaSelecionadaIntegracoes();
  if (!instId) {
    mostrarNotificacaoToast('Selecione uma instância primeiro.', 'aviso');
    return;
  }

  tipoCaixaAtualModal = tipo;
  const modal = document.getElementById('modal-config-caixa');
  const icone = document.getElementById('modal-caixa-icone');
  const titulo = document.getElementById('modal-caixa-titulo');
  const desc = document.getElementById('modal-caixa-descricao');
  const alerta = document.getElementById('modal-caixa-alerta');
  const campos = document.getElementById('modal-caixa-campos');
  const checkAtivo = document.getElementById('modal-caixa-ativo');

  if (alerta) alerta.style.display = 'none';
  if (campos) campos.innerHTML = '';

  const infoMap = {
    chatwoot: {
      nome: 'Chatwoot • Atendimento Multicanal',
      cor: '#1F93FF',
      icone: '💬',
      desc: 'Sincronização bidirecional de mensagens, contatos e anexos com o Chatwoot'
    },
    typebot: {
      nome: 'Typebot • Chatbot & Enquetes',
      cor: '#0042DA',
      icone: '🤖',
      desc: 'Acione fluxos interativos de Typebot com suporte nativo a enquetes WhatsApp'
    },
    n8n: {
      nome: 'n8n • Automação & Workflows',
      cor: '#EA4B71',
      icone: '⚡',
      desc: 'Disparo de webhooks estruturados para workflows do n8n com dados de tráfego'
    },
    kommo: {
      nome: 'Kommo CRM • Funil & Vendas',
      cor: '#0055FE',
      icone: '📊',
      desc: 'Criação automática de Leads, Contatos e notas na timeline do Kommo'
    },
    rdstation: {
      nome: 'RD Station • Marketing & CRM',
      cor: '#00A868',
      icone: '📢',
      desc: 'Conversão instantânea no RD Marketing e oportunidades no RD CRM'
    },
    webhook: {
      nome: 'Webhooks Universais',
      cor: '#10B981',
      icone: '🌐',
      desc: 'Múltiplos destinos HTTP com assinatura HMAC-SHA256'
    }
  };

  const info = infoMap[tipo] || { nome: 'Configuração', cor: '#059669', icone: '🔌', desc: '' };
  if (icone) {
    icone.style.background = info.cor;
    icone.innerText = info.icone;
  }
  if (titulo) titulo.innerText = info.nome;
  if (desc) desc.innerText = info.desc;

  let configSalva = {};
  let estaAtivo = false;

  try {
    const res = await fetchAutenticado(`/api/v1/instancia/${instId}/integracoes/${tipo}`);
    const dados = await res.json();
    if (dados && dados.configuracoes) {
      estaAtivo = dados.ativo === true;
      try {
        configSalva = typeof dados.configuracoes === 'string' ? JSON.parse(dados.configuracoes) : (dados.configuracoes || {});
      } catch (e) {
        configSalva = {};
      }
    }
  } catch (err) {
    console.error(err);
  }

  if (checkAtivo) checkAtivo.checked = estaAtivo;

  if (campos) {
    if (tipo === 'chatwoot') {
      campos.innerHTML = `
        <div class="form-grupo">
          <label class="form-label">URL da Instância Chatwoot</label>
          <input type="url" id="cw-url" class="form-input" placeholder="https://app.chatwoot.com ou https://seu-chatwoot.com" value="${escaparHTML(configSalva.url || '')}">
        </div>
        <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 0.75rem;">
          <div class="form-grupo">
            <label class="form-label">Account ID</label>
            <input type="text" id="cw-account" class="form-input" placeholder="Ex: 1" value="${escaparHTML(configSalva.account_id || '')}">
          </div>
          <div class="form-grupo">
            <label class="form-label">Inbox ID (Opcional)</label>
            <input type="text" id="cw-inbox" class="form-input" placeholder="Ex: 10" value="${escaparHTML(configSalva.inbox_id || '')}">
          </div>
        </div>
        <div class="form-grupo">
          <label class="form-label">User Access Token / Bot Token</label>
          <input type="password" id="cw-token" class="form-input" placeholder="Token da API do Chatwoot" value="${escaparHTML(configSalva.token || '')}">
        </div>
        <div style="margin-top: 0.75rem; display: flex; flex-direction: column; gap: 0.5rem;">
          <label style="display: flex; align-items: center; gap: 0.5rem; font-size: 0.85rem; cursor: pointer;">
            <input type="checkbox" id="cw-reabrir" ${configSalva.reabrir_conversa !== false ? 'checked' : ''}>
            Reabrir conversa resolvida quando o cliente mandar nova mensagem
          </label>
        </div>
        <div style="background: rgba(31, 147, 255, 0.08); border: 1px solid rgba(31, 147, 255, 0.2); padding: 0.75rem; border-radius: 8px; margin-top: 1rem; font-size: 0.78rem;">
          <strong>Webhook de Resposta do Atendente:</strong><br>
          Configure no Chatwoot (Configurações > Webhooks):<br>
          <code style="word-break: break-all; color: #1e40af;">${window.location.origin}/api/v1/integracoes/chatwoot/webhook?instancia_id=${instId}</code>
        </div>
      `;
    } else if (tipo === 'typebot') {
      campos.innerHTML = `
        <div class="form-grupo">
          <label class="form-label">URL do Typebot</label>
          <input type="url" id="tb-url" class="form-input" placeholder="https://typebot.io ou https://bot.seudominio.com" value="${escaparHTML(configSalva.url || '')}">
        </div>
        <div style="display: grid; grid-template-columns: 2fr 1fr; gap: 0.75rem;">
          <div class="form-grupo">
            <label class="form-label">Slug ou Public ID do Bot</label>
            <input type="text" id="tb-id" class="form-input" placeholder="meu-bot-atendimento-xyz" value="${escaparHTML(configSalva.public_id || '')}">
          </div>
          <div class="form-grupo">
            <label class="form-label">Expiração (Minutos)</label>
            <input type="number" id="tb-expira" class="form-input" value="${Number(configSalva.expiracao_minutos) || 30}" min="1" max="1440">
          </div>
        </div>
        <div class="form-grupo">
          <label class="form-label">Palavra-chave de Transbordo Humano</label>
          <input type="text" id="tb-transbordo" class="form-input" placeholder="Ex: #humano, falar com atendente" value="${escaparHTML(configSalva.palavra_transbordo || '')}">
          <small style="color: #64748b; font-size: 0.75rem;">Ao digitar esta palavra, o bot pausa a automação para atendimento humano.</small>
        </div>
        <div style="margin-top: 0.75rem; display: flex; flex-direction: column; gap: 0.5rem;">
          <label style="display: flex; align-items: center; gap: 0.5rem; font-size: 0.85rem; cursor: pointer;">
            <input type="checkbox" id="tb-ignorar-grupos" ${configSalva.ignorar_grupos !== false ? 'checked' : ''}>
            Ignorar mensagens em grupos (ativar bot apenas em conversas privadas)
          </label>
        </div>
      `;
    } else if (tipo === 'n8n') {
      campos.innerHTML = `
        <div class="form-grupo">
          <label class="form-label">URL do Webhook Trigger no n8n</label>
          <input type="url" id="n8n-url" class="form-input" placeholder="https://n8n.seudominio.com/webhook/..." value="${escaparHTML(configSalva.webhook_url || '')}">
        </div>
        <div class="form-grupo">
          <label class="form-label">Token Secreto / Header X-N8N-Token (Opcional)</label>
          <input type="password" id="n8n-token" class="form-input" placeholder="Token secreto para validação HMAC" value="${escaparHTML(configSalva.token_secreto || '')}">
        </div>
        <div style="background: rgba(234, 75, 113, 0.08); border: 1px solid rgba(234, 75, 113, 0.2); padding: 0.75rem; border-radius: 8px; margin-top: 1rem; font-size: 0.78rem;">
          <strong>Dica n8n:</strong> O WooHub entrega os parâmetros enriquecidos com contato, origem, UTMs e metadados no payload JSON.
        </div>
      `;
    } else if (tipo === 'kommo') {
      campos.innerHTML = `
        <div class="form-grupo">
          <label class="form-label">Subdomínio Kommo CRM</label>
          <input type="text" id="kommo-sub" class="form-input" placeholder="minhaempresa (sem https:// e sem .kommo.com)" value="${escaparHTML(configSalva.subdominio || '')}">
        </div>
        <div class="form-grupo">
          <label class="form-label">Long-Lived Access Token do Kommo</label>
          <input type="password" id="kommo-token" class="form-input" placeholder="Bearer Token gerado na aba Integrações do Kommo" value="${escaparHTML(configSalva.token || '')}">
        </div>
        <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 0.75rem;">
          <div class="form-grupo">
            <label class="form-label">Pipeline ID (Funil)</label>
            <input type="number" id="kommo-pipe" class="form-input" placeholder="Ex: 12345" value="${Number(configSalva.pipeline_id) || ''}">
          </div>
          <div class="form-grupo">
            <label class="form-label">Status ID (Etapa Inicial)</label>
            <input type="number" id="kommo-status" class="form-input" placeholder="Ex: 67890" value="${Number(configSalva.status_id) || ''}">
          </div>
        </div>
        <div style="margin-top: 0.75rem; display: flex; flex-direction: column; gap: 0.5rem;">
          <label style="display: flex; align-items: center; gap: 0.5rem; font-size: 0.85rem; cursor: pointer;">
            <input type="checkbox" id="kommo-lead" ${configSalva.criar_lead_automatico !== false ? 'checked' : ''}>
            Criar Lead automaticamente no Funil ao receber nova mensagem
          </label>
          <label style="display: flex; align-items: center; gap: 0.5rem; font-size: 0.85rem; cursor: pointer;">
            <input type="checkbox" id="kommo-notas" ${configSalva.sincronizar_notas !== false ? 'checked' : ''}>
            Sincronizar histórico de mensagens como notas na timeline do Lead
          </label>
        </div>
      `;
    } else if (tipo === 'rdstation') {
      campos.innerHTML = `
        <div class="form-grupo">
          <label class="form-label">Token Privado (RD Station Marketing)</label>
          <input type="password" id="rd-token-mkt" class="form-input" placeholder="API Token do RD Station Marketing" value="${escaparHTML(configSalva.token_privado || '')}">
        </div>
        <div class="form-grupo">
          <label class="form-label">Identificador da Conversão</label>
          <input type="text" id="rd-identificador" class="form-input" placeholder="Ex: whatsapp-inbound" value="${escaparHTML(configSalva.identificador_conversao || '')}">
        </div>
        <div class="form-grupo">
          <label class="form-label">Token do RD Station CRM (Opcional)</label>
          <input type="password" id="rd-token-crm" class="form-input" placeholder="Token do CRM para criação de oportunidades" value="${escaparHTML(configSalva.token_crm || '')}">
        </div>
        <div style="margin-top: 0.75rem; display: flex; flex-direction: column; gap: 0.5rem;">
          <label style="display: flex; align-items: center; gap: 0.5rem; font-size: 0.85rem; cursor: pointer;">
            <input type="checkbox" id="rd-oportunidade" ${configSalva.criar_oportunidade_crm ? 'checked' : ''}>
            Criar Oportunidade no RD Station CRM automaticamente
          </label>
        </div>
      `;
    } else if (tipo === 'webhook') {
      let urlsTexto = '';
      if (configSalva.destinos && Array.isArray(configSalva.destinos)) {
        urlsTexto = configSalva.destinos.map(d => d.url).join('\n');
      }
      campos.innerHTML = `
        <div class="form-grupo">
          <label class="form-label">URLs de Destino (uma por linha)</label>
          <textarea id="wh-urls" class="form-input" style="height: 120px; font-family: monospace; font-size: 0.82rem;" placeholder="https://api.empresa.com/webhook&#10;https://backup.empresa.com/hook">${escaparHTML(urlsTexto)}</textarea>
        </div>
      `;
    }
  }

  if (modal) modal.classList.add('aberto');
}

function fecharModalConfigCaixa() {
  const modal = document.getElementById('modal-config-caixa');
  if (modal) modal.classList.remove('aberto');
}

function coletarConfiguracoesDoModal(tipo) {
  if (tipo === 'chatwoot') {
    return {
      url: document.getElementById('cw-url') ? document.getElementById('cw-url').value.trim() : '',
      account_id: document.getElementById('cw-account') ? document.getElementById('cw-account').value.trim() : '',
      inbox_id: document.getElementById('cw-inbox') ? document.getElementById('cw-inbox').value.trim() : '',
      token: document.getElementById('cw-token') ? document.getElementById('cw-token').value.trim() : '',
      reabrir_conversa: document.getElementById('cw-reabrir') ? document.getElementById('cw-reabrir').checked : true
    };
  } else if (tipo === 'typebot') {
    return {
      url: document.getElementById('tb-url') ? document.getElementById('tb-url').value.trim() : '',
      public_id: document.getElementById('tb-id') ? document.getElementById('tb-id').value.trim() : '',
      expiracao_minutos: parseInt(document.getElementById('tb-expira')?.value, 10) || 30,
      palavra_transbordo: document.getElementById('tb-transbordo') ? document.getElementById('tb-transbordo').value.trim() : '',
      ignorar_grupos: document.getElementById('tb-ignorar-grupos') ? document.getElementById('tb-ignorar-grupos').checked : true
    };
  } else if (tipo === 'n8n') {
    return {
      webhook_url: document.getElementById('n8n-url') ? document.getElementById('n8n-url').value.trim() : '',
      token_secreto: document.getElementById('n8n-token') ? document.getElementById('n8n-token').value.trim() : ''
    };
  } else if (tipo === 'kommo') {
    return {
      subdominio: document.getElementById('kommo-sub') ? document.getElementById('kommo-sub').value.trim() : '',
      token: document.getElementById('kommo-token') ? document.getElementById('kommo-token').value.trim() : '',
      pipeline_id: parseInt(document.getElementById('kommo-pipe')?.value, 10) || 0,
      status_id: parseInt(document.getElementById('kommo-status')?.value, 10) || 0,
      criar_lead_automatico: document.getElementById('kommo-lead') ? document.getElementById('kommo-lead').checked : true,
      sincronizar_notas: document.getElementById('kommo-notas') ? document.getElementById('kommo-notas').checked : true
    };
  } else if (tipo === 'rdstation') {
    return {
      token_privado: document.getElementById('rd-token-mkt') ? document.getElementById('rd-token-mkt').value.trim() : '',
      identificador_conversao: document.getElementById('rd-identificador') ? document.getElementById('rd-identificador').value.trim() : '',
      token_crm: document.getElementById('rd-token-crm') ? document.getElementById('rd-token-crm').value.trim() : '',
      criar_oportunidade_crm: document.getElementById('rd-oportunidade') ? document.getElementById('rd-oportunidade').checked : false
    };
  } else if (tipo === 'webhook') {
    const el = document.getElementById('wh-urls');
    const linhas = el ? el.value.split('\n').map(l => l.trim()).filter(l => l.length > 0) : [];
    return {
      destinos: linhas.map(u => ({ url: u }))
    };
  }
  return {};
}

async function salvarConfiguracaoCaixa(event) {
  if (event) event.preventDefault();
  const instId = obterInstanciaSelecionadaIntegracoes();
  const tipo = tipoCaixaAtualModal;
  if (!instId || !tipo) return;

  const ativoEl = document.getElementById('modal-caixa-ativo');
  const ativo = ativoEl ? ativoEl.checked : false;
  const configObj = coletarConfiguracoesDoModal(tipo);

  const btnSalvar = document.getElementById('btn-salvar-caixa');
  if (btnSalvar) {
    btnSalvar.disabled = true;
    btnSalvar.innerText = 'Salvando...';
  }

  try {
    const res = await fetchAutenticado(`/api/v1/instancia/${instId}/integracoes/${tipo}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        ativo: ativo,
        configuracoes: configObj
      })
    });
    const dados = await res.json();
    if (dados.sucesso) {
      mostrarNotificacaoToast(`Caixa ${tipo.toUpperCase()} salva com sucesso!`, 'sucesso');
      fecharModalConfigCaixa();
      carregarCaixasIntegracao();
    } else {
      mostrarNotificacaoToast('Erro ao salvar: ' + (dados.erro || 'Falha'), 'erro');
    }
  } catch (err) {
    mostrarNotificacaoToast('Erro de comunicação: ' + err.message, 'erro');
  } finally {
    if (btnSalvar) {
      btnSalvar.disabled = false;
      btnSalvar.innerText = 'Salvar Caixa';
    }
  }
}

async function testarConexaoCaixaAtiva() {
  const instId = obterInstanciaSelecionadaIntegracoes();
  const tipo = tipoCaixaAtualModal;
  if (!instId || !tipo) return;

  const alerta = document.getElementById('modal-caixa-alerta');
  if (alerta) {
    alerta.style.display = 'block';
    alerta.style.background = '#f8fafc';
    alerta.style.color = '#334155';
    alerta.style.border = '1px solid #cbd5e1';
    alerta.innerText = '🧪 Testando conexão com ' + tipo.toUpperCase() + '...';
  }

  const configObj = coletarConfiguracoesDoModal(tipo);

  try {
    const res = await fetchAutenticado(`/api/v1/instancia/${instId}/integracoes/${tipo}/testar`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ configuracoes: configObj })
    });
    const dados = await res.json();
    if (dados.sucesso) {
      if (alerta) {
        alerta.style.background = '#ecfdf5';
        alerta.style.color = '#065f46';
        alerta.style.border = '1px solid #10b981';
        alerta.innerHTML = `✅ <strong>${escaparHTML(dados.mensagem || 'Conexão efetuada com sucesso!')}</strong>`;
      }
      mostrarNotificacaoToast('Conexão validada com sucesso!', 'sucesso');
    } else {
      if (alerta) {
        alerta.style.background = '#fef2f2';
        alerta.style.color = '#991b1b';
        alerta.style.border = '1px solid #f87171';
        alerta.innerHTML = `❌ <strong>Falha na conexão:</strong> ${escaparHTML(dados.erro || 'Não foi possível validar')}`;
      }
    }
  } catch (err) {
    if (alerta) {
      alerta.style.background = '#fef2f2';
      alerta.style.color = '#991b1b';
      alerta.style.border = '1px solid #f87171';
      alerta.innerText = '❌ Erro de requisição: ' + err.message;
    }
  }
}

async function testarConexaoRapida(tipo) {
  abrirModalConfigCaixa(tipo);
  setTimeout(() => {
    testarConexaoCaixaAtiva();
  }, 300);
}

// --- Rastreamento de Origem & Campanhas ---

async function carregarAtribuicoesOrigem() {
  const instId = obterInstanciaSelecionadaIntegracoes();
  const corpoTabela = document.getElementById('tabela-atribuicoes-corpo');
  if (!corpoTabela) return;

  if (!instId) {
    corpoTabela.innerHTML = '<tr><td colspan="7" style="text-align: center; color: #64748b; padding: 2rem;">Selecione uma instância para ver atribuições.</td></tr>';
    return;
  }

  try {
    const res = await fetchAutenticado(`/api/v1/instancia/${instId}/atribuicoes?limite=100`);
    const dados = await res.json();
    if (!dados || !dados.atribuicoes) {
      corpoTabela.innerHTML = '<tr><td colspan="7" style="text-align: center; color: #ef4444; padding: 2rem;">Falha ao carregar dados de rastreamento.</td></tr>';
      return;
    }

    const lista = dados.atribuicoes;

    let totalMeta = 0;
    let totalGoogle = 0;
    let totalOutros = 0;

    lista.forEach(a => {
      const canalLower = (a.origem_canal || '').toLowerCase();
      if (canalLower.includes('meta') || canalLower.includes('instagram') || canalLower.includes('facebook')) {
        totalMeta++;
      } else if (canalLower.includes('google')) {
        totalGoogle++;
      } else {
        totalOutros++;
      }
    });

    const mTotal = document.getElementById('metric-total-atrib');
    const mMeta = document.getElementById('metric-meta-atrib');
    const mGoogle = document.getElementById('metric-google-atrib');
    const mOutros = document.getElementById('metric-outros-atrib');

    if (mTotal) mTotal.innerText = lista.length;
    if (mMeta) mMeta.innerText = totalMeta;
    if (mGoogle) mGoogle.innerText = totalGoogle;
    if (mOutros) mOutros.innerText = totalOutros;

    if (lista.length === 0) {
      corpoTabela.innerHTML = '<tr><td colspan="7" style="text-align: center; color: #64748b; padding: 2rem;">Nenhum disparo com atribuição de campanha registrado ainda.</td></tr>';
      return;
    }

    corpoTabela.innerHTML = '';
    lista.forEach(a => {
      const tr = document.createElement('tr');

      let badgeClasse = 'badge-origem organico';
      const canalLower = (a.origem_canal || '').toLowerCase();
      if (canalLower.includes('instagram')) badgeClasse = 'badge-origem instagram';
      else if (canalLower.includes('facebook') || canalLower.includes('meta')) badgeClasse = 'badge-origem facebook';
      else if (canalLower.includes('google')) badgeClasse = 'badge-origem google';
      else if (canalLower.includes('site')) badgeClasse = 'badge-origem site';

      const clickID = a.ctwa_clid || a.ad_source_id || a.referencia || '-';
      const utmInfo = (a.utm_source || a.utm_medium) ? `${a.utm_source || '-'} / ${a.utm_medium || '-'}` : '-';
      const dataHora = a.atualizado_em ? new Date(a.atualizado_em).toLocaleString('pt-BR') : '-';

      tr.innerHTML = `
        <td><strong style="color: #0f172a;">${escaparHTML(a.numero_formatado || a.jid)}</strong></td>
        <td><span class="${badgeClasse}">${escaparHTML(a.origem_canal || 'Orgânico')}</span></td>
        <td>${escaparHTML(a.campanha || '-')}</td>
        <td><code>${escaparHTML(utmInfo)}</code></td>
        <td><span style="font-family: monospace; font-size: 0.78rem; color: #475569;" title="${escaparHTML(clickID)}">${escaparHTML(clickID.substring(0, 16))}...</span></td>
        <td style="max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;" title="${escaparHTML(a.referrer_url || '-')}">${escaparHTML(a.referrer_url || '-')}</td>
        <td style="font-size: 0.8rem; color: #64748b;">${dataHora}</td>
      `;
      corpoTabela.appendChild(tr);
    });
  } catch (err) {
    corpoTabela.innerHTML = `<tr><td colspan="7" style="text-align: center; color: #ef4444; padding: 2rem;">Erro ao carregar atribuições: ${escaparHTML(err.message)}</td></tr>`;
  }
}
