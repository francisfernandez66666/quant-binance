// 文件职责：币安 API 错误码 → 本地处置分类（PLAN_BINANCE_MULTI_ASSET §15.1）。
// 单一权威映射表：码→{Retryable, Fatal, IdempotentHit, UserAction}；golden 契约
// contract/binance_error_codes.json 与本表由 Go 测试双向锁定。分层口径（§2.5/§15.7）：
// 预检 SAPI 负码优先于业务 486xxx；HTTP 5XX=执行状态未知，绝不盲重发下单。
package trading

import "fmt"

// BinanceErrClass 单码处置分类。
type BinanceErrClass struct {
	Code          int    // 币安错误码（负码=SAPI 预检层；486xxx=美股业务层）
	Name          string // 语义名（日志/告警直读）
	Retryable     bool   // 可安全自动重试（幂等前提：同 clientOrderId/查重后）
	Fatal         bool   // 配置/资格级故障：需要人介入（Key 权限、disclaimer、封禁）
	IdempotentHit bool   // 重复受理命中：查 detail 回填，而非再下单
	UserAction    string // 给前端/告警的处置建议（空=系统自动处理）
}

// binanceErrTable §15.1 全量子集（本项目会实际遇到的码）。未列码按 binanceErrUnknown 处置。
var binanceErrTable = map[int]BinanceErrClass{
	// —— 现货 SAPI 负码（-1xxx/-2xxx）——
	-1000: {Code: -1000, Name: "UNKNOWN_EXCHANGE_ERROR", UserAction: "查 order/query 确认受理状态后人工决定重放"},
	-1003: {Code: -1003, Name: "TOO_MANY_REQUESTS", Retryable: true, UserAction: "限速器已退避；持续出现请下调下单频率"},
	-1013: {Code: -1013, Name: "FILTER_FAILURE", UserAction: "数量/金额/价格未过滤（LOT_SIZE/PRICE_FILTER/MIN_NOTIONAL）——核对 exchangeInfo 缓存与取整链"},
	-1015: {Code: -1015, Name: "TOO_MANY_ORDERS", Retryable: true, UserAction: "暂停新单窗口已开，稍后自动恢复"},
	-1021: {Code: -1021, Name: "INVALID_TIMESTAMP", Retryable: true, UserAction: "已自动校时重签；反复出现查系统时钟/NTP"},
	-1022: {Code: -1022, Name: "INVALID_SIGNATURE", Fatal: true, UserAction: "API Secret 配置错误——熔断该 broker，人工核对密钥"},
	-1102: {Code: -1102, Name: "MANDATORY_PARAM_EMPTY", UserAction: "请求缺必填参数——代码缺陷，报修"},
	-1111: {Code: -1111, Name: "BAD_PRECISION", UserAction: "价格/数量精度越界——核对 stepSize/tickSize 取整链"},
	-1121: {Code: -1121, Name: "INVALID_SYMBOL", UserAction: "标的不存在/未上线——加入当轮观察黑名单"},
	-1125: {Code: -1125, Name: "INVALID_LISTEN_KEY", Retryable: true, UserAction: "listenKey 过期已自动重取"},
	-2010: {Code: -2010, Name: "NEW_ORDER_REJECTED", UserAction: "柜台拒单（余额/风控）——占位行降级发送失败，查 msg 定位"},
	-2011: {Code: -2011, Name: "CANCEL_REJECTED", UserAction: "撤单被拒（已成交/已撤）——状态由回报推进"},
	-2013: {Code: -2013, Name: "ORDER_NOT_FOUND", UserAction: "查无此单——视为未受理，按查重结果决定重放"},
	-2014: {Code: -2014, Name: "API_KEY_FORMAT", Fatal: true, UserAction: "API Key 格式非法——人工核对"},
	-2015: {Code: -2015, Name: "REJECTED_MBX_KEY", Fatal: true, UserAction: "Key/IP/权限被拒（Enable Trading? IP 白名单?）——熔断该 broker"},
	// —— 美股业务码（486xxx 与 -26004/-3113/-3114/-11001）——
	486217: {Code: 486217, Name: "EQUITY_ORDER_ID_OR_CLORD_REQUIRED", UserAction: "查询缺 orderId/clientOrderId——代码缺陷，报修"},
	486218: {Code: 486218, Name: "EQUITY_ORDER_NOT_FOUND", UserAction: "查无此单——按未受理处理"},
	486405: {Code: 486405, Name: "EQUITY_INSUFFICIENT_BALANCE", UserAction: "余额不足——拒单（不可自动重试）"},
	486408: {Code: 486408, Name: "EQUITY_SYMBOL_NO_ODD_LOT", UserAction: "该票不支持碎股——改整股数量重下"},
	486409: {Code: 486409, Name: "EQUITY_PREMARKET_NO_ODD_LOT", UserAction: "盘前不支持碎股——转 RTH 或整股"},
	486410: {Code: 486410, Name: "EQUITY_DISCLAIMER_UNSIGNED", Fatal: true, UserAction: "美股风险披露未签——/api/binance/disclaimer 签署后恢复"},
	486416: {Code: 486416, Name: "EQUITY_QTY_BELOW_MIN", UserAction: "数量低于最小——前置闸应已拦"},
	486417: {Code: 486417, Name: "EQUITY_QTY_ABOVE_MAX", UserAction: "数量超上限——核 exchangeInfo lot 规则"},
	486418: {Code: 486418, Name: "EQUITY_QTY_LOT_STEP", UserAction: "数量非 lot 步长整数倍——取整链缺陷，报修"},
	486419: {Code: 486419, Name: "EQUITY_NOTIONAL_BELOW_MIN", UserAction: "金额低于最小名义额（$1 起）——加大或合并"},
	486420: {Code: 486420, Name: "EQUITY_NOTIONAL_ABOVE_MAX", UserAction: "金额超上限——拆单"},
	486421: {Code: 486421, Name: "EQUITY_PRICE_ABOVE_RANGE", UserAction: "限价超允许区间——核对现价"},
	486422: {Code: 486422, Name: "EQUITY_PRICE_BELOW_RANGE", UserAction: "限价低于允许区间——核对现价"},
	486423: {Code: 486423, Name: "EQUITY_TOO_MANY_OPEN_ORDERS", Retryable: true, UserAction: "在途单达上限——等待清账后自动重试"},
	486426: {Code: 486426, Name: "EQUITY_BUY_FORBIDDEN", UserAction: "该票禁买——标黑当轮"},
	486427: {Code: 486427, Name: "EQUITY_SELL_FORBIDDEN", UserAction: "该票禁卖——等待状态恢复"},
	486428: {Code: 486428, Name: "EQUITY_HALTED", UserAction: "停牌中——等复牌（tradingStatus 流驱动）"},
	486434: {Code: 486434, Name: "EQUITY_NOTIONAL_MAX", UserAction: "名义额超票级上限——拆单"},
	486439: {Code: 486439, Name: "EQUITY_SELL_ONLY", UserAction: "账户仅可卖（资格/保证金）——拒买+告警"},
	486440: {Code: 486440, Name: "EQUITY_PDT_RESTRICTION", UserAction: "PDT 保证金保护——拒买+告警，人工入金或等平仓"},
	486441: {Code: 486441, Name: "EQUITY_PREMARKET_LIMIT_ONLY", UserAction: "盘前仅限价单——市价单改限价"},
	486442: {Code: 486442, Name: "EQUITY_ODD_LOT_MARKET_ONLY", UserAction: "碎股仅市价单——碎股 LIMIT 改 MARKET 或整股"},
	486443: {Code: 486443, Name: "EQUITY_TYPE_NO_ODD_LOT", UserAction: "该单型不支持碎股——改单型"},
	486449: {Code: 486449, Name: "EQUITY_DUPLICATE_CLIENT_ORDER_ID", IdempotentHit: true, UserAction: "重复 clientOrderId——查 order/detail 回填真实单号"},
	-26004: {Code: -26004, Name: "EQUITY_UNKNOWN_SYMBOL", UserAction: "标的不存在——标黑并核对代码形态"},
	-3113:  {Code: -3113, Name: "BSTOCKS_RESTRICTED", Fatal: true, UserAction: "bStocks 限制——美股通道降级，仅走现货"},
	-3114:  {Code: -3114, Name: "BSTOCKS_NOT_QUALIFIED", Fatal: true, UserAction: "美股资格不合格——联系券商/换区，暂禁美股下单"},
	-11001: {Code: -11001, Name: "EQUITY_DO_NOT_HAVE_AN_ACCOUNT", Fatal: true, UserAction: "账户未开通 Stocks 子户（Phase0-Q1 资格项）"},
}

// binanceErrUnknown 未列码的缺省处置：不自动重试（状态未知面保守），提示查单。
var binanceErrUnknown = BinanceErrClass{Code: 0, Name: "UNCLASSIFIED", UserAction: "未分类错误——查 order/detail 核实受理状态后再决定重放"}

// classifyBinanceError 查表分类（未列码返回 UNCLASSIFIED 并带原码）。
func classifyBinanceError(code int) BinanceErrClass {
	if c, ok := binanceErrTable[code]; ok {
		return c
	}
	u := binanceErrUnknown
	u.Code = code
	return u
}

// binanceErrMessage 生成带语义名与处置建议的错误文案（进 OrderResult.Err / opslog）。
func binanceErrMessage(code int, msg string) string {
	c := classifyBinanceError(code)
	label := c.Name
	if label == "UNCLASSIFIED" {
		label = fmt.Sprintf("UNCLASSIFIED(%d)", code)
	}
	if c.UserAction != "" {
		return fmt.Sprintf("binance %s: %s（处置: %s）", label, msg, c.UserAction)
	}
	return fmt.Sprintf("binance %s: %s", label, msg)
}
