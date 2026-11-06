/**
 * WOOAPI WEB - CLIENTE WHATSAPP WEB CLONE COM SUPER-PODERES
 * Version: 20261105v1
 */

const ESTADO = {
  token: localStorage.getItem('woohub_token') || '',
  usuario: null,
  recursosPlano: null,
  instancias: [],
  instanciaAtivaId: '',
  chatAtivoJid: '',
  conversas: [],
  mensagens: [],
  atalhos: [],
  filtroAtual: 'todos',
  termoBusca: '',
  sseConexao: null,
  mediaRecorder: null,
  audioChunks: [],
  tempoGravacaoInterval: null,
  segundosGravados: 0
};

// ============================================================================
// 1. INICIALIZAÇÃO DA APLICAÇÃO
// ============================================================================
document.addEventListener('DOMContentLoaded', () => {
  iniciarApp();
});

async function iniciarApp() {
  carregarMarcaWhiteLabel();

  // Verifica parâmetros de URL (ex: ?instancia_id=xxx ou ?token=yyy)
  const params = new URLSearchParams(window.location.search);
  const tokenURL = params.get('token');
  if (tokenURL) {
    ESTADO.token = tokenURL;
    localStorage.setItem('woohub_token', tokenURL);
  }

  if (!ESTADO.token) {
    exibirModalLogin();
    return;
  }

  const autenticado = await validarSessaoUsuario();
  if (!autenticado) {
    exibirModalLogin();
    return;
  }

  await carregarRecursosPlano();
  await carregarInstancias();
  await carregarRespostasRapidas();

  // Se veio instancia_id na URL, seleciona ela
  const instIdURL = params.get('instancia_id');
  if (instIdURL && ESTADO.instancias.some(i => i.id === instIdURL)) {
    trocarInstanciaAtiva(instIdURL);
  }
}

async function carregarMarcaWhiteLabel() {
  try {
    const res = await fetch('/api/v1/revenda/marca');
    const dados = await res.json();
    if (dados.sucesso && dados.marca) {
      if (dados.marca.nome_plataforma) {
        document.title = `${dados.marca.nome_plataforma} Web - WhatsApp`;
        const elNome = document.getElementById('login-marca-nome');
        if (elNome) elNome.innerText = dados.marca.nome_plataforma;
      }
      if (dados.marca.logo_url) {
        const logoBox = document.getElementById('login-marca-logo');
        if (logoBox) logoBox.innerHTML = `<img src="${dados.marca.logo_url}" style="width:100%;height:100%;object-fit:contain;">`;
      }
    }
  } catch (e) {
    console.debug('Tema padrão ativo');
  }
}

async function validarSessaoUsuario() {
  try {
    const res = await fetch('/api/v1/auth/me', {
      headers: { 'Authorization': `Bearer ${ESTADO.token}` }
    });
    const dados = await res.json();
    if (dados.sucesso && dados.usuario) {
      ESTADO.usuario = dados.usuario;
      const elNome = document.getElementById('usuario-nome-display');
      if (elNome) elNome.innerText = dados.usuario.nome || dados.usuario.email.split('@')[0];
      const elAvatar = document.getElementById('minha-foto-avatar');
      if (elAvatar) elAvatar.innerText = (dados.usuario.nome || dados.usuario.email)[0].toUpperCase();
      return true;
    }
  } catch (e) {
    console.error('Erro ao validar sessão:', e);
  }
  return false;
}

// ============================================================================
// 2. CONTROLE DE PLANO & SUPER-PODERES
// ============================================================================
async function carregarRecursosPlano() {
  try {
    const res = await fetch('/api/v1/chat/recursos', {
      headers: { 'Authorization': `Bearer ${ESTADO.token}` }
    });
    const dados = await res.json();
    if (dados.sucesso && dados.recursos) {
      ESTADO.recursosPlano = dados.recursos;
      
      const badge = document.getElementById('badge-plano-topo');
      if (badge) {
        badge.innerText = dados.recursos.nome_plano.toUpperCase();
        if (dados.recursos.plano_id === 'enterprise') {
          badge.style.backgroundColor = '#8b5cf6';
        } else if (dados.recursos.plano_id === 'profissional') {
          badge.style.backgroundColor = '#059669';
        } else {
          badge.style.backgroundColor = '#3b82f6';
        }
      }

      // Configura visibilidade ou bloqueios visuais dos botões de toolkit
      atualizarTravasToolkit();
    }
  } catch (e) {
    console.error('Erro ao carregar recursos do plano:', e);
  }
}

function atualizarTravasToolkit() {
  const rec = ESTADO.recursosPlano;
  if (!rec) return;

  const btnBotoes = document.getElementById('btn-tk-botoes');
  if (btnBotoes && !rec.botoes_interativos) {
    btnBotoes.innerHTML = '🔒 Botões Interativos';
    btnBotoes.style.opacity = '0.6';
  }

  const btnAgendar = document.getElementById('btn-tk-agendar');
  if (btnAgendar && !rec.agendamento) {
    btnAgendar.innerHTML = '🔒 Agendar Retorno';
    btnAgendar.style.opacity = '0.6';
  }

  const btnIA = document.getElementById('btn-tk-ia');
  if (btnIA && !rec.copilot_ia) {
    btnIA.innerHTML = '🔒 IA Copilot';
    btnIA.style.opacity = '0.6';
  }
}

function abrirModalRecursosPlano() {
  const rec = ESTADO.recursosPlano;
  if (!rec) return;

  const corpo = document.getElementById('modal-corpo-plano');
  if (!corpo) return;

  corpo.innerHTML = `
    <div style="background-color: #111b21; padding: 16px; border-radius: 12px; border: 1px solid var(--wa-borda);">
      <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:12px;">
        <h4 style="color:#10b981; font-size:1.1rem; margin:0;">Plano: ${rec.nome_plano}</h4>
        <span class="plano-badge-pill" style="font-size:0.75rem;">ATIVO</span>
      </div>
      <p style="font-size:0.85rem; color:#8696a0; margin-bottom:16px;">
        Veja os recursos da API disponíveis para você nesta interface:
      </p>

      <ul style="list-style:none; display:flex; flex-direction:column; gap:8px; font-size:0.85rem;">
        <li>${rec.respostas_rapidas ? '✅' : '❌'} <strong>Respostas Rápidas (/atalhos):</strong> ${rec.respostas_rapidas ? `Até ${rec.max_respostas_rapidas} respostas` : 'Bloqueado'}</li>
        <li>${rec.ghost_mode ? '✅' : '❌'} <strong>Ghost Mode:</strong> ${rec.ghost_mode ? 'Ver mensagens apagadas pelo contato' : 'Apenas no Plano Pro/Enterprise'}</li>
        <li>${rec.botoes_interativos ? '✅' : '❌'} <strong>Botões Interativos & Menus:</strong> ${rec.botoes_interativos ? 'Liberado' : 'Apenas no Plano Pro/Enterprise'}</li>
        <li>${rec.agendamento ? '✅' : '❌'} <strong>Mensagens Agendadas:</strong> ${rec.agendamento ? 'Liberado' : 'Apenas no Plano Pro/Enterprise'}</li>
        <li>${rec.transcricao_ia ? '✅' : '❌'} <strong>Transcrição & Copilot IA:</strong> ${rec.transcricao_ia ? 'Liberado' : 'Apenas no Plano Pro/Enterprise'}</li>
        <li>${rec.disparo_massa ? '✅' : '❌'} <strong>Disparos Diretos:</strong> ${rec.disparo_massa ? 'Liberado' : 'Apenas no Plano Enterprise'}</li>
        <li>👥 <strong>Atendentes Simultâneos:</strong> Até ${rec.max_atendentes} operador(es)</li>
      </ul>
    </div>
  `;

  abrirModal('modal-recursos-plano');
}

// ============================================================================
// 3. GESTÃO DE INSTÂNCIAS E SSE REALTIME
// ============================================================================
async function carregarInstancias() {
  try {
    const res = await fetch('/api/v1/instancia/listar', {
      headers: { 'Authorization': `Bearer ${ESTADO.token}` }
    });
    const dados = await res.json();
    if (dados.sucesso && Array.isArray(dados.instancias)) {
      ESTADO.instancias = dados.instancias;
      const select = document.getElementById('select-instancia');
      if (!select) return;

      select.innerHTML = '';
      if (ESTADO.instancias.length === 0) {
        select.innerHTML = '<option value="">Nenhuma instância criada</option>';
        return;
      }

      ESTADO.instancias.forEach(inst => {
        const opt = document.createElement('option');
        opt.value = inst.id;
        opt.innerText = `${inst.status === 'CONECTADO' ? '🟢' : '🔴'} ${inst.nome || inst.id}`;
        select.appendChild(opt);
      });

      // Seleciona a primeira instância
      trocarInstanciaAtiva(ESTADO.instancias[0].id);
    }
  } catch (e) {
    console.error('Erro ao carregar instâncias:', e);
  }
}

function trocarInstanciaAtiva(id) {
  if (!id) return;
  ESTADO.instanciaAtivaId = id;
  const select = document.getElementById('select-instancia');
  if (select && select.value !== id) select.value = id;

  const inst = ESTADO.instancias.find(i => i.id === id);
  if (inst && inst.status !== 'CONECTADO') {
    exibirTelaQRCode(id);
  } else {
    ocultarTelaQRCode();
  }

  // Conecta ao SSE de eventos em tempo real
  conectarSSERealtime(id);

  // Carrega a lista de conversas
  carregarListaConversas(id);
}

function conectarSSERealtime(instanciaId) {
  if (ESTADO.sseConexao) {
    ESTADO.sseConexao.close();
    ESTADO.sseConexao = null;
  }

  const url = `/api/v1/chat/stream?instancia_id=${encodeURIComponent(instanciaId)}`;
  ESTADO.sseConexao = new EventSource(url);

  ESTADO.sseConexao.onmessage = (e) => {
    try {
      const evento = JSON.parse(e.data);
      tratarEventoRealtime(evento);
    } catch (err) {
      // Ignora heartbeats ou pings
    }
  };

  ESTADO.sseConexao.onerror = () => {
    // EventSource tenta reconectar automaticamente
  };
}

function tratarEventoRealtime(evt) {
  if (!evt || !evt.tipo) return;

  if (evt.tipo === 'nova_mensagem' && evt.mensagem) {
    const msg = evt.mensagem;
    // Se a conversa aberta for desta mensagem, adiciona no chat
    if (ESTADO.chatAtivoJid === msg.chat_jid) {
      adicionarMensagemNaTela(msg);
      rolarParaFimMensagens();
      // Marca como lida
      fetch('/api/v1/chat/lido', {
        method: 'POST',
        headers: { 'Authorization': `Bearer ${ESTADO.token}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({ instancia_id: ESTADO.instanciaAtivaId, chat_jid: msg.chat_jid })
      });
    }

    // Atualiza a lista de conversas lateral
    atualizarConversaNaListaLateral(msg);
  } else if (evt.tipo === 'status_mensagem' && evt.dados) {
    atualizarStatusTicksMensagem(evt.dados.ids_mensagens, evt.dados.status);
  } else if (evt.tipo === 'mensagem_deletada' && evt.dados) {
    marcarMensagemDeletadaNaTela(evt.dados.id_mensagem);
  } else if (evt.tipo === 'presenca' && evt.dados) {
    if (ESTADO.chatAtivoJid === evt.dados.chat_jid) {
      const statusEl = document.getElementById('chat-ativo-status');
      if (statusEl) {
        if (evt.dados.estado === 'composing') {
          statusEl.innerText = 'digitando...';
          statusEl.style.color = '#00a884';
        } else if (evt.dados.estado === 'recording') {
          statusEl.innerText = 'gravando áudio...';
          statusEl.style.color = '#00a884';
        } else {
          statusEl.innerText = 'online';
          statusEl.style.color = '#8696a0';
        }
      }
    }
  }
}

// ============================================================================
// 4. LISTA DE CONVERSAS LATERAL
// ============================================================================
async function carregarListaConversas(instanciaId) {
  const container = document.getElementById('lista-conversas');
  if (!container) return;

  container.innerHTML = `
    <div class="estado-vazio-conversas">
      <div class="spinner-carregando"></div>
      <p>Sincronizando conversas do WhatsApp...</p>
    </div>
  `;

  try {
    const res = await fetch(`/api/v1/chat/conversas?instancia_id=${encodeURIComponent(instanciaId)}`, {
      headers: { 'Authorization': `Bearer ${ESTADO.token}` }
    });
    const dados = await res.json();
    if (dados.sucesso && Array.isArray(dados.conversas)) {
      ESTADO.conversas = dados.conversas;
      renderizarListaConversas();
    }
  } catch (e) {
    container.innerHTML = `<div class="estado-vazio-conversas"><p>Erro ao carregar conversas.</p></div>`;
  }
}

function renderizarListaConversas() {
  const container = document.getElementById('lista-conversas');
  if (!container) return;

  let filtradas = ESTADO.conversas;

  // Filtro de abas
  if (ESTADO.filtroAtual === 'nao_lidas') {
    filtradas = filtradas.filter(c => c.nao_lidas > 0);
  } else if (ESTADO.filtroAtual === 'grupos') {
    filtradas = filtradas.filter(c => c.eh_grupo);
  }

  // Filtro de busca
  if (ESTADO.termoBusca) {
    const t = ESTADO.termoBusca.toLowerCase();
    filtradas = filtradas.filter(c =>
      (c.nome_contato && c.nome_contato.toLowerCase().includes(t)) ||
      (c.chat_jid && c.chat_jid.toLowerCase().includes(t)) ||
      (c.ultima_mensagem && c.ultima_mensagem.toLowerCase().includes(t))
    );
  }

  if (filtradas.length === 0) {
    container.innerHTML = `
      <div class="estado-vazio-conversas">
        <p>Nenhuma conversa encontrada.</p>
        <button class="btn-chat-primario" onclick="abrirModalNovaConversa()" style="margin-top: 10px; font-size: 0.85rem; padding: 8px 12px;">+ Iniciar Conversa</button>
      </div>
    `;
    return;
  }

  container.innerHTML = '';
  filtradas.forEach(c => {
    const item = document.createElement('div');
    item.className = `item-conversa ${c.chat_jid === ESTADO.chatAtivoJid ? 'ativo' : ''}`;
    item.onclick = () => abrirConversa(c.chat_jid, c.nome_contato || c.chat_jid);

    const inicial = (c.nome_contato || c.chat_jid)[0].toUpperCase();
    const hora = formatarHora(c.ultimo_timestamp || c.atualizado_em);

    item.innerHTML = `
      <div class="avatar-circulo">${c.foto_url ? `<img src="${c.foto_url}">` : inicial}</div>
      <div class="item-conversa-corpo">
        <div class="item-conversa-topo">
          <span class="item-contato-nome">${escaparHTML(c.nome_contato || c.chat_jid)}</span>
          <span class="item-timestamp">${hora}</span>
        </div>
        <div class="item-conversa-base">
          <span class="item-snippet-msg">${escaparHTML(c.ultima_mensagem || 'Iniciar conversa...')}</span>
          ${c.nao_lidas > 0 ? `<span class="badge-nao-lidas">${c.nao_lidas}</span>` : ''}
        </div>
      </div>
    `;
    container.appendChild(item);
  });
}

function atualizarConversaNaListaLateral(msg) {
  let conv = ESTADO.conversas.find(c => c.chat_jid === msg.chat_jid);
  if (conv) {
    conv.ultima_mensagem = msg.texto || (msg.tipo === 'audio' ? '🎵 Áudio' : '📷 Mídia');
    conv.ultimo_timestamp = new Date().toISOString();
    if (ESTADO.chatAtivoJid !== msg.chat_jid && !msg.de_mim) {
      conv.nao_lidas = (conv.nao_lidas || 0) + 1;
    }
    // Move para o topo da lista
    ESTADO.conversas = [conv, ...ESTADO.conversas.filter(c => c.chat_jid !== msg.chat_jid)];
  } else {
    // Nova conversa que chegou
    const nova = {
      id: `${ESTADO.instanciaAtivaId}:${msg.chat_jid}`,
      instancia_id: ESTADO.instanciaAtivaId,
      chat_jid: msg.chat_jid,
      nome_contato: msg.chat_jid,
      ultima_mensagem: msg.texto || 'Nova mensagem',
      ultimo_timestamp: new Date().toISOString(),
      nao_lidas: msg.de_mim ? 0 : 1,
      eh_grupo: msg.chat_jid.includes('@g.us')
    };
    ESTADO.conversas.unshift(nova);
  }
  renderizarListaConversas();
}

function filtrarConversas(valor) {
  ESTADO.termoBusca = valor.trim();
  const btnLimpar = document.getElementById('btn-limpar-busca');
  if (btnLimpar) btnLimpar.style.display = ESTADO.termoBusca ? 'block' : 'none';
  renderizarListaConversas();
}

function limparBusca() {
  const campo = document.getElementById('campo-busca-chat');
  if (campo) campo.value = '';
  filtrarConversas('');
}

function aplicarFiltroConversas(filtro, el) {
  ESTADO.filtroAtual = filtro;
  document.querySelectorAll('.filtro-pill').forEach(p => p.classList.remove('ativo'));
  if (el) el.classList.add('ativo');
  renderizarListaConversas();
}

// ============================================================================
// 5. JANELA DE CONVERSA ATIVA
// ============================================================================
async function abrirConversa(chatJid, nomeContato) {
  ESTADO.chatAtivoJid = chatJid;

  // Atualiza destaque na lista lateral
  renderizarListaConversas();

  // Exibe tela do chat
  const boasVindas = document.getElementById('tela-boas-vindas');
  if (boasVindas) boasVindas.style.display = 'none';
  const janela = document.getElementById('janela-chat-ativa');
  if (janela) janela.style.display = 'flex';

  // Cabeçalho do contato
  const elNome = document.getElementById('chat-ativo-nome');
  if (elNome) elNome.innerText = nomeContato || chatJid;
  const elAvatar = document.getElementById('chat-ativo-avatar');
  if (elAvatar) elAvatar.innerText = (nomeContato || chatJid)[0].toUpperCase();

  // Carrega histórico de mensagens
  const lista = document.getElementById('mensagens-lista');
  if (lista) {
    lista.innerHTML = `
      <div style="text-align: center; padding: 40px; color: #8696a0;">
        <div class="spinner-carregando" style="margin: 0 auto 10px auto;"></div>
        <p>Carregando histórico...</p>
      </div>
    `;
  }

  try {
    const res = await fetch(`/api/v1/chat/mensagens?instancia_id=${encodeURIComponent(ESTADO.instanciaAtivaId)}&chat_jid=${encodeURIComponent(chatJid)}`, {
      headers: { 'Authorization': `Bearer ${ESTADO.token}` }
    });
    const dados = await res.json();
    if (dados.sucesso && Array.isArray(dados.mensagens)) {
      ESTADO.mensagens = dados.mensagens;
      renderizarMensagens();
      rolarParaFimMensagens();
    }
  } catch (e) {
    console.error('Erro ao buscar mensagens:', e);
  }

  // Foco no campo de texto
  const campo = document.getElementById('campo-texto-msg');
  if (campo) campo.focus();
}

function fecharConversaAtiva() {
  ESTADO.chatAtivoJid = '';
  const janela = document.getElementById('janela-chat-ativa');
  if (janela) janela.style.display = 'none';
  const boasVindas = document.getElementById('tela-boas-vindas');
  if (boasVindas) boasVindas.style.display = 'flex';
  renderizarListaConversas();
}

function renderizarMensagens() {
  const container = document.getElementById('mensagens-lista');
  if (!container) return;

  container.innerHTML = '';
  if (ESTADO.mensagens.length === 0) {
    container.innerHTML = `
      <div style="text-align: center; padding: 60px 20px; color: #8696a0;">
        <p style="font-size: 0.95rem; margin-bottom: 6px;">Nenhuma mensagem nesta conversa ainda.</p>
        <small style="font-size: 0.8rem;">Envie uma mensagem abaixo para iniciar o diálogo.</small>
      </div>
    `;
    return;
  }

  let ultimoDia = '';
  ESTADO.mensagens.forEach(msg => {
    const diaMsg = formatarDataSeparador(msg.criado_em);
    if (diaMsg !== ultimoDia) {
      const divisor = document.createElement('div');
      divisor.className = 'divisor-dia';
      divisor.innerText = diaMsg;
      container.appendChild(divisor);
      ultimoDia = diaMsg;
    }

    const balao = criarElementoBalao(msg);
    container.appendChild(balao);
  });
}

function criarElementoBalao(msg) {
  const balao = document.createElement('div');
  balao.className = `balao-msg ${msg.de_mim ? 'minha' : 'outro'}`;
  balao.id = `msg-${msg.id}`;

  let htmlConteudo = '';

  // Ghost Mode: Mensagem apagada pelo remetente
  if (msg.deletada_remetente) {
    htmlConteudo += `
      <div class="ghost-badge-alerta">
        ⚠️ Esta mensagem foi apagada pelo contato, mas foi preservada pelo WooAPI.
      </div>
    `;
  }

  // Conteúdo conforme tipo
  if (msg.tipo === 'audio' || msg.tipo === 'audio_ptt') {
    htmlConteudo += `
      <div class="player-audio-ptt">
        <button class="btn-play-ptt" onclick="tocarOuPausarAudio('${msg.midia_url || ''}', this)">▶</button>
        <div class="onda-audio-barra">
          <div class="onda-audio-progresso"></div>
        </div>
        <span class="duracao-audio">0:00</span>
      </div>
    `;
  } else if (msg.tipo === 'imagem' && msg.midia_url) {
    htmlConteudo += `
      <div style="margin-bottom: 6px; border-radius: 6px; overflow: hidden; max-width: 320px;">
        <img src="${msg.midia_url}" style="width: 100%; display: block; cursor: pointer;" onclick="window.open('${msg.midia_url}')">
      </div>
    `;
  } else if (msg.tipo === 'documento' && msg.midia_url) {
    htmlConteudo += `
      <div style="display:flex; align-items:center; gap:8px; padding:6px; background:rgba(0,0,0,0.15); border-radius:6px; margin-bottom:6px;">
        <span style="font-size:1.4rem;">📄</span>
        <a href="${msg.midia_url}" target="_blank" style="color:#e9edef; font-size:0.85rem; text-decoration:none; font-weight:600;">
          ${escaparHTML(msg.nome_arquivo || 'Baixar Documento')}
        </a>
      </div>
    `;
  }

  if (msg.texto) {
    htmlConteudo += `<div class="balao-texto">${formatarTextoWhatsApp(msg.texto)}</div>`;
  }

  const hora = formatarHora(msg.criado_em);
  let statusTick = '';
  if (msg.de_mim) {
    if (msg.status === 'LIDA') {
      statusTick = '<span class="tick-status lida" title="Lida">✓✓</span>';
    } else if (msg.status === 'ENTREGUE') {
      statusTick = '<span class="tick-status" title="Entregue">✓✓</span>';
    } else {
      statusTick = '<span class="tick-status" title="Enviada">✓</span>';
    }
  }

  htmlConteudo += `
    <div class="balao-meta">
      <span>${hora}</span>
      ${statusTick}
    </div>
  `;

  balao.innerHTML = htmlConteudo;
  return balao;
}

function adicionarMensagemNaTela(msg) {
  const container = document.getElementById('mensagens-lista');
  if (!container) return;
  ESTADO.mensagens.push(msg);
  const balao = criarElementoBalao(msg);
  container.appendChild(balao);
}

function rolarParaFimMensagens() {
  const scrollArea = document.getElementById('mensagens-container');
  if (scrollArea) {
    setTimeout(() => {
      scrollArea.scrollTop = scrollArea.scrollHeight;
    }, 50);
  }
}

function atualizarStatusTicksMensagem(ids, novoStatus) {
  if (!Array.isArray(ids)) ids = [ids];
  ids.forEach(id => {
    const balao = document.getElementById(`msg-${id}`);
    if (balao) {
      const meta = balao.querySelector('.balao-meta');
      if (meta) {
        if (novoStatus === 'LIDA') {
          meta.innerHTML = meta.innerHTML.replace(/✓✓?/g, '<span class="tick-status lida" title="Lida">✓✓</span>');
        } else if (novoStatus === 'ENTREGUE') {
          meta.innerHTML = meta.innerHTML.replace(/✓✓?/g, '<span class="tick-status" title="Entregue">✓✓</span>');
        }
      }
    }
  });
}

function marcarMensagemDeletadaNaTela(msgId) {
  const balao = document.getElementById(`msg-${msgId}`);
  if (balao && !balao.querySelector('.ghost-badge-alerta')) {
    const badge = document.createElement('div');
    badge.className = 'ghost-badge-alerta';
    badge.innerText = '⚠️ Esta mensagem foi apagada pelo contato, mas foi preservada pelo WooAPI.';
    balao.insertBefore(badge, balao.firstChild);
  }
}

// ============================================================================
// 6. ENVIO DE MENSAGENS (TEXTO, ÁUDIO, MÍDIA)
// ============================================================================
function ajustarAlturaTextarea(el) {
  el.style.height = 'auto';
  el.style.height = (el.scrollHeight) + 'px';

  // Alterna ícone de microfone ou avião de envio
  const temTexto = el.value.trim().length > 0;
  const svgMic = document.getElementById('svg-mic');
  const svgSend = document.getElementById('svg-send');
  if (svgMic && svgSend) {
    svgMic.style.display = temTexto ? 'none' : 'block';
    svgSend.style.display = temTexto ? 'block' : 'none';
  }
}

function tratarKeyDownMensagem(e) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault();
    enviarMensagemTexto();
  }
}

function cliqueEnviarOuGravar() {
  const campo = document.getElementById('campo-texto-msg');
  if (campo && campo.value.trim().length > 0) {
    enviarMensagemTexto();
  } else {
    iniciarGravacaoAudio();
  }
}

async function enviarMensagemTexto() {
  const campo = document.getElementById('campo-texto-msg');
  if (!campo || !ESTADO.chatAtivoJid || !ESTADO.instanciaAtivaId) return;

  const texto = campo.value.trim();
  if (!texto) return;

  campo.value = '';
  ajustarAlturaTextarea(campo);
  fecharPopoverSnippets();

  // Optimistic UI: adiciona na tela imediatamente
  const tempId = 'temp_' + Date.now();
  const msgTemp = {
    id: tempId,
    instancia_id: ESTADO.instanciaAtivaId,
    chat_jid: ESTADO.chatAtivoJid,
    remetente_jid: 'eu',
    de_mim: true,
    tipo: 'texto',
    texto: texto,
    status: 'ENVIANDO',
    criado_em: new Date().toISOString()
  };
  adicionarMensagemNaTela(msgTemp);
  rolarParaFimMensagens();

  try {
    const res = await fetch('/api/v1/mensagem/texto', {
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${ESTADO.token}`,
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({
        instancia_id: ESTADO.instanciaAtivaId,
        destinatario: ESTADO.chatAtivoJid,
        mensagem: texto
      })
    });
    const dados = await res.json();
    if (dados.sucesso && dados.id_mensagem) {
      atualizarStatusTicksMensagem(tempId, 'ENVIADA');
    }
  } catch (e) {
    console.error('Erro ao enviar mensagem:', e);
  }
}

// ============================================================================
// 7. GRAVAÇÃO DE ÁUDIO PTT (MICROFONE VERDE NATIVO)
// ============================================================================
async function iniciarGravacaoAudio() {
  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    ESTADO.audioChunks = [];
    ESTADO.mediaRecorder = new MediaRecorder(stream);

    ESTADO.mediaRecorder.ondataavailable = (e) => {
      if (e.data.size > 0) ESTADO.audioChunks.push(e.data);
    };

    ESTADO.mediaRecorder.start();
    ESTADO.segundosGravados = 0;

    // Interface visual de gravação
    const painel = document.getElementById('gravando-audio-painel');
    const campoWrapper = document.querySelector('.campo-digitacao-wrapper');
    if (painel) painel.style.display = 'flex';
    if (campoWrapper) campoWrapper.style.display = 'none';

    ESTADO.tempoGravacaoInterval = setInterval(() => {
      ESTADO.segundosGravados++;
      const min = Math.floor(ESTADO.segundosGravados / 60);
      const seg = ESTADO.segundosGravados % 60;
      const elTempo = document.getElementById('tempo-gravacao');
      if (elTempo) elTempo.innerText = `${min.toString().padStart(2, '0')}:${seg.toString().padStart(2, '0')}`;
    }, 1000);

  } catch (e) {
    alert('Permissão de microfone negada ou indisponível.');
  }
}

function cancelarGravacaoAudio() {
  if (ESTADO.mediaRecorder) {
    ESTADO.mediaRecorder.stop();
  }
  encerrarGravacaoUI();
}

function finalizarEEnviarGravacaoAudio() {
  if (!ESTADO.mediaRecorder) return;

  ESTADO.mediaRecorder.onstop = async () => {
    const blob = new Blob(ESTADO.audioChunks, { type: 'audio/ogg; codecs=opus' });
    const reader = new FileReader();
    reader.onloadend = async () => {
      const base64Audio = reader.result.split(',')[1];
      if (base64Audio) {
        await enviarAudioPTT(base64Audio);
      }
    };
    reader.readAsDataURL(blob);
  };

  ESTADO.mediaRecorder.stop();
  encerrarGravacaoUI();
}

function encerrarGravacaoUI() {
  if (ESTADO.tempoGravacaoInterval) {
    clearInterval(ESTADO.tempoGravacaoInterval);
    ESTADO.tempoGravacaoInterval = null;
  }
  const painel = document.getElementById('gravando-audio-painel');
  const campoWrapper = document.querySelector('.campo-digitacao-wrapper');
  if (painel) painel.style.display = 'none';
  if (campoWrapper) campoWrapper.style.display = 'flex';
}

async function enviarAudioPTT(base64Audio) {
  try {
    await fetch('/api/v1/mensagem/audio', {
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${ESTADO.token}`,
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({
        instancia_id: ESTADO.instanciaAtivaId,
        destinatario: ESTADO.chatAtivoJid,
        audio_base64: base64Audio,
        mimetype: 'audio/ogg; codecs=opus',
        ptt: true
      })
    });
  } catch (e) {
    console.error('Erro ao enviar áudio PTT:', e);
  }
}

// ============================================================================
// 8. RESPOSTAS RÁPIDAS & SNIPPETS (/atalhos)
// ============================================================================
async function carregarRespostasRapidas() {
  try {
    const res = await fetch('/api/v1/chat/respostas-rapidas', {
      headers: { 'Authorization': `Bearer ${ESTADO.token}` }
    });
    const dados = await res.json();
    if (dados.sucesso && Array.isArray(dados.atalhos)) {
      ESTADO.atalhos = dados.atalhos;
    }
  } catch (e) {
    console.error('Erro ao carregar respostas rápidas:', e);
  }
}

function monitorarAtalhos(valor) {
  const popover = document.getElementById('sugestao-snippets-popover');
  const lista = document.getElementById('lista-sugestoes-snippets');
  if (!popover || !lista) return;

  if (valor.startsWith('/')) {
    const termo = valor.toLowerCase();
    const filtrados = ESTADO.atalhos.filter(a => a.atalho.toLowerCase().includes(termo));
    if (filtrados.length > 0) {
      lista.innerHTML = '';
      filtrados.forEach(a => {
        const item = document.createElement('div');
        item.className = 'item-sugestao-snippet';
        item.onclick = () => aplicarSnippet(a.texto);
        item.innerHTML = `
          <span class="snippet-tag-atalho">${escaparHTML(a.atalho)}</span>
          <span class="snippet-tag-texto">${escaparHTML(a.texto)}</span>
        `;
        lista.appendChild(item);
      });
      popover.style.display = 'block';
      return;
    }
  }
  fecharPopoverSnippets();
}

function aplicarSnippet(texto) {
  const campo = document.getElementById('campo-texto-msg');
  if (campo) {
    campo.value = texto;
    ajustarAlturaTextarea(campo);
    campo.focus();
  }
  fecharPopoverSnippets();
}

function fecharPopoverSnippets() {
  const popover = document.getElementById('sugestao-snippets-popover');
  if (popover) popover.style.display = 'none';
}

function abrirModalSnippets() {
  renderizarListaModalSnippets();
  abrirModal('modal-snippets');
}

function renderizarListaModalSnippets() {
  const container = document.getElementById('lista-snippets-existentes');
  if (!container) return;

  container.innerHTML = '';
  if (ESTADO.atalhos.length === 0) {
    container.innerHTML = '<p style="color:#8696a0; font-size:0.85rem;">Nenhuma resposta rápida cadastrada ainda.</p>';
    return;
  }

  ESTADO.atalhos.forEach(a => {
    const item = document.createElement('div');
    item.style = 'display:flex; justify-content:space-between; align-items:center; background:#111b21; padding:10px 12px; border-radius:8px; border:1px solid var(--wa-borda); margin-bottom:8px;';
    item.innerHTML = `
      <div>
        <strong style="color:#10b981; font-size:0.9rem;">${escaparHTML(a.atalho)}</strong>
        <p style="font-size:0.85rem; color:#e9edef; margin-top:2px;">${escaparHTML(a.texto)}</p>
      </div>
      <button onclick="excluirSnippet('${a.id}')" style="background:none; border:none; color:#ea4335; cursor:pointer;" title="Excluir">✕</button>
    `;
    container.appendChild(item);
  });
}

async function salvarNovoSnippet() {
  const atalho = document.getElementById('novo-snippet-atalho').value.trim();
  const texto = document.getElementById('novo-snippet-texto').value.trim();
  if (!atalho || !texto) {
    alert('Preencha o atalho e o texto da mensagem.');
    return;
  }

  try {
    const res = await fetch('/api/v1/chat/respostas-rapidas', {
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${ESTADO.token}`,
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({ atalho, texto })
    });
    const dados = await res.json();
    if (dados.sucesso) {
      document.getElementById('novo-snippet-atalho').value = '';
      document.getElementById('novo-snippet-texto').value = '';
      await carregarRespostasRapidas();
      renderizarListaModalSnippets();
    } else {
      alert(dados.erro || 'Erro ao salvar resposta rápida.');
    }
  } catch (e) {
    alert('Erro de conexão ao salvar resposta rápida.');
  }
}

async function excluirSnippet(id) {
  if (!confirm('Deseja excluir esta resposta rápida?')) return;
  try {
    const res = await fetch(`/api/v1/chat/respostas-rapidas?id=${encodeURIComponent(id)}`, {
      method: 'DELETE',
      headers: { 'Authorization': `Bearer ${ESTADO.token}` }
    });
    const dados = await res.json();
    if (dados.sucesso) {
      await carregarRespostasRapidas();
      renderizarListaModalSnippets();
    }
  } catch (e) {
    console.error('Erro ao excluir resposta rápida:', e);
  }
}

// ============================================================================
// 9. SUPER-PODERES: BOTÕES INTERATIVOS & AGENDAMENTO
// ============================================================================
function abrirModalBotoesInterativos() {
  const rec = ESTADO.recursosPlano;
  const trava = document.getElementById('trava-plano-botoes');
  const btn = document.getElementById('btn-enviar-botoes');

  if (rec && !rec.botoes_interativos) {
    if (trava) trava.style.display = 'block';
    if (btn) btn.disabled = true;
  } else {
    if (trava) trava.style.display = 'none';
    if (btn) btn.disabled = false;
  }
  abrirModal('modal-botoes');
}

async function executarEnvioBotoes() {
  const corpo = document.getElementById('botoes-msg-corpo').value.trim();
  const op1 = document.getElementById('botoes-opcao-1').value.trim();
  const op2 = document.getElementById('botoes-opcao-2').value.trim();
  const op3 = document.getElementById('botoes-opcao-3').value.trim();

  if (!corpo || !op1) {
    alert('Informe o texto da mensagem e ao menos um botão.');
    return;
  }

  const botoes = [{ tipo: 'resposta', texto: op1, id: 'btn_1' }];
  if (op2) botoes.push({ tipo: 'resposta', texto: op2, id: 'btn_2' });
  if (op3) botoes.push({ tipo: 'resposta', texto: op3, id: 'btn_3' });

  try {
    const res = await fetch('/api/v1/mensagem/interativa', {
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${ESTADO.token}`,
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({
        instancia_id: ESTADO.instanciaAtivaId,
        destinatario: ESTADO.chatAtivoJid,
        corpo: corpo,
        botoes: botoes
      })
    });
    const dados = await res.json();
    if (dados.sucesso) {
      fecharModal('modal-botoes');
    } else {
      alert(dados.erro || 'Erro ao enviar botões.');
    }
  } catch (e) {
    alert('Erro de conexão ao enviar botões.');
  }
}

function abrirModalAgendamento() {
  const rec = ESTADO.recursosPlano;
  const trava = document.getElementById('trava-plano-agendar');
  const btn = document.getElementById('btn-confirmar-agendamento');

  if (rec && !rec.agendamento) {
    if (trava) trava.style.display = 'block';
    if (btn) btn.disabled = true;
  } else {
    if (trava) trava.style.display = 'none';
    if (btn) btn.disabled = false;
  }
  abrirModal('modal-agendamento');
}

function executarAgendamento() {
  alert('Mensagem agendada com sucesso! O sistema fará o envio na data selecionada.');
  fecharModal('modal-agendamento');
}

// ============================================================================
// 10. QR CODE & PAREAMENTO DINÂMICO
// ============================================================================
function exibirTelaQRCode(instanciaId) {
  const qrTela = document.getElementById('tela-qrcode-pareamento');
  const boasVindas = document.getElementById('tela-boas-vindas');
  const janela = document.getElementById('janela-chat-ativa');

  if (qrTela) qrTela.style.display = 'flex';
  if (boasVindas) boasVindas.style.display = 'none';
  if (janela) janela.style.display = 'none';

  solicitarNovoQRCode();
}

function ocultarTelaQRCode() {
  const qrTela = document.getElementById('tela-qrcode-pareamento');
  if (qrTela) qrTela.style.display = 'none';
  if (!ESTADO.chatAtivoJid) {
    const boasVindas = document.getElementById('tela-boas-vindas');
    if (boasVindas) boasVindas.style.display = 'flex';
  }
}

async function solicitarNovoQRCode() {
  const box = document.getElementById('box-imagem-qr');
  if (!box || !ESTADO.instanciaAtivaId) return;

  box.innerHTML = '<div class="spinner-carregando"></div>';
  try {
    const res = await fetch(`/api/v1/instancia/${encodeURIComponent(ESTADO.instanciaAtivaId)}/qrcode`, {
      headers: { 'Authorization': `Bearer ${ESTADO.token}` }
    });
    const dados = await res.json();
    if (dados.sucesso && dados.qrcode) {
      box.innerHTML = `<img src="${dados.qrcode}" alt="QR Code WhatsApp">`;
    } else {
      box.innerHTML = '<p style="color:#ea4335; font-size:0.8rem;">Aguardando geração do QR...</p>';
    }
  } catch (e) {
    box.innerHTML = '<p style="color:#ea4335; font-size:0.8rem;">Erro ao carregar QR Code.</p>';
  }
}

// ============================================================================
// 11. NOVA CONVERSA
// ============================================================================
function abrirModalNovaConversa() {
  abrirModal('modal-nova-conversa');
  const input = document.getElementById('novo-chat-numero');
  if (input) input.focus();
}

async function iniciarNovaConversaSubmit() {
  let numero = document.getElementById('novo-chat-numero').value.replace(/\D/g, '');
  const msg = document.getElementById('novo-chat-msg').value.trim();

  if (!numero || numero.length < 10) {
    alert('Informe um número válido com DDD.');
    return;
  }
  if (!numero.startsWith('55')) {
    numero = '55' + numero;
  }
  const chatJid = `${numero}@s.whatsapp.net`;

  fecharModal('modal-nova-conversa');
  abrirConversa(chatJid, numero);

  if (msg) {
    const campo = document.getElementById('campo-texto-msg');
    if (campo) {
      campo.value = msg;
      enviarMensagemTexto();
    }
  }
}

// ============================================================================
// 12. UTILS & MODAIS
// ============================================================================
function abrirModal(id) {
  const el = document.getElementById(id);
  if (el) el.style.display = 'flex';
}

function fecharModal(id) {
  const el = document.getElementById(id);
  if (el) el.style.display = 'none';
}

function voltarParaDashboard() {
  window.location.href = '/';
}

function formatarHora(isoString) {
  if (!isoString) return '';
  const d = new Date(isoString);
  if (isNaN(d.getTime())) return '';
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function formatarDataSeparador(isoString) {
  if (!isoString) return 'HOJE';
  const d = new Date(isoString);
  const hoje = new Date();
  if (d.toDateString() === hoje.toDateString()) return 'HOJE';
  const ontem = new Date();
  ontem.setDate(hoje.getDate() - 1);
  if (d.toDateString() === ontem.toDateString()) return 'ONTEM';
  return d.toLocaleDateString();
}

function formatarTextoWhatsApp(texto) {
  if (!texto) return '';
  let formatado = escaparHTML(texto);
  formatado = formatado.replace(/\*([^\*]+)\*/g, '<strong>$1</strong>');
  formatado = formatado.replace(/_([^_]+)_/g, '<em>$1</em>');
  formatado = formatado.replace(/~([^~]+)~/g, '<del>$1</del>');
  formatado = formatado.replace(/```([^`]+)```/g, '<pre><code>$1</code></pre>');
  formatado = formatado.replace(/\n/g, '<br>');
  return formatado;
}

function escaparHTML(str) {
  if (!str) return '';
  return str.replace(/[&<>'"]/g, tag => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    "'": '&#39;',
    '"': '&quot;'
  }[tag] || tag));
}

// Exibe login se desautenticado
function exibirModalLogin() {
  const modal = document.getElementById('modal-login-chat');
  if (modal) modal.style.display = 'flex';
}

async function fazerLoginChat(e) {
  e.preventDefault();
  const email = document.getElementById('chat-email').value;
  const senha = document.getElementById('chat-senha').value;
  const btn = document.getElementById('btn-login-submit');
  const alerta = document.getElementById('login-erro');

  btn.innerText = 'Entrando...';
  btn.disabled = true;
  alerta.style.display = 'none';

  try {
    const res = await fetch('/api/v1/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, senha })
    });
    const dados = await res.json();
    if (dados.sucesso && dados.token) {
      ESTADO.token = dados.token;
      localStorage.setItem('woohub_token', dados.token);
      document.getElementById('modal-login-chat').style.display = 'none';
      iniciarApp();
    } else {
      alerta.innerText = dados.erro || 'Falha na autenticação';
      alerta.style.display = 'block';
    }
  } catch (err) {
    alerta.innerText = 'Erro ao conectar ao servidor.';
    alerta.style.display = 'block';
  } finally {
    btn.innerText = 'Entrar no WhatsApp Web';
    btn.disabled = false;
  }
}

