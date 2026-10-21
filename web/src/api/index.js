// ── API 封装层 index.js（§AUDIT1019-M8 拆分后：统一出口，2026-10-21）──
// 本文件原本 2161 行 / 162 个导出混在一个文件里（god module），已按领域拆到同级模块：
//   shared(基础设施) / auth_store(登录态) / request(请求封装) / auth_api(认证) /
//   signals / status / alerts / positions / market / trade / sse / config /
//   consult / research / admin。
// 本文件保留两件事：① 下面的「模块分组端点函数清单」索引表；② 用 export * 把各模块
//   的导出重新汇到同一入口——外部 `import * as api from '../api/index.js'` 与
//   `import { isAdmin } from '../api/index.js'` 两种引用形态零变更。
// 新增接口请放进对应领域模块，不要再往本文件堆（本文件行数受门禁 §88 上限锁约束）。

// ═══ 模块分组端点函数清单（导出函数索引，全部经 request() 走统一封装）═══
// ── 模块分组端点函数清单（导出函数索引，全部经 request() 走统一封装）──
//
// 【认证与登录态】
//   login()                 POST /api/auth/login        登录换取 token 并落盘账号/角色/权限
//   logout()                POST /api/auth/logout       服务端吊销会话 + 立即清本地凭据
//   clearAuth()             ——                          本地清除 token/账号/角色/权限
//   isLoggedIn() / getAccount() / getRole() / getPerms() / isAdmin() / hasPerm()
//                           ——                          本地登录态读取与权限判定
//   refreshMe()             GET  /api/auth/me           拉取并缓存最新角色/权限位
//   getStoredServer() / setStoredServer()
//                           ——                          后端服务器地址的读取与持久化
//
// 【通用请求封装】
//   request(path, opts)     ——                          统一 fetch：拼 baseUrl、自动附 Bearer 头、
//                                                       超时中断、401 清态广播、错误归一、同源回退重试
//
// 【策略信号与行情】
//   fetchSignals()          GET  /api/signals           策略信号列表
//   fetchMinute() / fetchDepth()
//                           GET  /api/minute | /api/depth/{code}
//                                                       分时（含 MACD）/ 盘口五档
//   fetchStatus() / fetchDashboard() / fetchEngineHealth()
//                           GET  /api/status | /api/dashboard | /api/engine_health
//                                                       系统状态 / 仪表盘 / 引擎健康
//   fetchSectorHot() / fetchSectorHotRecords()
//                           GET  /api/sector/hot[/records]   板块热点与轮次记录
//   fetchSnapshot() / fetchHotSnapshot()
//                           GET  /api/snapshot[/hot]    全市场 / 热门个股行情快照
//   fetchEvaluations()      GET  /api/evaluations       个股多维度战法评分
//   fetchStockLookup()      GET  /api/stock/lookup      个股速查（名称/现价）
//   fetchDataSourceHealth() / fetchNewsSourceHealth()
//                           GET  /api/data_source_health | /api/news_source_health
//                                                       行情/新闻数据源健康探测
//
// 【消息提醒】
//   fetchAlerts()           GET    /api/alerts          消息列表
//   clearAlerts()           DELETE /api/alerts          清空全部消息
//   deleteAlert(id)         DELETE /api/alerts/{id}     删除单条消息
//   reviewPositions()       POST   /api/review/positions   手动触发盘后持仓 LLM 复盘
//
// 【持仓管理】
//   fetchHoldings() / updateHoldings() / updateHoldingsBalance()
//                           GET|POST /api/holdings[/balance]   持仓列表与可用资金
//   addHoldingLot() / setHoldingCost() / closeHolding() / sellHoldingLot()
//                           POST /api/holdings/{code}/add|cost|close|sell
//                                                       加仓/改成本/清仓/减仓
//   fetchRealPositions() / fetchRealAdvice() / executeRealAction()
//                           GET|POST /api/positions/real|advice|execute
//                                                       实盘真实持仓 / 处理建议 / 手动委托
//
// 【实盘 QMT 网关】
//   fetchQMTState() / fetchQMTBroker() / switchQMTBroker()
//                           GET|POST /api/qmt/state|broker     互通健康 / 双路径通道切换
//   fetchQMTConfig() / updateQMTConfig()
//                           GET|POST /api/config/qmt           实盘参数局部更新与热生效
//   fetchQMTTrades() / fetchQMTOrders() / qmtHalt() / qmtCancel()
//                           GET|POST /api/qmt/trades|orders|halt|cancel/{id}
//                                                       流水 / 委托 / kill-switch / 撤单
//   qmtSettle() / fetchQMTSettleHistory()
//                           POST|GET /api/qmt/settle[/history] 交割单三方对账与历史
//
// 【模拟盘】
//   fetchPaperState() / fetchPaperPositions() / fetchPaperTrades() / fetchPaperOrders()
//   fetchPaperEquity() / fetchPaperSelfCheck()
//                           GET /api/paper/state|positions|trades|orders|equity|selfcheck
//                                                       模拟盘状态/持仓/成交/订单/净值/自检
//   fetchPaperStrategies() / updatePaperStrategies()
//                           GET|POST /api/paper/strategies     战法黑白名单
//   fetchPaperConfig() / updatePaperConfig()
//                           GET|POST /api/paper/config         撮合配置（改后热生效）
//   buyPaperPosition() / sellPaperPosition() / shortCoverPaper()
//                           POST /api/paper/buy|sell|short_cover
//                                                       手动买卖与融券买平（开仓由信号自动触发）
//   resetPaperPool() / configPaperPools() / resetPaper() / paperResetV2()
//                           POST /api/paper/pool/reset|pool/config|reset
//                                                       单池清盘 / 分仓配置 / 总清盘
//
// 【信号控制器、风控与开关】
//   fetchSignalVerdicts()   GET  /api/signalctl/verdicts   信号裁定留痕
//   fetchRiskGates()        GET  /api/risk/gates           风控闸口状态（admin）
//   actionSignal()          POST /api/action               信号买入/忽略
//   fetchShortStatus() / toggleShort()
//                           GET|POST /api/short/status|toggle   做空开关（做多总闸 /api/long/* 由后端保留，暂无 UI）
//   fetchNewsShowAllStatus() / toggleNewsShowAll()
//                           GET|POST /api/news/showall      资讯显示全部开关
//
// 【资讯、自选与日历】
//   fetchNews() / reanalyzeNews()
//                           GET|POST /api/news[/reanalyze]  资讯列表与手动 LLM 重推
//   fetchWatchlist() / addWatchlist() / removeWatchlist()
//                           GET|POST|DELETE /api/watchlist  自选股增删查
//   fetchIPOCalendar()      GET  /api/ipo/calendar          打新日历（按日本地缓存）
//
// 【LLM 与战法配置、诊断】
//   fetchLLMConfig() / setLLMConfig()
//                           GET|POST /api/config/llm        LLM 配置读写
//   fetchStrategyConfig() / setStrategyConfig()
//                           GET|POST /api/config/strategy   四战法参数读写
//   fetchLLMDebug() / fetchStageRecords() / fetchSignalLogs()
//                           GET  /api/llm-debug | /api/stage-records | /api/signal-logs
//                                                       LLM 诊断与盘后复盘数据
//   consultChat() / fetchConsultHistory() / clearConsultHistory()
//   fetchConsultProMode() / setConsultProMode()
//                           POST|GET|DELETE|PUT /api/consult*   股票咨询多轮对话与专业模式
//
// 【B5 研究候选与回测】
//   fetchResearchProgress() / getSchedulerStatus() / getResearchTaskLog() / fetchResearchFactors()
//                           GET  /api/research/progress | /api/scheduler/status
//                                | /api/research/task/{id}/log | /api/research/factors
//                                                       研究进度 / 调度快照 / 任务日志 / 因子元数据
//   fetchResearchCandidates() / approveResearchCandidate() / grayscaleResearchCandidate()
//   rejectResearchCandidate() / backtestResearchCandidate()
//                           GET|POST /api/research/candidates*   候选列表与审批/灰度/驳回/回测
//   cancelBacktest() / pauseBacktest() / resumeBacktest()
//                           POST /api/research/backtest/{id}/cancel|pause|resume
//                                                       回测任务的取消/暂停/恢复
//   backtestLibraryRule() / fetchBacktestStatus() / fetchRunningBacktests() / fetchAllBacktests()
//                           POST|GET /api/research/library/{id}/backtest | /api/research/backtest*
//                                                       战法库回测与任务状态查询
//   fetchResearchLibrary() / setResearchLibraryEnabled() / deleteResearchLibrary()
//   renameResearchLibrary()
//                           GET|POST /api/research/library*  战法库管理与启停
//   fetchBacktestToggle() / setBacktestToggle()
//                           GET|POST /api/research/backtest-toggle  全量回测总开关
//   fetchBacktestConfig() / saveBacktestConfig()
//                           GET|PUT  /api/research/backtest-config  回测增强配置
//   fetchOptimizations() / approveOptimization() / rejectOptimization()
//   fetchSweepPools() / saveSweepPool() / enqueueOptimize()
//                           GET|POST|PUT /api/research/optimizations* | sweep-pools
//                                | POST /api/backtest/optimize
//                                                       参数寻优结果与审批、扫参任务
//
// 【用户/账号、租户与运维日志（admin）】
//   fetchAdminUsers() / createAdminUser() / setAdminUserRole() / setAdminUserPerms()
//   setAdminUserPassword() / setAdminUserEnabled() / setAdminUserExpiry() / deleteAdminUser()
//   cleanupAdminUsers() / moveUserTenant()
//                           CRUD /api/admin/users*          用户与权限/密码/有效期管理
//   fetchAdminStrategyConfig() / setAdminStrategyConfig() / fetchAdminD1Config()
//   setAdminD1Config() / fetchAdminLongShortConfig() / setAdminLongShortConfig()
//   fetchAdminLLMConfig() / setAdminLLMConfig()
//                           GET|POST /api/admin/users/{id}/config/*   按账号读取/保存各配置
//   fetchTenants() / createTenant() / updateTenant() / fetchTenantUsage()
//                           GET|POST|PUT /api/tenants*      租户管理与配额用量
//   fetchOpslogDates() / fetchOpslog()
//                           GET  /api/opslog[/dates]        每日系统运行日志（admin）
//
// 【SSE 实时推送与会话追踪】
//   onSSE() / connectSSE() / disconnectSSE()
//                           POST /api/events/ticket + GET /api/events?ticket=...
//                                                       SSE 长连接（短时效票据鉴权）与回调分发
//   getLastSession() / setLastSession() / isNewSession() / isTradingSession()
//                           ——                              市场会话期号追踪（非交易时段仅首载）
//
//
// 路径拼接原理：接口函数均传入相对路径（如 '/api/signals'），
// URL joining: every API function takes a relative path (e.g. '/api/signals'),
// 最终请求地址 = baseUrl()（用户配置的服务器地址）+ 相对路径，
// final request URL = baseUrl() (user-configured server) + relative path,
// 使前端代码与具体后端地址解耦，便于切换服务器。
// decoupling frontend code from a specific backend address so the server can be switched easily.

// 全局基础路径前缀（预留，当前为空串；实际前缀来自用户配置的服务器地址）
// Global base path prefix (reserved; currently empty since the real prefix comes from the user-configured server)
const BASE = ''

// localStorage 存储键名
// localStorage storage keys
// 统一带 'liangzai_' 前缀，避免键名与其他应用冲突
// Namespaced with 'liangzai_' prefix to avoid collisions with other apps
// STORAGE_KEY    存储 JWT 访问令牌
// STORAGE_KEY    holds the JWT access token
// STORAGE_SERVER 存储用户配置的后端服务器地址
// STORAGE_SERVER holds the user-configured backend server URL
// STORAGE_ACCOUNT 存储当前登录账号名
// STORAGE_ACCOUNT holds the currently logged in account name
const STORAGE_KEY = 'liangzai_token'
const STORAGE_SERVER = 'liangzai_server_url'
const STORAGE_ACCOUNT = 'liangzai_account'
const STORAGE_ROLE = 'liangzai_role'
// STORAGE_ROLE 存储当前登录账号角色（admin/member），角色位图随登录响应写入
// STORAGE_ROLE holds the logged-in account role (admin/member); the role bitmap is written from the login response
const STORAGE_PERMS = 'liangzai_perms'

// §NATIVEAUTH（2026-09-22 C批）：探测移动端原生壳注入的安全存储桥 window.AndroidAuth
// §NATIVEAUTH (2026-09-22 batch C): detect the native-shell bridge window.AndroidAuth for secure token storage
// 桥只存在于新版 APK 内嵌 WebView（getToken/setToken(str):bool/clearToken():bool 三个同步方法），
// 纯浏览器与旧 APK 均无此桥，自然回落 localStorage 路径，行为完全不变。
// The bridge only exists inside the new APK's embedded WebView (three sync methods:
// getToken/setToken(str):bool/clearToken():bool); plain browsers & old APK have none and fall back to localStorage.
// 为什么不做 origin 判定：原生注入本身即信任边界（只对 appassets 内嵌页注入），
// Why no origin check: the native injection itself is the trust boundary (injected only into the appassets page),
// token 由此落入 EncryptedSharedPreferences（AndroidKeyStore 托管主密钥），root/adb-backup 的静态读取面收小；
// the token then lives in EncryptedSharedPreferences (master key held by AndroidKeyStore), shrinking the static
// read surface via root/adb-backup;
// JS 运行时可达性与 localStorage 等价（威胁模型不劣化）。
// JS-runtime reachability stays equivalent to localStorage (no threat-model regression).
function nativeAuthBridge() {
  return (typeof window !== 'undefined' && window.AndroidAuth &&
          typeof window.AndroidAuth.setToken === 'function') ? window.AndroidAuth : null
}

// 从 localStorage 读取服务器基础地址
// Reads the base server URL from localStorage
// 说明：所有请求均基于该地址拼接相对路径；未配置时返回空串，表示使用同源相对请求
// Note: every request joins relative paths onto this base; returns '' when unset, meaning same-origin relative requests
// 追加兜底：在移动端 WebView（https://appassets.androidplatform.net 同源内嵌）里，
// 若 localStorage 尚无 server_url（例如首次安装且 JS 初始化早于原生注入），
// 直接回退到默认服务器地址，避免请求打到 appassets 无效源而报 "Failed to fetch"。
// (Fallback for the mobile WebView: when running on the appassets origin with no stored
// server URL yet, use the default server so fetches don't hit the invalid appassets origin.)
function baseUrl() {
  const stored = localStorage.getItem(STORAGE_SERVER)
  if (stored) return stored
  if (typeof location !== 'undefined' && location.origin &&
      location.origin.indexOf('appassets.androidplatform.net') >= 0) {
    return 'https://quant-trading.top'
  }
  return ''
}

// 读取当前 JWT 令牌：原生桥优先，无桥回落 localStorage
// Reads the JWT token: native secure-storage bridge first, falls back to localStorage when absent
// 说明：令牌由登录接口写入，后续每个请求都会携带该令牌完成鉴权
// Note: the token is written by the login endpoint; every later request carries it for authentication
// §NATIVEAUTH（2026-09-22 C批）迁移语义：新版 APK 首启且用户未重新登录时，桥内为空但
// §NATIVEAUTH (2026-09-22 batch C) migration semantics: on the new APK's first launch without a re-login,
// localStorage 里还留着旧版明文 token —— 读到即自动上桥迁移（setToken 成功才删本地明文副本），
// the bridge is empty while localStorage still holds the legacy plaintext token — migrate it up on read
// 失败则暂留 localStorage 继续返回旧值，保证升级后不掉登录态。
// (setToken success is required before removing the local plaintext copy; on failure keep and return it,
//  so an upgrade never drops the logged-in state).
// 导出供 Settings.jsx 替换对 'liangzai_token' 的直读（诊断展示当前 token）。
// Exported so Settings.jsx replaces its direct 'liangzai_token' read (diagnostic token display).

// ── 统一出口（export * 保留全部导出面，勿改为逐个手写——漏一个就是接口静默消失）──
export * from './auth_store.js'
export * from './market.js'
export * from './request.js'
export * from './auth_api.js'
export * from './signals.js'
export * from './status.js'
export * from './alerts.js'
export * from './positions.js'
export * from './trade.js'
export * from './sse.js'
export * from './config.js'
export * from './consult.js'
export * from './research.js'
export * from './admin.js'
