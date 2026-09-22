// 文件职责：§BINANCE-P1 配置 API——GET/POST /api/config/binance（admin），写法镜像 /api/config/qmt。
// 契约：GET 只回脱敏凭证（api_key/api_secret 掩码回显）；POST 指针字段=本次要改的、nil=保持原值，
// 脱敏哨兵/空串不回写覆盖真值（§GAP2-W2 同口径）；落库前过 config.ValidateBinance（400 拒绝非法）。
// 零回归边界：本端点只读写 binance_config_json_v1 独立键与全局 rules.binance，不触碰 rules.qmt。
package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"quant-trading-v2/internal/config"
)

// handleGetBinanceConfig 处理 GET /api/config/binance：返回当前账号币安配置（凭证脱敏）。
func (s *Server) handleGetBinanceConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.GetBinanceConfigFor(userIDFor(r))
	writeJSON(w, 200, binanceConfigView(cfg))
}

// binanceConfigView 把币安配置渲染为对外响应形状：api_key/api_secret 永不回明文，
// 只给 *_masked 与"是否已配置"布尔（前端表单据此决定占位符）。
func binanceConfigView(cfg *config.BinanceConfig) map[string]interface{} {
	apiKeyMasked, apiSecretMasked := "", ""
	if cfg.APIKey != "" {
		apiKeyMasked = maskSecret(cfg.APIKey)
	}
	if cfg.APISecret != "" {
		apiSecretMasked = maskSecret(cfg.APISecret)
	}
	return map[string]interface{}{
		"enabled":              cfg.Enabled,
		"mode":                 cfg.Mode,
		"testnet":              cfg.Testnet,
		"api_key_masked":       apiKeyMasked,
		"api_secret_masked":    apiSecretMasked,
		"has_api_key":          cfg.APIKey != "",
		"timeout_sec":          cfg.TimeoutSec,
		"miss_heartbeat_sec":   cfg.MissHeartbeatSec,
		"halted":               cfg.Halted,
		"cancel_stale_sec":     cfg.CancelStaleSec,
		"disclaimer_signed_at": cfg.DisclaimerSignedAt,
		"quote_asset":          cfg.QuoteAsset,
		"stock":                cfg.Stock,
		"spot":                 cfg.Spot,
		"risk_gate":            cfg.RiskGate,
		"paper_separate":       cfg.PaperSeparate,
	}
}

// setBinanceConfigReq 局部更新请求：指针字段=「本次要改的」，nil=保持不变（同 setQMTConfigReq 惯例）。
// stock/spot/risk_gate 为整档案替换（指针 nil=不动），前端表单总是回传完整三段。
type setBinanceConfigReq struct {
	Enabled            *bool                        `json:"enabled"`
	Mode               *string                      `json:"mode"`
	Testnet            *bool                        `json:"testnet"`
	APIKey             *string                      `json:"api_key"`
	APISecret          *string                      `json:"api_secret"`
	TimeoutSec         *int                         `json:"timeout_sec"`
	MissHeartbeatSec   *int                         `json:"miss_heartbeat_sec"`
	Halted             *bool                        `json:"halted"`
	CancelStaleSec     *int                         `json:"cancel_stale_sec"`
	DisclaimerSignedAt *string                      `json:"disclaimer_signed_at"`
	QuoteAsset         *string                      `json:"quote_asset"`
	Stock              *config.BinanceMarketProfile `json:"stock"`
	Spot               *config.BinanceMarketProfile `json:"spot"`
	RiskGate           *config.RiskGateConfig       `json:"risk_gate"`
	PaperSeparate      *bool                        `json:"paper_separate"`
}

// handleSetBinanceConfig 处理 POST /api/config/binance：局部合并→校验→落该账号配置。
// halted 即时语义在 Phase 2 接线（Controller 构建后从这里取）；本阶段只管持久化。
func (s *Server) handleSetBinanceConfig(w http.ResponseWriter, r *http.Request) {
	var req setBinanceConfigReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	user := userIDFor(r)
	// 以目标账号当前配置为基线做局部合并（值拷贝，改完一次性写回）。
	cfg := *(s.cfg.GetBinanceConfigFor(user))

	if req.Enabled != nil {
		cfg.Enabled = *req.Enabled
	}
	if req.Mode != nil {
		m := strings.TrimSpace(*req.Mode)
		if m != "manual" && m != "auto" { // 枚举前置拦截，报错口径与 qmt 端点一致
			writeError(w, 400, "mode 仅允许 manual/auto")
			return
		}
		cfg.Mode = m
	}
	if req.Testnet != nil {
		cfg.Testnet = *req.Testnet
	}
	if req.APIKey != nil && *req.APIKey != "" && !isMaskedSecret(*req.APIKey) {
		cfg.APIKey = strings.TrimSpace(*req.APIKey) // 脱敏哨兵/空串不回写（前端回显掩码不覆盖真值）
	}
	if req.APISecret != nil && *req.APISecret != "" && !isMaskedSecret(*req.APISecret) {
		cfg.APISecret = strings.TrimSpace(*req.APISecret)
	}
	if req.TimeoutSec != nil {
		cfg.TimeoutSec = *req.TimeoutSec
	}
	if req.MissHeartbeatSec != nil {
		cfg.MissHeartbeatSec = *req.MissHeartbeatSec
	}
	if req.Halted != nil {
		cfg.Halted = *req.Halted
	}
	if req.CancelStaleSec != nil {
		cfg.CancelStaleSec = *req.CancelStaleSec
	}
	if req.DisclaimerSignedAt != nil {
		cfg.DisclaimerSignedAt = strings.TrimSpace(*req.DisclaimerSignedAt)
	}
	if req.QuoteAsset != nil {
		cfg.QuoteAsset = strings.TrimSpace(*req.QuoteAsset)
	}
	if req.Stock != nil {
		cfg.Stock = *req.Stock
	}
	if req.Spot != nil {
		cfg.Spot = *req.Spot
	}
	if req.RiskGate != nil {
		cfg.RiskGate = *req.RiskGate
	}
	if req.PaperSeparate != nil {
		cfg.PaperSeparate = *req.PaperSeparate
	}

	// 全量复校验（枚举/范围/启用一致性，config 包单一权威）：非法 400 且不落库。
	if err := config.ValidateBinance(&cfg); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.cfg.SetBinanceConfigFor(user, &cfg)
	writeJSON(w, 200, binanceConfigView(&cfg))
}
