// i18n system for cpa-live-routes
// Languages: pt-br (default), en, zh-cn
'use strict';

const I18N = {
  'pt-br': {
    // Page title & header
    pageTitle: 'Live Routes',
    liveRoutes: 'Rotas ao Vivo',
    connecting: 'Conectando',
    connected: 'Conectado · Ao vivo',
    connectedPaused: 'Conectado · Pausado',
    reconnecting: 'Reconectando',
    demoMode: 'Modo demo',
    demoModePaused: 'Modo demo · Pausado',
    
    // Controls
    pause: 'Pausar',
    resume: 'Continuar',
    animationSpeed: 'Velocidade da animação',
    timeRange: 'Intervalo de tempo',
    layout: 'Layout',
    tabs: 'Abas',
    split: 'Dividido',
    config: 'Configuração',
    logout: 'Sair',
    backToNow: 'Voltar ao agora',
    
    // Time units
    seconds30: '30s',
    minutes2: '2min',
    minutes10: '10min',
    
    // Stats chips
    intervalRequests: 'Requisições no intervalo',
    inProgress: 'Em andamento',
    successRate: 'Taxa de sucesso',
    outputToks: 'tok/s saída',
    outputToksTooltip: 'Tokens de saída ÷ tempo real de geração (união do primeiro token ao fim de cada requisição, exclui ociosidade)',
    avgLatency: 'Latência média',
    
    // Views
    view: 'Visualização',
    flow: 'Fluxo',
    timeline: 'Linha do tempo',
    modelLegend: 'Legenda de modelos',
    rangeInfo: '',
    
    // Canvas labels
    flowCanvasLabel: 'Entrada (OAuth / API), modelos CPA, harness / cliente, fluxo de sessões',
    timelineCanvasLabel: 'Linha do tempo de requisições por entrada',
    
    // Feed
    recentRequests: 'Requisições recentes',
    
    // Config panel
    activePluginConfig: 'Configuração ativa do plugin',
    configReadonly: 'Somente leitura. Para modificar, edite as chaves em <code class="mono">plugins.configs.live-routes</code> no <code class="mono">config.yaml</code> do CPA. Após salvar, o CPA reenvia a configuração e esta página recarrega automaticamente, sem reiniciar.',
    retentionMinutes: 'Retenção',
    maxRecords: 'Máximo de registros',
    activeWindowMinutes: 'Janela ativa do fluxo',
    defaultAnimationSpeed: 'Velocidade padrão da animação',
    reducedAnimation: 'Reduzir animação',
    inflightTracking: 'Rastrear requisições em andamento',
    defaultLayout: 'Layout padrão',
    minutes: ' min',
    records: ' reg',
    
    // Auth gate
    accessDenied: 'Acesso negado',
    accessDeniedMsg: 'A interface de gerenciamento do CPA recusou o acesso. Configure a chave de gerenciamento no CPA (remote-management.secret-key ou variável MANAGEMENT_PASSWORD); acesso local também exige configuração. Outras causas possíveis: configurações de acesso remoto não permitem, ou bloqueio temporário por falhas excessivas — 5 falhas consecutivas do mesmo IP resultam em bloqueio de 30 minutos. Verifique a configuração ou aguarde o término do bloqueio.',
    retryAuth: 'Inserir chave novamente',
    needKey: 'Chave de gerenciamento necessária',
    needKeyMsg: 'Cada requisição de dados usa a chave de gerenciamento do CPA para autenticação. A chave fica salva apenas nesta aba por padrão; recarregar não exige nova entrada, mas fechar a aba limpa a chave. Marcar "Lembrar chave neste navegador" salva a chave em texto simples no navegador — recomendado apenas em dispositivos pessoais. A chave é limpa automaticamente quando expira ou ao clicar em "Sair".',
    keyPlaceholder: 'Chave de gerenciamento',
    verify: 'Verificar',
    rememberKey: 'Lembrar chave neste navegador',
    invalidKey: 'Chave de gerenciamento inválida ou expirada, insira novamente',
    
    // Tooltip fields
    client: 'Cliente',
    clientId: 'Cliente#',
    unknownClient: 'Cliente desconhecido',
    keyIp: 'Chave / IP',
    session: 'Sessão',
    sessionId: 'ID da sessão',
    parentSession: 'Sessão pai',
    none: 'Nenhum',
    subAgents: 'Subagentes',
    subAgentsInProgress: 'Subagentes em andamento',
    exit: 'Saída',
    exitId: 'Saída#',
    unassigned: 'Não atribuído',
    credential: 'Credencial',
    recent429: '429 recentes',
    rule: 'Regra',
    ruleId: 'Regra#',
    unrecognized: 'Não reconhecido',
    model: 'Modelo',
    models: 'modelos',
    status: 'Status',
    start: 'Início',
    ttft: 'Tempo até primeiro token',
    totalDuration: 'Duração total',
    attempts: 'Tentativas',
    inputOutput: 'Entrada / Saída',
    cache: 'Cache',
    reasoning: 'Raciocínio',
    total: 'Total',
    requests: 'Requisições',
    tokens: 'tokens',
    rateWeight60s: 'Peso de taxa dos últimos 60s',
    failWeight: 'Peso de falha',
    collapsedSubagents: 'Subagentes recolhidos',
    clickExpandCollapse: 'Clique para expandir/recolher sessões',
    idle: 'Ocioso',
    idleMinutes: ' min ocioso',
    lastActivity: 'Última atividade',
    ago: 'atrás',
    followNow: 'Seguir agora',
    lookingBack: 'Olhando para trás',
    
    // Status labels
    queued: 'Na fila',
    routed: 'Roteado',
    done: 'Sucesso',
    failed: 'Falha',
    canceled: 'Cancelado',
    rejected: 'Rejeitado',
    lost: 'Perdido',
    other: 'Outro',
    
    // Empty states
    noRequestsInRange: 'Nenhuma requisição neste intervalo',
    noDataLoaded: 'Sem dados anteriores carregados',
    noSessions: 'Minutos sem sessões · O plugin só vê requisições que passaram pelo CPA após o carregamento',
    
    // Flow diagram
    clients: 'Clientes',
    sessions: 'Sessões',
    lineWidthHint: 'Espessura da linha = taxa de requisições dos últimos 60s (decaimento exponencial) · Anel = proporção de tráfego no intervalo / falhas · Clique no cliente para expandir/recolher sessões',
    windowRequests: 'janela',
    
    // Demo data
    workMachine: 'Estação de trabalho',
    devMachine: 'Máquina de desenvolvimento',
  },
  'en': {
    pageTitle: 'Live Routes',
    liveRoutes: 'Live Routes',
    connecting: 'Connecting',
    connected: 'Connected · Live',
    connectedPaused: 'Connected · Paused',
    reconnecting: 'Reconnecting',
    demoMode: 'Demo mode',
    demoModePaused: 'Demo mode · Paused',
    
    pause: 'Pause',
    resume: 'Resume',
    animationSpeed: 'Animation speed',
    timeRange: 'Time range',
    layout: 'Layout',
    tabs: 'Tabs',
    split: 'Split',
    config: 'Config',
    logout: 'Logout',
    backToNow: 'Back to now',
    
    seconds30: '30s',
    minutes2: '2min',
    minutes10: '10min',
    
    intervalRequests: 'Interval requests',
    inProgress: 'In progress',
    successRate: 'Success rate',
    outputToks: 'output tok/s',
    outputToksTooltip: 'Output tokens ÷ actual generation time (union of first-token-to-end per request, excludes idle)',
    avgLatency: 'Avg latency',
    
    view: 'View',
    flow: 'Flow',
    timeline: 'Timeline',
    modelLegend: 'Model legend',
    rangeInfo: '',
    
    flowCanvasLabel: 'Entry (OAuth / API), CPA models, harness / client, session flow',
    timelineCanvasLabel: 'Request timeline by entry',
    
    recentRequests: 'Recent requests',
    
    activePluginConfig: 'Active plugin configuration',
    configReadonly: 'Read-only. To modify, edit keys under <code class="mono">plugins.configs.live-routes</code> in CPA\'s <code class="mono">config.yaml</code>. After saving, CPA re-pushes config and this page auto-refreshes without restart.',
    retentionMinutes: 'Retention',
    maxRecords: 'Max records',
    activeWindowMinutes: 'Flow active window',
    defaultAnimationSpeed: 'Default animation speed',
    reducedAnimation: 'Reduced animation',
    inflightTracking: 'Track in-flight requests',
    defaultLayout: 'Default layout',
    minutes: ' min',
    records: ' records',
    
    accessDenied: 'Access denied',
    accessDeniedMsg: 'CPA management interface denied access. Configure the management key in CPA (remote-management.secret-key or MANAGEMENT_PASSWORD env var); local access also requires configuration. Other possible causes: remote management access settings disallow, or temporary ban from excessive failures — 5 consecutive auth failures from the same IP result in a 30-minute ban. Check your configuration or wait for the ban to expire.',
    retryAuth: 'Re-enter key',
    needKey: 'Management key required',
    needKeyMsg: 'Each data request uses the CPA management key for authentication. The key is stored only in this tab by default; reloading does not require re-entry, but closing the tab clears it. Checking "Remember key in this browser" saves the key in plaintext — recommended only on personal devices. The key is automatically cleared when invalid or when clicking "Logout".',
    keyPlaceholder: 'Management key',
    verify: 'Verify',
    rememberKey: 'Remember key in this browser',
    invalidKey: 'Management key invalid or expired, please re-enter',
    
    client: 'Client',
    clientId: 'Client#',
    unknownClient: 'Unknown client',
    keyIp: 'Key / IP',
    session: 'Session',
    sessionId: 'Session ID',
    parentSession: 'Parent session',
    none: 'None',
    subAgents: 'Sub-agents',
    subAgentsInProgress: 'Sub-agents in progress',
    exit: 'Exit',
    exitId: 'Exit#',
    unassigned: 'Unassigned',
    credential: 'Credential',
    recent429: 'Recent 429s',
    rule: 'Rule',
    ruleId: 'Rule#',
    unrecognized: 'Unrecognized',
    model: 'Model',
    models: 'models',
    status: 'Status',
    start: 'Start',
    ttft: 'Time to first token',
    totalDuration: 'Total duration',
    attempts: 'Attempts',
    inputOutput: 'Input / Output',
    cache: 'Cache',
    reasoning: 'Reasoning',
    total: 'Total',
    requests: 'Requests',
    tokens: 'tokens',
    rateWeight60s: 'Rate weight last 60s',
    failWeight: 'Fail weight',
    collapsedSubagents: 'Collapsed sub-agents',
    clickExpandCollapse: 'Click to expand/collapse sessions',
    idle: 'Idle',
    idleMinutes: ' min idle',
    lastActivity: 'Last activity',
    ago: 'ago',
    followNow: 'Follow now',
    lookingBack: 'Looking back',
    
    queued: 'Queued',
    routed: 'Routed',
    done: 'Success',
    failed: 'Failed',
    canceled: 'Canceled',
    rejected: 'Rejected',
    lost: 'Lost',
    other: 'Other',
    
    noRequestsInRange: 'No requests in this range',
    noDataLoaded: 'No earlier data loaded',
    noSessions: 'minutes without sessions · Plugin only sees requests that passed through CPA after loading',
    
    clients: 'Clients',
    sessions: 'Sessions',
    lineWidthHint: 'Line width = request rate last 60s (exponential decay) · Ring = interval traffic share / failures · Click client to expand/collapse sessions',
    windowRequests: 'window',
    
    workMachine: 'Workstation',
    devMachine: 'Dev machine',
  },
  'zh-cn': {
    pageTitle: 'Live Routes',
    liveRoutes: '实时链路',
    connecting: '连接中',
    connected: '已连接 · 实时',
    connectedPaused: '已连接 · 已暂停',
    reconnecting: '重连中',
    demoMode: '演示模式',
    demoModePaused: '演示模式 · 已暂停',
    
    pause: '暂停',
    resume: '继续',
    animationSpeed: '动画速度',
    timeRange: '时间范围',
    layout: '布局',
    tabs: '标签页',
    split: '分栏',
    config: '配置',
    logout: '退出',
    backToNow: '回到现在',
    
    seconds30: '30秒',
    minutes2: '2分钟',
    minutes10: '10分钟',
    
    intervalRequests: '区间请求',
    inProgress: '进行中',
    successRate: '成功率',
    outputToks: '输出 tok/s',
    outputToksTooltip: '输出 token ÷ 实际生成时长（各请求首字到结束的并集，不含空闲）',
    avgLatency: '平均延迟',
    
    view: '视图',
    flow: '流向',
    timeline: '时间轴',
    modelLegend: '模型图例',
    rangeInfo: '',
    
    flowCanvasLabel: '入口（OAuth / API）、CPA 模型、harness / 客户端、会话流向',
    timelineCanvasLabel: '按入口分道的请求时间线',
    
    recentRequests: '最近请求',
    
    activePluginConfig: '当前生效的插件配置',
    configReadonly: '这里只读。修改请编辑 CPA 的 <code class="mono">config.yaml</code> 中 <code class="mono">plugins.configs.live-routes</code> 下的键，保存后 CPA 会重新下发配置，本页自动刷新，无需重启。',
    retentionMinutes: '保留时长',
    maxRecords: '最大记录数',
    activeWindowMinutes: '流向活跃窗口',
    defaultAnimationSpeed: '默认动画速度',
    reducedAnimation: '减少动画',
    inflightTracking: '追踪进行中请求',
    defaultLayout: '默认布局',
    minutes: ' 分钟',
    records: ' 条',
    
    accessDenied: '无法访问',
    accessDeniedMsg: 'CPA 管理接口拒绝访问。请先在 CPA 配置管理密钥（remote-management.secret-key 或环境变量 MANAGEMENT_PASSWORD），本机访问也需要配置。其他可能原因包括远程管理访问设置不允许，或失败次数过多被暂时封禁：同一 IP 连续 5 次认证失败会被封禁 30 分钟。请检查配置，或等待封禁到期后重试。',
    retryAuth: '重新输入管理密钥',
    needKey: '需要管理密钥',
    needKeyMsg: '每次数据请求均使用 CPA 管理密钥验证。密钥默认只保存在当前标签页，刷新无需重输，关闭标签页即清除。勾选「在此浏览器记住密钥」会把密钥明文保存在本浏览器中，仅建议在个人设备上使用。密钥失效或点击「退出」时自动清除。',
    keyPlaceholder: '管理密钥',
    verify: '验证',
    rememberKey: '在此浏览器记住密钥',
    invalidKey: '管理密钥无效或已失效，请重新输入',
    
    client: '客户端',
    clientId: '客户端#',
    unknownClient: '未知客户端',
    keyIp: '密钥 / IP',
    session: '会话',
    sessionId: '会话 ID',
    parentSession: '父会话',
    none: '无',
    subAgents: '子代理',
    subAgentsInProgress: '子代理进行中',
    exit: '入口',
    exitId: '入口#',
    unassigned: '未分配',
    credential: '凭证',
    recent429: '近期 429',
    rule: '规则',
    ruleId: '规则#',
    unrecognized: '未识别',
    model: '模型',
    models: '个模型',
    status: '状态',
    start: '开始',
    ttft: '首字延迟',
    totalDuration: '总耗时',
    attempts: '尝试次数',
    inputOutput: '输入 / 输出',
    cache: '缓存',
    reasoning: '推理',
    total: '合计',
    requests: '请求',
    tokens: 'tokens',
    rateWeight60s: '近 60 秒速率权重',
    failWeight: '失败权重',
    collapsedSubagents: '折叠的子代理',
    clickExpandCollapse: '点击展开/收起会话',
    idle: '空闲',
    idleMinutes: ' 分钟空闲',
    lastActivity: '上次活动',
    ago: '前',
    followNow: '跟随现在',
    lookingBack: '回看',
    
    queued: '排队中',
    routed: '路由中',
    done: '成功',
    failed: '失败',
    canceled: '取消',
    rejected: '拒绝',
    lost: '丢失',
    other: '其他',
    
    noRequestsInRange: '该时间范围内暂无请求',
    noDataLoaded: '没有更早的数据',
    noSessions: '分钟没有会话 · 插件只能看到加载后经过 CPA 的请求',
    
    clients: '客户端',
    sessions: '会话',
    lineWidthHint: '线宽 = 近 60 秒请求速率（指数衰减）· 环 = 区间流量占比 / 失败 · 点击客户端展开 / 收起会话',
    windowRequests: '窗口',
    
    workMachine: '工作机',
    devMachine: '开发机',
  }
};

// Language detection and persistence
const LANG_KEY = 'lr-lang';
const SUPPORTED_LANGS = ['pt-br', 'en', 'zh-cn'];

function detectLang() {
  // 1. Saved preference
  const saved = (() => { try { return localStorage.getItem(LANG_KEY); } catch(e) { return null; } })();
  if (saved && SUPPORTED_LANGS.includes(saved)) return saved;
  
  // 2. Browser language
  const nav = (navigator.language || navigator.userLanguage || '').toLowerCase();
  if (nav.startsWith('pt')) return 'pt-br';
  if (nav.startsWith('zh')) return 'zh-cn';
  if (nav.startsWith('en')) return 'en';
  
  // 3. Default to PT-BR
  return 'pt-br';
}

let currentLang = detectLang();

function setLang(lang) {
  if (!SUPPORTED_LANGS.includes(lang)) return;
  currentLang = lang;
  try { localStorage.setItem(LANG_KEY, lang); } catch(e) {}
  document.documentElement.lang = lang === 'zh-cn' ? 'zh-CN' : lang === 'pt-br' ? 'pt-BR' : 'en';
  applyI18n();
}

function t(key) {
  return (I18N[currentLang] && I18N[currentLang][key]) || (I18N['en'] && I18N['en'][key]) || key;
}

function applyI18n() {
  // Static elements with data-i18n attribute
  document.querySelectorAll('[data-i18n]').forEach(el => {
    const key = el.dataset.i18n;
    const val = t(key);
    if (el.tagName === 'INPUT' && el.type !== 'submit') {
      el.placeholder = val;
    } else {
      el.textContent = val;
    }
  });
  
  // Elements with data-i18n-html (for HTML content)
  document.querySelectorAll('[data-i18n-html]').forEach(el => {
    el.innerHTML = t(el.dataset.i18nHtml);
  });
  
  // Aria labels
  document.querySelectorAll('[data-i18n-aria]').forEach(el => {
    el.setAttribute('aria-label', t(el.dataset.i18nAria));
  });
  
  // Title attributes
  document.querySelectorAll('[data-i18n-title]').forEach(el => {
    el.setAttribute('title', t(el.dataset.i18nTitle));
  });
  
  // Update dynamic content that depends on language
  if (typeof setPill === 'function') setPill();
  if (typeof renderCfg === 'function') renderCfg();
  if (typeof updateFeed === 'function') updateFeed();
}

// Export for use in main script
if (typeof window !== 'undefined') {
  window.I18N_DICT = I18N;
  window.t = t;
  window.setLang = setLang;
  window.currentLang = currentLang;
  window.applyI18n = applyI18n;
  window.SUPPORTED_LANGS = SUPPORTED_LANGS;
}