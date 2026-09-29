// uatfix929_fidelity_test.go §UATFIX929-E mock 保真五修的行为锁（09-29 全量 UAT 审计 E 组轻腿）。
// 文件职责：把 mock 与真实网关（qmt_gateway/gateway.py）四处口径分岔钉成单测——
//
//	③ 未知单撤单一律 409（真网关 _do_cancel 只分 400/409/200 三档，gateway.py:1437-1452）；
//	④ /state 发 broker_mode 且与 /health active 通道同源（真网关 _do_state:1470）；
//	⑤ /quotes 顶层发 feed_age_sec（真网关 _do_quotes:973/993；Go 侧纯观察、mock 恒 0）；
//	⑥ /settlement 日期校验补数字段（真网关 :1493 的 isdigit 腿，`abcdefghij` 必须 400）；
//	⑦ Bearer 比对换常量时间实现后判定结果逐位等价（错误口令/前缀口令/缺头全 401、正确 200、
//	   /health 豁免不变——常量时间是结构性质，本锁兜「换实现没换语义」）。
//
// 每条都对照真网关实码而非注释推演；重腿（①派发面 ②心跳/断线）另批立项（M4-heavy）。
// English: five fidelity locks aligning the mock with the real gateway after §UATFIX929-E
// light-leg fixes — cancel codes, broker_mode, feed_age_sec, settlement date digits,
// and constant-time bearer comparison with identical accept/reject semantics.
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fidGet 以指定 Authorization 头发 GET，返回状态码与解析体（体非 JSON 时返 nil）。
func fidGet(t *testing.T, h http.Handler, path, auth string) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// TestUATFIX929CancelUnknownOrderIs409 ③：未知单撤单 409（旧 mock 自创 404 档已收口）。
func TestUATFIX929CancelUnknownOrderIs409(t *testing.T) {
	h, _, _ := newTestGateway()
	code, body := post(t, h, "/cancel", `{"order_id":"GHOST-929"}`)
	if code != http.StatusConflict {
		t.Fatalf("未知单撤单必须 409（对齐真网关 gateway.py:1452 的失败一律 409），got %d body=%v", code, body)
	}
	if body["ok"] != false {
		t.Fatalf("409 响应体必须 ok=false，got %v", body)
	}
}

// TestUATFIX929StateCarriesBrokerMode ④：/state 顶层发 broker_mode，且与 /health 的
// active 通道同源——切换通道后两面必须一起变（同 b.activeBroker 一个变量）。
func TestUATFIX929StateCarriesBrokerMode(t *testing.T) {
	h, _, _ := newTestGateway()
	code, st := fidGet(t, h, "/state", "Bearer t0")
	if code != 200 || st["broker_mode"] != "xt" {
		t.Fatalf("缺省 /state 应 200 且 broker_mode=xt，got %d %v", code, st)
	}
	if _, hs := fidGet(t, h, "/health", ""); hs["broker_mode"] != "xt" {
		t.Fatalf("/health broker_mode 应同源 xt，got %v", hs)
	}
	// 切到 queued 通道后两面同步（/admin/broker 契约已有专测，这里只验同源性）
	if code, _ := post(t, h, "/admin/broker", `{"broker":"queued"}`); code != 200 {
		t.Fatalf("切换通道应 200，got %d", code)
	}
	_, st2 := fidGet(t, h, "/state", "Bearer t0")
	_, hs2 := fidGet(t, h, "/health", "")
	if st2["broker_mode"] != "queued" || hs2["broker_mode"] != "queued" {
		t.Fatalf("切换后两面必须同源 queued：state=%v health=%v", st2["broker_mode"], hs2["broker_mode"])
	}
}

// TestUATFIX929QuotesCarriesFeedAgeSec ⑤：/quotes 顶层恒发 feed_age_sec（mock 恒 0）。
func TestUATFIX929QuotesCarriesFeedAgeSec(t *testing.T) {
	h, _, _ := newTestGateway()
	code, body := fidGet(t, h, "/quotes?codes=600519.SH", "Bearer t0")
	if code != 200 {
		t.Fatalf("/quotes 应 200，got %d", code)
	}
	age, exists := body["feed_age_sec"]
	if !exists {
		t.Fatalf("/quotes 必须顶层发 feed_age_sec（对齐真网关 _do_quotes），got keys=%v", keysOf(body))
	}
	if age != float64(0) {
		t.Fatalf("mock feed 恒新鲜，feed_age_sec 必须为 0，got %v", age)
	}
}

// TestUATFIX929SettlementDateDigits ⑥：结构对但非数字的日期必须 400（真网关 isdigit 腿）。
func TestUATFIX929SettlementDateDigits(t *testing.T) {
	h, _, _ := newTestGateway()
	for _, bad := range []string{"abcdefghij", "20ab-cd-ef", "2026-0b-26", "20260926-1"} {
		code, _ := fidGet(t, h, "/settlement?date="+bad, "Bearer t0")
		if code != http.StatusBadRequest {
			t.Fatalf("非数字日期 %q 必须 400（对齐 gateway.py:1493 isdigit），got %d", bad, code)
		}
	}
	if code, _ := fidGet(t, h, "/settlement?date=2026-09-26", "Bearer t0"); code != 200 {
		t.Fatalf("合法日期必须 200，got %d", code)
	}
}

// TestUATFIX929BearerConstantTimeEquivalence ⑦：常量时间比对后判定面逐位等价——
// 正确口令 200、错误口令/纯前缀/缺 Bearer 头全 401、/health 无头豁免照旧。
func TestUATFIX929BearerConstantTimeEquivalence(t *testing.T) {
	h, _, _ := newTestGateway()
	if code, _ := fidGet(t, h, "/state", "Bearer t0"); code != 200 {
		t.Fatalf("正确口令必须 200，got %d", code)
	}
	for _, auth := range []string{"Bearer t", "Bearer t0x", "Bearer x0", "t0", ""} {
		code, _ := fidGet(t, h, "/state", auth)
		if code != http.StatusUnauthorized {
			t.Fatalf("口令 %q 必须 401（前缀/缺头/无 Bearer 均拒），got %d", auth, code)
		}
	}
	if code, _ := fidGet(t, h, "/health", ""); code != 200 {
		t.Fatalf("/health 豁免口径不得变，got %d", code)
	}
}

// keysOf 返回 map 键列表（失败信息用，避免整包 dump 撑爆日志）。
func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
