#!/usr/bin/env bash
# 改动全量验证脚本（含 2026-08-05 实时链路改动专项 + 2026-09-11 回测自动增强专项 + 2026-09-12 做空策略链路专项
#   + 2026-09-13 UAT 修复批/研究升级批/情绪面板 A/B/C 专项 + 2026-09-14 §DATA-OUTAGE 数据管道根治专项
#   + 2026-09-15 §ICP 备案合规 + §LIFECYCLE-WIRING 衰退降级接线专项 + 2026-09-17 §SIGNAL_CONTROLLER 信号控制器专项
#   + 2026-09-18 §UAT_FULLCHAIN 全链路修复批（费用腿/权限门控/死代码清理/CI-seed 守卫）专项
#   + 2026-09-18 §AUDIT20260918 全栈审计批（A1-A9）+ 生产实录 §PROD-T1/§PROD-LLM2 专项
#   + 2026-09-19 §ENH-0~5 增强批 A~E 专项 + §RFIX-1~5/§ENH-A/B 战法层修复增强批专项（见 18/18）
#   + 2026-09-20 §ENH-A 生产回填承载脚本静态守卫（见 19/19）+ §A7 版本漂移根治（部署脚本同步 web/dist，见 20/20）
#   + 2026-09-20 §FIX-5 UAT 自举脚本静态守卫（见 21/21）+ §FIX-20260920 今日修复批 FIX-1~7（见 22/22）
#   + 2026-09-20 §FIX-20260920 数据管道修复批（THS 单域熔断/f62 主站信任/板块列表镜像分页/EM-FFLOW 真值口径/THS-KLINE 5分钟/QUOTE-CHAIN 同花顺首选源，见 24/24）
#   + 2026-09-20 §A7-B/§A7-C 部署健壮性（产物指纹落盘 + 停服兜底 + 变量名吞噬，见 23/23）
#   + 2026-09-21 §CONSULT-UX 咨询页体验修复（见 25）+ §CB-TICKWINDOW/§CB-RUNSTART 盘前误熔根治（见 26）
#   + 2026-09-21 §SELLPOINT-UNIFY 卖出并轨统一裁决层 P1~P4 + §D1 护栏 + BUGFIX 缺陷1~4（见 27）
#   + 2026-09-22 §C1/§C1b 冻结账日期过滤 + 跨日陈旧买单无条件清扫（见 28）+ §C2 深破任意线型无条件升级（并入 27 锁⑩）+ §F1/§F12 实盘费用腿入盈亏/成本与成交额单口径（见 29）
#     + §H1/§H2 交割日期归一/卖出状态快照防别名（见 30）+ §H3 全局写端点收权 admin（见 31）+ §H4/§H5 卖单吞错/新鲜度主判据（见 32）+ §H6/§H7 SSE 续传/陈旧闭包（见 33）
#     + §H9/§M16 outbox 错位/网关 inflight 收割（见 34）+ §F2/§M4/§F5/§M5/§F3 回报接入段（见 35）+ §M14/§M15 同因熔断/夜链自愈（见 36）+ §H8/§F6 探针同源/构建指纹（见 37）
#     + 二波（2026-09-22 当日续）：§M1 golden 源契约单源（见 38）+ §M2/§M3 降级报成功族（见 39）+ §M6/§TZ/§REJECT 数据管道 py 批（见 40）+ §M8 推送三通道内聚/EXPVAR 收权（见 41）+ §M9/§M10/M11 快照与落盘批（见 42）+ §M7 部署清单收编（见 43）+ §M12/§M13 前端与移动壳一致性 + researchd 冒烟（见 44）
#     + C批（2026-09-22 晚间，owner 裁决清单四件套）：§XCHECK 价格复核闸接线/CrossCheckPrice 收编（见 45）+ §NATIVEAUTH 登录 token 迁原生加密存储（见 46）+ §ROOTQMT 根级死键防回潮 + §APPVER APK 服务端驱动强制更新（见 47））
#   + 2026-09-22 §BINANCE Phase 1 多市场地基（PLAN_BINANCE_MULTI_ASSET：BrokerConfig/rules.binance/store 市场迁移/Qty float64/时段+风控 14 闸×3 市场矩阵，见 57）
#     + 2026-09-23 §BINANCE Phase 2 执行/回报/隔离（BinanceExecutor 契约 golden 7 格/binance_report 三条腿/§15.2 ForMarket 隔离/运维端点 6 条，见 58）
#     + 2026-09-23 §BINANCE Phase 3+4 行情/状态双 WS+StdWsDial（纯标准库 RFC6455）/§9 第 15 闸 market_halt 证据链/xasset 三腿/前端市场维度+历史K线（见 59）
#     + 2026-09-23 §ENH 增强批 A+B（PLAN_ENHANCE_20260923：FNG 情绪腿/EDGAR+CryptoPanic 事件腿/观测面三件套/lightweight-charts 试点/walk-forward/滑点回灌挂价，见 60）
#     + 2026-09-24 §抄母仓 可抄榜批（母仓分叉后锤实的 8 项缺陷按序移植：§DISCIPLINE 延持终态失明/§PICKILL-SCOPE 跨 checkout 误杀/§UAT-PORTS e2e 端口单源/§NOTIFYADMIN 推送探测端点收权/§ALERTROUTE 指标告警出站+7×24 节拍/§DEADGAUGE 死规则通用守卫，见 64~69）
#     + 2026-09-24 §抄母仓 可抄榜项 6（§STRATEGY-MERGE 战法参数并发保存稀疏 merge + 版本冲突 409，见 70）
#     + 2026-09-24 §抄母仓 可抄榜项 7（日K复权口径捆绑批，四节同批生效缺一不可：§ADJ 因子前向填充+路由唯一入口 / §KLINE-CHAIN-3 日K三级兜底链 / §ADJ-BASIS 口径位进研究断点键 / §ADJ-BASIS-3 事件缓存口径位与降级保守，见 71~74）
#     + 2026-09-24 §抄母仓 可抄榜项 8（§LINTGATE 前端未定义符号静态门禁：no-undef 锁 error + 实盘链路渲染行为锁，见 75）
# ...
# §全链路 UAT 修复批（2026-09-18 §UAT_FULLCHAIN_VERIFY）专项（见 11/11）：
#   费用腿       ：成交回报 fee/stamp_tax 五路径透传（xt 回调/桥行/网关装配/mock/Go 落库），
#                  三方对账费用差腿首次可观测；旧格式回报缺省 0 保持字节兼容
#   权限门控     ：MsgCenter/Signals/Positions/LLMDebug 对成员隐藏 admin 守卫写入口（vitest + e2e 双锚）
#   占位可观测   ：dragon/double_bump/n_shape 的占位 Evaluate 返回 Level=stub（区别于 nodata）
#   死代码       ：internal/calendar 包、Agent.Scan 通用入口删除并设复活守卫
#   CI-seed      ：nightly 工作流 admin id 改读 auth.json（users 表引用设 grep 守卫）
#   桥防线       ：_record_seen 落盘失败 → _handle_cmd 拒执行（fail-closed，drill-3 补全）
#
# §买卖方向权威化（2026-09-18 §TRADE_SIDE）专项（见 12/14）：
#   现象         ：600580.SH 一笔真实手动卖出（800 股 @26.71）在成交流水里显示为「买入」。
#   桥侧         ：_deal_side 多字段投票（m_nDirection 0/48=买、1/49/50=卖；m_nOffsetFlag 48=买/50=卖；
#                  order_type 交叉否决；冲突不盲判、未命中组合留痕不抛异常）+ embed/xt 两路径同源
#   网关侧       ：本端派发过的单，dispatch.side 是方向唯一权威（旧 setdefault 从未生效——桥行恒带
#                  side）；未派发过的成交保持回报方向；派发项定位链 seq → 交易所委托号 → signal_id
#   纯净度       ：qmt_bridge_strategy.py 纯 ASCII 守卫（GBK 沙箱，中文注释同样乱码）
#
# §单日买入笔数改「已成交」口径（2026-09-18 §BUY_COUNT_FILLED）专项（见 13/14）：
#   口径         ：笔数闸从「orders 表已报笔数」改为「fills 表当日买入成交按委托去重」；
#                  金额闸同日升级为**动态冻结账模型**（§BUDGET_FREEZE_LEDGER）：
#                  占用 = 已成交（fills）+ 在途冻结（LocalBuyFrozen：报单冻结/成交扣除/
#                  撤单解冻）− 卖出回款（SumSellFilledAmountByDay，卖出即回血、钳 0 不放大预算）；
#                  闸3 近似资金 = 本金 − 持仓成本 − 冻结 + 今日已实现盈亏（成交成本已入持仓，
#                  不另扣成交额——修掉 held+filledAmt 双扣）；卖出方向永不设量闸
#   买入终点     ：唯一硬终点是闸1 今日已成交笔数达上限；预算/资金闸全部动态伸缩
#   附带效果     ：同一委托多次部分成交算 1 笔；QMT 客户端手工单（本地无 orders 行）计入额度
#   回归         ：三处锁旧口径的断言（gate/guards/engine）改写为钉新语义 + store 层边界用例
#                  + 冻结账端到端（TestGuardBudgetFreezeLedger 含卖出回血步）
#                  + 闸3 双扣修复与卖出回血（TestGuardCashDynamicSell）
#
# §LLM 热更新稳定性 + 地址规范化（2026-09-18 §LLM_HOTUPDATE / §LLM_BASEURL）专项（见 14/14）：
#   地址规范化   ：供应商 base URL（/v1、/v1beta、裸主机）自动补 /chat/completions；
#                  完整 endpoint 与自建网关非标准路径原样不动（不猜）
#   健康探测     ：ProbeConfig 逐把 key 并发发一次真实最小 chat 调用，一次验完鉴权+地址形态+模型名；
#                  429 算可用、仅 auth/model/quota 算「配置错」、超时夹逼 [15s,60s]
#   热更新语义   ：探测 → 切换 → 落库（落库失败只影响重启自愈、不回滚运行时）；
#                  拒绝必须有确凿证据（network/5xx/400 不算）；三入口共用 runLLMApplyFor；
#                  哨兵不落库也不成钥；force 不污染回滚点；探测端点只读
#   前端         ：拒绝时不得谎报「已热生效」，逐把展示探测结论
#
# §全栈审计批 + 生产实录修复（2026-09-18 §AUDIT20260918 / §PROD-T1 / §PROD-LLM2）专项（见 15/15）：
#   A1/A2        ：网关侧消费 strategy_type/max_order_amount（缺省字段显式策略、拒绝不耗幂等槽）；
#                  Go↔qmt-mock↔gateway 下单字段集 golden 契约文件锁死（TestOrderContractGolden）
#   A3/A4        ：SSE 慢客户端丢弃计数+越阈断连走补发；下单回报落库事务化（含乱序回报/用户隔离）
#   A5/A7        ：前端路由守卫等 /api/auth/me 服务端权威角色对账；/api/status 透传 build_commit +
#                  vite define 同指纹 + 版本漂移横幅（哨兵值静默）
#   PROD-T1      ：603468 当日买入即误推「持仓超期离场」根因三连修——ExecLog.EntryAt 零值守卫、
#                  real_positions.buy_date 透传建仓日、建议层 SellableQty T+1 硬闸（同源下单闸口径）
#   PROD-LLM2    ：「no response from LLM」黑盒修白——流式/非流式 content 全空时 reasoning_content
#                  兜底应答；仍为空则错误携带 finish_reason/usage/响应摘录（保留 no response 前缀）
#
# §信号控制器（2026-09-17 §SIGNAL_CONTROLLER）专项（见 10/10）：
#   组件层       ：internal/signalctl 全包——白名单（空白名单=内置四形态+库规则默认全集、动量/未知严格 opt-in）、
#                  个股/板块黑名单与影子模式、买入持续性确认窗（探针+双窗+两通道两账号隔离+消失重置）、
#                  非买入直通、批量 Evaluate 下标对齐、裁定留痕环（9 用例）+ risk.Gate 同源回归
#   引擎编排层  ：dispatchLive 单点收口——确认窗首探针受阻/满窗放行/消失重置、白名单外拦截标注、
#                  动量空白名单必拦且显式列名才交易（09-17 实盘误交易事故路径回归）
#   信号产生层  ：动量与其他战法同权恒产信号（交易裁决唯一归控制器白名单）
#   HTTP 契约层 ：GET/POST /api/paper/strategies（模拟盘战法开关）+ GET /api/signalctl/verdicts（裁定留痕）
#
# 用实盘数据快照（internal/e2e/testdata/fixtures.json / fixtures_600580.json）离线 mock 全部外部数据源，
# 专项验证改动：
#   1) LLM 超时配置   ：llm.Config.Timeout 默认 60s、自定义生效
#   2) D1 回退        ：mock LLM 对 D1 返回 500 → 3 次轮询重试失败 → 回退上一轮评分
#   3) N形门槛放开     ：D1>0 且总分≥60 → Valid（D2/D3/D4 仅贡献总分）
#   4) 咨询专业模式   ：注入真实实时行情（东财 quote/资金流/分钟 MACD），缺失严禁编造
#   5) HTTP 级全端点  ：/api/news、/api/signals change_pct、/api/signal-logs、/api/sector/hot 兜底、consult API
#   6) 政策反制/confrontation 落盘、ths 昨收推算、GetStockList 新浪→东财兜底、updateHotPool、resolveConflict、PE 预取
#
# §回测自动增强（2026-09-11 A0+A+B+C+D）专项（见 3/3）：
#   A0 配置管线   ：config.ValidateBacktest 区间/档位单调性 + FillDefaults；backtest_settings 读写；
#                  server GET/PUT /api/research/backtest-config + 三处入队注入 + runtask payload 回退链
#   A 动态滑点    ：paper_trades 实测校准中位数（战法级→全局→配置回退）+ 非对称买差 clamp
#   B 流动性约束  ：涨停封死/打开、跌停封死顺延、部分成交 fillRate、amount 千元单位自校
#   C Pareto      ：四维非支配前沿（vs 朴素参照）、推荐解（门槛全过取 Sharpe 最高）、payload 落库
#   D 前端        ：ParetoChart 渲染/交互 + BacktestConfigPanel 读写 + approveOptimization 推荐解请求体
#   回归保证      ：增强关闭（nil/Enabled=false）时旧行为逐字节一致（各包 _test 已含短路用例）
#
# §做空策略链路（2026-09-12 §SHORT 1-5）专项（见 4/4）：
#   四战法包       ：high_churn/break_down/leader_decay/good_news_fade + shortbase 共享输入（全用例跑）
#   接线           ：ScanShort 两层门控/ST 拦截/评分日志（TestScanShort*、TestBuildShortDataDerives）
#   自动卖出       ：SellAction→close、shortSellMarks 日幂等、止损级 advice（TestShortTacticCloseAdvices、
#                   TestAutoExecuteRealSellsShortTactic、TestPaperSellSignalsIncludeShortTactic）
#   融券做空簿     ：保证金/隔离/T+1/利息/止损/路由/e2e 权益连续（TestShort* paper 8 组）
#   全链路 e2e     ：真 runners→ScanShort→SellAction→paper 开空→关门静默（TestShortPipelineTactics）
#
# §市场风险因子（2026-09-12 §MARKET_RISK_GATE B1+B2+B3）专项（见 5/5）：
#   P0 数据源切换   ：涨停/跌停/炸板池 hithink 主源+东财永远兜底、涨跌家数真实弃权（GetBreadth 不编造
#                    1500/1500 假中性）、hithinkItemsToLimitUp 映射、双跑背离 opslog（TestRiskPool*、TestPool*）
#   P1 情绪广度纠偏 ：涨跌家数对涨停池口径单向纠偏（阈值未配/家数缺失=弃权；只降不升；反配兜底）
#                    （TestEmotionBreadthCorrection、TestEmotionBreadthReversedConfig）
#   P2 状态机接线   ：真实炸板率/上涨占比/指数 MA20·MA60 斜率灌入 MarketStateObserve + DetectEmotionPhaseV2
#                    （masLowSlope 口径 + 斜率日级缓存 NaN 弃权）
#   P3 风险档合成   ：情绪+市场状态+宏观三源→Red/Yellow/None + applyRiskTier 信号收紧（门槛/拦N形·动量/
#                    板块映射上浮）+ 总开关/情绪开关热回退（TestSynthesize*、TestApplyRiskTier*、TestComputeAndSet*）
#   P4/P5/P7        ：auto-buy 谨慎层(默认关)拒Red/缩Yellow、系统性风险持仓提醒(SellAction 不命中→绝不自动卖)、
#                    做空风险日置信加成（TestAutoCaution*、TestMarketRiskAlertsNotAuto、TestRiskTierShortBoost）
#
# §UAT 修复/研究升级/情绪面板（2026-09-13）专项（见 6/6）：
#   会话安全链 D1/D3/D7：PublicUser 剥凭据、Enabled 恒序列化、RevokeSession 自助吊销
#   部署自检 D5：verifyDeployment 四分支（禁用/缺 token/密钥池/nil auth）
#   情绪面板 A/B：历史端点日期归一化（YYYYMMDD→ISO）+ days 钳制 250 + 矩阵六相位/thin 纪律
#   研究升级 W6/W7：objective→ref_id 固定槽位（990~994/哈希 1000+）、min_triggers 按目标门槛
#   前端（见 vitest sentiment.test.jsx）：F45 Card actions 插槽、矩阵懒加载、hit_rate×100
#
# §数据管道停摆根治（2026-09-14 §DATA-OUTAGE）专项（见 7/7）：
#   market_risk_daily 历史回放：ths 池统计+日线广度装配回补行、引擎权威行跳过/--force 覆盖、
#   连板高度/炸板率百分转小数/无日线弃权、幂等落库（TestRiskBackfill* 3 组）
#   dataload 盘后保活：target 工作日回退、缺表安全弃权、三表全新鲜零调用短路、
#   日线落后触发补数且池未到位不判成（unittest 4 组）
#
# §备案合规 + 生命周期接线（2026-09-15 §ICP + §GAP-P1）专项（见 8/8）：
#   ICP 页脚       ：备案号常量（沪ICP备2026045551）+ 工信部外链 vitest；登录页/仪表盘两入口挂同一组件
#   衰退降级接线  ：PoolDailyStats 分池逐日聚合（strategy_type 池键修复）+ DemoteAppliedRules
#                  禁用落库/dry-run/无观测保守 keep（TestPoolDailyStats|TestDemoteAppliedRules 3 组）
#                  + stepTask lifecycle 映射与默认 Steps 含 lifecycle（TestLifecycleStepMapped）
#
# 用法:
#   ./scripts/verify_changes.sh                # 编译 + 全部专项（12 个历史专项 + 今日 3 个：方向权威化/笔数成交口径/LLM 热更新）
#   ./scripts/verify_changes.sh -full          # 再连相关全量单测 + QMT 网关 py 全量 + 前端 vitest 一起跑
#
# 说明：本机通常没有 pytest，脚本内 py_tests() 会自动退回标准库 unittest（CI 仍走 pytest）。
set -euo pipefail
cd "$(dirname "$0")/.."

# py_tests <目标文件或目录> [-k 过滤表达式]
#
# 运行 Python 测试。CI 装了 pytest，本机（macOS 开发机）通常没有，因此做一次能力探测后
# 退回标准库 unittest —— 否则脚本在本机会因「No module named pytest」而假失败，
# 让人误以为是被测代码坏了（二者现象一样、成因完全不同，最耗排查时间）。
py_tests() {
	local target="$1"
	local filter="${2:-}"
	if python3 -c 'import pytest' >/dev/null 2>&1; then
		if [ -n "$filter" ]; then
			python3 -m pytest "$target" -q -k "$filter"
		else
			python3 -m pytest "$target" -q
		fi
		return
	fi
	# 过滤串统一接受 pytest 的 "a or b" 写法：unittest 的 -k **不支持** or 表达式，
	# 每个 -k 是一个独立模式（pytest 的 '-k "a or b"' 在 unittest 下会静默匹配 0 个用例），
	# 因此在这里拆成多个 -k。$kstr 有意不加引号——模式都是脚本里写死的单词，依赖分词展开。
	local kstr=""
	for p in ${filter// or/ }; do kstr="$kstr -k $p"; done
	if [ -d "$target" ]; then
		# 目录形态：unittest 用 discover
		python3 -m unittest discover -s "$target" -p 'test_*.py' $kstr -v
	else
		# 文件形态：<dir>/tests/test_x.py → cd <dir> 后按模块 tests.test_x 运行
		# （unittest 的模块名相对 cwd 解析，直接把完整路径转模块名会导入失败）
		local pkg base
		pkg=$(basename "$(dirname "$target")")
		base=$(basename "$target" .py)
		( cd "$(dirname "$target")/.." && python3 -m unittest "$pkg.$base" $kstr -v )
	fi
}

echo "==> 1/8 编译检查..."
go build ./...
go vet ./internal/llm ./internal/combat_agent ./internal/engine ./internal/e2e ./internal/server ./internal/data ./internal/display ./cmd/quant \
	./internal/config ./internal/btreplay ./internal/store ./internal/scheduler ./internal/research ./cmd/research

echo "==> 2/8 实时链路改动专项 e2e（实盘快照 mock）..."
go test -count=1 -v ./internal/e2e/ \
	-run 'TestLLMTimeoutConfig|TestD1RetryQueueAcrossRuns|TestNShapeGateD1AndTotal|TestEndToEndFullPipeline|TestConsult|TestHTTP|TestAttachLiveBar' 2>&1 \
	| grep -E '^(=== RUN|--- (PASS|FAIL)|PASS|FAIL|ok)'

echo "==> 3/8 回测自动增强专项（A0+A+B+C+D，2026-09-11）..."
go test -count=1 -v ./internal/config ./internal/btreplay ./internal/store ./internal/server ./internal/scheduler ./cmd/research \
	-run 'TestValidateBacktest|TestFillDefaults|TestFillBacktestDefaults|TestBacktestSettingsRoundTrip|TestPaperSlippageCalib|TestBacktestConfigEndpoints|TestInjectBacktestPayload|TestPayloadBacktest|TestCostRoundTripPnlExCompat|TestSlippageTiers|TestSlippageBpsAsymmetric|TestFillRate|TestLimitBoardGating|TestCalibAudit|TestEntrySlipGating|TestUniformExit|TestFixAmountScale|TestAvgAmountWan|TestBuildSlipCtx|TestPareto|TestCapFront|TestRecommended|TestPointJSON|TestSaveSweepResultsParetoCarry' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'

echo "==> 4/8 做空策略链路专项（§SHORT 2026-09-12：四战法/接线/自动卖出/融券/e2e）..."
go test -count=1 ./internal/strategies/high_churn/... ./internal/strategies/break_down/... ./internal/strategies/leader_decay/... ./internal/strategies/good_news_fade/... ./internal/strategies/shortbase/... 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/combat_agent/... ./internal/paper/... ./internal/engine/... ./internal/e2e/ \
	-run 'Short|BuildShort|StrategyDisplay|AutoExecuteRealSells|AutoExitReportSells|PaperSellSignals' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'

echo "==> 5/8 市场风险因子专项（§MARKET_RISK_GATE 2026-09-12：P0 数据源 + P1 广度 + P2 状态机 + P3~P7 风险档）..."
go test -count=1 ./internal/data/ -run 'RiskPool|PoolLimit|PoolHelpers|Breadth|MasLowSlope|EmotionPhase|EmotionBreadth' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/config/ -run 'Data|Risk|Emotion|Macro|Scheduler|AutoCaution' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/research/ -run 'MarketState|StateTracker|Classify' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/combat_agent/ -run 'RiskTier|ApplyRiskTier|ComputeAndSet|Synthesize|MarketRisk|AutoCaution|MacroGate' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'

echo "==> 6/8 今日修复专项（2026-09-13：UAT D1/D3/D5/D7 + 情绪面板 A/B + 研究升级 W6/W7）..."
go test -count=1 ./internal/auth/ ./cmd/quant/ -run 'TestPublicUserStripsSessionCredentials|TestEnabledSerializesWhenFalse|TestRevokeSessionSelfLogout|TestVerifyDeployment' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/store/ -run 'TestEmotionStrategyMatrixBuckets|TestEmotionMatrixFallbackPhase|TestEmotionMatrixBadJSONSkipped' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/server/ -run 'TestEmotionHistoryDateNormalize|TestEmotionStrategyMatrixShape|TestOptRefIDForSlots' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/btreplay/ -run 'TestMinTriggersForObjDefaults' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'

echo "==> 7/8 数据管道停摆根治专项（§DATA-OUTAGE 2026-09-14：market_risk_daily 历史回放 + dataload 盘后保活）..."
go test -count=1 ./cmd/research/ -run 'TestRiskBackfill' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'
py_tests qmt_gateway/tests/test_dataload_keepalive.py 2>&1 \
	| grep -E "passed|failed|error|Ran [0-9]+ test|OK"

echo "==> 8/8 备案合规 + 生命周期接线专项（2026-09-15 §ICP + §GAP-P1）..."
# ICP 备案号合规文案守护：常量改动即失败（管局备案文案，变更需先核对备案回执）
grep -q '沪ICP备2026045551' web/src/components/IcpFooter.jsx && grep -q 'beian.miit.gov.cn' web/src/components/IcpFooter.jsx \
	&& echo "ok - icp 备案常量在位" || { echo "FAIL - icp 备案常量缺失"; exit 1; }
( cd web && npm test -- icp_footer ) 2>&1 | grep -E 'Test Files|passed|failed'
go test -count=1 ./internal/research/ -run 'TestPoolDailyStats|TestDemoteAppliedRules' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/scheduler/ -run 'TestLifecycleStepMapped' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'

echo "==> 9/9 加固件1/2 专项（2026-09-15 §HARDENING：ntfy 通道 + 备份/监控脚本在位）..."
go vet ./internal/notify ./cmd/quant
go test -count=1 ./internal/notify/ -run 'TestNtfy|TestPushGatewayDualDispatch|TestPushGatewayOnlyOneChannel' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'
# 备份链路三件套在位：广州快照 ps1+py、Mac 拉取器、launchd 清单
[ -f deploy/qmt-win/backup_snapshot.ps1 ] && [ -f deploy/qmt-win/backup_snap.py ] \
	&& [ -f deploy/mac/restic_pull_backup.sh ] && [ -f deploy/mac/com.quant.backup.plist ] \
	&& echo "ok - 备份链路脚本在位" || { echo "FAIL - 备份链路脚本缺失"; exit 1; }
# Kuma 无头初始化脚本在位（监控重建的最小物料）
[ -f deploy/mac/kuma_seed.js ] && echo "ok - kuma_seed.js 在位" || { echo "FAIL - kuma_seed.js 缺失"; exit 1; }

echo "==> 10/10 信号控制器专项（2026-09-17 §SIGNAL_CONTROLLER：动量误交易根治 + 白名单/黑名单/持续性单点）..."
# A 组件层：白名单（空白名单=内置四形态+库规则全集、动量/未知严格 opt-in、矛盾语义回归）、
#           个股/板块黑名单与影子模式、买入持续性确认窗（探针/双窗/双通道双账号隔离/消失重置）、
#           非买入直通、批量 Evaluate 对齐与清理、裁定留痕环（internal/signalctl 全包 9 用例）
# B 引擎编排层：dispatchLive 收口——确认窗首探针受阻/满窗放行/消失重置（TestDispatchLiveBuyConfirmWindow、
#           TestDispatchLivePruneOnAbsence）；白名单外不下单且标注拦截、动量空白名单必拦/显式列名
#           才交易（TestDispatchLiveSkipsWhitelist——09-17 事故路径回归）
# C 信号产生层：动量与其他战法同权恒产信号、准入裁决归控制器（TestScorePoolMomentumAlwaysEmitted）
# D HTTP 契约层：模拟盘战法开关端点读写/未知战法 400/裁定审计端点形状（TestPaperStrategiesEndpoints）
# E 黑名单口径  ：gate 与控制器同源（checkWhitelist 委托 signalctl.AdmitStrategy，risk 全包回归）
go test -count=1 ./internal/signalctl/ 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/engine/ ./internal/combat_agent/ \
	-run 'TestDispatchLive|TestScorePoolMomentumAlwaysEmitted|TestScorePoolMomentumNoTradePreOpen|TestScorePoolMomentumBelowThreshold' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/server/ ./internal/risk/ ./internal/config/ \
	-run 'TestPaperStrategiesEndpoints|TestGateWhitelistAndMaxPositions|TestGateSTAndBlacklist' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'

echo "==> 11/14 全链路 UAT 修复批专项（2026-09-18 §UAT_FULLCHAIN_VERIFY：费用腿 + 权限门控 + 死代码 + CI-seed 守卫）..."
# A 费用腿（P2-FEE）：Go 回报→ApplyRealFill 落 fills.fee（旧格式缺省 0 兼容）、
#   mock /settlement 费用腿非零、网关 store 入库/输出、handler 多字段名探测、桥 fail-closed
go test -count=1 ./internal/server/ ./cmd/qmt-mock/ -run 'TestHandleQMTReportTradeFeeLeg|TestMockSettlementEndpoint' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'
py_tests qmt_gateway/tests/test_gateway.py 'fee or settlement_endpoint or trade_push' 2>&1 \
	| grep -E "passed|failed|error|Ran [0-9]+ test|OK"
py_tests qmt_gateway/tests/test_bridge_strategy_adapter.py 'record_seen' 2>&1 | grep -E "passed|failed|error|Ran [0-9]+ test|OK"
# B 占位可观测（P2-STUB）：dragon/double_bump/n_shape 占位 Evaluate 必返回 Level=stub
go test -count=1 ./internal/strategies/dragon/ ./internal/strategies/double_bump/ ./internal/strategies/n_shape/ \
	-run 'TestEvaluateStubLevel|TestEvaluatePlaceholder' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# C 前端权限门控（P1-PERM1）：成员隐藏 admin 守卫写入口（MsgCenter 删除/清空/模拟卖出）
( cd web && npm test -- perm_gate ) 2>&1 | grep -E 'Test Files|passed|failed'
# D CI-seed 守卫（P1-CI1）：nightly 禁止引用已废弃的 trading.db users 表（auth 已迁 auth.json）
#   只查真实代码行——注释里引用旧 SQL 文案作说明用途，不算违规（先剔除 # 注释行再匹配）。
#   注意用 POSIX 字符类 [[:space:]] 而非 \s：\s 不是 POSIX ERE，BSD/toybox grep 不支持，
#   会导致缩进注释剔除失败 → 注释里的旧 SQL 文案被误判成真实引用（2026-09-18 实测踩坑）。
grep -vE '^[[:space:]]*#' .github/workflows/nightly-e2e.yml | grep -q 'FROM users' && { echo "FAIL - nightly seed 仍引用 users 表"; exit 1; } || true
grep -q 'auth.json' .github/workflows/nightly-e2e.yml && echo "ok - nightly seed 读 auth.json" || { echo "FAIL - nightly seed 未接 auth.json"; exit 1; }
# E 死代码在位守卫（P2-BE-DEAD）：calendar 包与 Agent.Scan 通用入口已删除，防止半成品复活
[ ! -d internal/calendar ] && echo "ok - internal/calendar 已移除（真实现在 data/trade_time.go）" || { echo "FAIL - calendar 包复活"; exit 1; }
grep -q 'func (a \*Agent) Scan(' internal/combat_agent/agent.go && { echo "FAIL - 无调用方的 Agent.Scan 通用入口复活"; exit 1; } || true

echo "==> 12/14 买卖方向权威化专项（2026-09-18 §TRADE_SIDE：派发项方向唯一权威 + 桥侧多字段投票）..."
# A 桥侧方向解析（qmt_gateway/tests/test_deal_direction.py）：
#   m_nDirection 双枚举空间（0/48=买、1/49/50=卖，柜台既有 offset 口径也有 ASCII '0'/'1' 口径）、
#   m_nOffsetFlag 48=买/50=卖（08-31 现金流出实证口径，49 从未被真实卖出验证过）、
#   order_type 交叉否决、冲突不盲判落到下一权威、未命中组合留痕且不抛异常、描述串保持纯 ASCII
py_tests qmt_gateway/tests/test_deal_direction.py
# B 网关侧方向权威化（qmt_gateway/tests/test_file_bridge.py）：
#   本端派发过的单 → dispatch.side 覆盖桥的误判方向（最恶劣形态：归因对、方向反）；
#   未派发过的成交（客户端手工单/对账来源）→ 无权威方向可依，回报方向原样保留
py_tests qmt_gateway/tests/test_file_bridge.py 'dispatch or unattributed'
# C 桥脚本纯 ASCII 守卫：该脚本在 GBK 沙箱执行，任何非 ASCII 字节（含中文注释）都会乱码
python3 - <<'PY'
b = open('qmt_gateway/qmt_bridge_strategy.py', 'rb').read()
n = sum(1 for c in b if c > 127)
print('ok - qmt_bridge_strategy.py 非 ASCII 字节 %d' % n if n == 0 else
      'FAIL - qmt_bridge_strategy.py 出现非 ASCII 字节 %d（GBK 沙箱会乱码）' % n)
raise SystemExit(0 if n == 0 else 1)
PY

echo "==> 13/14 单日买入笔数「已成交」口径 + 动态冻结账专项（2026-09-18 §BUY_COUNT_FILLED / §BUDGET_FREEZE_LEDGER）..."
# 口径分工（动态冻结账模型，勿混成一个）：
#   笔数闸 → fills 表当日买入成交，按委托去重（order_id → 券商交割流水号 → 行 ID 依次兜底）；
#            **买入唯一硬终点**：今日已成交笔数达上限
#   金额闸 → 占用 = 已成交金额（SumBuyFilledAmountByDay）+ 在途冻结（LocalBuyFrozen 状态派生）
#            − 卖出回款（SumSellFilledAmountByDay，钳 0）——撤单释放、成交扣除、卖出回血；
#            闸3 = 本金 − 持仓成本 − 冻结 + 今日已实现盈亏（成交成本已入持仓，不另扣成交额）
# A store 层计数边界：部分成交去重 / 卖出不计 / 非当日不计 / 他账号不计 / 无委托号不合并
go test -count=1 ./internal/store/ -run 'TestCountBuyFilled' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# A2 store 层冻结账金额聚合：买入占用半边 + 卖出回款半边（旧数据 amount=0 回落 price×qty）
go test -count=1 ./internal/store/ -run 'TestSum.*FilledAmountByDay' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# B 闸口层：成交笔数达上限才拦（旧口径是报单即占额度，被废单会锁死当天买入权）
go test -count=1 ./internal/risk/ -run 'TestGateBuyDiscipline' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# C 交易层：达上限后拒绝新买入、卖出不受限
go test -count=1 ./internal/trading/ -run 'TestGuardDailyBuysCap' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# C2 交易层动态冻结账：报单冻结 → 撤单解冻 → 成交扣除 → 卖出回款释放预算；
#    闸3 不双扣成交成本、随卖出回血
go test -count=1 ./internal/trading/ -run 'TestGuardBudgetFreezeLedger|TestGuardLocalFrozen|TestGuardCashDynamicSell' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# D 引擎层：auto 下单路径同口径（0 成交时不拦，成交满额才拦）
go test -count=1 ./internal/engine/ -run 'TestAutoPlaceDailyCap' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'

echo "==> 14/14 LLM 热更新稳定性 + 地址规范化专项（2026-09-18 §LLM_HOTUPDATE / §LLM_BASEURL）..."
# A 地址规范化（internal/llm）：供应商 base URL（/v1、/v1beta、裸主机、带尾斜杠）自动补
#   /chat/completions；已是完整 endpoint 或自建网关非标准路径**原样不动**（不猜）；
#   端到端断言真实请求路径，防止只改了字符串却仍打到 base 路径（供应商 404/405）
go test -count=1 ./internal/llm/ -run 'TestProviderBaseURLIsNormalized|TestChatHitsChatCompletionsPath' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'
# B 候选配置健康探测：逐把 key 真实最小调用；429 算可用（证明鉴权已过）、
#   仅 auth/model/quota 算「配置错」、超时夹逼 [15s,60s]、max_tokens 过小自动放宽、不泄漏密钥
go test -count=1 ./internal/llm/ -run 'TestProbe' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# C 热更新语义（internal/server）：探测 → 切换 → 落库；拒绝时运行时与磁盘**都不动**；
#   network/5xx/400 不构成拒绝证据（采用 + 标记未验证）；三入口共用一份实现；
#   全哨兵且无原值 → 400 且不重建；force 不污染回滚点；探测/回滚端点只读语义
go test -count=1 ./internal/server/ -run 'TestHotUpdate|TestSetLLMConfig|TestAdminSetLLM' 2>&1 \
	| grep -E '^(--- FAIL|FAIL|ok)'
# D 前端不得谎报（web/src/__tests__/llm_hotupdate_ui.test.jsx）：拒绝时不得出现「已热生效」，
#   逐把展示探测结论；只有 applied 才更新基线与「已配置」状态
( cd web && npm test -- llm_hotupdate_ui ) 2>&1 | grep -E 'Test Files|passed|failed'

echo "==> 15/17 全栈审计批 + 生产实录修复（2026-09-18 §AUDIT20260918 A1-A9 / §PROD-T1 / §PROD-LLM2）..."
# A1/A2 下单契约：Go↔qmt-mock↔gateway 字段集 golden 文件锁死；网关侧消费
#   strategy_type/max_order_amount（缺字段显式策略），拒绝不消耗幂等槽
go test -count=1 ./internal/trading/ -run 'TestOrderContractGolden' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
py_tests qmt_gateway/tests/test_order_gates.py 2>&1 | grep -E "passed|failed|error|Ran [0-9]+ test|OK"
# A3 SSE 慢客户端：越阈断连淘汰 + 丢弃计数排空归零
go test -count=1 ./internal/server/ -run 'TestSSEEvictsStalledClient|TestSSEDropCounterResetsOnDrain' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# A4 下单主链事务化：回报落库 UPDATE+INSERT 同事务、乱序回报、用户隔离
go test -count=1 ./internal/store/ -run 'TestApplyOrderReportTx' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# A5 前端强对账门：/api/auth/me 回读前不放行 admin 壳；错误状态码透出
( cd web && npm test -- a5_auth_reconcile ) 2>&1 | grep -E 'Test Files|passed|failed'
# A7 版本可见性：/api/status 透传 build_commit（未注入=空串）+ 前端漂移告警纯函数（哨兵静默）
go test -count=1 ./internal/server/ -run 'TestStatusBuildCommitField' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
( cd web && npm test -- utils.test ) 2>&1 | grep -E 'Test Files|passed|failed'
# PROD-T1 当日买入误报超期 + 建议层 T+1 闸：EntryAt 零值守卫、buy_date 透传、
#   卖出五路只喂可卖持仓（缺数据 fail-open）；真实远古建仓仍判超期（防过度抑制）
go test -count=1 ./internal/combat_agent/ -run 'TestBuildExitContextZeroEntryAtIsEmpty|TestGenericTrailingExitStillTimeoutsForOldEntry' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/trading/ -run 'TestExecLogsFromRealEntryDate|TestAdviseT1LockedSkipsSellSide' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/store/ -run 'TestRealPositionsBuyDateRoundTrip' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# PROD-LLM2 空响应诊断：reasoning_content 兜底（流式/非流式）、错误携带
#   finish_reason/usage/响应摘录且保留 "no response from LLM" 前缀（§FIX-0921 回落判定）
go test -count=1 ./internal/llm/ -run 'TestStreamChatReasoningOnlyFallback|TestStreamChatEmptyCarriesEvidence|TestNonStreamEmptyChoicesEvidence|TestNonStreamReasoningFallback|TestNonStreamEmptyContentEvidence' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'

if [ "${1:-}" = "-full" ]; then
	echo ""
	echo "==> 附加：QMT 网关 Python 全量单测（文件桥/保活/降级/清仓护栏/成交方向解析）..."
	py_tests qmt_gateway/tests/
	echo ""
	echo "==> 附加：实时链路相关全量单测..."
	go test -count=1 ./internal/combat_agent/... ./internal/engine/... ./internal/llm/... ./internal/strategies/... ./internal/paper/... \
		./internal/data/... ./internal/display/... ./internal/newsagent/... ./internal/e2e/ ./internal/server/ ./cmd/quant/...
	echo ""
	echo "==> 附加：回测增强全量单测（含回归短路）..."
	go test -count=1 ./internal/config/... ./internal/btreplay/... ./internal/store/... ./internal/scheduler/... ./cmd/research/...
	echo ""
	echo "==> 附加：前端回测增强 + 组件测试（vitest run）..."
	( cd web && npm test )
fi

echo "==> 16/17 §ENH-0~4 增强批 A~D 专项（日历缓存 / 咨询检索增强 / 资金流第二源 / 事件因子）..."
# A 交易日历落盘缓存（§ENH-0）：写读回环、空集合与"无文件"分离、坏文件分流、QUANT_DATA_DIR 路径口径
go test -count=1 ./internal/data/ -run 'TestTradingCalendarCacheRoundTrip|TestLoadTradingCalendarCacheAbsentAndCorrupt|TestCalendarCacheFilePathUsesDataDirEnv' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# B 历史事件归档 + 咨询检索增强（§ENH-3）：历史同类事件注入 ⟦DATA⟧、无归档文件静默降级（e2e 全链路）
go test -count=1 ./internal/e2e/ -run 'TestConsultHistoryEventsInjected|TestConsultNoHistoryFileSilent' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# C 同花顺资金流第二源（§ENH-2）：capital-flow 装配（单位=元）、null 缺数拒拼凑、东财失败回退合并错误、sina/腾讯富集
go test -count=1 ./internal/data/ -run 'TestHithinkStockMoneyFlow|TestGetStockMoneyFlowFallbackToHithink|TestEnrichFlowFromHithinkOnSina' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# D 事件因子（§ENH-4）：news_score 聚合口径（同日取 max|score|、板块互含匹配、宏观排除）+ 面板对齐 NaN 语义
go test -count=1 ./internal/research/ -run 'TestEventFactor' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'

echo "==> 17/17 §ENH-5 批E QMT Level-1 行情 feed 专项（qmt_feed 合并注入 + /quotes 契约 + 交易链路隔离）..."
# A Go feed：命中才注入/共享指针复制/缺失字段保留/超龄与停牌丢弃（-race 锁竞态）
go test -count=1 -race ./internal/data/ -run 'TestQMTFeed' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# B 客户端契约：codes 带后缀出网、Bearer 携带、响应 key 归一裸码、非 200 透传
go test -count=1 -race ./internal/trading/ -run 'TestQMTClientQuotes' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# C mock /quotes 契约（nightly 的 L1 数据源）+ feed_connected 观察字段
go test -count=1 ./cmd/qmt-mock/ -run 'TestMockQuotesEndpoint|TestMockAuthRequired' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# D Python feed：单元降级 + /quotes 鉴权/400/空ticks + broker 断连行情仍 200（隔离铁律回归锁）
py_tests qmt_gateway/tests/test_quote_feed.py '' 2>&1 | grep -E "passed|failed|error|Ran [0-9]+ test|OK"

echo "==> 18/18 §RFIX-1~5 + §ENH-A/B 战法层修复增强批专项（2026-09-19：MACD逐股/方向拟合/阈值闭环/口径治理/崩溃fail-fast/涨停微结构/DSR+PBO）..."
# RFIX-1 btreplay n_shape MACD 逐股重算：跨股污染修复 + curIdx 越界钳位退化（生产 #264 panic 实录）
go test -count=1 ./internal/btreplay/ -run 'TestBacktestStockRefreshesMacdPerStock|TestNShapeTriggerClampsOutOfRange|TestNShapeRunTwoStocksNoPanic' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# RFIX-2 样本内方向拟合：负制度翻转收割（两内核）、防未来函数（IS正OOS负仍拒）、裁决纯函数
go test -count=1 ./internal/research/ -run 'TestDirFitFlipsNegativeRegime|TestDirFitNoLookahead|TestFitDirsByInSampleSign|TestWindowedDirFitFlipsNegativeRegime' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# RFIX-3 阈值闭环：预期触发率估算（runner 分位口径/无未来函数）+ lifecycle 零观测反静默 + apply 守卫 warning + reason 特征 token
go test -count=1 ./internal/research/ -run 'TestTriggerRate|TestFactorTrigEst|TestDemoteAppliedRulesZeroObsAlert' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./cmd/research/ -run 'TestFactorTrigNote' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/server/ -run 'TestThresholdOverrideWarning' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# RFIX-4 口径治理：寻优 pending 30 天过期状态机 + backtest_jobs 僵尸行启动恢复（MarkRunningInterrupted 复活）
go test -count=1 ./internal/store/ -run 'TestExpireStalePendingOptimizations|TestMarkRunningInterruptedRevived' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# RFIX-5 确定性崩溃 fail-fast：panic/fatal 特征提取（含生产 #264 形态回归）
go test -count=1 ./internal/scheduler/ -run 'TestCrashMarkerLine' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# ENH-A 涨停微结构：三池→StockSeries 四列装配 + CatLimit 因子族事件日掩码 + 注册表 9 大类
go test -count=1 ./internal/research/ -run 'TestAssembleLimitMicroColumns|TestLimitMicroFactorsOnPanel' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/factor/ -run 'TestRegistry' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# ENH-B 多重检验：DeflatedIR 公式数值/白噪声海选分层判据/真信号不误杀 + PBO-lite 分块 + 内核字段产出
go test -count=1 ./internal/research/ -run 'TestDeflatedIR|TestPBOSignConsistency|TestDiscoveryRobustnessFields' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# 前端：寻优列表 expired 默认过滤契约用例登记于 web/e2e/uat_full.spec.mjs（RW-1/RW-2，随 E2E 全量跑）

echo "==> 19/19 §ENH-A 生产回填承载脚本静态守卫（2026-09-20 回填实录：PS5.1 无控制台吞 stderr 首跑失败教训）..."
# run_ths_backfill.ps1 四条铁律回归锁：①恰好一个 UTF-8 BOM（双 BOM 令 PS 报「?# 不是 cmdlet」）；
# ②输出必须走 cmd /c 重定向（& exe 2>&1 管道在 ErrorActionPreference=Stop 下把原生 stderr 转
#    NativeCommandError 静默终止——09-20 首跑退出码1日志0字节实录）；③密钥只从机器注册表读、
# 脚本内不得内嵌任何 key 字面量；④退出码显式上抛（计划任务 LastTaskResult 可信）。
rb=deploy/qmt-win/run_ths_backfill.ps1
python3 - "$rb" <<'PY' || { echo "--- FAIL: $rb BOM 检查"; exit 1; }
import sys
d = open(sys.argv[1], 'rb').read()
boms = 0
while d.startswith(b'\xef\xbb\xbf'):
    boms += 1
    d = d[3:]
assert boms == 1 and b'\r\n' in d and b'\n\n' not in d.replace(b'\r\n', b''), 'BOM/CRLF 不合规'
PY
grep -q 'cmd /c' "$rb" || { echo "--- FAIL: $rb 缺 cmd /c 重定向承载"; exit 1; }
grep -q "GetEnvironmentVariable('HITHINK_FINANCE_API_KEY', 'Machine')" "$rb" || { echo "--- FAIL: $rb 密钥未走机器注册表"; exit 1; }
! grep -qE 'sk-[A-Za-z0-9]{8,}' "$rb" || { echo "--- FAIL: $rb 疑似内嵌密钥字面量"; exit 1; }
grep -q 'exit \$LASTEXITCODE' "$rb" || { echo "--- FAIL: $rb 未上抛退出码"; exit 1; }
echo "ok"

echo "==> 20/20 §A7 版本漂移根治专项（2026-09-20：部署脚本必须同步前端 web/dist）..."
# 根因：旧版 deploy_guangzhou.sh 只同步二进制/gateway/pydata，从不传 web/dist，
#       云端 Caddy 前端冻结在旧 buildCommit，浏览器端误报「前端与服务器版本不一致」。
#       静态守卫：脚本必须含 web/dist 同步段（漏传即失败，防回归）。
dg=scripts/deploy_guangzhou.sh
grep -q 'web/dist' "$dg" && echo "ok - $dg 含 web/dist 同步段（版本漂移回归锁）" \
  || { echo "--- FAIL: $dg 未同步前端 web/dist（§A7 版本漂移根因）"; exit 1; }

echo "==> 21/21 §FIX-5 UAT 自举脚本静态守卫（2026-09-20：全栈 UAT 必须可从本地一键复跑）..."
# 根因：mock+engine+前端+seed 的启动流程此前只内联在 .github/workflows/nightly-e2e.yml，
#       本机无处可跑——只能照抄 YAML，漏 seed 情绪/持仓即用例 skip 或假红。
#       守卫：脚本存在 + 可执行 + 语法通过 + 关键 seed 步齐备（退化成空脚本即失败）。
ub=scripts/uat_bootstrap.sh
[ -f "$ub" ] || { echo "--- FAIL: 缺 ${ub}（UAT 不可一键复跑）"; exit 1; }
[ -x "$ub" ] || { echo "--- FAIL: $ub 不可执行（需 chmod +x）"; exit 1; }
bash -n "$ub" || { echo "--- FAIL: $ub 语法错误"; exit 1; }
grep -q 'api/paper/buy' "$ub" || { echo "--- FAIL: $ub 缺模拟盘持仓 seed（卖出分支会 skip）"; exit 1; }
grep -q 'market_risk_daily' "$ub" || { echo "--- FAIL: $ub 缺情绪日线 seed"; exit 1; }
grep -q 'api/tenants/t_default' "$ub" || { echo "--- FAIL: $ub 缺租户配额上调（429 假红根因）"; exit 1; }
echo "ok - $ub 自举流程完整（构建 + 起栈 + seed + 配额）"

echo "==> 22/22 §FIX-20260920 今日修复批（候选 nil panic / QMT 待生效可观测性 / 租户 429 头 / panic trace-id / vitest 并发超时 / sqlite 关闭）..."
# ① FIX-1 候选 store 层哨兵错误：CandidateByID 的 no-rows 分支必须返回 ErrCandidateNotFound。
#    旧实现 return nil,nil → 只判 err 的调用方解引用 panic（server/research.go、cmd/research/auto.go、
#    internal/btreplay/replay.go 三处）。注：LatestCandidate 的 (nil,nil) 是既有显式契约（调用方已全判 nil），不在此列。
sc=internal/store/candidates.go
grep -q 'ErrCandidateNotFound' "$sc" || { echo "--- FAIL: $sc 缺 ErrCandidateNotFound（FIX-1 回归）"; exit 1; }
grep -q 'return nil, ErrCandidateNotFound' "$sc" || { echo "--- FAIL: $sc CandidateByID 未返回哨兵错误"; exit 1; }
# ② FIX-2 QMT 待生效可观测性：Snapshot 暴露 PendingEnabled；诊断日志区分 ctrlEnabled
grep -q 'PendingEnabled' internal/trading/controller.go || { echo "--- FAIL: controller.go 缺 PendingEnabled（FIX-2）"; exit 1; }
grep -q 'ctrlEnabled' internal/server/qmt.go || { echo "--- FAIL: qmt.go 诊断未含 ctrlEnabled（FIX-2）"; exit 1; }
# ③ FIX-3 租户级 429 必须带 Retry-After（统一走 rejectRateLimit）
grep -q 'rejectRateLimit(w, time.Minute)' internal/server/server.go || { echo "--- FAIL: 租户限流未走 rejectRateLimit（FIX-3）"; exit 1; }
# ③b FIX-3 收口：429 只允许出现在统一出口 rejectRateLimit 内部（全文件恰好 1 处 writeError(w, 429）
#      ——登录/setup/租户三档若各自裸吐 429，就会缺 Retry-After 头（2026-09-20 实测残留两处，已收口）
[ "$(grep -c 'writeError(w, 429' internal/server/server.go)" = "1" ] || { echo "--- FAIL: 存在绕过 rejectRateLimit 的裸 429（FIX-3）"; exit 1; }
# ④ FIX-4 panic 可追溯：genTraceID + X-Trace-Id 回写
grep -q 'func genTraceID' internal/server/server.go || { echo "--- FAIL: 缺 genTraceID（FIX-4）"; exit 1; }
grep -q 'X-Trace-Id' internal/server/server.go || { echo "--- FAIL: recover 未回写 X-Trace-Id（FIX-4）"; exit 1; }
# ⑤ FIX-6 vitest 并发超时收敛：Testing Library asyncUtilTimeout 放宽
grep -q 'asyncUtilTimeout' web/src/__tests__/setup.js || { echo "--- FAIL: setup.js 未放宽 asyncUtilTimeout（FIX-6）"; exit 1; }
# ⑥ FIX-7 python sqlite 关闭：reset_test_row.py 用 contextlib.closing
grep -q 'closing(' scripts/reset_test_row.py || { echo "--- FAIL: reset_test_row.py 未用 closing（FIX-7）"; exit 1; }
echo "ok - 静态守卫 10/10 通过"
# 动态：FIX-1 回归用例——候选不存在须 404 且不 panic→500
go test -count=1 ./internal/store/ -run 'TestCandidateByID_NotFoundReturnsErr' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/server/ -run 'TestResearchApproveMissingCandidate' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'

echo "==> 23/23 §A7-B/§A7-C 部署健壮性专项（2026-09-20：前端产物指纹 + 停服兜底 + shell 变量名吞噬）..."
# ① §A7-B 产物指纹必须落盘：只有 dist/BUILD_COMMIT 才能让部署脚本在**上传前**判定
#    "这个前端是哪一版"（注入 JS 的那份被压缩混淆，只能猜）。缺它则「先 build 后 commit」
#    导致的漂移无法被自动拦住（2026-09-20 两次踩中，用户端报版本不一致横幅）。
grep -q 'buildCommitMarker' web/vite.config.js || { echo "--- FAIL: vite.config.js 缺 buildCommitMarker（§A7-B）"; exit 1; }
grep -q 'BUILD_COMMIT' web/vite.config.js || { echo "--- FAIL: vite.config.js 未落盘 dist/BUILD_COMMIT（§A7-B）"; exit 1; }
grep -q 'BUILD_COMMIT' "$dg" || { echo "--- FAIL: $dg 未校验 dist/BUILD_COMMIT（§A7-B）"; exit 1; }
grep -q 'BUILD_COMMIT' scripts/build_apk.sh || { echo "--- FAIL: build_apk.sh 未校验内嵌指纹（§A7-B）"; exit 1; }
grep -q 'web:fingerprint' scripts/verify_deploy_guangzhou.sh || { echo "--- FAIL: 验证脚本缺前端指纹探针 4b（§A7-B）"; exit 1; }
# ② §A7-C 停服必须可逆：[2/5] 的 net stop 之后只有 [4/5] 会拉起，而 `-s` 跳过 [4/5]、
#    任何提前退出在 set -e 下直接结束 —— 两处都会把线上引擎留在停机态（2026-09-20 实录，
#    引擎停了约 3 分钟且无提示）。守卫锁死「EXIT 兜底 + -s 分支显式拉起」两个出口。
grep -q 'trap restore_services_on_exit EXIT' "$dg" || { echo "--- FAIL: $dg 缺停服 EXIT 兜底（§A7-C）"; exit 1; }
grep -q '^start_engine_services()' "$dg" || { echo "--- FAIL: $dg 缺 start_engine_services（§A7-C）"; exit 1; }
grep -q 'SERVICES_STOPPED=1' "$dg" || { echo "--- FAIL: $dg 未在停服处置位 SERVICES_STOPPED（§A7-C）"; exit 1; }
# ③ shell 变量名吞噬：`$VAR` 紧跟全角字符（如 `$ds）`）时 bash 会把多字节并入变量名，
#    在 set -u 下报 unbound variable 直接中止（2026-09-20 实测：部署在「指纹校验通过」那行
#    崩溃，前端因此漏传）。全仓 12 处，已全部改为 ${VAR}；此守卫防新增写法回归。
#    注：必须用 python 扫——grep 无法可靠匹配 Unicode 区间（toybox grep 对 -P/码位静默失灵）。
python3 - <<'PY' || { echo "--- FAIL: shell 脚本存在「变量名后紧跟非 ASCII 字符」写法（bash 会吞进变量名）"; exit 1; }
import pathlib, re, sys
pat = re.compile(r'\$[A-Za-z_][A-Za-z0-9_]*(?=[^\x00-\x7F])')
bad = []
for p in sorted(pathlib.Path('scripts').rglob('*.sh')):
    for i, line in enumerate(p.read_text(encoding='utf-8').splitlines(), 1):
        if line.lstrip().startswith('#'):
            continue
        for m in pat.finditer(line):
            bad.append("%s:%d: %s   <-- 应写成 ${%s}" % (p, i, m.group(0), m.group(0)[1:]))
if bad:
    print("\n".join(bad))
    sys.exit(1)
PY
echo "ok - 静态守卫 9/9 通过（产物指纹 5 + 停服兜底 3 + 变量名吞噬全仓扫描 1）"

echo "==> 24/24 §FIX-20260920 数据管道修复批专项（THS 单域熔断 + 东财 f62 主站信任 + 板块列表镜像分页 + EM-FFLOW 真值口径 + THS-KLINE 5分钟 + QUOTE-CHAIN 同花顺首选源）..."
# A THS 单域熔断（per-operation isolation）：分钟 K 不支持档位不得误伤其他域；空 op 是 no-op；
#   分钟源失败只熔断「分钟」域，不得污染 quote/kline/boards/boardstocks 域（2026-09-20 实测：
#   旧整源熔断会让一次 unsupported-period 把整个同花顺出口打死，咨询页行情链全盘失效）。
go test -count=1 ./internal/data/ -run 'TestTHSBreaker|TestTHSMinuteUnsupportedPeriodError|TestTripThsEmptyOp|TestMinuteKLineUnsupportedPeriod|TestMinuteKLineProviderFailureTrips|TestQuoteChainUnaffectedByMinuteBreaker' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# B 东财 f62 主站信任 / 镜像弃用：主站 stock/get 的 f62 是真实主力净流入；push2 镜像的 f62
#   是已知占位值 2，绝不可采信（否则净流入恒为 2 元）。按「服务主机」判定，而非供应商。
go test -count=1 ./internal/data/ -run 'TestEastMoneyQuoteF62' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# C 板块列表镜像分页：主站 pz=500 行为字节不变；镜像按 pn 显式分页取全 496（100/100/100/100/96）；
#   跨页按 f12 去重；任一页失败整体拒绝（不返回半成品）；页数控卫防失控请求。
go test -count=1 ./internal/data/ -run 'TestSectorList' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# D EM-FFLOW 真值口径：东财 fflow 实测 6 列（日期 + 主力/小/中/大/超大 净额），不返回 in/out 对；
#   四档净额之和≈0（资金守恒）、主力净=大净+超大净。主源 hithink 落库时一并算好 Net 统一口径。
go test -count=1 ./internal/data/ -run 'TestParseMoneyFlowRealRowShape|TestEmFFlowURLHasRequiredParams' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# E THS-KLINE 5 分钟档：咨询页要的是 5 分钟 MACD，GetTHSMinuteKLine 必须把 5 传下去；
#   不支持档位（非 1/5/30/60）在发请求前返回哨兵 ErrTHSUnsupportedPeriod（不熔断、不 panic）。
go test -count=1 ./internal/data/ -run 'TestGetTHSMinuteKLine|TestTHSKLine' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# F QUOTE-CHAIN 回归：同花顺注入行情客户端后成为咨询页行情链**首选源**（东财不行用同花顺兜底，
#   换手率只有同花顺能给）。引擎 buildStockBlock + e2e 全链路双锚。
go test -count=1 ./internal/engine/ -run 'TestConsultBlock' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/e2e/ -run 'TestConsultRealtimeQuoteNetInflow|TestConsultNetInflowMissingHint|TestConsultNetInflowTrueZero' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'

echo "==> 25 §CONSULT-UX 咨询页体验修复专项（2026-09-21：markdown 渲染 + 双口径防误判 + 防编造语气）..."
# 背景：用户实测反馈咨询 tab 三个问题——①数据丢失（满屏 [数据缺失]）②一大段不分点、结构差
#       ③太枯燥不易读。根因与修复：
#   A 防误判  ：auditNumbers 只认白名单里的原样数字；模型把"股"改写成"手"/加逗号/换算单位
#              就被判编造、整段隐成 [数据缺失]。修复=数据块给"股/手"双口径 + 提示词"数字引用铁律"
#              （原样照抄、禁改写）。引擎双口径与提示词须同步——任一方漏改都会让 [数据缺失] 复发。
#   B 结构化  ：前端此前把 AI 回复当纯文本渲染（markdown 不可见），加自写轻量 Markdown 渲染器
#              （React 节点输出、不 dangerouslySetInnerHTML，杜绝 XSS）+ 提示词要求 2-4 个 ## 小标题。
#   C 防编造  ：盘前主力净流入等常"数据源未返回"，提示词要求直接说"暂未取到"而非补数字。
# A 引擎双口径（internal/engine/engine.go）：成交量/近5日量能同时给 股 与 手，断言双口径锁定
go test -count=1 ./internal/engine/ -run 'TestConsultBlockCarriesTurnoverAndIndustryOnPrimaryOutage' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# B 提示词铁律（internal/llm/llm.go → consult_prompt_test.go）：必须含 原样照抄/回答结构/数据源未返回/手/300字
go test -count=1 ./internal/llm/ -run 'TestConsultSystemPromptConsultUX' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# C 前端 Markdown 渲染器（web/src/components/Markdown.jsx → markdown.test.jsx）：标题/列表/粗斜体/
#   行内代码/数字原样/无 XSS（不渲染 <img onerror>）
( cd web && npm test -- markdown ) 2>&1 | grep -E 'Test Files|passed|failed'

echo "==> 26 §CB-TICKWINDOW/§CB-RUNSTART 盘前误熔+真断线漏熔修复批（2026-09-21 实录）..."
# 背景：广州 09:10:27 误熔 / 09:26:08 二熔 / 09:30:17 开盘自愈。三处根因对应三道锁：
#   A 窗口语义：lastFailAt=「本轮连续失联起点」，成功探测必须清零（否则被成功隔开的两次
#     失败凑窗误熔）；连续失败满 miss 才熔（旧实现相邻失败间隔=探测周期恒 < miss → 真断线永不熔断）。
#   B 探测门控：健康探测只在连续竞价窗口跑（9:30-11:30/13:00-14:57）——QMT 桥心跳是
#     handlebar-tick 驱动，盘前/竞价/午休静默属正常，计入失联天天误熔。
#   C 桥时钟：XtItClient 内嵌解释器可能是 UTC 钟，ts 必须 epoch+8h 展开（现网 "02:25+08:00"
#     实际 09:25，落后 8 小时）；且 strategy 文件必须保持纯 ASCII（QMT 编辑器按 GBK 加载）。
go test -count=1 ./internal/trading/ -run 'TestBreakerFailRunStart|TestControllerTripAndIdempotent' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/data/ -run 'TestIsContinuousTrade' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# A 静态锁：成功分支清零 lastFailAt；探测失败分支不得再每轮覆写窗口起点
grep -q 'c.lastFailAt = time.Time{}' internal/trading/controller.go || { echo "--- FAIL: HealthCheck 成功探测未清零失联窗口（§CB-RUNSTART 回归）"; exit 1; }
# B 静态锁：pushRealAdvice 的 HealthCheck 必须包在 IsContinuousTrade 门内
grep -q 'data.IsContinuousTrade(time.Now())' internal/engine/scoring_loop.go || { echo "--- FAIL: 健康探测未受连续竞价窗口门控（§CB-TICKWINDOW 回归）"; exit 1; }
# C 静态锁：桥端 ts 走 _now_cn_str/_now_bj，不得回到裸 strftime 本地钟面；strategy 保持纯 ASCII
# （用 if 而非 `grep && {fail}`：无 set -e 时后者会静默吞掉判定，且 §A7-E 实录该形态易打死脚本）
if grep -q 'time.strftime("%Y-%m-%dT%H:%M:%S+08:00")' qmt_gateway/qmt_bridge_strategy.py; then echo "--- FAIL: strategy 仍用本地钟面+硬编码 +08:00（桥 ts 回归）"; exit 1; fi
grep -q '_now_bj().strftime' qmt_gateway/qmt_bridge.py || { echo "--- FAIL: qmt_bridge.py ts 未走 _now_bj（桥 ts 回归）"; exit 1; }
python3 - <<'PY' || { echo "--- FAIL: qmt_bridge_strategy.py 混入非 ASCII（QMT GBK 编辑器会撕裂源码）"; exit 1; }
import sys
d = open('qmt_gateway/qmt_bridge_strategy.py', 'rb').read()
sys.exit(0 if all(b < 128 for b in d) else 1)
PY
grep -q 'qmt_bridge_strategy.py' scripts/deploy_guangzhou.sh || { echo "--- FAIL: 部署 [2b] 清单缺 qmt_bridge_strategy.py（桥修复不会随部署下发）"; exit 1; }
echo "ok - §CB 专项守卫通过（行为回归 2 组 + 静态锁 6 道）"

echo "==> 27 §SELLPOINT-UNIFY 卖出并轨统一裁决层专项（2026-09-21：P1 signalctl 状态机 / P1-b 影子 / P2 live 切闸 / P3 模拟盘并轨 / P4 探测器修复 + §D1 护栏 + BUGFIX 缺陷1~4）..."
# 背景（docs/REFACTOR_UNIFIED_SELL_20260921.md + docs/BUGFIX_SELLPOINT_FALSEPOSITIVE_20260921.md）：
#   五路卖出信号收编为 signalctl 单一裁决通道（键=(channel,账号,代码)状态机）：
#   触硬线→锁线+固定观察窗→窗结算只认新鲜做多信号(边界⑥)→深破−12无条件全清(④)→
#   触线+双源验证利空(bearTier=dual)当轮即时硬清、未触线只预警(①)，废除 reason 利空/抛售
#   子串→止损高。paper/report 双账与 live 同口径（P3），三档灰度 ""/shadow→off→on，
#   切闸前 shadow 全量观察、影子零资金行为变化。P4 探测器修复：派发判定加跌幅下限−1.5%
#   +缩量地板+上午阈值2.2+连续两轮确认；做空/超期顶「止盈」帽改结构化 SellLevel 定帽；
#   正值回撤不再渲染。§D1 护栏1：受益个股改确定性来源（源 stock_list ∪ 标题全称匹配 ∪
#   板块成分股"名称(代码)"标签），CleanBatch 输出幂等可再清洗。
# A 裁决状态机 + 影子行为 + 护栏4 端到端（engine 侧留痕/延持/清零/双源硬清单源不硬清）
go test -count=1 ./internal/signalctl/ 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/engine/ -run 'TestSellShadow' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# B P2/P3 切闸与双账：paper 影子不执行/on 走唯一出口 ApplyUnifiedSell、report @report 账本
#   处置与镜像去重、13e 旧路在 on 下关闭
go test -count=1 ./internal/engine/ -run 'TestUnifiedSellGateSigs|TestRunPaperUnifiedJudge|TestJudgePaperLedgers|TestApplyReportVerdicts|TestJudgeReportLedger' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/paper/ -run 'TestApplyUnifiedSell|TestSellProbes' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# C P4 探测器回归锁四把：缩量任何时段不命中派发 / 跌幅>-1.5 不命中 / 做空减仓级不产止盈帽 /
#   利空子串升级已废除（含否定句式）+ 两轮确认 + 正值回撤不渲染
go test -count=1 ./internal/combat_agent/ -run 'TestSellFactor|TestAssessSellSide' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/trading/ -run 'TestFromSignal' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/engine/ -run 'TestSyncLiveAdviceAlerts' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# D §D1 护栏1 确定性归因：cleaner 幂等（名称(代码)/名称|代码 混合格式全清洗）+ 咨询/归因链
go test -count=1 ./internal/data/ -run 'TestClean' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/newsagent/ 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# E 全链路 e2e（事件归因/板块传播/个股分流/行情/N形 全绿才算并轨无回归）
go test -count=1 ./internal/e2e/ -run 'TestEndToEndFullPipeline' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# F 静态锁①：reason 利空/抛售子串→止损高 的映射必须已物理删除（owner 口径①，防复活）
if grep -q 'Contains(sig.Reason, "利空")' internal/trading/advice.go; then echo "--- FAIL: advice.go 利空子串→止损映射复活（§SELLPOINT 护栏①）"; exit 1; fi
# G 静态锁②：派发判据三要素在位（跌幅下限/缩量地板/时段阈值）+ 两轮确认状态机
grep -q 'md.ChangePct <= -1.5' internal/combat_agent/sell_side.go || { echo "--- FAIL: 派发跌幅下限丢失（P4 缺陷1）"; exit 1; }
grep -q 'q.Volume >= avgV' internal/combat_agent/sell_side.go || { echo "--- FAIL: 缩量地板丢失（P4 缺陷2）"; exit 1; }
grep -q 'distributionRatioThreshold' internal/combat_agent/sell_side.go || { echo "--- FAIL: 时段阈值未接线（P4 缺陷2）"; exit 1; }
grep -q 'distConfirmed' internal/combat_agent/sell_side.go || { echo "--- FAIL: 两轮确认状态机丢失（P4 缺陷1）"; exit 1; }
# H 静态锁③：三档灰度模式与影子默认在位（默认空=shadow，绝不默认 on）
grep -q 'SellUnifiedMode' internal/config/config.go || { echo "--- FAIL: sell_unified_mode 配置项缺失"; exit 1; }
grep -q 'func (e \*Engine) sellUnifiedModeEngine' internal/engine/sell_shadow.go || { echo "--- FAIL: 统一模式读取口缺失"; exit 1; }
# I 静态锁④：paper 自动卖出唯一入口 + 主循环每轮裁决钩子
grep -q 'func (e \*Engine) ApplyUnifiedSell' internal/paper/unified_sell.go || { echo "--- FAIL: paper 统一出口缺失（P3）"; exit 1; }
grep -q 'judgePaperLedgers' internal/engine/engine.go || { echo "--- FAIL: 主循环 paper/report 每轮裁决未接线（P3）"; exit 1; }
# J 静态锁⑤（§C2 2026-09-22 负向）：深破升级判定不得残留 `&& st.Line == SellLineStopLoss` 前置——
#   残留即止盈/移动止盈锁线砸穿 −12% 绕过边界④（FIX#12 事故形态）；行为锁见
#   TestSellDeepBreachUpgrades{TakeProfit,Trail} / TestSellDeepBreachUpgradeIsOneWay 三例
if grep -q 'SellLineDeepBreach && st.Line == SellLineStopLoss' internal/signalctl/sell.go; then echo "--- FAIL: 深破升级复活 StopLoss 前置条件（§C2 回归，边界④被架空）"; exit 1; fi
go test -count=1 ./internal/signalctl/ -run 'TestSellDeepBreachUpgradesTakeProfit|TestSellDeepBreachUpgradesTrail|TestSellDeepBreachUpgradeIsOneWay' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
echo "ok - §SELLPOINT-UNIFY 专项守卫通过（行为回归 6 组 + 静态锁 10 道）"

echo "==> 28 §C1/§C1b 冻结账日期过滤 + 跨日陈旧买单无条件清扫（2026-09-22 修复批，AUDIT_FULL_UAT_20260921C CRITICAL#1）..."
# 背景（docs/FIX_PLAN_20260922.md §2.1）：LocalBuyFrozen 旧实现接收 day 参数却从不用于过滤，
# 在途冻结按全历史累计——前日网关崩溃遗留的 已报 买单僵尸行永久占用当日预算闸（闸2/闸3），
# 且读取错误被吞成 frozen=0 放水。修复三件套：
#   A 签名 (float64,error) + SQL 加 substr(created_at,1,10)=? 当日过滤（orders 两种建单格式日期前缀均在 1~10）；
#   B gate.go 冻结读取错误 fail-closed 拒绝买入，与相邻两本账同姿势；
#   C §C1b SweepOrders 最前置跨日已报/部成买单无条件降级废单——纯本地账，不受
#     Enabled/Tripped/cancel_stale_sec=-1 早退管辖，独立 30s 节流戳 lastStaleSweepAt。
go test -count=1 ./internal/store/ -run 'TestLocalBuyFrozenDayFilter|TestSweepStaleBuyOrders' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/risk/ -run 'TestGateBuyDisciplineFailClosedOnReadError' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/trading/ -run 'TestSweepOrdersStaleBuyUnconditional' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# A 静态锁①：LocalBuyFrozen 的 SQL 必须含当日日期过滤（缺了就是 C1 原缺陷复活）
grep -A6 'func (d \*DB) LocalBuyFrozen' internal/store/real_positions.go | grep -q "substr(created_at,1,10)=?" || { echo "--- FAIL: LocalBuyFrozen 日期过滤丢失（§C1 回归）"; exit 1; }
# A 静态锁②（负向）：gate 侧不得回到单值吞错形态 `frozen := g.st.LocalBuyFrozen(...)`
if grep -q 'frozen := g.st.LocalBuyFrozen' internal/risk/gate.go; then echo "--- FAIL: 冻结账读取错误被吞回单值形态（§C1 fail-open 复活）"; exit 1; fi
# C 静态锁③：跨日清扫必须接在 SweepOrders 早退之前（引用 + 独立节流戳同时在位）
grep -q 'SweepStaleBuyOrders(c.userID, beforeDay, c.MarketKey())' internal/trading/controller.go || { echo "--- FAIL: §C1b 跨日陈旧买单清扫未接线（§MR-3 起须带市场键）"; exit 1; }
grep -q 'lastStaleSweepAt' internal/trading/controller.go || { echo "--- FAIL: §C1b 独立节流戳丢失（会与 Enabled 早退共享节流而失效）"; exit 1; }
echo "ok - §C1 专项守卫通过（行为回归 3 组 + 静态锁 4 道）"

echo "==> 29 §F1/§F12 实盘费用腿入盈亏与成本 + 成交额单口径（2026-09-22 修复批，UAT_BYTE_LEVEL F1/F12）..."
# 背景（docs/FIX_PLAN_20260922.md §6.1）：实盘三处费用腿凭空蒸发——①ApplyRealFill 买入
# 成本只记成交均价（不含佣金），②/api/qmt/trades 统计重放 sellPnl 不扣 fee/stamp_tax、
# 买入摊成本不含费，③RealFills SELECT 丢列 + outFills 流水不回显费用。账面系统性偏乐观、
# 实盘/paper 两账口径分裂、settlement_diff.fee_diff 永不收敛。§F12：统计金额改取落库
# Amount（回退 Price×Qty），消除双口径。paper 侧本就是含费正解（paper.go:1480/:1735-1742），
# 本批全部向 paper 口径对齐。
go test -count=1 ./internal/store/ -run 'TestApplyRealFillBuyCostIncludesFee' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/server/ -run 'TestQMTTradesFeeInclusiveReplay' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# A 静态锁①：卖出 pnl 必须含费用减项（回归旧形态 (f.Price-ps.cost)*qty 无减项即 FAIL）
grep -q 'sellPnl := (f.Price-ps.cost)\*float64(sellQty) - (f.Fee + f.StampTax)' internal/server/qmt.go || { echo "--- FAIL: trades 重放卖出 pnl 费用减项丢失（§F1 回归）"; exit 1; }
# A 静态锁②：买入摊成本必须含佣金
grep -q 'costAmt := f.Price\*float64(f.Qty) + f.Fee' internal/server/qmt.go || { echo "--- FAIL: trades 重放买入成本佣金摊入丢失（§F1 回归）"; exit 1; }
grep -q 'f.Price\*float64(f.Qty) + f.Fee) / float64(newQty)' internal/store/real_positions.go || { echo "--- FAIL: ApplyRealFill 加仓含费摊薄丢失（§F1 回归）"; exit 1; }
# B 静态锁③：RealFills SELECT 必须带回费用腿列、outFills 必须回显
grep -q 'COALESCE(fee,0), COALESCE(stamp_tax,0)' internal/store/real_positions.go || { echo "--- FAIL: RealFills 费用腿列丢失（§F1 回归）"; exit 1; }
grep -q '"stamp_tax": f.StampTax' internal/server/qmt.go || { echo "--- FAIL: trades 流水 fee/stamp_tax 回显丢失（§F1 回归）"; exit 1; }
# C 静态锁④（§F12）：统计金额以落库 Amount 为准（回退重算），双口径不得复活
grep -q 'if f.Amount > 0 {' internal/server/qmt.go || { echo "--- FAIL: 统计金额落库单口径丢失（§F12 回归）"; exit 1; }
echo "ok - §F1/§F12 专项守卫通过（行为回归 2 组 + 静态锁 6 道）"


echo "==> 30 §H1/§H2 交割单日期归一 + 卖出状态机快照防别名（2026-09-22 修复批，代理A/E批次收尾核验）..."
# §H1：TradingDayDate 产 20060102 而网关只收 YYYY-MM-DD——normalizeSettleDay 归一 + 失败
# 计数 metrics.SettleFailed + opslog 留痕；§H2：sellProbe 原地改写并回传同一指针，旧实现
# prev 与 st 别名导致跃迁判定恒 false（VerdictHold 永不留痕），现先值拷贝 prevSnap 再探针。
go test -count=1 ./internal/trading/ -run 'TestSettleDayFormatNormalized' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/signalctl/ -run 'TestSellTransitionRecords' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
grep -q 'func normalizeSettleDay' internal/trading/settlement.go || { echo "--- FAIL: 交割单日期归一函数丢失（§H1 回归）"; exit 1; }
grep -q 'SettleFailed()' internal/trading/settlement.go || { echo "--- FAIL: 交割对账失败计数接线丢失（§H1 回归）"; exit 1; }
grep -q 'prevSnap = \*prev' internal/signalctl/sell.go || { echo "--- FAIL: 跃迁判定值快照丢失，prev/st 别名复活（§H2 回归）"; exit 1; }
echo "ok - §H1/§H2 专项守卫通过（行为回归 2 组 + 静态锁 3 道）"

echo "==> 31 §H3 全局写端点收权 admin（/api/action + news showall/reanalyze，2026-09-22 修复批）..."
# ctrlFor 恒取运营账号引擎——成员写 showall/reanalyze/ignore 都在改全局状态（ignore 写
# 运营信号簿、reanalyze 烧全局 LLM 额度），路由整体升 adminMiddleware + 前端按钮隐藏 +
# 实盘下单受理 opslog.Audit("live_order") 留痕。
go test -count=1 ./internal/server/ -run 'TestH3ActionAdminOnly|TestH3NewsShowAllAndReanalyzeAdminOnly' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
grep -q 'POST /api/action", s.adminMiddleware(s.handleFixAction)' internal/server/server.go || { echo "--- FAIL: /api/action 路由回退 authMiddleware（§H3 越权复活）"; exit 1; }
grep -q 'POST /api/news/reanalyze", s.adminMiddleware(s.handleNewsReanalyze)' internal/server/server.go || { echo "--- FAIL: reanalyze 路由回退 authMiddleware（§H3）"; exit 1; }
grep -q 'POST /api/news/showall", s.adminMiddleware(s.handleNewsShowAllToggle)' internal/server/server.go || { echo "--- FAIL: showall 路由回退 authMiddleware（§H3）"; exit 1; }
grep -q 'admin && row.can_open' web/src/pages/Signals.jsx || { echo "--- FAIL: 信号页买入/忽略按钮管理员门控丢失（§H3 前端面）"; exit 1; }
grep -q 'api.isAdmin() &&' web/src/pages/Hotspot.jsx || { echo "--- FAIL: 资讯页手动补推按钮管理员门控丢失（§H3 前端面）"; exit 1; }
echo "ok - §H3 专项守卫通过（行为回归 1 组 + 静态锁 5 道）"

echo "==> 32 §H4/§H5 实盘卖单吞错/假成功 + 做多信号新鲜度主判据（2026-09-22 修复批，代理A）..."
# §H4：网关 200+ok:false 业务拒单不再按成功返回（旧只打日志回 nil 烧掉减仓槽）；减仓幂等槽
# 「先成功后烧」，失败 opslog.OncePer 节流留痕、下一轮可重试；M8 兜底清仓同修吞错。
# §H5：新鲜度主判据改打分自身 StockScores.UpdatedAt（5s/5min 两轮都写入），e.scoresAt 降级
# 兜底基准且 5min 批量轮同样推进——批量轮信号不再被 5s 轮时钟误判过期。
go test -count=1 ./internal/engine/ -run 'TestH4TrimSlotRetryableAfterSellFailure|TestH5BullFreshnessFromOwnTimestamp|TestH5BullFreshnessFallbackToGlobalClock|TestH5StaleSignalAgeOutDespiteFreshGlobalClock' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
if grep -q '_ = e.sellRealPosition' internal/engine/scoring_loop.go; then echo "--- FAIL: 卖单错误回退 _ = 吞错形态（§H4 回归）"; exit 1; fi
grep -q 'realTrimDone\[p.TsCode\] = trimDay' internal/engine/scoring_loop.go || { echo "--- FAIL: 减仓槽「成功后烧」回写丢失（§H4 回归）"; exit 1; }
grep -q 'sell %s rejected' internal/engine/scoring_loop.go || { echo "--- FAIL: 网关业务拒单错误上抛丢失（§H4 假成功复活）"; exit 1; }
grep -q 'at := sc.UpdatedAt' internal/engine/sell_shadow.go || { echo "--- FAIL: 新鲜度主判据回退全局时钟（§H5 回归）"; exit 1; }
grep -q 'e.scoresAt = time.Now()' internal/engine/engine.go || { echo "--- FAIL: 5min 批量轮未推进兜底打分时钟（§H5 回归）"; exit 1; }
echo "ok - §H4/§H5 专项守卫通过（行为回归 4 例 + 静态锁 5 道）"

echo "==> 33 §H6/§H7 SSE 续传通道保全 + auth:expired 陈旧闭包（2026-09-22 修复批，代理B）..."
# §H6：onerror 不再手动 close+新建——CONNECTING 态留给浏览器原生重连（唯一携带
# Last-Event-ID 的通道，服务端补发环只认它）；仅 CLOSED 才换票重建（去重定时器），
# disconnectSSE 同步清定时器防登出后复活。§H7：once-registered 监听器经 ref 转发壳
# 读最新 loggedIn/闭包，首帧 false 冻结闭包不再吞掉登出提示。
( cd web && npm test -- sse_h6_reconnect auth_expired_h7 )
grep -q 'sseReconnectTimer' web/src/api/index.js || { echo "--- FAIL: CLOSED 兜底重建定时器丢失（§H6 回归）"; exit 1; }
grep -q 'readyState !== 2' web/src/api/index.js || { echo "--- FAIL: 原生重连让位判定丢失，onerror 回到无条件 close+new（§H6 回归）"; exit 1; }
grep -q 'loggedInRef.current' web/src/App.jsx || { echo "--- FAIL: auth:expired 回到渲染态闭包读取（§H7 回归）"; exit 1; }
grep -q 'authExpiredHandler.current' web/src/App.jsx || { echo "--- FAIL: 转发壳未读最新闭包（§H7 回归）"; exit 1; }
echo "ok - §H6/§H7 专项守卫通过（vitest 2 文件 + 静态锁 4 道）"

echo "==> 34 §H9/§M16 outbox 批量错位 + 网关 inflight 收割（2026-09-22 修复批，代理C）..."
# §H9：outbox pump 回写按条目唯一 id 定位（旧数组下标在同批前序出队后整体移位，身份校验
# 必失败→整批每秒重投）；出队行定位改 id 线性查找。注意：修复落位 internal/notify/outbox.go
# （FIX_PLAN 原文写 internal/trading 系笔误）。§M16：dispatch inflight 超时收割
# （inflight_at 龄判据 + 启动收割 + 60s 巡检线程，判废不重排防重复下单）；HTTP 桥补
# diag kind 处理 + unknown kind 负回执，派发行不再永挂。
go test -count=1 ./internal/notify/ -run 'TestOutboxBatchPumpSingleDelivery|TestOutboxBatchPumpMixedOutcome' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
py_tests qmt_gateway/tests 'dispatch_reap_stale_inflight or unknown_kind or diag_dispatch' 2>&1 | grep -E "passed|failed|error|Ran [0-9]+ test|OK"
if grep -q 'jobs = append(jobs, job{idx: i' internal/notify/outbox.go; then echo "--- FAIL: pump 回退数组下标定位（§H9 风暴复活）"; exit 1; fi
grep -q 'id: o.nextID' internal/notify/outbox.go || { echo "--- FAIL: 出队条目唯一 ID 分配丢失（§H9 回归）"; exit 1; }
grep -q 'inflight_at' qmt_gateway/store.py || { echo "--- FAIL: inflight 取单时刻落列丢失（§M16 回归）"; exit 1; }
grep -q '_reap_dispatch_inflight' qmt_gateway/gateway.py || { echo "--- FAIL: 网关 inflight 收割线程未接线（§M16 回归）"; exit 1; }
grep -q '_execute_diag' qmt_gateway/qmt_bridge.py || { echo "--- FAIL: 桥 diag kind 处理丢失（§M16 永挂复活）"; exit 1; }
echo "ok - §H9/§M16 专项守卫通过（Go 2 例 + py 3 组 + 静态锁 5 道）"

echo "==> 35 §F2/§M4/§F5/§M5/§F3 回报接入段五修（2026-09-22 修复批，代理D）..."
# §F2 持仓快照 ts_code 整批字段校验（ErrInvalidPositionReport→400→网关死信）；
# §M4 委托状态腿落库失败回 500 让 outbox 重推（旧吞错仍回 ok 永久丢回报）；
# §F5 缺 order_id 显式拒收留痕、仅缺 signal_id 用 ext:<order_id> 占位键落最小状态行
# （旧双键齐全才进块，手工单状态永久隐身）；§M5 strategy/signal_id COALESCE 防对账洗空；
# §F3 muxWithJSONErrors 统一 404/405 JSON 信封（本工具链 ServeMux 无 NotFound 字段，
# 用 mux.Handler 预判 + 3xx 放行）；golden 契约 report_fields.json ↔ qmtReportEvent 反射锁。
go test -count=1 ./internal/server/ -run 'TestReportContractGolden|TestHandleQMTReportPositionsInvalidTsCode400|TestHandleQMTReportOrderStoreError500|TestHandleQMTReportOrderMissingKeys' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/store/ -run 'TestReconcileRejectsInvalidTsCode' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
py_tests qmt_gateway/tests/test_channel_position_fields.py
grep -q 'ErrInvalidPositionReport' internal/store/real_positions.go || { echo "--- FAIL: 持仓快照字段校验哨兵丢失（§F2 回归）"; exit 1; }
grep -q 'errors.Is(err, store.ErrInvalidPositionReport)' internal/server/qmt.go || { echo "--- FAIL: 400 映射丢失，校验失败回退 500 无限重推（§F2 回归）"; exit 1; }
if grep -q 'if ev.OrderID != "" && ev.SignalID != "" {' internal/server/qmt.go; then echo "--- FAIL: 双键齐全才进块复活，缺键回报静默丢弃（§F5 回归）"; exit 1; fi
grep -q '"ext:" + ev.OrderID' internal/server/qmt.go || { echo "--- FAIL: 占位信号键丢失（§F5 回归）"; exit 1; }
grep -q 'COALESCE(NULLIF(excluded.strategy' internal/store/real_positions.go || { echo "--- FAIL: 对账洗空保护丢失（§M5 回归）"; exit 1; }
grep -q 'muxWithJSONErrors' internal/server/server.go || { echo "--- FAIL: 404/405 JSON 信封出口丢失（§F3 回归）"; exit 1; }
grep -q 'can_use_qty' qmt_gateway/broker.py || { echo "--- FAIL: xt 直连通道 T+1 可卖量字段丢失（§M5 通道字段对齐回归）"; exit 1; }
echo "ok - §F2 回报接入段专项守卫通过（Go 5 例 + py 1 文件 + 静态锁 7 道）"

echo "==> 36 §M14/§M15 队列同因连败熔断 + 夜链半截自愈（2026-09-22 修复批，代理E）..."
# §M14：RequeueFailedTask 以 error 前 200 字为指纹，同因连败 SameReasonFailLimit=10 挂起
# failed_needs_attention（不再重排，人工 RequeueTask 复活清零计数）；异因不设上限保留旧
# 设计；worker 三处失败收口统一 notifySameCauseBreaker（留痕+清冷却+state+高优告警一次，
# researchd 经 SetAlertFunc 接 PushGateway）。§M15：夜链改「计划序位即 chain_seq +
# ChainTaskSeqs 占用补缺」，单环入队失败不中断整链、后续 tick 自动补投，state 记
# chain_issued/chain_total，缺额 opslog.OncePer(30min) 留痕。
go test -count=1 ./internal/scheduler/ -run 'TestSameCauseBreakerParksTask|TestDifferentCauseFailuresNeverBreaker|TestNightlyChainHalfEnqueueBackfill|TestNightlyChainMissingSeqBackfilledAcrossRestart' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/store/ -run 'TestTaskSameCauseBreaker' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
grep -q 'SameReasonFailLimit = 10' internal/store/research_tasks.go || { echo "--- FAIL: 同因熔断阈值常量丢失（§M14 回归）"; exit 1; }
if grep -q 'researchTaskRetryCap' internal/store/research_tasks.go; then echo "--- FAIL: 旧异因混计上限复活（§M14 设计为同因取代）"; exit 1; fi
grep -q 'failed_needs_attention' internal/store/research_tasks.go || { echo "--- FAIL: 挂起终态缺失（§M14 回归）"; exit 1; }
grep -q 'ChainTaskSeqs' internal/scheduler/worker.go || { echo "--- FAIL: 夜链序位补缺丢失，半截链复活（§M15 回归）"; exit 1; }
grep -q 'SetAlertFunc' cmd/researchd/main.go || { echo "--- FAIL: researchd 告警通道未接线（§M14 静默死亡复活）"; exit 1; }
echo "ok - §M14/§M15 专项守卫通过（行为回归 5 例 + 静态锁 5 道）"

echo "==> 37 §H8/§F6 运维探针同源 + UAT 构建指纹（2026-09-22 修复批，代理F）..."
# §H8：watchdog/daily_ops_check/register 三方探针 URL 收敛 service_probe_config.ps1 单源
# （quant→:8081/setup 无鉴权、web→Caddy :8080、researchd→scheduler_status.json mtime≤5min
# 文件心跳）；旧三连错（:8080/api/status 鉴权 401 误熔、虚构 :9091/health、探引擎根路径）
# 静态锁死不得复活；deploy_guangzhou.sh 同步清单必须带上新文件（教训=quote_feed 漏列）。
# §F6：uat_bootstrap 构建注入与生产同款 -ldflags buildCommit，A7 指纹用例口径对齐。
test -f deploy/qmt-win/service_probe_config.ps1 || { echo "--- FAIL: 探针同源配置文件缺失（§H8）"; exit 1; }
grep -q 'service_probe_config.ps1' deploy/qmt-win/all_service_watchdog.ps1 || { echo "--- FAIL: watchdog 未 dot-source 同源配置（§H8 回归）"; exit 1; }
grep -q 'service_probe_config.ps1' scripts/daily_ops_check.ps1 || { echo "--- FAIL: daily_ops_check 未 dot-source 同源配置（§H8 回归）"; exit 1; }
grep -q 'service_probe_config.ps1' deploy/qmt-win/register_engine_services.ps1 || { echo "--- FAIL: register 未接同源配置（§H8 回归）"; exit 1; }
if grep -qE '127\.0\.0\.1:9091|127\.0\.0\.1:8080/api/status' deploy/qmt-win/all_service_watchdog.ps1 scripts/daily_ops_check.ps1; then echo "--- FAIL: 旧误熔探针形态复活（§H8：:9091 虚构口/鉴权口探活）"; exit 1; fi
grep -q 'service_probe_config.ps1' scripts/deploy_guangzhou.sh || { echo "--- FAIL: 探针配置未入部署同步清单（§H8/§ENH-5 教训）"; exit 1; }
grep -q 'main.buildCommit' scripts/uat_bootstrap.sh || { echo "--- FAIL: UAT 构建指纹注入丢失（§F6 回归）"; exit 1; }
echo "ok - §H8/§F6 专项守卫通过（静态锁 8 道）"

echo "==> 38 §M1 quote_source 契约单源化 golden 双向锁（2026-09-22 修复批二波，代理G/K）..."
# M1：行情源名散落六处（Go 枚举 / 前端下拉 / 网关日志文本 / 桥策略 / E2E 断言 / mock 注入）
# 过去各写各的，源名一改即静默失配。现收敛单源 qmt_gateway/contract/quote_sources.json：
# Go 侧 AST 双向锁（golden≡AllQuoteSources()≡各站点字面量）、E2E 加载器与 uat_bootstrap
# 注入值同读 golden（§3.1-1），前端 dropdown 亦由 enum 派生。反例=任何一处绕过 golden。
go test -count=1 ./internal/data/ -run 'TestQuoteSourcesGolden|TestQuoteSourceSitesMatchEnum' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
test -f qmt_gateway/contract/quote_sources.json || { echo "--- FAIL: golden 源清单文件缺失（M1 回到各写各的）"; exit 1; }
grep -q 'QMT-L1' qmt_gateway/contract/quote_sources.json || { echo "--- FAIL: golden 内容漂移（缺 QMT-L1）"; exit 1; }
grep -q '同花顺（新）' qmt_gateway/contract/quote_sources.json || { echo "--- FAIL: golden 内容漂移（缺 同花顺（新））"; exit 1; }
grep -q 'quote_sources.json' web/e2e/quote_sources.mjs || { echo "--- FAIL: E2E golden 加载器丢失（M1 盲区复活）"; exit 1; }
grep -q 'quote_sources.json' scripts/uat_bootstrap.sh || { echo "--- FAIL: uat_bootstrap 注入未读 golden（§3.1-1 复活）"; exit 1; }
grep -q 'E2E_QUOTE_SOURCE' scripts/uat_bootstrap.sh || { echo "--- FAIL: E2E_QUOTE_SOURCE 注入通道丢失（M1）"; exit 1; }
echo "ok - §M1 专项守卫通过（行为锁 2 例 + 静态锁 6 道）"

echo "==> 39 §M2/§M3 降级报成功族：last-known-good 保底 + 新闻源真实探测（2026-09-22 修复批二波，代理G）..."
# M2：行情整轮全源失败旧实现清空快照并把 lastOK 刷成"刚成功"——面板显示旧数据却报新鲜。
# 现保留上一份 last-known-good、不推进 lastOK、60s 节流告警；从未拿到过才回空。
# M3：/api/news/source-health 旧实现写死三源全 ok（探测函数根本没调上游）——收编为
# NewsSourceHealth 真实探测结构（unknown≠ok），键名 cainanshe 笔误更正 cailanshe，前端仪表盘对齐。
go test -count=1 ./internal/data/ -run 'TestGetIndexDataLastKnownGoodFallback|TestGetSectorsStaleCacheFallback|TestGetSectorStocksStaleCacheFallback' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/data/ -run 'TestNewsSourceHealthUnknownBeforeAnyProbe|TestNewsSourceHealthDrivenByRealFetches' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
grep -q 'last-known-good' internal/data/fetcher.go || { echo "--- FAIL: M2 全源失败保留上一份语义丢失"; exit 1; }
grep -q 'NewsSourceHealth' internal/data/source.go || { echo "--- FAIL: M3 真实探测结构丢失"; exit 1; }
grep -q '"cailanshe"' internal/data/market.go || { echo "--- FAIL: 财联社正名键常量丢失（M3 契约键）"; exit 1; }
# 负锁只扫非注释、非测试行：修复说明注释与 M3 测试自身的"永绝后迹"断言合法含该拼写。
if grep -rn 'cainanshe' internal web/src cmd 2>/dev/null | grep -v '//' | grep -v '_test\.go' | grep -v Binary >/dev/null; then echo "--- FAIL: 笔误键 cainanshe 复活（M3 已正名 cailanshe）"; exit 1; fi
grep -q 'cailanshe' web/src/pages/Dashboard.jsx || { echo "--- FAIL: 仪表盘新闻源未对齐 M3 键名口径"; exit 1; }
echo "ok - §M2/M3 专项守卫通过（行为锁 5 例 + 静态锁 4 道）"

echo "==> 40 §M6/§TZ/§REJECT 数据管道 py 批：pctChg 大小写 / 时区单源 / 双空回报拒收（2026-09-22 修复批二波，代理H）..."
# M6：dataload_keepalive 读 csv 键 "pctchg"，服务端契约键是驼峰 "pctChg"（DictReader 大小写
#   敏感）→ change/pct_chg 恒 0 且"写入成功"；现读 0 值预校验，整轮不健康则 ok=False、rc≠0。
# TZ：全仓 .py 手拼 "+08:00" 的 time.strftime 假时区收编为 store._now_cn()/桥 _now_cn_str()
#   单源（静态锁见下），时间格式契约 golden 化 qmt_gateway/contract/time_formats.json。
# REJECT：/dispatch/result 的 trade 回报 trade_id 与 order_id 皆空 → 400 显式拒收不落垃圾行；
#   outbox seq 先落空串、拿 rowid 后回填，杜绝"回报存在但无法归属派发"。
py_tests qmt_gateway/tests/test_time_formats.py '' 2>&1 | grep -E "passed|failed|error|Ran [0-9]+ test|OK"
py_tests qmt_gateway/tests/test_trade_identity_reject.py '' 2>&1 | grep -E "passed|failed|error|Ran [0-9]+ test|OK"
py_tests qmt_gateway/tests/test_dataload_keepalive.py '' 2>&1 | grep -E "passed|failed|error|Ran [0-9]+ test|OK"
grep -q 'INDEX_PCT_COL = "pctChg"' scripts/dataload_keepalive.py || { echo "--- FAIL: M6 pctChg 契约键修正回退（大小写错键复活）"; exit 1; }
test -f qmt_gateway/contract/time_formats.json || { echo "--- FAIL: 时间格式 golden 缺失（§TZ/§3.1-5）"; exit 1; }
# 负锁与 H 的 test_time_formats 静态锁同 regex 口径：只拦「time.strftime( 裸调用喂 +08:00」，
# 放行收敛点自身（datetime.now(CN_TZ).strftime(...)，钟面与标签恒一致）。
if grep -rn --include='*.py' -E 'time\.strftime\([^)\n]*\+08:00' qmt_gateway scripts 2>/dev/null | grep -v '/tests/' >/dev/null; then
	echo "--- FAIL: .py 假时区字面量复活（§TZ：+08:00 只许出自 _now_cn/_now_cn_str 单源）"; exit 1; fi
grep -q '§REJECT' qmt_gateway/handler.py || { echo "--- FAIL: 双空回报拒收逻辑移除（M-REJECT 静默垃圾行复活）"; exit 1; }
grep -q '§REJECT' qmt_gateway/gateway.py || { echo "--- FAIL: /dispatch/result 侧 400 拒收移除（M-REJECT）"; exit 1; }
echo "ok - §M6/§TZ/§REJECT 专项守卫通过（py 行为锁 3 组 + 静态锁 5 道）"

echo "==> 41 §M8 推送三通道内聚（禁双发铁律）+ EXPVAR 收权（2026-09-22 修复批二波，代理L/J）..."
# M8：JPush 网关通道过去挂在业务调用方 Push 之后再补一刀 PushGateway——两处调用点两处漏，
# 且 researchd 侧干脆没接（推送分裂）。现 Push 内聚三通道（WS/webhook/网关），网关扇出在
# 释放 RLock 后执行（RWMutex 重入死锁防线），级别闸 gatewayMinLevel 默认中级别。
# 禁双发铁律：业务侧（internal/engine、cmd/quant）出现 Push 后再调 .PushGateway( 即回归。
# EXPVAR：/debug/vars 与 /metrics 旧实现对成员全开（持仓/资金暴露面）——现 admin-only。
go test -count=1 ./internal/notify/ -run 'TestM8' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/server/ -run 'TestExpvarMetricsAdminOnly|TestDebugVarsNotMounted' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
if grep -rn '\.PushGateway(' internal/engine cmd/quant --include='*.go' 2>/dev/null | grep -v _test.go | grep -v 'NewJPushGateway\|NewNtfyGateway\|NewWebhookGateway' >/dev/null; then
	echo "--- FAIL: 禁双发铁律回归——业务调用方在 Push 之外又直调 PushGateway（M8 双发/分裂复活）"; exit 1; fi
grep -q 'gatewayMinLevel' internal/notify/notify.go || { echo "--- FAIL: 网关级别闸丢失（M8）"; exit 1; }
echo "ok - §M8/EXPVAR 专项守卫通过（行为锁 7 例 + 静态锁 2 道）"

echo "==> 42 §M9/§M10/§M11 轮首快照 + 卖出锚点落盘 + 买入队列防丢 + InsertRows 面校验（2026-09-22 修复批二波，代理J）..."
# M9：sell_unified_mode 一轮内被多处各自重读——轮中翻转会前半轮影子后半轮执行。
#   现轮首快照一次、以参数贯传到全部同轮消费方（param-plumbed，禁中途重读）。
# M10：模拟盘移动止损高点锚过去每轮自抬、重启即清零（止损位漂移）。现原子落盘
#   <acctDir>/paper_sell_anchors.json，重启回读合并取较高者（锚单调不降，防重启倒退）。
# M11：内存买队列停机即丢在途单——StopBuyDispatcher 排空落盘、StartBuyDispatcher 回读
#   并按 SignalID 去重；InsertRows 表面对象（列名裸标识符+白名单/PRAGMA 兜底）收口注入面。
go test -count=1 ./internal/engine/ -run 'TestSellRoundModeShadowToOnFlip|TestSellRoundModeOnToShadowFlip|TestSellRoundModeParamAuthoritative' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/engine/ -run 'TestPaperSellAnchorSurvivesRestart|TestPaperSellAnchorAccountIsolation' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/engine/ -run 'TestBuyQueueShutdownDrainAndRestore|TestBuyQueueRestoreDedupUnit' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/store/ -run 'TestInsertRowsWhitelist' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
grep -q 'paper_sell_anchors.json' internal/engine/sell_anchor.go || { echo "--- FAIL: M10 锚点文件持久化移除（重启清零复活）"; exit 1; }
grep -q '轮首快照' internal/engine/engine.go || { echo "--- FAIL: M9 轮首快照语义移除（轮中翻转复活）"; exit 1; }
grep -q 'validateInsertSurface' internal/store/store.go || { echo "--- FAIL: M11 InsertRows 表面校验移除"; exit 1; }
echo "ok - §M9/M10/M11 专项守卫通过（行为锁 7 例 + 静态锁 3 道）"

echo "==> 43 §M7 部署清单收编：quant-web 注册 / 网关配置生成 / 静态锁总闸（2026-09-22 修复批二波，代理L）..."
# M7a/b/c：verify_deploy 要求的 quant-web(Caddy) 站点与网关 config.xt.json 过去全是手工
# 一次性操作（部署面与校验面各说各话）——现 register_web_service.ps1 / ensure_gateway_config.ps1
# 幂等收编进 deploy_guangzhou.sh；29 道部署口径静态锁收敛在 check_deploy_static_locks.sh，
# 本段直接执行该总闸（端口单源/清单缺项/GUANGZHOU_CADDY 手工步骤残留等一次全验）。
test -f deploy/qmt-win/register_web_service.ps1 || { echo "--- FAIL: web 站点注册脚本缺失（M7b 手工步骤复活）"; exit 1; }
test -f deploy/qmt-win/ensure_gateway_config.ps1 || { echo "--- FAIL: 网关配置生成脚本缺失（M7c 秒起秒死复活）"; exit 1; }
grep -q 'register_web_service.ps1' scripts/deploy_guangzhou.sh || { echo "--- FAIL: web 注册未接入部署链（M7b）"; exit 1; }
grep -q 'ensure_gateway_config.ps1' scripts/deploy_guangzhou.sh || { echo "--- FAIL: 网关配置生成未接入部署链（M7c）"; exit 1; }
bash ./scripts/check_deploy_static_locks.sh || { echo "--- FAIL: 部署静态锁总闸未过（M7 口径漂移）"; exit 1; }
echo "ok - §M7 专项守卫通过（静态锁 4 道 + 部署口径总闸）"

echo "==> 44 §M12/§M13 移动壳凭据面 + 前端权限一致性 + researchd 主链冒烟（2026-09-22 修复批二波，代理K/L/J）..."
# M13：成员账号进 Quant/Paper 页——①403 后轮询定时器照跑（opslog 灌噪声）；②判 403 用
#   e.message.indexOf('无权限')，permMiddleware 回英文必漏判。现 isForbidden(状态码) 单口径
#   + 403 即停全部轮询；vitest 反例锁四把 + 源码静态锁防 indexOf('无权限') 复活。
# M12：Android 壳 WebView 无条件 setWebContentsDebuggingEnabled(true)（release 也可被
#   chrome://inspect 摘 token）→ BuildConfig.DEBUG 门控；注入 JS 字符串全走 jsQuote 转义。
# 冒烟：researchd main 装配链（SetAlertFunc→PushGateway 等接线）过去只有编译期保证——
#   现 TestSmokeResearchdMainChain 行为级冒烟（M14 告警接线不再可能"编译过=接线对"）。
go test -count=1 ./internal/scheduler/ -run 'TestSmokeResearchdMainChain' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
( cd web && npm test -- m13_forbidden_poll ) 2>&1 | grep -E 'Test Files|passed|failed'
# 负锁过滤注释行（:197/:267/:433 的"旧写法不得复活"说明合法含该字面量），只拦真代码。
if grep -nE "indexOf\('无权限'\)|includes\('无权限'\)" web/src/pages/Quant.jsx web/src/pages/Paper.jsx | grep -vE ':[0-9]+:[[:space:]]*//' >/dev/null; then
	echo "--- FAIL: M13 中文文案判 403 复活（permMiddleware 英文形态漏判）"; exit 1; fi
grep -q 'isForbidden' web/src/pages/Quant.jsx || { echo "--- FAIL: Quant 页状态码判定丢失（M13）"; exit 1; }
grep -q 'isForbidden' web/src/pages/Paper.jsx || { echo "--- FAIL: Paper 页状态码判定丢失（M13）"; exit 1; }
grep -q 'BuildConfig.DEBUG' mobile/app/src/main/java/com/liangzai/quant/MainActivity.kt || { echo "--- FAIL: M12 调试门控回退为无条件开启"; exit 1; }
grep -q 'jsQuote' mobile/app/src/main/java/com/liangzai/quant/MainActivity.kt || { echo "--- FAIL: M12 注入转义通道丢失"; exit 1; }
echo "ok - §M12/M13/SMOKE 专项守卫通过（行为锁 2 组 + 静态锁 5 道）"

echo "==> 45 §XCHECK 价格复核闸接线（CrossCheckPrice 收编，2026-09-22 C批，代理A）..."
# §XCHECK：下单守卫第 12 道闸 price_cross_check——委托参考价 vs 独立复核源价
# （DataCoordinator.CrossCheckPrice，新浪→腾讯→东财多源链）偏离超阈值即命中；
# 默认关（cross_check_pct=0）+ 默认影子（命中仅 risk_gates 留痕放行），零配置零行为变化。
# 死代码不得复活：CrossCheckPrice 必须保有生产消费者（registry 接线）。
go test -count=1 ./internal/risk/ -run 'TestGatePriceCrossCheck' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
grep -q 'price_cross_check' internal/risk/gate.go || { echo "--- FAIL: 价格复核闸从闸清单消失（§XCHECK）"; exit 1; }
grep -q 'checkPriceCross' internal/risk/gate.go || { echo "--- FAIL: 复核闸判定函数丢失（§XCHECK）"; exit 1; }
grep -q 'SetCrossPriceSource' internal/engine/registry.go || { echo "--- FAIL: registry 装配注入丢失（§XCHECK 接线断裂，复核源永不生效）"; exit 1; }
grep -q 'cross_check_pct' internal/config/config.go || { echo "--- FAIL: 复核闸配置键丢失（§XCHECK）"; exit 1; }
if ! grep -rn 'CrossCheckPrice' internal cmd --include='*.go' 2>/dev/null | grep -v _test.go | grep -v 'internal/data/source.go' | grep -q .; then
	echo "--- FAIL: CrossCheckPrice 又成死代码（§XCHECK 消费链断裂，§LOW(a) 死代码形态复活）"; exit 1; fi
echo "ok - §XCHECK 专项守卫通过（行为锁 1 组 6 态 + 静态锁 5 道）"

echo "==> 46 §NATIVEAUTH 登录 token 迁原生加密存储（2026-09-22 C批，代理A2/A3）..."
# token 出 WebView localStorage（明文域文件，root/adb backup 可读）→ AndroidAuth 桥
# + EncryptedSharedPreferences（AndroidKeyStore 托管主密钥）；纯浏览器/旧 APK 无桥自然回落。
( cd web && npm test -- native_auth ) 2>&1 | grep -E 'Test Files|passed|failed'
test -f mobile/app/src/main/java/com/liangzai/quant/SecureAuthStore.kt || { echo "--- FAIL: 原生加密存储实现缺失（§NATIVEAUTH）"; exit 1; }
grep -q '"AndroidAuth"' mobile/app/src/main/java/com/liangzai/quant/MainActivity.kt || { echo "--- FAIL: AndroidAuth 桥未注册（§NATIVEAUTH 前端迁而无门）"; exit 1; }
grep -q 'security-crypto' mobile/app/build.gradle.kts || { echo "--- FAIL: EncryptedSharedPreferences 依赖丢失（§NATIVEAUTH）"; exit 1; }
grep -q 'nativeAuthBridge' web/src/api/index.js || { echo "--- FAIL: 前端桥探测丢失（§NATIVEAUTH）"; exit 1; }
# 负锁：业务代码不得再直读 liangzai_token 字面量（api/index.js 存储层单点之外、滤注释行与测试）。
if grep -rn "liangzai_token" web/src --include='*.js' --include='*.jsx' 2>/dev/null | grep -v __tests__ | grep -v 'src/api/index.js' | grep -vE ':[0-9]+:[[:space:]]*//' | grep -q .; then
	echo "--- FAIL: localStorage token 直读复活（§NATIVEAUTH 必须收敛在 api 存储层）"; exit 1; fi
echo "ok - §NATIVEAUTH 专项守卫通过（行为锁 1 组 + 静态锁 4 道 + 直读负锁）"

echo "==> 47 §ROOTQMT 根级死键防回潮 + §APPVER 强制更新通道（2026-09-22 C批，代理A4/A5）..."
# §ROOTQMT：config.json 根级 qmt 死键（旧生成器层级错误产物，§M7a）——清理脚本收编进
# 部署链 [3a] 常态执行，防回潮；引擎 Save 只写 {rules,d1}，清理无回写竞态。
test -f deploy/qmt-win/clean_root_qmt.ps1 || { echo "--- FAIL: 根级 qmt 清理脚本缺失（§ROOTQMT 手工告警复活）"; exit 1; }
grep -q 'clean_root_qmt' scripts/deploy_guangzhou.sh || { echo "--- FAIL: 清理未接入部署链（§ROOTQMT 防回潮失效）"; exit 1; }
# §APPVER：公开版本端点（登录前检查必须免鉴权）+ Caddy /dl 分发块 + v2 原生更新闸。
go test -count=1 ./internal/server/ -run 'TestAppVersionPublicEndpoint' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
grep -q 'GET /api/app/version' internal/server/server.go || { echo "--- FAIL: 版本端点注册丢失（§APPVER）"; exit 1; }
# 负锁：该端点注册行若被 authMiddleware 包裹=登录前更新检查死锁（v1 登不进即永远收不到更新单）。
if grep -n 'GET /api/app/version' internal/server/server.go | grep -q 'authMiddleware'; then
	echo "--- FAIL: /api/app/version 被套鉴权（§APPVER 登录前检查死锁复活）"; exit 1; fi
grep -q 'handle /dl/\*' deploy/caddy/guangzhou.conf || { echo "--- FAIL: Caddy APK 分发块丢失（§APPVER 下载 404）"; exit 1; }
grep -q 'quant-latest.apk' scripts/deploy_guangzhou.sh || { echo "--- FAIL: APK 分发同步步丢失（§APPVER [3c]）"; exit 1; }
grep -q 'UpdateGate' mobile/app/src/main/java/com/liangzai/quant/MainActivity.kt || { echo "--- FAIL: 原生强制更新闸未接线（§APPVER）"; exit 1; }
grep -qE 'versionCode = ([2-9]|[1-9][0-9])' mobile/app/build.gradle.kts || { echo "--- FAIL: APK 版本仍停在无更新通道的 1（§APPVER 跃迁回退）"; exit 1; }
echo "ok - §ROOTQMT/§APPVER 专项守卫通过（行为锁 1 组 + 静态锁 7 道 + 鉴权负锁）"

echo "==> 48 §UPDLINK SSE 双关死锁根因修复 + 上行自监控（2026-09-22 PM批 H-4，P0）..."
# 根因：同一客户端 channel 被 §A3 evict 与 handleFixSSE defer 两条路径双关 → 持 b.mu 时 panic →
# 广播锁永久泄漏 → POST /api/qmt/report 必经的 BroadcastTo 全线挂死（生产实录 1h45m 静默）。
# 行为锁：幂等注销/跨分组回退/并发双关不泄漏锁/有界广播放弃/回报端点存活。
go test -count=1 ./internal/server/ -run 'TestUnsubscribeFor|TestEvictAndHandlerConcurrent|TestBroadcastToWithinGivesUp|TestQMTReportSurvivesStuckSSELock' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# 静态锁①：注销必须走"注册表命中才关"的幂等路径（无条件 close(ch) 形态即 H-4 本体，不得复活）。
# 取 UnsubscribeFor 函数体做范围断言，避免整文件计数被文档注释里的 close(ch) 字样干扰。
UNSIB=$(awk '/^func \(b \*SSEBroker\) UnsubscribeFor/{f=1} f{print} f&&/^}$/{exit}' internal/server/sse.go)
[ -n "$UNSIB" ] || { echo "--- FAIL: 找不到 UnsubscribeFor 函数体（§UPDLINK 静态锁失效）"; exit 1; }
echo "$UNSIB" | grep -q 'unregisterLocked' || { echo "--- FAIL: 幂等注销判定丢失（§UPDLINK 双关复活）"; exit 1; }
[ "$(echo "$UNSIB" | grep -c 'close(ch)')" = "1" ] || { echo "--- FAIL: UnsubscribeFor 内 close(ch) 不是唯一一处（§UPDLINK）"; exit 1; }
grep -q 'func (b \*SSEBroker) unregisterLocked' internal/server/sse.go || { echo "--- FAIL: 幂等注销实现丢失（§UPDLINK 双关复活）"; exit 1; }
# 静态锁②：UnsubscribeFor 必须 defer 解锁——锁内 panic 跳过 Unlock 是 H-4 的第二根因。
echo "$UNSIB" | grep -q 'defer b\.mu\.Unlock()' || { echo "--- FAIL: UnsubscribeFor 不再 defer 解锁（持锁 panic 会再次泄漏广播锁，§UPDLINK）"; exit 1; }
# 静态锁③：上行回报入口必须用有界广播（无界 BroadcastTo 在同族锁事故里会再次把资金账本陪葬）。
grep -q 'BroadcastToWithin' internal/server/qmt.go || { echo "--- FAIL: /api/qmt/report 退回无界广播（§UPDLINK 兜底失效）"; exit 1; }
# 静态锁④：告警规则必须有真实数据源——audit N-1 的形态就是"有规则、无 SetGauge"。
grep -q 'SetGauge("quote_staleness_sec"' internal/engine/scoring_loop.go || { echo "--- FAIL: quote_staleness_sec 又成死规则（§UPDLINK/audit N-1）"; exit 1; }
grep -q 'SetGauge("uplink_staleness_sec"' internal/engine/scoring_loop.go || { echo "--- FAIL: 上行新鲜度量规断供（H-4 那 1h45m 将再次无人知晓）"; exit 1; }
grep -q 'refreshStalenessGauges()' internal/engine/scoring_loop.go || { echo "--- FAIL: 量规喂养未接入 scoreCycle（告警永不自愈）"; exit 1; }
go test -count=1 ./internal/engine/ -run 'TestRefreshStalenessGauges|TestUplinkStaleRuleRegistered' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
echo "ok - §UPDLINK 专项守卫通过（行为锁 2 组 + 静态锁 6 道）"

echo "==> 49 §SIDEGATE 下单方向三层白名单（2026-09-22 PM批 M-1/N-4，资金安全）..."
# 缺陷原文：网关 /order 只判 `side=="卖出"`，其余任意串按买入整手校验后被 broker/桥的
# 三元式（`STOCK_BUY if side=="买入" else STOCK_SELL`）下成**卖单**；Go 侧 handleExecuteAction
# 同样只把空串缺省成买入；risk.Gate 十余道闸按 Side 精确匹配，非法串让 T+1 卖出限制、
# 涨停拒买、跌停拒卖三道方向闸同时静默跳过。方向是全部方向性守卫的判定前提，前提不可信
# 时唯一安全姿势是拒单——故 HTTP 入口 400、通道层 fail-close、风控闸入口 side_unknown 三层各拦一次。
go test -count=1 ./internal/risk/ -run 'TestGateUnknownSide' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/server/ -run 'TestExecuteRejectsNonCanonicalSide|TestExecuteSellSideStillAccepted' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/trading/ -run 'TestPlaceOrderUnknownSideNeverReachesExecutor' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
py_tests qmt_gateway/tests/test_order_gates.py 'side' 2>&1 | grep -E "passed|failed|error|Ran [0-9]+ test|OK"
grep -q 'ORDER_SIDES = ("买入", "卖出")' qmt_gateway/gateway.py || { echo "--- FAIL: 网关方向白名单常量丢失（§SIDEGATE-PY）"; exit 1; }
grep -q 'def is_valid_side' qmt_gateway/broker.py || { echo "--- FAIL: 通道层方向校验丢失（§SIDEGATE-PY 第二道闸）"; exit 1; }
grep -q 'side != trading.SideBuy && side != trading.SideSell' internal/server/qmt.go || { echo "--- FAIL: 手动单 HTTP 入口白名单丢失（§SIDEGATE-GO）"; exit 1; }
grep -q 'o.Side != SideBuy && o.Side != SideSell' internal/risk/gate.go || { echo "--- FAIL: 风控闸方向 fail-close 丢失（§N-4）"; exit 1; }
grep -q 'side_unknown' internal/risk/gate.go || { echo "--- FAIL: 未知方向留痕标识丢失（§N-4 命中不可归因）"; exit 1; }
# 负向锁：非法方向绝不许"缺省成买入"继续往下走（滤注释行，只拦真代码形态）。
if grep -nE '^\s*side = trading\.SideBuy\s*$' internal/server/qmt.go | grep -vE ':[0-9]+:\s*//' | grep -q .; then
	if ! grep -q 'side != trading.SideBuy && side != trading.SideSell' internal/server/qmt.go; then
		echo "--- FAIL: 手动单方向又只剩空串缺省（§SIDEGATE-GO 白名单被绕开）"; exit 1; fi; fi
echo "ok - §SIDEGATE 专项守卫通过（行为锁 4 组 + 静态锁 5 道 + 缺省回退负锁）"

echo "==> 50 §SETTLE 日终结算失败当日可重试 + §M13 熔断广播载荷 + §D5 注释对齐（2026-09-22 PM批）..."
# 旧实现把 `c.lastSettleDay = day` 放在 SettleDay **之前**，一次网关超时即永久烧掉当日唯一一次
# 三方对账（失败分支只 log+计指标、无补偿路径）；现改为「成功才记账 + 10 分钟节流重试 + 当日
# 失败次数进 opslog 与 settle_fail_streak 量规」。顺序断言比文本断言可靠：置位行必须在调用之后。
go test -count=1 ./internal/trading/ -run 'TestSettleFailure' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# 注意过滤注释行：§D4 注释里引用了「旧实现把 c.lastSettleDay = day 放在调用之前」的缺陷原文，
# 不过滤会命中注释行造成顺序假红（负向/顺序锁须滤注释——本仓既有教训）。
SET_ASSIGN=$(grep -n 'c.lastSettleDay = day' internal/trading/settlement.go | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | head -1 | cut -d: -f1)
SET_CALL=$(grep -n 'diff, err := c.SettleDay(' internal/trading/settlement.go | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | head -1 | cut -d: -f1)
[ -n "$SET_ASSIGN" ] && [ -n "$SET_CALL" ] || { echo "--- FAIL: 找不到结算置位/调用行（§D4 静态锁失效）"; exit 1; }
[ "$SET_ASSIGN" -gt "$SET_CALL" ] || { echo "--- FAIL: lastSettleDay 又回到 SettleDay 之前置位（失败当日永久不再对账，§D4 复活）"; exit 1; }
grep -q 'settleRetryInterval' internal/trading/settlement.go || { echo "--- FAIL: 失败重试节流窗丢失（§D4 会打爆网关或不再重试）"; exit 1; }
grep -q 'c.settleFailCount++' internal/trading/settlement.go || { echo "--- FAIL: 当日失败计数丢失（§D4 连续失败不可数）"; exit 1; }
grep -q 'metrics.SetGauge("settle_fail_streak"' internal/trading/settlement.go || { echo "--- FAIL: settle_fail_streak 量规断供（§N-1 死规则形态复活）"; exit 1; }
grep -q '"settle_failed"' internal/metrics/alerter.go || { echo "--- FAIL: settle_failed 告警规则丢失（§D4）"; exit 1; }
# §M13：熔断状态必须随 qmt_report 广播带出（前端只在字段存在时才更新徽标）。
grep -q '"tripped": ctrl != nil && ctrl.Tripped()' internal/server/qmt.go || { echo "--- FAIL: qmt_report 载荷熔断字段丢失（§M13 徽标会被无关回报瞬清）"; exit 1; }
go test -count=1 ./internal/server/ -run 'TestQMTReportBroadcastCarriesTripped' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# §D5：注释与实现对齐后的命名不得回退（tripReasonLocked 自称不加锁却自己 RLock=自锁死锁陷阱）。
grep -q 'func (c \*Controller) currentTripReason()' internal/trading/controller.go || { echo "--- FAIL: currentTripReason 改名回退（§D5）"; exit 1; }
if grep -n 'tripReasonLocked' internal/trading/controller.go | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | grep -q .; then
	echo "--- FAIL: tripReasonLocked 真代码复活（§D5 命名/注释漂移陷阱）"; exit 1; fi
echo "ok - §SETTLE/§M13/§D5 专项守卫通过（行为锁 2 组 + 顺序断言 + 静态锁 6 道 + 命名负锁）"

echo "==> 51 §UX-TRUTH 前端错误呈现层与移动壳推送定向（2026-09-22 PM批 H-2/M-6/M-9/M-11/M-13/N-2/N-3）..."
# 主题=「失败绝不伪装成空态/成功」：保存失败要回滚+真 toast、脏余额不进 localStorage、
# 403 止血必须覆盖在飞轮询链尾、四个页面的首屏失败要有错误态、推送别名按账号派生、
# 空 apk_url 不能只剩「退出」。
( cd web && npm test -- h2_positions_balance m9_error_tri_state m13_forbidden_poll ) 2>&1 | grep -E 'Test Files|passed|failed'
grep -q "import { showToast } from '../ui.jsx'" web/src/pages/Positions.jsx || { echo "--- FAIL: showToast 导入再次缺失（H-2 失败分支 ReferenceError 复活）"; exit 1; }
grep -q 'balancePendingRef' web/src/pages/Positions.jsx || { echo "--- FAIL: 脏余额不入缓存守卫丢失（H-2 第三腿）"; exit 1; }
grep -q 'pollingDeadRef' web/src/pages/Quant.jsx || { echo "--- FAIL: 在飞轮询链止血标志丢失（M-6：stopPolling 管不住链尾 /api/risk/gates）"; exit 1; }
grep -q 'noteForbidden(e)' web/src/pages/Quant.jsx || { echo "--- FAIL: 挂载探测又吞 403（M-6 第二半）"; exit 1; }
grep -q 'QUANT_POLL_EP' web/e2e/uat_full.spec.mjs || { echo "--- FAIL: MP-3 用例端点集又混入壳层轮询（N-2 假红/假绿源）"; exit 1; }
grep -q 'pushAliasFor' mobile/app/src/main/java/com/liangzai/quant/MainActivity.kt || { echo "--- FAIL: 推送别名不再按账号派生（M-11 广播面复活）"; exit 1; }
grep -q 'alias_desired' mobile/app/src/main/java/com/liangzai/quant/MainActivity.kt || { echo "--- FAIL: 别名对账幂等键丢失（M-11 换号竞态）"; exit 1; }
grep -q '版本更新提醒' mobile/app/src/main/java/com/liangzai/quant/UpdateGate.kt || { echo "--- FAIL: 空 apk_url 无软提示兜底（N-3 只剩「退出」）"; exit 1; }
echo "ok - §UX-TRUTH 专项守卫通过（行为锁 1 组 + 静态锁 8 道）"

echo "==> 52 §MONEYGATE 资金三态 fail-close + 撤单零成交可重放（2026-09-22 PM批 M-12/H-1，owner 裁决 A）..."
# M-12：旧两态口径把「真 0」与「口径不可得」折叠成同一个 0——H-4 实录证明两个方向都是事故
# （冻结碎钱→全拦当日买入；冻结值恰 0→无资金约束放行）。三态化后消费端必须显式判 fresh。
# H-1：已撤+零成交的保护性卖单旧口径下同幂等键整天 duplicate 猝死，放行集合须含该支。
go test -count=1 ./internal/engine/ -run 'TestAutoPlaceCashThreeStates' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/trading/ -run 'TestAvailableCashThreeStates' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/store/ -run 'TestResetCancelledZeroFillReplayable|TestResetSendFailedStillReplayable' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
grep -q 'cash, cashFresh := ctrl.AvailableCash()' internal/engine/engine.go || { echo "--- FAIL: 自动买入腿未走三态消费形态（§M12）"; exit 1; }
grep -q 'if !cashFresh {' internal/engine/engine.go || { echo "--- FAIL: 资金口径不可得的 fail-close 分支丢失（§M12）"; exit 1; }
grep -q 'json:"cash_stale"' internal/trading/controller.go || { echo "--- FAIL: /api/qmt/state 的 cash_stale 暴露丢失（§M12 前端降级横幅数据源断供）"; exit 1; }
grep -q 'state.cash_stale' web/src/pages/Quant.jsx || { echo "--- FAIL: 前端资金口径不可得降级横幅丢失（§M12）"; exit 1; }
# §H1-MG 放行集须同时覆盖「发送失败」与「已撤+零成交」，且成交判定走 fills 相关子查询（与 SumFilledQty 同前缀口径）。
grep -q "status='发送失败'" internal/store/real_positions.go || { echo "--- FAIL: 发送失败可重放腿丢失（§GAP2-W1 回归）"; exit 1; }
grep -q "status='已撤' AND NOT EXISTS" internal/store/real_positions.go || { echo "--- FAIL: 已撤零成交可重放腿丢失（§H1-MG 撤单猝死复活）"; exit 1; }
# 负向锁（滤注释行，只拦真代码形态）：旧两态消费 `if cash := ctrl.AvailableCash(); cash > 0` 不得复活。
if grep -nE 'if cash := ctrl\.AvailableCash\(\); cash > 0' internal/engine/engine.go | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | grep -q .; then
	echo "--- FAIL: 旧两态资金消费形态复活（真 0 又被当成不设限，§M12 复活）"; exit 1; fi
echo "ok - §MONEYGATE 专项守卫通过（行为锁 3 组 + 静态锁 6 道 + 两态复活负锁）"

echo "==> 53 §CONTRACT 回报契约双向锁 + 财务报告期字段 + SSE 续传（2026-09-22 PM批 M-4/N-5/M-5）..."
# 主题=「契约的两条腿都要有人看」：M-4 旧 golden 只有 Go→网关单向，网关常年发的
# trade_id/name/created_at 在 Go 信封无 tag、被 encoding/json 静默丢弃（丢腿）且 fills
# 复合唯一键把同秒两笔真部成判成重放；N-5 FinancialData 没有报告期字段、财务新鲜度无从
# 判定且查库错误被当成"没有财报"静默吞掉；M-5 票据消费即废让原生重连必然 401、手动重建
# 又收不到续读位置，补发环两条路都不可达。
# ── M-4 行为锁：Go 侧丢腿回归 + 契约文档 + Python 侧 emitted==golden ──
go test -count=1 ./internal/server/ -run 'TestReportContractGolden|TestReportTradeNameLegPersists|TestReportTradeIDAnchorsPartialFills|TestReportOrderCreatedAtLeg|TestReportEnvelopeDecodesAllEmittedLegs' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
py_tests qmt_gateway/tests/test_report_contract.py 2>&1 | grep -E "passed|failed|error|Ran [0-9]+ test|OK"
# ── M-4 静态锁：信封补 tag、fills 判重键两段化、幂等去重按 trade_id 优先 ──
grep -q 'json:"trade_id"' internal/server/qmt.go || { echo "--- FAIL: qmtReportEvent 的 trade_id tag 丢失（网关成交编号又被静默丢弃，§M4 丢腿复活）"; exit 1; }
grep -q 'json:"created_at"' internal/server/qmt.go || { echo "--- FAIL: 委托创建时间 created_at tag 丢失（§M4 丢腿复活）"; exit 1; }
grep -q 'ALTER TABLE fills ADD COLUMN trade_id' internal/store/store.go || { echo "--- FAIL: fills.trade_id 迁移丢失（§M4）"; exit 1; }
grep -q 'idx_fills_trade ON fills(trade_id) WHERE' internal/store/store.go || { echo "--- FAIL: trade_id 部分唯一索引丢失（§M4 判重锚）"; exit 1; }
grep -q 'idx_fills_idem_notid' internal/store/store.go || { echo "--- FAIL: 无编号行的复合键部分索引丢失（§M4 旧行保护）"; exit 1; }
grep -q 'DROP INDEX IF EXISTS idx_fills_idem' internal/store/store.go || { echo "--- FAIL: 旧复合唯一索引未拆除（同秒两笔真部成又会 500，§M4 复活）"; exit 1; }
grep -q 'WHERE trade_id=?' internal/store/real_positions.go || { echo "--- FAIL: ApplyRealFill 的 trade_id 优先判重腿丢失（§M4）"; exit 1; }
grep -q '"gateway_emitted_fields"' qmt_gateway/contract/report_fields.json || { echo "--- FAIL: 契约金标又退回单向（只锁 Go→网关，不锁网关→Go，§M4 根因）"; exit 1; }
# ── N-5 行为锁 + 静态锁：报告期字段存在、查库错误不再当"没有财报" ──
go test -count=1 ./cmd/quant/ -run 'TestFinaCache' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
grep -q 'EndDate string' internal/strategy_engine/types.go || { echo "--- FAIL: FinancialData 报告期字段丢失（M-7 新鲜度闸又没有可判之物）"; exit 1; }
grep -q 'AnnDate string' internal/strategy_engine/types.go || { echo "--- FAIL: FinancialData 披露日字段丢失（§N-5）"; exit 1; }
grep -q 'COALESCE(ann_date' internal/store/store.go || { echo "--- FAIL: FinaHistory 对 NULL ann_date 的 COALESCE 丢失（一个空值即令整查询报错、财务因子全 0，§N-5 根因复活）"; exit 1; }
# 负向锁（滤注释行）：fina_cache 旧「err == nil && len(rows) > 0」把查库错误折叠成"没有财报"的写法不得复活。
if grep -n 'err == nil && len(rows) > 0' cmd/quant/fina_cache.go | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | grep -q .; then
	echo "--- FAIL: fina_cache 又用单一条件吞掉查库错误（§N-5 静默因子丢失复活）"; exit 1; fi
# ── M-5 行为锁：TTL 内可复用 + query 续传 + e2e 建链 ──
go test -count=1 ./internal/server/ -run 'TestSSETicketReusableWithinTTL|TestSSEQueryLastEventIDReplaysRing' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/e2e/ -run 'TestHTTPSSeTicket' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
( cd web && npm test -- sse_h6_reconnect sse_ticket ) 2>&1 | grep -E 'Test Files|passed|failed'
# ── M-5 静态锁：服务端收 query、票据不作废、前端重建带续读位置 ──
grep -q 'Query().Get("last_event_id")' internal/server/handlers_fix.go || { echo "--- FAIL: SSE 不再收 ?last_event_id= query（手动重建路径补发环又不可达，§M5 复活）"; exit 1; }
grep -q 'func (s \*Server) useSSETicket' internal/server/sse.go || { echo "--- FAIL: useSSETicket 改名回退（票据又回到消费即废语义，§M5 复活）"; exit 1; }
grep -q 'last_event_id=' web/src/api/index.js || { echo "--- FAIL: 前端重建 URL 不带续读位置（§M5 前端腿丢失）"; exit 1; }
# 负向锁（滤注释行）：消费即废的旧函数形态不得复活（改名即报警）。
if grep -rn 'consumeSSETicket' internal/ --include='*.go' | grep -vE ':[0-9]+:[[:space:]]*(//|\*)' | grep -q .; then
	echo "--- FAIL: consumeSSETicket 真代码复活（§M5 语义回退）"; exit 1; fi
echo "ok - §CONTRACT 专项守卫通过（行为锁 4 组 + 静态锁 14 道 + 吞错/作废负锁 2 道）"

echo "==> 54 §H3 打分链日K复权优先 + 不复权拒参与 + 腾讯静默回退拒收（2026-09-22 PM批 H-3）..."
# 旧链 新浪(不复权)第一、只判 len>0、腾讯 qfqday 缺失静默拿不复权 day 冒充前复权——
# 除权日 MA/动量/止损价系统性失真（全系统 qfq 契约的漏网链，§D6 收口后剩余那条）。
# 现：东财(qfq)→腾讯(仅 qfq)优先，每源过 ValidateKLine；新浪/同花顺只作**带标记**的末位兜底，
# 不复权序列不进 md.KLines（因子战法经 len 守卫自然拒参与）。
# 2026-09-24 §KLINE-CHAIN-3 后链序已扩为 腾讯(仅qfq)→东财(qfq)→库内日K→不复权(带标记)：本节的
# 「qfq 腿在不复权腿之前」是不等式断言，链序怎么插都仍成立；本段只守这一条不变式，具体四腿顺序见 72。
go test -count=1 ./internal/strategy_engine/ -run 'TestFetchDayKLine|TestApplyDayKLine|TestFetchMinuteKLine' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/data/ -run 'TestGetTencentKLineRefusesUnadjustedFallback|TestParseTencent' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# 顺序锁（比文本断言可靠）：fetchDayKLine 内东财 qfq 腿必须排在新浪不复权腿之前。
# 三处赋值必须带 `|| true`：grep 无命中时退出码 1，在 set -euo 下会把整道锁变成「脚本静默中止」
# 而不是「跑到下面的判红」——静默中止看起来也是"没报错"，属锁自伤。
H3_QFQ=$(grep -n 'GetKLine(code, "101", 120)' internal/strategy_engine/engine.go | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | head -1 | cut -d: -f1 || true)
H3_UNADJ=$(grep -n 'GetSinaKLine(code, 120)' internal/strategy_engine/engine.go | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | head -1 | cut -d: -f1 || true)
[ -n "$H3_QFQ" ] && [ -n "$H3_UNADJ" ] || { echo "--- FAIL: 找不到复权/不复权腿（§H3 静态锁失效）"; exit 1; }
[ "$H3_QFQ" -lt "$H3_UNADJ" ] || { echo "--- FAIL: 日K链又是不复权优先（除权日因子失真复活，§H3）"; exit 1; }
# 每源校验闸：fetchDayKLine 的四条腿都要过 ValidateKLine（旧实现只判 len>0）。
H3_VALIDATE=$(grep -c 'err == nil && data.ValidateKLine(klines)' internal/strategy_engine/engine.go || true)
[ "$H3_VALIDATE" -ge 4 ] || { echo "--- FAIL: ValidateKLine 闸数量 $H3_VALIDATE < 4（有腿退回只判 len>0，§H3/§D8 复活）"; exit 1; }
grep -q 'KLineUnadj  *bool' internal/strategy_engine/types.go || { echo "--- FAIL: StockMarketData 不复权标记字段丢失（§H3 拒参与不可见）"; exit 1; }
grep -q 'dayk-unadjusted-fallback' internal/strategy_engine/engine.go || { echo "--- FAIL: 复权链降级的 opslog 告警丢失（§H3 降级不可观测）"; exit 1; }
# 负向锁（滤注释行）：① 腾讯 qfqday 缺失静默回退不复权；② 日K不经 applyDayKLine 闸门直写。
if grep -n 'rows = stk.Day' internal/data/tencent.go | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | grep -q .; then
	echo "--- FAIL: 腾讯无 qfqday 又静默回退不复权 day（冒充前复权，§H3 旁支复活）"; exit 1; fi
if grep -n 'md.KLines = e\.fetchDayKLine\|md.KLines, md.MoneyFlow = e\.cachedKLine' internal/strategy_engine/engine.go | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | grep -q .; then
	echo "--- FAIL: 日K又绕过 applyDayKLine 直写 KLines（不复权兜底会静默进因子计算，§H3 复活）"; exit 1; fi
echo "ok - §H3 专项守卫通过（行为锁 2 组 + 顺序断言 + 静态锁 3 道 + 负锁 2 道）"

echo "==> 55 §ROBUST 财务新鲜度停用闸 + 降级报成功族留痕/非零退出 + outbox 单写者（2026-09-22 PM批 M-7/M-8/N-6/N-7）..."
# M-7：研究库 fina_indicator 断更时打分照用半年前财报且无人知晓 → 报告期滞后 >240 天停用（按缺失计入）。
# M-8/N-6：dataload 估值/财务两同步与 research 逐窗装配、sector_agent 成分股验证，旧实现吞错仍报
#          "完成/验证 N"且 exit 0——失败计数>0 一律降级文案，CLI 侧非零退出，库侧留痕降级行。
# N-7：outbox saveLocked 每次变更各起一个写协程并发 AtomicWrite 同一文件（Windows rename 互相踩踏
#      Access is denied）+ 旧快照可能后写覆盖新快照 → 收敛为单写者 + 最新快照胜出 + Stop 终刷。
go test -count=1 ./cmd/quant/ -run 'TestFina' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/notify/ -run 'TestOutbox' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/research/ -run 'TestNoteWindowFailTrace' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# M-7 静态锁：闸必须被 Lookup 实际调用（定义了不调用=死代码假修复）+ 告警键在位。
grep -q 'const finaStaleMaxDays = 240' cmd/quant/fina_cache.go || { echo "--- FAIL: §M-7 阈值常量丢失"; exit 1; }
grep -q 'finaReportStale(fina' cmd/quant/fina_cache.go || { echo "--- FAIL: §M-7 新鲜度闸未被 Lookup 调用（定义了个寂寞）"; exit 1; }
grep -q 'fina-stale-report' cmd/quant/fina_cache.go || { echo "--- FAIL: §M-7 停用告警 opslog 键丢失"; exit 1; }
if grep -n 'YYYY-MM-DD，如 2026-06-30' internal/strategy_engine/types.go | grep -q .; then
	echo "--- FAIL: FinancialData 报告期注释又写回 YYYY-MM-DD（研究库实际 YYYYMMDD，M-7 解析口径会被误导）"; exit 1; fi
# M-8/N-6 CLI：两同步函数必须返回 error 且分发点转成非零退出（log.Fatalf）。
grep -q 'func cmdHithinkSyncValuations(client \*data.HithinkClient, db \*store.DB) error {' cmd/dataload/hithink_sync.go || { echo "--- FAIL: 估值同步又无返回值（批次失败无法非零退出，§M-8 复活）"; exit 1; }
grep -q 'func cmdHithinkSyncFinIndicators(client \*data.HithinkClient, db \*store.DB, args \[\]string) error {' cmd/dataload/hithink_sync.go || { echo "--- FAIL: 财务指标同步又无返回值（§M-8/N-6 复活）"; exit 1; }
H_ERR=$(grep -c 'if err := cmdHithinkSync\(Valuations\|FinIndicators\)' cmd/dataload/hithink_sync.go)
[ "$H_ERR" -ge 2 ] || { echo "--- FAIL: 分发点吃 err 的非零退出腿 $H_ERR < 2（§M-8 降级错误又被吞）"; exit 1; }
# M-8/N-6 research：五处逐窗装配失败计数 + 统一降级行；ckpt save 双吞（_ =）必须绝迹。
R_FAIL=$(grep -c 'failed++' internal/research/windowed.go)
[ "$R_FAIL" -ge 5 ] || { echo "--- FAIL: 窗口装配失败计数点 $R_FAIL < 5（有腿又静默 continue，§N-6 复活）"; exit 1; }
grep -q 'func noteWindowFail' internal/research/windowed.go || { echo "--- FAIL: 缺窗降级留痕函数丢失（§N-6）"; exit 1; }
if grep -nE '_ = c\.db\.PutWindowCkpt' internal/research/windowed.go | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | grep -q .; then
	echo "--- FAIL: 断点落库又 \`_ =\` 吞错（整晚断点没存上不可见，§N-6 复活）"; exit 1; fi
# M-8/N-6 sector_agent：成分股验证失败计数 + 降级文案。
S_FAIL=$(grep -c 'verifyFailed' internal/sector_agent/agent.go)
[ "$S_FAIL" -ge 3 ] || { echo "--- FAIL: 板块成分股验证失败计数点 $S_FAIL < 3（又零留痕报「验证 N 个板块」，§M-8 复活）"; exit 1; }
# N-7：单写者三要素——flushPending 存在、pending 快照最新胜出、saveWG.Add 先于 go o.loop（顺序锁）。
grep -q 'func (o \*Outbox) flushPending' internal/notify/outbox.go || { echo "--- FAIL: outbox 单写者 flushPending 丢失（§N-7 复活）"; exit 1; }
grep -q 'o.pending = items' internal/notify/outbox.go || { echo "--- FAIL: 最新快照胜出登记丢失（旧快照可后写覆盖新状态，§N-7）"; exit 1; }
N7_ADD=$(grep -n 'o\.saveWG\.Add(1)' internal/notify/outbox.go | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | head -1 | cut -d: -f1)
N7_GO=$(grep -n 'go o\.loop(stopCh)' internal/notify/outbox.go | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | head -1 | cut -d: -f1)
[ -n "$N7_ADD" ] && [ -n "$N7_GO" ] || { echo "--- FAIL: saveWG/go loop 锚点丢失（§N-7 顺序锁失效）"; exit 1; }
[ "$N7_ADD" -lt "$N7_GO" ] || { echo "--- FAIL: WaitGroup 计数又落在 loop 内（Stop 的 Wait 可在 Add 前返回，§N-7 -race 实录）"; exit 1; }
# 负锁：每次变更各起一个写协程（并发 rename 互踩的根形）必须绝迹。
if grep -nE 'go func\(path string, items \[\]outboxPersistItem\)' internal/notify/outbox.go | grep -q .; then
	echo "--- FAIL: saveLocked 又按变更各起写协程（Windows 并发 AtomicWrite Access denied 根因，§N-7 复活）"; exit 1; fi
echo "ok - §ROBUST 专项守卫通过（行为锁 2 组 + 静态锁 9 道 + 顺序断言 + 负锁 3 道）"

echo "==> 56 §清扫批 写端点收权普查 + C6 幂等键同源 + C9 ntfy 通道短路 + A5 真日历 + notify-test 实探（2026-09-22 PM批 M-14/C6/C9/A5）..."
# M-14：/api/news/test-attribution 从成员可写收权 admin；防回潮升级为全量普查锁——
# C6：买入幂等键与 signalctl 准入探针同源（StrategyKeyOf），杜绝「探针合、幂等键分」双单敞口。
# C9：ntfy 通道整体宕机时逐条吃 5s 超时+逐条入补投（风暴放大），且通道自哑无人知——
#     现连续 3 败开短路窗 + 经站内通道自监控播报。A5：网关时段判定接 Go 落盘真交易日历。
go test -count=1 ./internal/server/ -run 'TestWriteEndpointsAllGated|TestH3' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/notify/ -run 'TestNtfyBreaker|TestPushGatewayShortCircuitSkipsEnqueue|TestAlertChannelDown' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/engine/ -run 'TestAutoPlaceIdempotencyKey' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
py_tests qmt_gateway/tests/test_trading_calendar.py
# M-14 静态锁：test-attribution 必须挂 adminMiddleware（负锁：authMiddleware 形态绝迹）。
grep -E 'test-attribution.*adminMiddleware' internal/server/server.go >/dev/null || { echo "--- FAIL: §M-14 test-attribution 收权丢失（成员又可无限烧 LLM Stage2）"; exit 1; }
if grep -qE '"POST /api/news/test-attribution", s\.authMiddleware' internal/server/server.go; then
	echo "--- FAIL: §M-14 又回退成 authMiddleware（普通成员可越权写全局信号簿）"; exit 1; fi
# C6 静态锁：买入幂等键战法分量必须由 StrategyKeyOf 单点派生；旧 StrategyID?:Strategy 分叉形态绝迹。
grep -q 'stratKey := signalctl.StrategyKeyOf(sig)' internal/engine/engine.go || { echo "--- FAIL: §C6 幂等键又绕开 StrategyKeyOf（与准入探针分叉复活）"; exit 1; }
if grep -nE 'if sig\.StrategyID != "" \{[[:space:]]*stratKey' internal/engine/engine.go | grep -q .; then
	echo "--- FAIL: §C6 旧键派生（StrategyID 优先回退显示名）复活"; exit 1; fi
# C9 静态锁：短路三要素在位（哨兵错误 / 阈值常量 / 自监控播报走站内两路）。
grep -q 'ErrNtfyShortCircuit = ' internal/notify/ntfy.go || { echo "--- FAIL: §C9 短路哨兵错误丢失"; exit 1; }
grep -q 'alertChannelDown' internal/notify/notify.go || { echo "--- FAIL: §C9 通道自监控播报丢失（报丧鸟哑了没人知道）"; exit 1; }
grep -q 'errors.Is(err, ErrNtfyShortCircuit)' internal/notify/gateway.go || { echo "--- FAIL: §C9 短路窗内仍逐条入补投队列（风暴放大复活）"; exit 1; }
# notify-test 实探：空 stub（只 log 就回 ok）不得复活。
if grep -A 3 'func (s \*Server) handleFixNotifyTest' internal/server/handlers_fix.go | grep -q 'writeJSON(w, 200, map\[string\]string{"status": "ok"})$'; then
	echo "--- FAIL: /api/notify-test 又回退成永远 ok 的空 stub（假反馈，§F-3 同族）"; exit 1; fi
# A5 静态锁：网关两处时段判定都必须消费 trading_calendar；旧「仅 weekday」裸启发式绝迹（注释行除外）。
grep -q 'from trading_calendar import' qmt_gateway/handler.py || { echo "--- FAIL: handler.py 又断开真日历接线（§A5 节假日误判复活）"; exit 1; }
grep -q 'from trading_calendar import' qmt_gateway/qmt_bridge.py || { echo "--- FAIL: qmt_bridge.py 又断开真日历接线（§A5）"; exit 1; }
if grep -nE '^    if now\.weekday\(\) >= 5:$' qmt_gateway/qmt_bridge.py | grep -vE '^[0-9]+:\s*#' | grep -q .; then
	echo "--- FAIL: qmt_bridge 工作日启发式又做主判定（应只在无日历兜底分支）"; exit 1; fi
grep -q 'closed_days' qmt_gateway/trading_calendar.py || { echo "--- FAIL: 交易日历读取模块丢失（§A5）"; exit 1; }
# §A5 部署清单锁（09-21 qmt_bridge_strategy 漏列同族教训）：日历模块必须随 [2b] 下发，
# 否则现网网关 ImportError 静默降级 weekday 启发式——修复形同虚设且无任何报错。
grep -q 'qmt_gateway/trading_calendar.py' scripts/deploy_guangzhou.sh || { echo "--- FAIL: 部署 [2b] 清单缺 trading_calendar.py（§A5 现网不会生效）"; exit 1; }
# M-10 行为锁：交错轮询的迟到响应整包丢弃（工具语义 3 例 + Signals 整页交错回归 1 例）。
( cd web && npm test -- m10_stale_guard )
# M-10 静态锁：守卫工具在位；三个轮询页均 import createStaleGuard 且真正 begin/isStale（漏一页=该页倒挂复活）。
grep -q 'export function createStaleGuard' web/src/utils/staleGuard.js || { echo "--- FAIL: §M-10 staleGuard 工具丢失"; exit 1; }
for f in Dashboard Signals Positions; do
	grep -q "import { createStaleGuard }" "web/src/pages/$f.jsx" || { echo "--- FAIL: §M-10 $f 页未接入陈旧守卫（交错覆盖复活）"; exit 1; }
	grep -q 'isStale(' "web/src/pages/$f.jsx" || { echo "--- FAIL: §M-10 $f 页只建守卫不用（begin/isStale 半接线）"; exit 1; }
done
echo "ok - §清扫批 专项守卫通过（行为锁 5 组 + 静态锁 12 道 + 负锁 4 道）"

echo "==> 57 §BINANCE Phase 1 多市场地基：发布闸 + store 市场迁移 + Qty float64 + 时段/风控 14 闸×3 市场矩阵（2026-09-22 PLAN_BINANCE_MULTI_ASSET）..."
# 背景：quant-binance Phase 1（加市场不换市场）——CN/QMT 链字节级不动，CRYPTO/US 以
# market 列/键分层落库；本段把「发布闸缺省关、qty REAL 全量、市场短路矩阵、契约三点锁」
# 钉成静态+行为双守卫，Phase 2 接线时任何回退（如 DDL 又写 INTEGER、闸漏市场短路）即红。
# ---- 行为锁（新测试组 + 存量 CN 回归组各跑一遍）----
go test -count=1 ./internal/config/ -run 'Binance|Broker' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/server/ -run 'TestBinanceConfig' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/store/ -run 'P1C|P1D' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/data/ -run 'TestSession|TestNormalizeMarketKey' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/risk/ -run 'TestGate' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/trading/ -run 'TestOrderContractGolden|TestSweepOrdersStaleCancel' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
py_tests qmt_gateway/tests/test_order_gates.py
py_tests qmt_gateway/tests/test_trade_identity_reject.py
# ---- 静态锁 ----
# 发布闸：rules.binance 出厂总开关必须 false（一键推送配置即唤醒币安链路是最高危回退）。
grep -q 'Enabled:          false,' internal/config/binance.go || { echo "--- FAIL: §BINANCE 发布闸漂移（DefaultBinanceConfig 顶层 Enabled 非 false）"; exit 1; }
# store：orders/fills/shadow_orders 三表 DDL 全量 qty REAL（正锁 ≥3 处）；INTEGER 形态收窄到 2 处豁免（下方等值锁）。
NQ=$(grep -c 'qty REAL NOT NULL' internal/store/store.go)
[ "$NQ" -ge 3 ] || { echo "--- FAIL: §BINANCE qty REAL 覆盖不足（store.go 仅 $NQ 处，三表 DDL+重建路应有 ≥3）"; exit 1; }
# qty INTEGER 等值锁：全文件仅允许 2 处合法豁免——
# ① paper_trades（CN 模拟盘簿，整数手数，不在 PLAN §5.2-3 P1-d 迁移范围）；
# ② migrateRealPositionsPK 史前单主键库的过渡建表 DDL（随后 migrateRealPositionsMarketPK
#    以 qty REAL 接力重建，链式顺序由 store.go Open 内注释钉死，过渡态不对外暴露）。
# 第 3 处出现即 §P1-d 回潮（CRYPTO 碎量被截成 0）；新增豁免必须连同本锁计数一起改。
QI=$(grep -c 'qty INTEGER NOT NULL' internal/store/store.go)
[ "$QI" -eq 2 ] || { echo "--- FAIL: §BINANCE qty INTEGER 计数=${QI}≠2（豁免面=模拟盘簿+史前迁移过渡态，回潮或扩豁免都需显式对齐本锁）"; exit 1; }
# 时段：Session(market) 三市场分发在位，且 CN 分支必须委托旧谓词而非另写一份（双口径漂移源）。
grep -q 'func Session(market string) SessionRule' internal/data/market_session.go || { echo "--- FAIL: §BINANCE Session(market) 入口丢失"; exit 1; }
grep -q 'return IsActiveSession(t)' internal/data/market_session.go || { echo "--- FAIL: §BINANCE CN 时段分支不再委托 IsActiveSession（复制口径，双源漂移）"; exit 1; }
# 风控矩阵：CN 专属四闸（st/t1/limit_up_down/price_cross_check）市场短路等值锁——
# `o.Market != "CN"` 恰好 4 处；增删 CN 专属闸必须同步过矩阵测试后改本锁（防无声加减）。
NM=$(grep -c 'o.Market != "CN"' internal/risk/gate.go)
[ "$NM" -eq 4 ] || { echo "--- FAIL: §BINANCE CN 市场短路计数=${NM}≠4（矩阵 §9 的 CN 专属闸增减需显式对齐 PLAN）"; exit 1; }
grep -q 'func (g \*Gate) checkMinNotional' internal/risk/gate.go || { echo "--- FAIL: §BINANCE min_notional 闸丢失"; exit 1; }
grep -q 'func (g \*Gate) checkLotPrecision' internal/risk/gate.go || { echo "--- FAIL: §BINANCE lot_precision 闸丢失"; exit 1; }
# 契约三点锁的 Go 可见面：market 必须留在网关 ignored 声明（漏声明会被 §A2 双红，这里先给中文归因）。
grep -q '"market"' qmt_gateway/contract/order_fields.json || { echo "--- FAIL: §BINANCE order_fields.json 缺 market 键声明"; exit 1; }
echo "ok - §BINANCE Phase 1 专项守卫通过（行为锁 8 组 + 静态锁 9 道〔等值锁 2：qty INTEGER=2 豁免 / CN 短路=4 闸，余为正锁+定点锁〕）"

echo "==> 58 §BINANCE Phase 2 执行/回报/隔离：契约 golden 双锁 + 回报三条腿 + §15.2 币种隔离 + 运维端点（2026-09-23 PLAN_BINANCE_MULTI_ASSET）..."
# 背景：Phase 2 落地 BinanceExecutor（US/CRYPTO 真实下单）、binance_report.go（listenKey+WS+REST
# 差分三条腿收敛到 ApplyOrderReportTx/ApplyRealFill 权威写口）、store/risk §15.2 币种隔离
# （*ForMarket 聚合 + real_account (user_id,market) 复合主键惰性重建）、/api/binance/* 运维端点。
# 本段把这些面的回退（参数面漂移/臆造终态/隔离漏改/端点裸奔）钉成静态+行为双守卫。
# ---- 行为锁 ----
go test -count=1 ./internal/trading/ -run 'TestBinanceOrderFieldsGolden|TestBinanceErrorCodesGolden|TestBinanceDefaultDisabled' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/trading/ -run 'TestBinanceStatusMapLock|TestReporter' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/store/ -run 'TestP2' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/backtest/ -run 'Cost|P4' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/server/ -run 'TestWriteRoutePerms|TestBinanceAPI' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/risk/ -run 'TestGateMarketMatrixP1E' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# ---- 静态锁 ----
# 契约 golden：order 参数面等值锁 7 格（§2.2 四格 + §2.6 现货三形），第 8 格出现即参数面扩张未过审。
NF=$(python3 -c "import json;print(len(json.load(open('qmt_gateway/contract/binance_order_fields.json'))['cells']))")
[ "$NF" -eq 7 ] || { echo "--- FAIL: §P2 契约 golden 格数=${NF}≠7（参数面增减必须显式过 PLAN §2.2/§2.6 对齐本锁）"; exit 1; }
test -f qmt_gateway/contract/binance_error_codes.json || { echo "--- FAIL: §P2 错误码 golden 丢失"; exit 1; }
# 回报腿：状态映射唯一权威表 + 落库必须复用 QMT 链同一套写口（自写 INSERT=秩守卫旁路，账本可被乱序回报写脏）。
grep -q 'func mapBinanceReportStatus' internal/trading/binance_report.go || { echo "--- FAIL: §P2 回报状态映射表丢失"; exit 1; }
grep -q 'ApplyOrderReportTx' internal/trading/binance_report.go || { echo "--- FAIL: §P2 回报腿绕过 ApplyOrderReportTx 权威写口"; exit 1; }
grep -q '"ext:"' internal/trading/binance_report.go || { echo "--- FAIL: §P2 缺 §F5 ext: 占位信号键（手工/先到回报会撞 UNIQUE）"; exit 1; }
# 负锁：回报腿绝不许直写 INSERT 旁路（必须走 store 权威写口，秩守卫/幂等判重都在那里）。
if grep -q 'INSERT INTO orders\|INSERT INTO fills' internal/trading/binance_report.go; then echo "--- FAIL: §P2 回报腿直写 INSERT（必须走 store 权威写口）"; exit 1; fi
# §15.2 隔离：ForMarket 聚合族在位 + real_account 复合主键（漏改=跨币种串账/账户行互相覆盖）。
NG=$(grep -c 'func (d \*DB) .*ForMarket' internal/store/risk_gates.go)
[ "$NG" -ge 5 ] || { echo "--- FAIL: §P2 ForMarket 聚合仅 $NG 处（<5，日亏/集中度/买入纪律族漏改）"; exit 1; }
grep -q 'PRIMARY KEY (user_id, market)' internal/store/real_account.go || { echo "--- FAIL: §P2 real_account 复合主键迁移丢失"; exit 1; }
# 运维端点普查：8 条=6×/api/binance（state/orders/exchange_info GET + cancel/halt/disclaimer POST）
# + 2×/api/config/binance（GET/POST，§P1-b 契约前缀）；写端点裸挂由 §M-14 全量普查测试
# （TestWriteRoutePerms）拦截，这里只钉数量回退。
NB=$(grep -cE 'mux.HandleFunc\("[A-Z]* (/api/binance|/api/config/binance)' internal/server/server.go)
[ "$NB" -ge 8 ] || { echo "--- FAIL: §P2 /api/binance(/config) 端点数=${NB}＜8（运维面被裁剪需对齐 PLAN §6.4 端点表）"; exit 1; }
# 引擎装配锁：exchangeInfo→风控闸 + 回报器注册必须接进 registry（缺一条=闸拿不到真规则恒 fail-open / 回报链整体静默）。
grep -q 'SetSymbolRulesSource' internal/engine/registry.go || { echo "--- FAIL: §P2 exchangeInfo→risk.Gate 规则源未接线"; exit 1; }
grep -q 'RegisterReporter' internal/engine/registry.go || { echo "--- FAIL: §P2 回报接收器未接入引擎生命周期"; exit 1; }
# 回测成本三市场：CostModelForMarket 已进 chain.Run 缺省路（漏接=CRYPTO 回测按 A 股印花税计价）。
grep -q 'CostModelForMarket(opts.Market)' internal/backtest/chain.go || { echo "--- FAIL: §P2 全链回测缺省成本未走市场选择"; exit 1; }
echo "ok - §BINANCE Phase 2 专项守卫通过（行为锁 6 组 + 静态锁 10 道〔等值锁 1：契约 7 格；余为正锁/定点锁/负锁〕）"

echo "==> 59 §BINANCE Phase 3+4 行情/状态 WS + market_halt 证据闸 + xasset 战法 + 前端市场维度（2026-09-23 PLAN_BINANCE_MULTI_ASSET）..."
# 背景：Phase 3 落地 BinanceWS 状态机 + StdWsDial（纯标准库 RFC6455 客户端，go.mod 零 websocket
# 依赖）、现货/美股两条行情流 + 美股 tradingStatus/tradability 状态流、§9 第 15 道 market_halt
# 证据闸（无证据≠可交易，GateSource→SetHaltEvidenceSource 接进 registry）、xasset 三腿战法
# （MA 交叉/RSI 动量/新闻事件，信号自带 Market）。Phase 4 前端市场维度（utils.market.js/
# market.jsx/BinanceConfigPanel/BinanceStatusCard）+ 历史 K 线 REST（BinanceRestClient.KLines）。
# 本段把「分流大小写回潮、客户端依赖回潮、装配断链、源 golden 漂移」钉成静态+行为双守卫。
# ---- 行为锁（全部实跑通过后才入册）----
go test -count=1 ./internal/data/ -run 'TradingStatus|StatusFeed|QuoteFeed|SymbolRules' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/data/ -run 'TestStdWs|TestBinanceWS' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/data/ -run 'TestQuoteSources' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/strategies/xasset/ 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/risk/ -run 'TestGateMarketHaltP3' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/trading/ -run 'Feed' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# 前端市场维度 + M-10 轮询后到丢弃守卫（两条 vitest 文件级锁，非全量 npm test——全量在 -full 档）
( cd web && npx vitest run src/__tests__/binance_market_p4.test.jsx src/__tests__/m10_stale_guard.test.jsx >/dev/null 2>&1 ) || { echo "--- FAIL: §P4 前端市场维度/staleGuard vitest 未通过"; exit 1; }
# ---- 静态锁 ----
# 缺陷①负锁：组合流分流比较必须走 LowerForm——混大小写直比 "@"+常量 的旧写法复活即红
# （旧形态下 tradingStatus 分支永远不可达，闸恒"无证据"，US 单被 fail-close 全拒还是假绿全靠这条钉）。
if grep -Fq '"@"+EquityStreamTradingStatus' internal/data/binance_trading_status_io.go; then echo "--- FAIL: §P3 状态分流大小写直比回潮（缺陷①）"; exit 1; fi
# 依赖回潮负锁：WS 客户端必须留在纯标准库（引入 gorilla/nhooyr 等即违 PLAN §2.9 依赖面纪律）。
if grep -qi 'websocket' go.mod; then echo "--- FAIL: §P3 go.mod 出现 websocket 依赖（必须走内建 StdWsDial）"; exit 1; fi
# §ENH-X1 镜像端点正锁（GAP §5 遗留锁①收口）：现货公共行情必须保留 data-api.binance.vision 镜像
# 常量（本机/广州实测唯一可达面）——被删=行情链回退主域直连，公网出口受限时整链静默瞎掉。
grep -q 'data-api\.binance\.vision' internal/data/binance_ws.go || { echo "--- FAIL: §P3 镜像端点常量丢失（GAP G-1/W-4 结论被回退）"; exit 1; }
grep -q 'func StdWsDial' internal/data/binance_ws_std.go || { echo "--- FAIL: §P3 StdWsDial 拨号器丢失"; exit 1; }
grep -q 'func (f \*BinanceStatusFeed) GateSource' internal/data/binance_status_feed.go || { echo "--- FAIL: §P3 状态 feed 的 GateSource 注入面丢失"; exit 1; }
# 装配链锁：feed 订阅/闸证据源/观测注册三条都必须在 registry 里（断一条=行情盲区或闸恒惰性）。
grep -q 'data.StdWsDial' internal/engine/registry.go || { echo "--- FAIL: §P3 registry 未接 StdWsDial（feed 拨号断链）"; exit 1; }
grep -q 'SetHaltEvidenceSource' internal/engine/registry.go || { echo "--- FAIL: §P3 market_halt 证据源未接进 registry（闸恒惰性）"; exit 1; }
grep -q 'RegisterFeed' internal/engine/registry.go || { echo "--- FAIL: §P3 feed 观测未注册（/api/binance/state 盲区）"; exit 1; }
grep -q 'StatusSymbols' internal/engine/registry.go || { echo "--- FAIL: §P3 状态流订阅池配置读取丢失"; exit 1; }
grep -q 'out\["feeds"\]' internal/server/binance_api.go || { echo "--- FAIL: §P3 /api/binance/state 的 feeds 观测节丢失"; exit 1; }
# 源 golden 等值锁：quote_sources.json 里 BINANCE-* 源恰好 2 个（SPOT/STK）——增删行情源
# 必须显式过 §M1 契约测试再生成 golden，不允许"顺手多一源少一源"。
NBS=$(python3 -c "import json;print(sum(1 for x in json.load(open('qmt_gateway/contract/quote_sources.json')) if x.startswith('BINANCE-')))")
[ "$NBS" -eq 2 ] || { echo "--- FAIL: §P3 quote_sources golden BINANCE-* 计数=${NBS}≠2（源面增减需过 §M1 契约重生成）"; exit 1; }
# xasset 信号族等值锁：三条新市场 SignalType 恰 3 个且 Market 章在位（缺腿=新市场只剩 CN 战法）。
NX=$(grep -cE 'SignalType = "(ma_cross|rsi_momentum|news_xasset)"' internal/strategy/types.go)
[ "$NX" -eq 3 ] || { echo "--- FAIL: §P3 xasset 信号族计数=${NX}≠3"; exit 1; }
# 历史 K 线 + 前端市场维度文件在位（缺文件=Phase 4 面板/回测数据面整块蒸发）。
grep -q 'func (c \*BinanceRestClient) KLines' internal/data/binance_kline.go || { echo "--- FAIL: §P4 BinanceRestClient.KLines 历史K线入口丢失"; exit 1; }
test -f web/src/utils.market.js && test -f web/src/market.jsx || { echo "--- FAIL: §P4 前端市场维度模块丢失（utils.market.js/market.jsx）"; exit 1; }
echo "ok - §BINANCE Phase 3+4 专项守卫通过（行为锁 7 组 + 静态锁 13 道〔等值锁 2：BINANCE 源=2 / xasset 信号=3；负锁 2：分流大小写 / websocket 依赖；§ENH-X1 镜像端点正锁；余为正锁/定点锁〕）"

echo ""
echo "==> 60 §ENH 增强批 A+B：FNG/EDGAR/CryptoPanic 证据腿 + 观测面三件套 + lightweight-charts 试点 + walk-forward + 滑点回灌（2026-09-23 PLAN_ENHANCE_20260923）..."
# 背景：A1 FNG 恐慌贪婪指数（data.FNGClient + registry 惰性节流闭包 + /api/binance/state "fng" 节，
# 零配置零行为）；A4/B8 SEC EDGAR 8-K（US）与 CryptoPanic（CRYPTO）事件腿（BinanceConfig.Events
# 凭证面 + registry per-market 缓存 + "events" 观测节，token 掩码与 api_key 同口径）；A2/A3 观测面
# 交付物（ops/gatus + ops/grafana + ops/prometheus）；B5 lightweight-charts 试点（XProChart 组件，
# 仅 web 依赖，Go 零新依赖红线不变）；B6 walk-forward 样本外验证（btreplay Top-K→OOS 重放）；
# B7 滑点校准回灌实单挂价（RiskGateConfig.SlippagePassthrough 比例上限，0=关=字节兼容）。
# 本段把「证据链断装、无证据≠中性回潮、工厂默认值被改、观测面文件缺失」钉成静态+行为双守卫。
# ---- 行为锁（全部实跑通过后才入册）----
go test -count=1 ./internal/data/ -run 'FNG|Edgar|CryptoPanic|XEvent' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/strategies/xasset/ -run 'Sentiment' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/trading/ -run 'Feed' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/btreplay/ -run 'WalkForward' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/engine/ -run 'SlipFracs|FNGSnapshot|XEvents' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/server/ -run 'BinanceKline|BinanceStateFNG|BinanceStateXEvents|BinanceConfigEvents' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# B5 前端图表试点两文件（文件级锁，非全量 npm test——全量在 -full 档）
( cd web && npx vitest run src/__tests__/xpro_chart_p5.test.jsx src/__tests__/binance_xpro_p5.test.jsx >/dev/null 2>&1 ) || { echo "--- FAIL: §ENH-B5 XProChart vitest 两文件未通过"; exit 1; }
# ---- 静态锁：正锁（装配断链=证据腿整块蒸发）----
grep -q 'func (r \*Registry) FNGSnapshot' internal/engine/registry_fng.go || { echo "--- FAIL: §ENH-A1 Registry.FNGSnapshot 证据出口丢失"; exit 1; }
grep -q 'func (r \*Registry) attachXEventSource' internal/engine/registry_events.go || { echo "--- FAIL: §ENH-A4/B8 Registry.attachXEventSource 事件腿装配口丢失"; exit 1; }
grep -q 'func (r \*Registry) XEventsJSON' internal/engine/registry_events.go || { echo "--- FAIL: §ENH-A4/B8 XEventsJSON 观测面出口丢失"; exit 1; }
grep -q 'DedupeEvents' internal/data/xasset_events.go || { echo "--- FAIL: §ENH-A4/B8 DedupeEvents 幂等去重丢失"; exit 1; }
grep -q 'type FNGClient struct' internal/data/fng.go || { echo "--- FAIL: §ENH-A1 data.FNGClient 丢失"; exit 1; }
grep -q 'type EDGARClient struct' internal/data/edgar.go || { echo "--- FAIL: §ENH-A4 data.EDGARClient 丢失"; exit 1; }
grep -q 'type CryptoPanicClient struct' internal/data/cryptopanic.go || { echo "--- FAIL: §ENH-B8 data.CryptoPanicClient 丢失"; exit 1; }
grep -q 'SlippagePassthrough float64' internal/config/config.go || { echo "--- FAIL: §ENH-B7 RiskGateConfig.SlippagePassthrough 字段丢失"; exit 1; }
# B7 消费面等值锁：买卖两条挂价链各恰好 1 处 slipFracs 调用（缺一条=半边回灌断链）。
NS=$(grep -c 'e.slipFracs(' internal/engine/engine.go internal/engine/scoring_loop.go | awk -F: '{s+=$2} END {print s}')
[ "$NS" -eq 2 ] || { echo "--- FAIL: §ENH-B7 slipFracs 消费点计数=${NS}≠2（autoPlace+sellRealPosition 两条链）"; exit 1; }
grep -q 'srv.SetFNGSource' cmd/quant/main.go || { echo "--- FAIL: §ENH-A1 main.go SetFNGSource 装配断链"; exit 1; }
grep -q 'srv.SetXEventsSource' cmd/quant/main.go || { echo "--- FAIL: §ENH-A4/B8 main.go SetXEventsSource 装配断链"; exit 1; }
grep -q 'out\["events"\]' internal/server/binance_api.go || { echo "--- FAIL: §ENH-A4/B8 /api/binance/state events 观测节丢失"; exit 1; }
grep -q 's.mux.HandleFunc("GET /api/binance/kline", s.authMiddleware(s.handleBinanceKline))' internal/server/server.go || { echo "--- FAIL: §ENH-B5 kline 路由鉴权装配行丢失"; exit 1; }
grep -q 'func (r \*BrokerRouter) ShutdownLive' internal/trading/router.go || { echo "--- FAIL: §ENH-X2 BrokerRouter.ShutdownLive 对称出口丢失"; exit 1; }
grep -q 'RegisterFeedWithStop' internal/trading/router.go || { echo "--- FAIL: §ENH-X2 RegisterFeedWithStop 生命周期配对丢失"; exit 1; }
test -f web/src/components/XProChart.jsx || { echo "--- FAIL: §ENH-B5 XProChart 组件文件丢失"; exit 1; }
grep -q 'lightweight-charts' web/package.json || { echo "--- FAIL: §ENH-B5 web 图表依赖声明丢失"; exit 1; }
# ---- 静态锁：负锁（工厂默认值/依赖红线/页级未授权接入）----
# B6 负锁：装配层不得出现 WalkForward: nil 显式占位（nil=关闭由零值天然达成，显式写出=口径漂移信号；测试夹具除外）。
if grep -rn 'WalkForward: nil' internal cmd --include='*.go' | grep -v _test.go | grep -q .; then echo "--- FAIL: §ENH-B6 非测试代码出现 WalkForward: nil 显式占位"; exit 1; fi
# B7 工厂默认锁：internal/config/ 任何 .go 不得字面量初始化 SlippagePassthrough（工厂必须 0=关=字节兼容）。
if grep -rn 'SlippagePassthrough:' internal/config/ --include='*.go' | grep -q .; then echo "--- FAIL: §ENH-B7 internal/config/ 出现 SlippagePassthrough 非零初始化（工厂必须保持 0=关）"; exit 1; fi
# Go 零新依赖红线（§2.9）：图表依赖只准留在 web/，go.mod/go.sum 出现 lightweight 即违例。
if grep -qi 'lightweight' go.mod go.sum; then echo "--- FAIL: §ENH-B5 go.mod/go.sum 出现 lightweight 依赖（Go 侧零新依赖红线）"; exit 1; fi
# B5 试点范围锁：XProChart 原为「组件+测试」试点；§市场分家-1（2026-09-24 owner 缺陷单）
# 只授权扩展 Positions.jsx 展开行（US/CRYPTO 专业蜡烛图，替代 CN 分时误挂）。白名单外的
# pages/ 出现 XProChart 仍是越界——试点扩面要逐页授权，不允许借修复批整体解禁。
if grep -rl 'XProChart' web/src/pages/ | grep -v '^web/src/pages/Positions.jsx$' | grep -q .; then echo "--- FAIL: §ENH-B5 XProChart 进入未授权页面（白名单仅 Positions.jsx）"; exit 1; fi
# ---- A2/A3 观测面交付物：三文件在位 + 可解析 + 明文密钥负锁 ----
test -f ops/gatus/config.yaml && test -f ops/grafana/dashboard.json && test -f ops/prometheus/prometheus.yml || { echo "--- FAIL: §ENH-A2/A3 ops/ 观测面三件套缺失"; exit 1; }
ruby -ryaml -e 'YAML.safe_load(File.read(ARGV[0]))' ops/prometheus/prometheus.yml || { echo "--- FAIL: §ENH-A2 prometheus.yml YAML 解析失败"; exit 1; }
ruby -ryaml -e 'YAML.safe_load(File.read(ARGV[0]))' ops/gatus/config.yaml || { echo "--- FAIL: §ENH-A2 gatus config.yaml YAML 解析失败"; exit 1; }
python3 -c "import json;json.load(open('ops/grafana/dashboard.json'))" || { echo "--- FAIL: §ENH-A3 grafana dashboard.json JSON 解析失败"; exit 1; }
# 明文密钥负锁：ops/ 任何 yml/yaml/json 不得出现 token/key/secret 明文赋值（ENV 占位除外）。
if grep -RniE '(token|key|secret)[[:space:]]*[:=][[:space:]]*["'"'"']?[A-Za-z0-9_-]{8,}' ops --include='*.yml' --include='*.yaml' --include='*.json' | grep -v '\${' | grep -viE 'cryptopanic_token"|_masked|has_crypto' | grep -q .; then echo "--- FAIL: §ENH-A2/A3 ops/ 出现明文密钥赋值（只准 ENV 变量占位）"; exit 1; fi
echo "ok - §ENH 增强批 A+B 专项守卫通过（行为锁 7 组 + 静态锁 26 道〔等值锁 1：slipFracs=2；负锁 5：WalkForward 占位 / config 工厂默认 / Go 依赖红线 / pages 试点越界 / ops 明文密钥；ops 解析锁 3；余为正锁/定点锁〕）"

echo "==> 61 §MR 市场实际改造批：数据面/交易面拆分 + 7×24 维护节拍 + NYSE 规则日历 + 清扫按市场（2026-09-23 PLAN_MARKET_REALITY）..."
# 背景（docs/PLAN_MARKET_REALITY_20260923.md §MR-1/2/3）：旧口径下 binance.enabled 一开关同时
# 决定「装配观测链」与「允许真下单」，无凭证即整链关闭——本地/演示环境永远空态；维护节拍
# 挂在 CN 时段门上，美股休市语义用 weekday 近似、跨日清扫用北京日界，且 SweepStaleBuyOrders
# 无市场过滤会跨市场误伤。本批：data_plane 拆平面、MR-2 独立 60s ticker、MR-3 规则化 NYSE
# 节假日/半日 + 市场时区日界 + 清扫带 market 键。
# 行为锁①：§MR-1 数据面四链（无凭证合法 / 子开关全关拒 / BrokerEnabled=true 且 TradingActive=false / enabled 仍强制凭证）
go test -count=1 ./internal/config/ -run 'TestBinanceDataPlane' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §MR-1 data_plane 行为回归未过"; exit 1; }
go test -count=1 ./internal/server/ -run 'TestBinanceConfigDataPlane' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §MR-1 data_plane HTTP 端点回归未过"; exit 1; }
# 行为锁②：§MR-3 NYSE 规则日历（10 节假日+位移+受难日；半日 13:00/独立日前 16:00；EXTENDED 半日 17:00 收）
go test -count=1 ./internal/data/ -run 'TestUSMarketCalendar|TestUSSessionActiveHalfDay' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §MR-3 US 日历/半日行为回归未过"; exit 1; }
# 行为锁③：§MR-3 跨日清扫按市场隔离 + trading 侧原 C1b 回归不破
go test -count=1 ./internal/store/ -run 'TestSweepStaleBuyOrders' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §MR-3 SweepStaleBuyOrders 市场隔离回归未过"; exit 1; }
go test -count=1 ./internal/trading/ -run 'TestSweepOrdersStaleBuyUnconditional' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §C1b 清扫接线回归未过"; exit 1; }
# ---- 静态锁：MR-1 配置契约三处 + 语义判定 ----
grep -q 'json:"data_plane"' internal/config/binance.go || { echo "--- FAIL: §MR-1 config 段 data_plane 字段丢失"; exit 1; }
grep -q '"data_plane"' internal/server/binance_config.go || { echo "--- FAIL: §MR-1 GET 视图不再下发 data_plane"; exit 1; }
grep -q 'req.DataPlane != nil' internal/server/binance_config.go || { echo "--- FAIL: §MR-1 POST 不再合并 data_plane"; exit 1; }
grep -q 'binance.data_plane=true' internal/config/binance.go || { echo "--- FAIL: §MR-1 validate「数据面须≥1子开关」规则丢失"; exit 1; }
grep -qE 'TradingActive\(\) bool[[:space:]]+\{ return v\.Cfg\.Enabled' internal/config/binance.go || { echo "--- FAIL: §MR-1 TradingActive 判定被改写（真交易必须只认 enabled）"; exit 1; }
grep -q '(bnCfg.Enabled || bnCfg.DataPlane) && !opts.ShadowExec' internal/engine/registry.go || { echo "--- FAIL: §MR-1 装配门未接数据面或丢了 ShadowExec 旁路"; exit 1; }
grep -q 'view.TradingActive() && view.Cfg.APIKey != "" && view.Cfg.APISecret != ""' internal/engine/registry.go || { echo "--- FAIL: §MR-1 真执行器门不再要求 TradingActive+双凭证"; exit 1; }
# 负锁：执行器门绝不得回退到 BrokerEnabled（平面含数据面，会把观测面接上真下单=红线）
if grep -q 'BrokerEnabled() && view.Cfg.APIKey' internal/engine/registry.go; then echo "--- FAIL: §MR-1 真执行器误用 BrokerEnabled（数据面可下单=红线）"; exit 1; fi
# MR-1 前端锁：配置面板 data_plane 开关三接线（提交体 + 开关本体）
grep -q 'data_plane: form.data_plane' web/src/components/BinanceConfigPanel.jsx || { echo "--- FAIL: §MR-1 前端保存体不再提交 data_plane"; exit 1; }
grep -q "set('data_plane', v)" web/src/components/BinanceConfigPanel.jsx || { echo "--- FAIL: §MR-1 前端 data_plane 开关未接线"; exit 1; }
# ---- 静态锁：MR-2 维护节拍（Engine 单次入口 + main.go 独立 60s ticker，7×24 不经 CN 时段门）----
grep -q 'func (e \*Engine) MaintenanceBinanceOnce' internal/engine/engine.go || { echo "--- FAIL: §MR-2 MaintenanceBinanceOnce 入口丢失"; exit 1; }
grep -q 'bnMaint := time.NewTicker(60 \* time.Second)' cmd/quant/main.go || { echo "--- FAIL: §MR-2 币安维护 60s 独立 ticker 丢失"; exit 1; }
grep -q 'e.MaintenanceBinanceOnce(time.Now())' cmd/quant/main.go || { echo "--- FAIL: §MR-2 ticker 未驱动 MaintenanceBinanceOnce"; exit 1; }
# ---- 静态锁：MR-3 日界键 + 市场过滤 + Easter 月基修正 ----
grep -q 'data.Session(c.MarketKey()).Loc()' internal/trading/controller.go || { echo "--- FAIL: §MR-3 清扫日界不再走市场会话时区（回退北京/UTC 日界）"; exit 1; }
grep -A8 'func (d \*DB) SweepStaleBuyOrders' internal/store/real_positions.go | grep -q 'AND market=?' || { echo "--- FAIL: §MR-3 SweepStaleBuyOrders 市场过滤丢失（跨市场误伤复活）"; exit 1; }
# Easter 月基修正必须在位（匿名格里高利历月序 0=三月，缺 +3 则受难日整体错位一年）
grep -q '/31+3' internal/data/market_session.go || { echo "--- FAIL: §MR-3 easterSunday 月基 +3 修正丢失（受难日错位）"; exit 1; }
# 行为锁④：§MR-EDGAR-ENC EDGAR Atom 声明 ISO-8859-1 必须可解（现网 US 事件腿曾因 CharsetReader nil 恒失败）
go test -count=1 ./internal/data/ -run 'TestEdgarISO8859CharsetDecode' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §MR-EDGAR-ENC Latin-1 解码回归未过"; exit 1; }
grep -q 'dec.CharsetReader = latin1ToUTF8Reader' internal/data/edgar.go || { echo "--- FAIL: §MR-EDGAR-ENC CharsetReader 注入丢失（xml.Unmarshal 裸奔=US 事件腿全灭复活）"; exit 1; }
echo "ok - §MR 市场实际改造批专项守卫通过（行为回归 5 组 + 静态锁 17 道〔含 MR-1 执行器误用负锁〕）"

echo "==> 62 §MR-4/战法批/CN-MASTER：做空+合约四链派发、纸面柜台、EDGAR 映射、A股总开关（2026-09-23 PLAN_MARKET_REALITY §MR-4A/4B + §战法批 + §CN-MASTER）..."
# 背景：本批把「战法→信号→下单」在做多/做空 × 现货/合约四链全线打通（无密钥纸面柜台承载），
# 加 EDGAR CIK→ticker 映射兜底、派发摘要进状态端点/设置页，并以 rules.cn.enabled（缺省关）
# 把 A股装配腿做成总开关。行为锁按包分组跑本批测试族；静态锁钉死"门在位且没把币安链关进门里"。
# ---- 行为锁①：§CN-MASTER 配置默认关 + /api/status 双向旗标 ----
go test -count=1 ./internal/config/ -run 'TestCNMaster' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §CN-MASTER 配置出厂默认关回归未过"; exit 1; }
go test -count=1 ./internal/server/ -run 'TestStatusCNMaster' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §CN-MASTER 状态旗标回归未过"; exit 1; }
# ---- 行为锁②：§MR-4B/§战法批-1 配置域（合约 URL/派发校验/纸面卫生/打分键全有或全无）----
go test -count=1 ./internal/config/ -run 'TestFutures|TestValidateMR4BMatrix|TestValidateDispatch|TestValidatePaperHygiene|TestValidateLLMKeys|TestPaperActive|TestDispatchEffective' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §MR-4/战法批 配置域回归未过"; exit 1; }
# ---- 行为锁③：§战法批-4 派发核四链（多/空×现货/合约+止盈止损+关闸零发+幂等键）----
go test -count=1 ./internal/engine/ -run 'TestDispatch|TestAsyncDispatcher' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §战法批-4 派发链回归未过"; exit 1; }
# ---- 行为锁④：§战法批-2 无密钥纸面柜台四方向 + §MR-4B 资金费/合约回报 ----
go test -count=1 ./internal/trading/ -run 'TestPaper' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §战法批-2 纸面柜台回归未过"; exit 1; }
go test -count=1 ./internal/trading/ -run 'TestReporterFutures|TestReporterFunding|TestFuturesForkNegativeLocks|TestBinanceSideMR4|TestReporterMR4' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §MR-4B 合约回报/方向映射回归未过"; exit 1; }
# ---- 行为锁⑤：§MR-4A 风控做空闸 + 账本短向 + §战法批-3 事件打分/空头腿 + EDGAR 映射 ----
go test -count=1 ./internal/risk/ -run 'TestGateMR4' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §MR-4A/4B 风控闸回归未过"; exit 1; }
go test -count=1 ./internal/store/ -run 'TestMR4|TestMR4BFunding' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §MR-4 账本/资金费回归未过"; exit 1; }
go test -count=1 ./internal/data/ -run 'TestScoreXEvent|TestParseSentimentJSON|TestXEventScorer|TestXEventLLMClient' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §战法批-3 事件打分器回归未过"; exit 1; }
go test -count=1 ./internal/data/ -run 'TestEdgarTicker|TestEdgarPaddedCIKKey' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §MR-EDGAR-TKR CIK→ticker 映射回归未过"; exit 1; }
go test -count=1 ./internal/strategies/xasset/ -run 'Bear' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §战法批-3 xasset 空头腿回归未过"; exit 1; }
# ---- 行为锁⑥：§战法批-5 状态端点 dispatch 节（缺省省略/形状/按账号）----
go test -count=1 ./internal/server/ -run 'TestBinanceStateDispatch' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §战法批-5 dispatch 节回归未过"; exit 1; }
# ---- 静态锁：§CN-MASTER 装配门五处包裹 + 币安节拍不在门内（红线）----
grep -q 'cnEnabled := cfgMgr.Rules.CN.Enabled' cmd/quant/main.go || { echo "--- FAIL: §CN-MASTER boot 快照丢失"; exit 1; }
grep -q 'srv.SetCNMaster(cnEnabled)' cmd/quant/main.go || { echo "--- FAIL: §CN-MASTER 旗标未注入 server"; exit 1; }
grep -qE '"cn_master":[[:space:]]+s\.cnMaster' internal/server/handlers_fix.go || { echo "--- FAIL: §CN-MASTER /api/status 字段丢失"; exit 1; }
grep -q 'A股主时段循环未启动' cmd/quant/main.go || { echo "--- FAIL: §CN-MASTER 常驻待命腿丢失（关链时进程会直接退出）"; exit 1; }
grep -q 'registry.GetOrCreate(authMgr.AdminID())' cmd/quant/main.go || { echo "--- FAIL: §CN-MASTER 关链时运营引擎预建丢失（重启后派发停摆）"; exit 1; }
# 红线负锁：币安维护/派发 tick 两行必须裸挂在 bnMaint 循环里——被 cnEnabled 包住=关 A股连带杀币安链
if awk '/bnMaint := time.NewTicker/,/^\t\}\(\)$/' cmd/quant/main.go | grep -q 'cnEnabled'; then echo "--- FAIL: §CN-MASTER 币安 7×24 节拍被总开关误包（红锁）"; exit 1; fi
# ---- 静态锁：§战法批 派发接线 + 前端往返 + EDGAR 映射锚 ----
grep -q 'func (e \*Engine) DispatchBinanceOnce' internal/engine/binance_dispatch.go || { echo "--- FAIL: §战法批-4 派发单次入口丢失"; exit 1; }
grep -q 'e.DispatchBinanceOnce(time.Now())' cmd/quant/main.go || { echo "--- FAIL: §战法批-4 维护节拍未挂派发"; exit 1; }
grep -q 'body.dispatch = {' web/src/components/BinanceConfigPanel.jsx || { echo "--- FAIL: §战法批-5 设置页不再提交 dispatch 七键"; exit 1; }
grep -q '未派发过' web/src/components/BinanceStatusCard.jsx || { echo "--- FAIL: §战法批-5 状态卡「从未派发」如实呈现丢失"; exit 1; }
grep -q 'EDGARTickersPath = "/files/company_tickers.json"' internal/data/edgar_tickers.go || { echo "--- FAIL: §MR-EDGAR-TKR 映射表路径丢失"; exit 1; }
echo "ok - §MR-4/战法批/CN-MASTER 专项守卫通过（行为回归 12 组 + 静态锁 12 道〔含币安节拍误包负锁〕）"

echo "==> 63 §市场分家-1：顶部市场切换全局生效 + /api/binance/quote 现价腿 + 币安 WS 帧 E 字段碰撞修复（2026-09-24）..."
# 背景：用户缺陷「切到美股/加密货币后全站仍是 A股信息」。后端补 US/CRYPTO 单票现价端点
# （feed 优先→REST TTL 兜底，CN/缺 market 一律 400 分轨）；前端把全局开关接进 Signals/
# Watchlist/Paper/Dashboard/Hotspot/EmotionReview/MsgCenter/Consult/命令面板/详情抽屉。
# 附带实锤修复：币安真实帧的 "e"（事件名字符串）被 Go json 大小写不敏感回退灌进
# struct 的 int64 E 字段 → miniTicker/ticker 每帧解析失败（订阅成功、永远 0 命中）。
# ---- 行为锁①：§市场分家-1 现价源三态（feed 命中/REST 兜底+TTL/诚实失败）+ market 闸 ----
go test -count=1 ./internal/engine/ -run 'TestQuoteSource|TestEngineQuote' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §市场分家-1 现价源回归未过"; exit 1; }
go test -count=1 ./internal/server/ -run 'TestQuoteEndpoint' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §市场分家-1 /api/binance/quote 端点回归未过"; exit 1; }
# 行为锁②：币安现货帧解析回归（真 "e" 字段帧必须可解——回归即整链 0 命中复辟）
go test -count=1 ./internal/data/ -run 'TestParseSpot' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §市场分家-1 现货帧解析回归未过"; exit 1; }
# 行为锁③：前端全局过滤 8 例（面板代码识别/四页 tab 门/自选添加入口拒收/抽屉现价腿双向互斥）
( cd web && npx vitest run src/__tests__/mkt_split_p1.test.jsx >/dev/null 2>&1 ) || { echo "--- FAIL: §市场分家-1 前端市场过滤 vitest 未通过"; exit 1; }
# ---- 静态锁：后端接线三点 + 碰撞吸收字段 ----
grep -q '"GET /api/binance/quote"' internal/server/server.go || { echo "--- FAIL: §市场分家-1 现价路由丢失"; exit 1; }
grep -q 'srv.SetQuoteSource(' cmd/quant/main.go || { echo "--- FAIL: §市场分家-1 现价闭包未注入（端点恒「链未装配」）"; exit 1; }
grep -q 'e.SetBinanceQuoteFeeds(bnFeeds)' internal/engine/registry.go || { echo "--- FAIL: §市场分家-1 feed 池未接引擎（现价只剩 REST 一条腿）"; exit 1; }
# E 字段碰撞修复双锁：两个解析 struct 各留一个 Ev 吸收字段，删一个=对应帧族复活全灭
[ "$(grep -c 'Ev string' internal/data/binance_spot_parse.go)" -ge 2 ] || { echo "--- FAIL: §市场分家-1 \"e\" 事件名吸收字段丢失（帧解析碰撞复活）"; exit 1; }
# ---- 静态锁：前端分轨与入口守卫 ----
grep -q "if (mkt === 'CN') {" web/src/components/StockDetailDrawer.jsx || { echo "--- FAIL: §市场分家-1 抽屉现价腿市场分轨丢失（新市场回退 CN 四级链）"; exit 1; }
grep -q '仅支持 A股' web/src/pages/Watchlist.jsx || { echo "--- FAIL: §市场分家-1 自选池添加入口非 CN 拒收丢失（BTCUSDT 混池路径复活）"; exit 1; }
grep -q 'signals-market-empty' web/src/pages/Signals.jsx || { echo "--- FAIL: §市场分家-1 信号页非 CN 去向提示卡丢失"; exit 1; }
grep -q 'consult-market-locked' web/src/pages/Consult.jsx || { echo "--- FAIL: §市场分家-1 咨询页非 CN 拒答锁丢失（A股人设静默作答复活）"; exit 1; }
# 负锁：命令面板旧「纯六位才算代码」判定不得复活（新市场代码被它挡在门外）
if grep -Fq '/^\d{6}$/.test(query.trim())' web/src/components/CommandPalette.jsx; then echo "--- FAIL: §市场分家-1 命令面板旧六位代码判定复活"; exit 1; fi
# 负锁：Watchlist 添加入口 api.addWatchlist 之前必须已过市场拒收闸（守卫后置=混池条目先入库再报错）
if awk '/addMkt !== .CN./{guard=NR} /api\.addWatchlist\(code\)/{if(!guard||guard>NR){print "BAD";exit}}' web/src/pages/Watchlist.jsx | grep -q BAD; then echo "--- FAIL: §市场分家-1 自选添加守卫被挪到入库调用之后（顺序红锁）"; exit 1; fi
echo "ok - §市场分家-1 专项守卫通过（行为回归 3 组 + vitest 文件锁 1 + 静态锁 8 道〔含面板旧判定/守卫顺序 2 负锁〕）"

# ══════════════════════════════════════════════════════════════════════════════
# 64：§抄母仓-2（2026-09-24）纪律引擎延持态「终态失明」止血（母仓 §P0-C / bddcccb）
# English: section 64 locks the ported §P0-C discipline blindness fix.
# ══════════════════════════════════════════════════════════════════════════════
echo "==> 64 §DISCIPLINE 延持态终态失明止血 + 重评估非对称守卫（抄母仓 §P0-C，2026-09-24）..."
# 现象：纪律引擎一旦置位 Settled&&!Confirmed（延持），下一轮直接在函数顶部 early-return 不再出卡，
# 价格继续跌破更深一档线也视而不见 = 终态失明（该走的仓位永远不走，资金级漏卖）。
# 修法：延持态每轮重评估，出卡需「本轮仍破线且不轻于原始锁定线」（reevalAllowsSettle）；
# 反向情形（止损延持后反弹进止盈区）一律不收卡，避免把失明换成「按反弹后的止盈价挂止损标签卖」。
go test -count=1 ./internal/trading/ -run 'TestDiscipline' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §P0-C 纪律引擎回归未过（含对称行为锁 3 例）"; exit 1; }
grep -q 'func reevalAllowsSettle(' internal/trading/discipline.go || { echo "--- FAIL: §P0-C 重评估准入判据丢失"; exit 1; }
grep -q 'if extendHold && !reevalAllowsSettle(st.Line, line)' internal/trading/discipline.go || { echo "--- FAIL: §P0-C 准入判据未被调用（定义了个寂寞）"; exit 1; }
grep -q 'func isLossLine(' internal/trading/discipline.go || { echo "--- FAIL: §P0-C 损失族判定丢失（止盈延持跌进损失线无法识别）"; exit 1; }
# 负锁：延持态无条件 early-return 的旧形态绝迹（同族教训：只在注释里说改过、代码没改）。
if grep -nE 'if st\.Settled && !st\.Confirmed \{[[:space:]]*return' internal/trading/discipline.go | grep -q .; then
	echo "--- FAIL: §P0-C 延持态又无条件 early-return（终态失明复活）"; exit 1; fi
# 正锁：已确认态短路仍在（Settled && Confirmed 重放处置单，修复不得顺手放宽成重复出卡）
grep -q 'if st.Settled && st.Confirmed {' internal/trading/discipline.go || { echo "--- FAIL: §P0-C 已确认态短路丢失（确认后会逐轮重复出卡）"; exit 1; }
echo "ok - §DISCIPLINE 专项守卫通过（行为锁 TestDiscipline 全组 + 静态锁 4 道 + 负锁 1 道）"

# ══════════════════════════════════════════════════════════════════════════════
# 65~66：§抄母仓-1（2026-09-24）测试栈「误杀别人 / 打错对象」三修（母仓 cf3be1f §PICKILL-SCOPE §UAT-PORTS）
# English: sections 65-66 lock the ported UAT-stack scoping fixes (scoped kill + mock URL single source).
# ══════════════════════════════════════════════════════════════════════════════
echo "==> 65 §PICKILL-SCOPE UAT 自举的兜底 kill 只圈本项目拉起的进程（抄母仓 cf3be1f）..."
# 现象（母仓 2026-09-23 实测误伤、本仓 2026-09-24 走查同形复犯）：`uat_bootstrap.sh stop` 的兜底
#       `pkill -f "quant.*QUANT_ADDR=:18080"` 会杀掉同机另一份 checkout 的 UAT 引擎。
# 根因：macOS 的 pkill/pgrep -f 把**进程环境变量一并计入匹配串**（`pgrep -fl` 输出里命令行后拖着整段 env 即证），
#       于是"端口名"成了跨项目的通配；两份 checkout 共用 18080/18789 端口约定 ⇒ 一条 stop 停掉别人的栈。
# 前置：kill 判据必须含数据目录绝对路径（$PIDDIR 由 $DATA_DIR 派生，checkout 间天然不同），
#       且不能再按 env 里的端口名匹配。
if ! grep -qF 'pkill -f -- "$PIDDIR/quant"' scripts/uat_bootstrap.sh; then
  echo "--- FAIL: 兜底 kill 不再按本项目二进制绝对路径匹配（跨项目误伤的入口）"; exit 1; fi
if ! grep -qF 'pkill -f -- "$PIDDIR/qmt-mock"' scripts/uat_bootstrap.sh; then
  echo "--- FAIL: 假柜台的兜底 kill 缺失（只清引擎会留下占端口的 mock）"; exit 1; fi
# §VITE-ORPHAN：pid 记的是 npx 外壳，真占端口的 node(.bin/vite) 会活下来让下次 --strictPort 失败。
if ! grep -qF 'pkill -f -- "$ROOT/web/node_modules/\.bin/vite --port ${FRONT_PORT}' scripts/uat_bootstrap.sh; then
  echo "--- FAIL: vite 子进程兜底 kill 缺失（stop 后再 up 会卡在端口占用）"; exit 1; fi
# 负锁：端口名/env 形式的匹配串不得复活。只判**真正执行的 pkill 行**（行首无 #），
# 否则"描述旧写法为何危险"的注释本身会被这条锁打死（§静态负锁自伤形态）。
if grep -E '^[[:space:]]*pkill' scripts/uat_bootstrap.sh | grep -q 'QUANT_ADDR'; then
  echo "--- FAIL: pkill 又按 env 端口名匹配（会在任意 checkout 间互杀）"; exit 1; fi
# 行为锁（2026-09-24 实跑同形验证）：造三个诱饵进程——命令行分别带「别家 checkout 的 vite」「别家
# checkout 的 quant」「本仓 PIDDIR 的 quant」，跑一遍兜底 kill：别家两个必须活、本家必须死。
# 只判路径不判端口才成立；诱饵用 exec -a 伪造 argv[0]，stdout 重定向到 /dev/null 免得命令替换等管道 EOF。
decoy() { sh -c 'exec -a "$0" sleep 20' "$1" >/dev/null 2>&1 & echo $!; }
DEC_A="$(decoy "/Users/zhangzifei/Desktop/quant-trading-v2/web/node_modules/.bin/vite --port 5173 --strictPort")"
DEC_B="$(decoy "/Users/zhangzifei/Desktop/quant-trading-v2/.uat-data/pids/quant -addr :18080")"
DEC_M="$(decoy "$PWD/.uat-data/pids/quant -addr :18080")"
sleep 0.3
pkill -f -- "$PWD/web/node_modules/\.bin/vite --port 5173( |$)" 2>/dev/null || true
pkill -f -- "$PWD/.uat-data/pids/quant" 2>/dev/null || true
sleep 0.6
for d in "$DEC_A" "$DEC_B"; do
  kill -0 "$d" 2>/dev/null || { echo "--- FAIL: 兜底 kill 误伤了别家 checkout 的进程（跨项目误杀复活）"; kill "$DEC_M" 2>/dev/null; exit 1; }
done
if kill -0 "$DEC_M" 2>/dev/null; then
  kill "$DEC_M" 2>/dev/null
  echo "--- FAIL: 兜底 kill 没圈住本仓 PIDDIR 进程（锁与实现脱节）"; exit 1
fi
kill "$DEC_A" "$DEC_B" 2>/dev/null || true
echo "ok - §PICKILL-SCOPE 守卫通过（同源路径锁 3 + env 匹配负锁 1 + 跨 checkout 误杀行为锁 1）"

echo "==> 66 §UAT-PORTS e2e 打的是「本次自举的那套栈」，不是同机任意一套（抄母仓 cf3be1f）..."
# 现象（母仓 2026-09-23）：同机并存第二份 checkout 已占 18080/18789，本项目自举只能挪端口；
#       而 spec 里硬编 http://127.0.0.1:18789 + 「连不上 mock 就 skip」⇒ L1-2/QS-2 打到**别人那套 mock**
#       上拿到 200 照样绿。断言的对象都不是本仓库代码。
# 前置：mock 地址单一来源 = E2E_MOCK_URL（自举脚本与 CI 各自导出）；且"传了该变量=跑在自举栈上"
#       时连不上必须判红，软跳过只留给外部部署场景。
grep -qF 'const MOCK_URL = process.env.E2E_MOCK_URL' web/e2e/uat_full.spec.mjs \
  || { echo "--- FAIL: mock 地址不再单源自 E2E_MOCK_URL"; exit 1; }
grep -qF 'function mockUnavailableOrFail' web/e2e/uat_full.spec.mjs \
  || { echo "--- FAIL: mock 不可达的两种口径（自举=红／外部=skip）没有收在一处"; exit 1; }
[ "$(grep -cF 'if (!resp) mockUnavailableOrFail(lastErr)' web/e2e/uat_full.spec.mjs)" -eq 2 ] \
  || { echo "--- FAIL: mockUnavailableOrFail 接点数≠2（L1-2/QS-2 有一处仍在软跳过）"; exit 1; }
grep -qF 'export E2E_MOCK_URL=http://127.0.0.1:${MOCK_PORT}' scripts/uat_bootstrap.sh \
  || { echo "--- FAIL: 自举脚本不再按本次 MOCK_PORT 导出 E2E_MOCK_URL（spec 会退回默认口）"; exit 1; }
grep -qF 'E2E_MOCK_URL="http://127.0.0.1:${MOCK_PORT}"' scripts/uat_bootstrap.sh \
  || { echo "--- FAIL: run 模式未把 E2E_MOCK_URL 传给 playwright"; exit 1; }
grep -qF 'E2E_MOCK_URL: http://127.0.0.1:18789' .github/workflows/nightly-e2e.yml \
  || { echo "--- FAIL: CI 未导出 E2E_MOCK_URL（CI 本来就要求零 skip，软跳过在这里该失效）"; exit 1; }
# 负锁①：不得再有直连硬编端口的请求/断言字面量（注释里描述历史形态不在此列 ⇒ 只查调用式）。
if grep -qE "request\.get\('[^']*18789|toContainText\('127\.0\.0\.1:18789" web/e2e/uat_full.spec.mjs; then
  echo "--- FAIL: spec 里又出现硬编 18789 的调用"; exit 1; fi
# 负锁②：`test.skip(!resp...)` 的无条件软跳过不得复活（它会连"栈根本没起来"一起洗白）。
if grep -q 'test.skip(!resp' web/e2e/uat_full.spec.mjs; then
  echo "--- FAIL: 复活了无条件 skip(!resp)"; exit 1; fi
# 语法锁：spec 与脚本至少可解析（改的是 e2e 骨架，语法错会让 nightly 整栈假红）。
node --check web/e2e/uat_full.spec.mjs || { echo "--- FAIL: uat_full.spec.mjs 语法未通过"; exit 1; }
bash -n scripts/uat_bootstrap.sh || { echo "--- FAIL: uat_bootstrap.sh 语法未通过"; exit 1; }
echo "ok - §UAT-PORTS 守卫通过（单源锁 1 + 口径锁 2 + 导出锁 3 + 负锁 2 + 语法锁 2）"

# ══════════════════════════════════════════════════════════════════════════════
# 67：§抄母仓-3（2026-09-24）/api/notify-test 抬档 + 频控 + 审计（母仓 bddcccb §N-2）
# English: section 67 locks the ported notify-test admin gate, 60s throttle and opslog audit.
# ══════════════════════════════════════════════════════════════════════════════
echo "==> 67 §NOTIFYADMIN 全局推送探测端点抬档 + 频控（抄母仓 §N-2，2026-09-24）..."
# 现象：/api/notify-test 在 §C9 从空 stub 升级为**逐通道实弹探测**（消息级 LevelHigh），
# 但档位仍挂在普通登录态下 ⇒ 任何登录成员一次 POST 就能向 owner 的全部推送通道发实弹，
# 用噪声淹没真告警（告警通道本身成为攻击面）。修法：抬 adminMiddleware + 全进程 60s 最小间隔
# （探测打的是 server 级单例通道，按账号限流挡不住多管理员合流）+ 全路径 opslog 审计。
go test -count=1 ./internal/server/ -run 'TestNotifyTestAdminOnlyAndRateLimited' 2>&1 | grep -q '^ok' || { echo "--- FAIL: §N-2 notify-test 行为锁未过（成员 403/首击 200/二击 429）"; exit 1; }
grep -q '"POST /api/notify-test", s.adminMiddleware' internal/server/server.go || { echo "--- FAIL: §N-2 notify-test 抬档丢失（成员可轰炸 owner 推送通道）"; exit 1; }
if grep -q '"POST /api/notify-test", s.authMiddleware' internal/server/server.go; then
  echo "--- FAIL: §N-2 notify-test 又回退 authMiddleware"; exit 1; fi
grep -q 'Retry-After' internal/server/handlers_fix.go || { echo "--- FAIL: §N-2 频控未回 Retry-After（429 无语义，调用方盲重试）"; exit 1; }
# 审计留痕：受理与限流两条路径都必须落 opslog（事后要能回答"谁在什么时候打的"）。
[ "$(grep -c 'opslog.Audit("notify_test"' internal/server/handlers_fix.go)" -ge 4 ] || { echo "--- FAIL: §N-2 opslog 审计点数<4（noop/无限流/无通道/实弹有一处无人知晓）"; exit 1; }
# 空 stub 绝迹（§C9 的实探升级不得被回退掉）。
if grep -A 3 'func (s \*Server) handleFixNotifyTest' internal/server/handlers_fix.go | grep -q 'writeJSON(w, 200, map\[string\]string{"status": "ok"})$'; then
  echo "--- FAIL: §N-2 notify-test 又回退成永远 ok 的空 stub"; exit 1; fi
echo "ok - §NOTIFYADMIN 专项守卫通过（行为锁 1 组 + 静态锁 4 道 + 负锁 2 道）"

# ══════════════════════════════════════════════════════════════════════════════
# 68：§抄母仓-5（2026-09-24）指标型告警出口 + 7×24 评估节拍（母仓 bddcccb §高-3 / §ALERTDRIVE）
# English: section 68 locks the ported metric-alert egress and its 24/7 evaluation heartbeat.
# ══════════════════════════════════════════════════════════════════════════════
echo "==> 68 §ALERTROUTE 指标告警出站路由 + 7×24 评估节拍（抄母仓 §高-3，2026-09-24）..."
# 现象（母仓锤实、本仓同构）：alerter.go 的阈值规则每轮都在评估，但 RunAlertEvaluation 拿到事件后
# 只 log.Printf 就结束 ⇒ 「规则触发 = 没有任何人知道」，而且 R7 验收单当初勾过 ✅（声称已做）。
# 修法：AlertRoute(push/daily/log) 路由表 + 冷却窗（触发 30min／恢复 10min）+ alert/resolved 成对
# （被冷却窗挡下的销案挂起、到期补发，绝不只报不销）+ 跨日日汇总补发 + SetAlertSink 依赖倒置注入
# （metrics 包不 import notify，避免 engine→metrics→notify→engine 注入环）；未接线时首轮一条明确
# Warn + 每条兜底日志——告警系统自己哑了必须吵。范围照 owner 口径收窄：只接指标型，事件型腿不动。
#
# §ALERTDRIVE（本批相对母仓的增量，理由如下，不是顺手加的）：本仓唯一调用点在内
# internal/engine/scoring_loop.go 的 A股 scoreCycle，而 §CN-MASTER 出厂把 A股总开关关成 false
# ——那条节拍在缺省配置下根本不跑，等于「出口接好了、没人合闸」，§高-3 的静默失效换个形态复发。
# 因此把评估兜底节拍挂到 7×24 的币安维护拍（cmd/quant/main.go bnMaint）。两个调用点并存后，
# Alerter.states 是无锁 map，CN 开态下并发进入是 Go runtime 级 fatal，故串行化做在包内（evalMu）。
if [ "$(go test -count=1 ./internal/metrics/ -run 'TestPushRule|TestResolved|TestDailySummary|TestUnwiredSink|TestSinkReceives|TestRoutingCovers|TestRunAlertEvaluation|TestConfigureAlertRouting' -v 2>&1 | grep -c '^--- PASS')" -lt 8 ]; then
  echo "--- FAIL: §高-3 出站行为锁跑到 8 例以下（-run 过滤器打空=恒绿，先确认用例名）"; exit 1; fi
# -race 锁：先落变量再判定（直接 `go test | grep -q` 时 grep 提前退场会让上游吃 SIGPIPE，
# 在 set -o pipefail 下变成恒失败——本段实跑就是这样假红过一次）。
race_out=$(go test -race -count=1 ./internal/metrics/ 2>&1 || true)
printf '%s\n' "$race_out" | grep -q '^ok' \
  || { echo "--- FAIL: §高-3 §ALERTDRIVE 并发/竞态锁未过（-race）"; exit 1; }
grep -q 'func DefaultAlertRouting()' internal/metrics/alert_routing.go \
  || { echo "--- FAIL: 默认路由表丢失（规则无出口=评估了但没人知道）"; exit 1; }
grep -q 'metrics.SetAlertSink(' cmd/quant/main.go \
  || { echo "--- FAIL: 生产进程未注入 sink（路由表成为死码）"; exit 1; }
grep -q 'AlertSinkInjected()' internal/metrics/alert_routing.go \
  || { echo "--- FAIL: 未接线自检丢失"; exit 1; }
# p1 必须走既有高优通道（否则"熔断中"和"日汇总"同一优先级，等于没有优先级）。
awk '/metrics\.SetAlertSink\(/,/^\t\}\)$/' cmd/quant/main.go | grep -q 'notify.LevelHigh' \
  || { echo "--- FAIL: 注入闭包不再按 p1→LevelHigh 分档（必推类混入普通推送）"; exit 1; }
# §ALERTDRIVE 静态锁①：评估入口自带互斥（两个驱动点并存的前提）。
grep -q 'var evalMu sync.Mutex' internal/metrics/alerter.go \
  || { echo "--- FAIL: 评估互斥锁丢失（scoreCycle 与 bnMaint 并发进无锁 states map）"; exit 1; }
awk '/^func RunAlertEvaluation\(\)/{f=1} f{print} f&&/^}$/{exit}' internal/metrics/alerter.go \
  | grep -q 'evalMu.Lock()' \
  || { echo "--- FAIL: RunAlertEvaluation 未持 evalMu（锁定义了个寂寞）"; exit 1; }
# §ALERTDRIVE 静态锁②：7×24 兜底节拍真的挂在币安维护拍里（而不是挂在 A股时段门后）。
if ! awk '/bnMaint := time.NewTicker/,/^\t\}\(\)$/' cmd/quant/main.go | grep -q 'metrics.RunAlertEvaluation()'; then
  echo "--- FAIL: §ALERTDRIVE 币安 7×24 维护拍不再驱动告警评估（A股开关一关告警就没人评估）"; exit 1; fi
# 负锁：告警评估的兜底节拍不得被 A股总开关包住（与 §MR-2/§CN-MASTER 红锁同族）。
if awk '/bnMaint := time.NewTicker/,/^\t\}\(\)$/' cmd/quant/main.go | grep -q 'cnEnabled'; then
  echo "--- FAIL: §ALERTDRIVE 币安维护拍被 cnEnabled 误包（关 A股连带杀告警心跳，红锁）"; exit 1; fi
# 负锁：旧形态「拿到事件只 log.Printf 就结束」不得复活（同族教训：只在注释里说改过、代码没改）。
if awk '/^func runAlertEvaluationLocked\(\)/{f=1} f{print} f&&/^}$/{exit}' internal/metrics/alerter.go \
     | grep -qE '^\tlog\.Printf\(' ; then
  echo "--- FAIL: 评估主体又退回只写日志不出站（静默失效复活）"; exit 1; fi
echo "ok - §ALERTROUTE 专项守卫通过（行为锁 8 例 + -race + 静态锁 6 道 + 负锁 2 道）"

# ══════════════════════════════════════════════════════════════════════════════
# 69：§抄母仓-4（2026-09-24）每条告警规则必须有真实赋值点（母仓 9ff5061 §DEADGAUGE）
# English: section 69 ports the general dead-rule guard: every alert rule's metric needs a real writer.
# ══════════════════════════════════════════════════════════════════════════════
echo "==> 69 §DEADGAUGE 死规则通用守卫：每条规则量规都要有赋值点（抄母仓 §DEADGAUGE）..."
# 现象：默认告警规则里三条（order_fail_rate / settlement_diff / llm_cooldown）自注册以来
#       全仓找不到一处 SetGauge 赋值 ⇒ 评估器每轮读到 0 ⇒ gt 规则永不触发。这不是"没出过事"，
#       是"出了事也不会响"：p1「交割单对账出现差异」「下单失败率>5%」在现网等价于不存在。
#       同一形态此前被抓过两次（audit N-1 的 quote_staleness_sec、§CB 的 uplink_staleness_sec），
#       每次都靠人肉发现——本段把它变成机器锁。
# 修法：① 三条各接真实源（order_rate.go 用既有累计计数器做 5 分钟窗增量换算、settlement.go 用
#         三方对账三类差异条数之和、scoring_loop 用 llm.Client.KeysInCooldown）；
#       ② 通用守卫：从规则表反解全部 Metric 名，逐条要求非测试代码里存在 SetGauge("<名>") 赋值点；
#       ③ 负锁锁住三个已知假绿形态。
g_trading=$(go test -count=1 ./internal/trading/ -run 'TestSettleDayFeedsDiffGauge|TestSettleDaySkipBranchesWriteZero' 2>&1 || true)
printf '%s\n' "$g_trading" | grep -q '^ok' \
  || { echo "--- FAIL: §DEADGAUGE 交割差异量规行为锁未过"; exit 1; }
if [ "$(go test -count=1 ./internal/metrics/ -run 'TestOrderFailRate|TestRunAlertEvaluationRefreshesDerivedGauge' -v 2>&1 | grep -c '^--- PASS')" -lt 5 ]; then
  echo "--- FAIL: §DEADGAUGE 下单失败率窗口换算行为锁<5 例"; exit 1; fi
if [ "$(go test -count=1 ./internal/llm/ -run 'TestKeysInCooldown' -v 2>&1 | grep -c '^--- PASS')" -lt 2 ]; then
  echo "--- FAIL: §DEADGAUGE LLM 冷却计数行为锁<2 例"; exit 1; fi
if [ "$(go test -count=1 ./internal/engine/ -run 'TestRefreshFeedsLLMCooldownGauge|TestRefreshStalenessGauges' -v 2>&1 | grep -c '^--- PASS')" -lt 3 ]; then
  echo "--- FAIL: §DEADGAUGE 打分链量规刷新行为锁<3 例"; exit 1; fi
grep -q 'SetGauge("settlement_diff_count", int64(len(diff.MissingInLocal)+len(diff.ExtraInLocal)+len(diff.Mismatch)))' internal/trading/settlement.go \
  || { echo "--- FAIL: 交割差异量规不再等于三类条数之和（退化成布尔/单类计数会漏报）"; exit 1; }
grep -q 'func (c \*Client) KeysInCooldown() int' internal/llm/llm.go \
  || { echo "--- FAIL: LLM 冷却计数入口丢失（llm_cooldown 又成死规则）"; exit 1; }
grep -q 'metrics.SetGauge("llm_cooldown_count", llmCool)' internal/engine/scoring_loop.go \
  || { echo "--- FAIL: 打分链未再喂 llm_cooldown_count"; exit 1; }
grep -q 'SetGauge("order_fail_rate_milli", rate)' internal/metrics/order_rate.go \
  || { echo "--- FAIL: 下单失败率量规赋值丢失"; exit 1; }
# 通用守卫：规则表里每条 Metric 都要有赋值点。扫描面排除测试文件（测试直接 SetGauge 造场景，
# 算赋值点就是假绿）与规则/路由定义本体（防有人把赋值塞进规则文件糊弄守卫）。
rule_metrics=$(grep -oE 'Metric: "[a-z0-9_]+"' internal/metrics/alerter.go | sed -E 's/.*"([^"]+)"/\1/')
[ -n "$rule_metrics" ] || { echo "--- FAIL: 规则表解析为空（规则被搬走 = 守卫失明）"; exit 1; }
dead_rules=""
for m in $rule_metrics; do
	# 末尾 `|| true` 是必需的而不是装饰：某条量规没有赋值点时 grep 退出码 1，在
	# `set -euo pipefail` 下会让整个赋值命令失败、脚本**静默中止**（连 FAIL 文案都打不出来，
	# 表现成"跑到 69 就没了"）。判红交给下面的 `[ -n "$dead_rules" ]`，这里只负责数数。
	n=$(find internal cmd -name '*.go' ! -name '*_test.go' ! -name 'alerter.go' ! -name 'alert_routing.go' \
	    -print0 | xargs -0 grep -l "SetGauge(\"$m\"" 2>/dev/null | wc -l | tr -d ' ' || true)
	[ "${n:-0}" -ge 1 ] || dead_rules="$dead_rules $m"
done
if [ -n "$dead_rules" ]; then
	echo "--- FAIL: 以下量规有规则无赋值点（永不触发）：$dead_rules"; exit 1
fi
echo "  ok 通用守卫：$(echo "$rule_metrics" | wc -w | tr -d ' ') 条规则量规全部有赋值点"
# 负锁①：派生量规必须在取快照之前刷新（写在后面 = 每轮读到的都是上一轮值，等于没修）。
locked_body=$(awk '/^func runAlertEvaluationLocked\(\)/{f=1} f{print} f&&/^}$/{exit}' internal/metrics/alerter.go)
pos_refresh=$(printf '%s\n' "$locked_body" | grep -n 'refreshOrderFailRateGauge()' | head -1 | cut -d: -f1)
pos_snap=$(printf '%s\n' "$locked_body" | grep -n 'gaugeSnapshot()' | head -1 | cut -d: -f1)
{ [ -n "$pos_refresh" ] && [ -n "$pos_snap" ] && [ "$pos_refresh" -lt "$pos_snap" ]; } \
	|| { echo "--- FAIL: 刷新未发生在 gaugeSnapshot 之前（读到旧值）"; exit 1; }
# 负锁②：禁止用「累计值直接相除」冒充窗口失败率（早期一次失败会永久挂着 5% 红线）。
if grep -nE 'ordersRejected\.Load\(\) \* 1000 / \(ordersPlaced\.Load\(\) \+ ordersRejected\.Load\(\)\)' internal/metrics/*.go | grep -q .; then
	echo "--- FAIL: 又用全生命周期累计比冒充 5 分钟窗口失败率"; exit 1
fi
# 负锁③：静默跳过分支不得"什么都不写"（不写 = 保留上一轮残值，对账降级日会持续误报）。
if ! grep -q 'SetGauge("settlement_diff_count", 0)' internal/trading/settlement.go; then
	echo "--- FAIL: 对账跳过分支不再清零（残值冒充当日差异）"; exit 1
fi
echo "ok - §DEADGAUGE 专项守卫通过（行为锁 4 组 + 静态锁 4 道 + 通用死规则守卫 1 条 + 负锁 3 道）"

# ══════════════════════════════════════════════════════════════════════════════
# 70：§抄母仓-6（2026-09-24）战法参数保存稀疏 merge + 乐观锁 409（母仓 bddcccb §N-4/§中-6）
# English: section 70 locks the ported sparse-merge strategy-config writer and its 409 optimistic lock.
# ══════════════════════════════════════════════════════════════════════════════
echo "==> 70 §CFGSMASH 战法参数保存：稀疏 merge + 版本冲突 409 +  getter 快照（抄母仓 §N-4，2026-09-24）..."
# 现象（三重叠加，缺一不至于丢参数）：① 前端加载失败被 catch 吞 → 表单落在空对象；
# ② 数字字段 `?? 0` 把「键缺失」渲染成 0（缺失与真实 0 混同）；③ 后端把 body 反序列化成完整
# StrategyConfig 后**全量替换**落盘 ⇒ 一次「加载失败 + 保存」即把五套战法阈值清零、重启救不回。
# 另：GetStrategyConfig 返回内部指针、Set 系无锁写，与打分/热更新并发（-race 可复现）。
# 修法：逐字段 JSON 递归稀疏 merge（没传=保留旧值，要清 0 请明写 0）+ updated_at 乐观锁 409
# + 全部 getter 改快照拷贝、setter 加锁 + 前端三态加载（缺失渲空并保存前必填校验）。
g_cfg=$(go test -count=1 ./internal/config/ -run 'TestMergeStrategyConfig|TestSetStrategyConfig|TestStrategyConfigSaveWhileScoringRace' 2>&1 || true)
printf '%s\n' "$g_cfg" | grep -q '^ok' || { echo "--- FAIL: §N-4 稀疏 merge/乐观锁/并发行为锁未过"; exit 1; }
g_race=$(go test -race -count=1 ./internal/config/ 2>&1 || true)
printf '%s\n' "$g_race" | grep -q '^ok' || { echo "--- FAIL: §CFGSMASH-concurrency -race 未过（getter 又回退成别名活体）"; exit 1; }
g_srv=$(go test -count=1 ./internal/server/ -run 'TestSetStrategyConfig' 2>&1 || true)
printf '%s\n' "$g_srv" | grep -q '^ok' || { echo "--- FAIL: §N-4 端点级行为锁未过（部分键保存/409/坏结构 400）"; exit 1; }
( cd web && npx vitest run src/__tests__/n4_cfg_smash.test.jsx >/dev/null 2>&1 ) \
  || { echo "--- FAIL: §N-4 前端三态加载/409 冲突锁未过（E1/E3/E4 之一失效）"; exit 1; }
grep -q 'func (m \*Manager) MergeStrategyConfig(patch map\[string\]json.RawMessage' internal/config/config.go \
  || { echo "--- FAIL: 稀疏 merge 入口签名变更（回到整 struct 反序列化=缺键即清零）"; exit 1; }
grep -q 'var ErrStrategyVersionConflict' internal/config/config.go || { echo "--- FAIL: 乐观锁哨兵错误丢失"; exit 1; }
grep -q 'UpdatedAt string `json:"updated_at,omitempty"`' internal/config/config.go \
  || { echo "--- FAIL: 版本戳字段丢失（409 无从比对）"; exit 1; }
[ "$(grep -c 'next.UpdatedAt = time.Now().UTC()' internal/config/config.go || true)" -ge 3 ] \
  || { echo "--- FAIL: 版本戳推进点 < 3 处（有写路径不刷新 updated_at，409 形同虚设）"; exit 1; }
# 负锁 1：handler 又整份反序列化到 StrategyConfig（全量替换形态）——函数体内必须仍有 RawMessage 稀疏 merge。
if ! grep -q 'json.RawMessage' <(sed -n '/func (s \*Server) handleSetStrategyConfig/,/^}/p' internal/server/server.go); then
	echo "--- FAIL: handleSetStrategyConfig 不再走稀疏 merge（缺键清零复活）"; exit 1; fi
# 负锁 2：前端 renderField 的 `?? 0` 兜底绝迹（缺失渲染成 0 是本缺陷的第二重）。
if grep -nE '\?\? 0[[:space:]]*\}[[:space:]]*$|value=\{[^}]*\?\? 0\}' web/src/pages/Settings.jsx | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | grep -q .; then
	echo "--- FAIL: 前端又用 ?? 0 渲染缺失字段（空表单被提交成全 0）"; exit 1; fi
echo "ok - §CFGSMASH 专项守卫通过（行为锁 4 组含 -race + 静态锁 4 道 + 负锁 2 道）"

# ══════════════════════════════════════════════════════════════════════════════
# 71~74：2026-09-24 抄母仓「日K复权口径」捆绑批（可抄榜项 7，母仓 §ADJ P0-A / §KLINE-CHAIN-3 /
# §ADJ-BASIS / §ADJ-BASIS-3）。四节必须同批生效：前向填充改了数值，口径位进断点键负责让旧数值
# 缓存失效，三级兜底链负责让"网络复权腿全挂"时仍有可用日K——只搬其中一半，就会出现
# "修复已上线、结论仍是旧口径"或"兜底腿读的是没修的假价"。
# ══════════════════════════════════════════════════════════════════════════════

echo "==> 71 §ADJ 后复权因子前向填充 + 数据源路由唯一入口（抄母仓傍晚批 P0-A）..."
# 现象：adj_factor 是【事件稀疏】表（只在分红实施日有行），HfqBars 却按「因子日==行情日」等值
# LEFT JOIN → 非除权日全部落空 → COALESCE 兜成 1 → 后复权价退化为不复权价，回测/因子/图表全链路
# 失真且零报错。路由开关（PrimarySourceThsDaily/ThsFactorsReady）曾是包级导出变量，装配点只有
# cmd/quant ⇒ researchd/dataload/replay/backtest/research 各自按默认值（旧表）跑，同一份数据
# 两条口径。修法：前向填充子查询（与同包 ths_tables.go 的 LegacyAdjFactorAt 语义同源）+
# 开关收私有、唯一入口 store.ConfigureSource* 装配。
go test -count=1 ./internal/store/ -run 'TestHfqBarsAdj|TestConfigureSourceSingleEntry' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
grep -q 'ORDER BY a.trade_date DESC LIMIT 1), 1) AS adj' internal/store/store.go || { echo "--- FAIL: §P0-A 因子前向填充子查询丢失（后复权又退化成不复权）"; exit 1; }
# 负锁：baostock 侧 daily×adj_factor 的等值 JOIN 绝迹。ths 侧（ths_adj_factor 日累计全覆盖表）
# 等值语义正确，由 allow-legacy-adj-join-eq 显式豁免——把两个同名 JOIN 一起判红会造出永久性假红。
if grep -qE 'FROM daily d LEFT JOIN adj_factor a ON a\.ts_code=d\.ts_code AND a\.trade_date=d\.trade_date' internal/store/store.go; then
	echo "--- FAIL: §P0-A 旧等值 JOIN 复活（除权日之外因子恒为 1）"; exit 1; fi
[ "$(grep -c 'trade_date=b\.trade_date' internal/store/store.go || true)" -eq 1 ] || { echo "--- FAIL: 等值因子 JOIN 处数≠1（ths 日累计表那一处之外又冒出一处），§P0-A"; exit 1; }
# 路由唯一入口：导出变量形态绝迹 + 任何进程不得直改 + 装配点覆盖 8 个入口文件。
if grep -qE '^var (PrimarySourceThsDaily|ThsFactorsReady) ' internal/store/source_routing.go internal/store/store.go; then
	echo "--- FAIL: 路由开关又被导出成包级变量（cmd 可绕过唯一入口裸赋值，§P0-A）"; exit 1; fi
if grep -rn 'store\.PrimarySourceThsDaily\|store\.ThsFactorsReady' --include='*.go' cmd internal 2>/dev/null | grep -q .; then
	echo "--- FAIL: 又出现 store.PrimarySourceThsDaily 直改（§P0-A 唯一入口失效）"; exit 1; fi
[ "$(grep -rlE 'store\.ConfigureSource' --include='*.go' cmd internal | wc -l | tr -d ' ')" -ge 8 ] || { echo "--- FAIL: 路由装配点 < 8 个文件（有进程又走默认旧表口径）"; exit 1; }
echo "ok - §ADJ 专项守卫通过（行为锁 3 例 + 静态锁 5 道 + 负锁 3 道）"

echo "==> 72 §KLINE-CHAIN-3 日K三级兜底链：腾讯主源 + 东财降到复权链末尾 + 库内日K四道守卫（抄母仓 2026-09-23 夜间批）..."
# 现象：腾讯与东财两条复权腿同时不通 → fetchDayKLine 只剩"带标记的不复权兜底"，而 §H3 的守卫
#       正确地拒绝让不复权序列进 md.KLines → 日K为空、吃日K的战法整轮零分，当日只出不依赖
#       日K的战法信号。定性："报警没用，兜住才是"——所以本条不是加告警，是把第三条腿做实。
# 三条前置：① 链序 腾讯(仅qfq) → 东财(qfq，降级但不摘掉) → 库内日K → 不复权(带标记)；
#       ② 库内价是后复权，必须按实时昨收定锚归一 + 量纲(手→股) + 新鲜度 + 丢当日行，
#          四道守卫任一不过即拒用（锚错位的序列比空序列更坏：整体平移却不报错）；
#       ③ 库内腿靠引擎注入才有数据——装配漏掉时表现与"根本没有兜底"完全一致（静默跳过）。
go test -count=1 ./internal/strategy_engine/ -run 'TestFetchDayKLine|TestStoreDayKLine|TestCachedKLine|TestLastCloseSkipsStoreLeg|TestApplyDayKLine' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
go test -count=1 ./internal/engine/ -run 'TestDayBarsLookup' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# 链序锁（行号严格递增比文本断言可靠；沿用 54 的滤注释姿势）：腾讯 < 东财 < 库内 < 新浪。
K3_TC=$(grep -n 'GetTencentKLine(code, 120)' internal/strategy_engine/engine.go | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | head -1 | cut -d: -f1 || true)
K3_EM=$(grep -n 'GetKLine(code, "101", 120)' internal/strategy_engine/engine.go | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | head -1 | cut -d: -f1 || true)
K3_DB=$(grep -n 'e.storeDayKLine(code, prevClose)' internal/strategy_engine/engine.go | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | head -1 | cut -d: -f1 || true)
K3_SINA=$(grep -n 'GetSinaKLine(code, 120)' internal/strategy_engine/engine.go | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | head -1 | cut -d: -f1 || true)
[ -n "$K3_TC" ] && [ -n "$K3_EM" ] && [ -n "$K3_DB" ] && [ -n "$K3_SINA" ] \
	|| { echo "--- FAIL: 日K四条腿少了一条（§KLINE-CHAIN-3 链序锁失效）"; exit 1; }
[ "$K3_TC" -lt "$K3_EM" ] && [ "$K3_EM" -lt "$K3_DB" ] && [ "$K3_DB" -lt "$K3_SINA" ] \
	|| { echo "--- FAIL: 日K链序回退（必须 腾讯→东财→库内→不复权；东财不得被摘掉，也不得排到库内腿之后）"; exit 1; }
# 东财降级但仍在场："降到复权链末尾、不摘掉"，摘掉等于少一条独立复权源。
grep -q 'e.bumpKLineSrc("东财")' internal/strategy_engine/engine.go \
	|| { echo "--- FAIL: 东财复权腿被摘掉（裁决是降级不是删除）"; exit 1; }
# 库内腿四道守卫（缺一道即"错锚/错量纲/陈旧K/当日半成品"进因子计算）。
grep -q 'raw\[len(raw)-1\].Date.Format("20060102") >= today' internal/strategy_engine/engine.go \
	|| { echo "--- FAIL: 守卫⓪（丢当日及未来日期的行）丢失——实时昨收锚当日半成品K会把今天涨幅摊进整条基准"; exit 1; }
grep -q 'scale := prevClose / last.Close' internal/strategy_engine/engine.go \
	|| { echo "--- FAIL: 守卫①（按实时昨收定锚归一）丢失——后复权价会整体抬高 LastClose/止损价"; exit 1; }
grep -q 'lotsToShares' internal/strategy_engine/engine.go \
	|| { echo "--- FAIL: 守卫②（库内 Vol 手→股）丢失"; exit 1; }
grep -q 'const storeBarsMaxStale' internal/strategy_engine/engine.go \
	|| { echo "--- FAIL: 守卫③（新鲜度上限）常量丢失——夜间同步断了会长期用旧K"; exit 1; }
grep -q 'if prevClose <= 0 {' internal/strategy_engine/engine.go \
	|| { echo "--- FAIL: 无锚（昨收取不到）不再拒用库内腿（宁可无兜底也不出错锚）"; exit 1; }
# 拒用必须留痕：noteStoreBarsRejected 至少覆盖 无历史序列/末根非法/scale 非法/过期 四类分支。
K3_REJ=$(grep -c 'noteStoreBarsRejected(' internal/strategy_engine/engine.go || true)
[ "${K3_REJ:-0}" -ge 5 ] || { echo "--- FAIL: 库内腿拒用留痕只剩 ${K3_REJ:-0} 处（<5）——兜底静默失效不可见"; exit 1; }
# 装配锁：库内腿靠注入取数，装配点漏了就是永久静默跳过（形态同"没有兜底"）。
grep -q 'strategyEngine.SetDayBarsLookup(dayBarsLookup.Lookup)' cmd/quant/main.go \
	|| { echo "--- FAIL: 库内日K读取器未注入引擎（兜底腿形同虚设且零报错）"; exit 1; }
grep -q 'func (l \*DayBarsLookup) Lookup' internal/engine/day_bars_lookup.go \
	|| { echo "--- FAIL: 库内日K读取器实现丢失"; exit 1; }
# 缓存锁：命中缓存时必须回查昨收锚（锚一天一换，沿用旧锚=整条基准错位）。
grep -q 'func (ent \*klineCacheEntry) cacheReusable(prevClose float64) bool' internal/strategy_engine/engine.go \
	|| { echo "--- FAIL: 日K缓存的锚校验函数丢失"; exit 1; }
grep -q 'ent.cacheReusable(prevClose)' internal/strategy_engine/engine.go \
	|| { echo "--- FAIL: 缓存命中路径不再校验昨收锚（跨轮换锚后仍拿旧序列）"; exit 1; }
# 负锁①：库内腿只读——兜底链里绝不允许出现写库调用（按「到下一个顶层 func」圈定函数体，
# 比固定行窗口可靠；滤注释行，防打死"为何只准读"的说明）。
if LC_ALL=C awk '/^func \(e \*Engine\) storeDayKLine/{f=1;next} /^func /{if(f)exit} f' internal/strategy_engine/engine.go \
	| grep -vE '^[[:space:]]*//' | grep -qE 'UPDATE |DELETE |INSERT |\.Exec\('; then
	echo "--- FAIL: 库内日K腿里出现写库调用（打分链取数只准读）"; exit 1; fi
# 负锁②：不复权兜底不得重新进 KLines（§H3 的收口成果不许被本批"加兜底"顺手抵消）。
if grep -nE 'md\.KLines = .*GetSinaKLine|md\.KLines = .*GetTHSKLine' internal/strategy_engine/engine.go | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | grep -q .; then
	echo "--- FAIL: 不复权腿又直写 md.KLines（除权日因子失真复活，§H3）"; exit 1; fi
echo "ok - §KLINE-CHAIN-3 守卫通过（行为锁 2 组 + 链序锁 2 + 四道守卫锁 5 + 留痕计数锁 1 + 装配锁 2 + 缓存锁 2 + 负锁 2）"

echo "==> 73 §ADJ-BASIS 复权口径位进研究断点键（抄母仓，防「修好了但结论没重算」）..."
# resume_key 原本只含 区间/参数/股票池，**不含数值口径**。于是 §ADJ（因子前向填充）这种
# "入口不变、数值全变"的修复根本不会让旧断点失效——夜间链照旧逐窗命中改前装配好的面板并跳过
# 重算，新基线形同没上。修法是把口径折进 key（断点表自己的注释就写着"靠 key 轮换失效"），
# 而不是去生产库删行。
for site in internal/research/windowed.go internal/research/pattern.go; do
	grep -qF 'adjBasisTag' "$site" \
		|| { echo "--- FAIL: $site 的断点键不再携带复权口径位（复权修复后夜间链会照旧复用改前面板）"; exit 1; }
done
# 三处键生成点逐点核对（discoveryResumeKey / pfac-dedup / 形态 dp），漏一处就等于那一路永不重算。
grep -qE 'return fmt\.Sprintf\("df\|.*%s%s"' internal/research/windowed.go \
	|| { echo "--- FAIL: 因子发现主键 df| 模板丢失口径位"; exit 1; }
grep -qE '"pfac-dedup:" \+ start \+ ":" \+ end \+ adjBasisTag' internal/research/windowed.go \
	|| { echo "--- FAIL: 逐因子去重缓存键 pfac-dedup 丢失口径位"; exit 1; }
grep -qE 'fmt\.Sprintf\("dp\|.*%s%s"' internal/research/pattern.go \
	|| { echo "--- FAIL: 形态扫描键 dp| 丢失口径位"; exit 1; }
# 口径常量必须存在且被测试认识（改口径不 bump＝换键失败＝沿用旧面板）。
grep -qE 'AdjBaselineVersion = "hfq-forward-fill-1"' internal/research/windowed.go \
	|| { echo "--- FAIL: AdjBaselineVersion 常量形态变更（请同步本锁与取数口径注释）"; exit 1; }
if ! go test ./internal/research -run 'TestDiscoveryResumeKeyCarriesAdjBasis|TestCkptRotationOnBasisBump' -count=1 >/dev/null 2>&1; then
	echo "--- FAIL: §ADJ-BASIS 断点键回归测试未通过（旧键复用/新键不稳定）"; exit 1; fi
echo "ok - §ADJ-BASIS 守卫通过（键位正锁 3 + 常量锁 1 + 回归测试 2）"

echo "==> 74 §ADJ-BASIS-3 回测事件缓存口径位与降级保守（抄母仓）..."
grep -qE 'PRIMARY KEY \(candidate_id, event_date, industry, adj_basis\)' internal/store/store.go \
	|| { echo "--- FAIL: 事件缓存主键不再含 adj_basis（新旧口径数值继续互相覆盖）"; exit 1; }
grep -q 'func (d \*DB) migrateBacktestEventResultsAdjBasis() error' internal/store/store.go \
	&& grep -q 'd.migrateBacktestEventResultsAdjBasis()' internal/store/store.go \
	|| { echo "--- FAIL: 旧库主键重建迁移或其调用点丢失（现网旧表升不上去）"; exit 1; }
# 行为锁：迁移搬运守恒 / 塌行中止 / 未知主键形态降级不动表 / 新旧口径共存 / 读侧永不吐旧行 / 矩阵分桶。
# 下限取 9（当前实跑 10 例）：任一用例失联即红，但不因新增用例而上抬门槛。
KJ=$(go test -count=1 ./internal/store/ -run 'TestEventResults|TestEmotion' -v 2>&1 | grep -c '^--- PASS' || true)
[ "${KJ:-0}" -ge 9 ] || { echo "--- FAIL: §ADJ-BASIS-3 行为用例只跑到 ${KJ:-0} 例（<9，迁移/降级/矩阵任一组失联）"; exit 1; }
go test -count=1 ./internal/store/ -run 'TestEventResults' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
# 守恒守卫（四道，按「函数体」圈定而非全文匹配，防止只在别处出现同名文本就判绿）：
# 重建前后必须核对行数 + 逐三元组的行数与内容长度双向 EXCEPT，缺一即"迁移自己造差异"。
KBODY=$(LC_ALL=C awk '/^func \(d \*DB\) migrateBacktestEventResultsAdjBasis\(\) error \{/{f=1;next} f&&/^\}$/{exit} f' internal/store/store.go)
[ -n "$KBODY" ] || { echo "--- FAIL: 迁移函数体取不到（签名改名会让本段四道锁集体失效，§ADJ-BASIS-3）"; exit 1; }
printf '%s\n' "$KBODY" | grep -q 'EXCEPT' \
	|| { echo "--- FAIL: §ADJ-BASIS-3 缺逐组双向 EXCEPT 内容守恒核对"; exit 1; }
[ "$(printf '%s\n' "$KBODY" | grep -c 'COUNT(\*)' || true)" -ge 2 ] \
	|| { echo "--- FAIL: §ADJ-BASIS-3 缺重建前后行数守恒核对"; exit 1; }
printf '%s\n' "$KBODY" | grep -q 'markEventBasisDegraded' \
	|| { echo "--- FAIL: §ADJ-BASIS-3 守卫不通过时未转降级模式（硬失败或悄悄继续都不可接受）"; exit 1; }
printf '%s\n' "$KBODY" | grep -q 'SUM(LENGTH(' \
	|| { echo "--- FAIL: §ADJ-BASIS-3 缺逐组内容长度指纹（只比行数比不出数值被换掉）"; exit 1; }
# 降级=读未命中/写拒绝：两处都必须先看 EventBasisDegraded，且空串口径一律不当当前口径。
grep -qE 'if adjBasis == "" \|\| d.EventBasisDegraded\(\)' internal/store/backtest_jobs.go \
	|| { echo "--- FAIL: 读侧降级判断丢失（表结构不可用时仍查缓存=拿旧口径行当新结论）"; exit 1; }
grep -q '改前旧证据行的哨兵值' internal/store/backtest_jobs.go \
	|| { echo "--- FAIL: 写侧空串哨兵拒绝判据丢失（哨兵值可被当作当前口径写入）"; exit 1; }
# 情绪×战法矩阵跨全表聚合：不过滤口径就把改前/改后两套数值平均进同一格并对外发布。
grep -qE 'FROM backtest_event_results WHERE adj_basis = \?' internal/store/emotion_matrix.go \
	|| { echo "--- FAIL: 情绪矩阵聚合未按口径过滤（混桶=假统计）"; exit 1; }
grep -q 'if adjBasis == ""' internal/store/emotion_matrix.go \
	&& grep -q 'd.EventBasisDegraded()' internal/store/emotion_matrix.go \
	|| { echo "--- FAIL: 情绪矩阵缺「口径未装配/降级即拒绝聚合」的保守闸"; exit 1; }
# 负锁：读侧 SQL 不得出现字面量 adj_basis=''（空串是旧证据行哨兵，永远不能当查询目标口径）。
# 只用裸 grep 会误伤说明性文字（store.go 的注释与日志里就在描述"旧行落成 adj_basis=''"这件事，
# 那是正确行为，不是查询条件），故滤掉 // 行注释与 log.Printf 文案两族。
if grep -rn "adj_basis *= *''" internal/store/store.go internal/store/backtest_jobs.go internal/store/emotion_matrix.go \
	| grep -vE ':[0-9]+:[[:space:]]*(//|\*)' | grep -v 'log\.Printf' | grep -q .; then
	echo "--- FAIL: 出现按空串口径查询（把改前旧行当成可复用的当前口径结果）"; exit 1; fi
# 负锁：调用方不得图省事传空串（口径位必须由 research.AdjBaselineVersion 供给）。
if grep -rn 'ListEmotionStrategyMatrix(' internal/server/*.go | grep -vE ':[0-9]+:[[:space:]]*(//|\*)' | grep -vE 'research\.AdjBaselineVersion' | grep -q .; then
	echo "--- FAIL: 情绪矩阵调用点未传 research.AdjBaselineVersion（口径位又变成可选参数）"; exit 1; fi
echo "ok - §ADJ-BASIS-3 守卫通过（行为锁 2 组 + 主键锁 1 + 迁移锁 1 + 守恒锁 4 + 降级锁 2 + 聚合锁 3 + 负锁 2）"

echo "==> 75 §LINTGATE 前端未定义符号静态门禁（可抄榜项 8，抄母仓 §N-1）..."
# 现象：web/src/pages/Dashboard.jsx 的实盘状态轮询回调把 setQmtState 误写成 setQMTState（未声明
#       符号）→ ReferenceError 被同函数的空 catch 吞掉 → qmtState 恒 null → 概览页「实盘链路」
#       指示**永不渲染**，且每 15s 静默抛一次。类型检查不覆盖 .jsx、vitest 此前也没渲染过这张卡
#       ⇒ 这类拼写型运行时炸弹只有静态 lint 抓得到，而仓库原本没有 lint 门。
# 取舍（沿用母仓 owner 裁决）：最小集起步——no-undef 锁 error（未定义符号=运行时炸弹，正是本批
#       事故形态）；no-unused-vars 降 warn（存量 130 条多为无害死码，error 会让门禁首日即红、
#       失去可用性）。所以本段的实跑一律带 --quiet：只允许 error 阻塞。
grep -q "'no-undef': 'error'" web/eslint.config.js \
	|| { echo "--- FAIL: no-undef 未锁 error（同类拼写错误又能静默上线）"; exit 1; }
# 桩规则锁：仓内 18 处行内 `eslint-disable react-hooks/exhaustive-deps` 指令靠 no-op 桩解析；
# 删桩会让 ESLint 直接报「Definition for rule not found」——门禁因无关原因整红，比缺门禁更糟。
grep -q 'react-hooks-stub-for-inline-directives' web/eslint.config.js \
	|| { echo "--- FAIL: react-hooks 桩规则丢失（18 处行内 disable 指令令门禁首日整红）"; exit 1; }
grep -q '"lint": "eslint src"' web/package.json \
	|| { echo "--- FAIL: lint 脚本丢失（门禁无从挂起）"; exit 1; }
grep -qE '"eslint": ' web/package.json \
	|| { echo "--- FAIL: eslint 未落 devDependencies（CI 的 npm ci 装不到，门禁只在本地有效）"; exit 1; }
grep -q 'npm run lint -- --quiet' .github/workflows/ci.yml \
	|| { echo "--- FAIL: CI 未跑 lint（本地门禁不约束合并）"; exit 1; }
# 行为锁：健康载荷下「实盘链路」必须真的渲染出来（防"整行静默不渲染"回退——改名不重要，断言才是锁）。
if ! ( cd web && npx vitest run src/__tests__/n1_qmt_link.test.jsx ) >/dev/null 2>&1; then
	echo "--- FAIL: §LINTGATE 实盘链路渲染行为锁未通过（E1 渲染/E2 熔断如实展示/E3 未启用不渲染）"; exit 1; fi
# 负锁（滤注释行）：未声明符号的实调用形态绝迹。
if grep -n 'setQMTState' web/src/pages/Dashboard.jsx | grep -vE '^[0-9]+:[[:space:]]*(//|\*)' | grep -q .; then
	echo "--- FAIL: 又出现 setQMTState 实调用（未声明符号，会被空 catch 吞成静默失效）"; exit 1; fi
# 实跑：全 src 的 error 必须为 0（warn 不阻塞）。
if ! ( cd web && npx eslint src --quiet ); then
	echo "--- FAIL: eslint --quiet 判红（存在未定义符号级 error，本门禁只允许 error 阻塞）"; exit 1; fi
echo "ok - §LINTGATE 守卫通过（静态锁 5 道 + 行为锁 1 组 + 负锁 1 道 + lint 实跑）"

echo ""
echo "==> 全部通过"
