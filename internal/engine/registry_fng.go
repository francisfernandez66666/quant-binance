// registry_fng.go §ENH-A1 FNG 恐慌贪婪指数装配面（Registry → Server 的唯一注入链）。
//
// 分工（registry.go CRYPTO 分支留界注释的落地）：
//   - attachFNG：引擎构建的 CRYPTO 分支调用，进程级只建一个 data.FNGClient（零值即用，
//     官方 Base/10s 超时）——**不起后台轮询**（FNG 客户端纪律），Refresh 节奏归本文件；
//   - FNGSnapshot：server.SetFNGSource 闭包的实现（main.go 装配），懒踢刷新——缓存龄超
//     TTL（或从未取到）时另起 goroutine Refresh，HTTP 读路径绝不阻塞在外源慢链上；
//     踢频另按最小间隔节流，前端 M-10 轮询再密也只命中缓存读 + 每窗口至多一次外呼。
//
// 无证据姿势：未装配（CRYPTO 关）/从未成功 → ok=false，闭包不造 value（"无证据≠中性"）。
// English: §ENH-A1 assembly face for the Fear & Greed client — a process-wide lazy client
// attached at CRYPTO build time and a snapshot function (value/classification/age/ok) that
// kicks throttled async refreshes; never blocks the HTTP read path; no-evidence ⇒ ok=false.
package engine

import (
	"time"

	"quant-trading-v2/internal/data"
)

const (
	// fngEvidenceTTL 证据新鲜期：FNG 是日频指数，6h 重取一次足够新且不扰免费源。
	fngEvidenceTTL = 6 * time.Hour
	// fngKickMinInterval 异步刷新踢发最小间隔（失败回退也按此节奏重试，杜绝轮询风暴）。
	fngKickMinInterval = time.Minute
)

// attachFNG CRYPTO 分支装配点：幂等挂上进程级 FNG 客户端（多账号/重建引擎不重复建）。
func (r *Registry) attachFNG() {
	r.fngMu.Lock()
	defer r.fngMu.Unlock()
	if r.fng == nil {
		r.fng = &data.FNGClient{}
	}
}

// FNGSnapshot 只读证据面：返回 (value, classification, ageSec, ok)。
// 命中新鲜缓存零外呼；龄超 TTL/空缓存时踢一次异步 Refresh（受 fngKickMinInterval 节流），
// 本轮仍如实返回旧证据或 ok=false——调用方（/api/binance/state）永远不被外源拖住。
func (r *Registry) FNGSnapshot() (value int, classification string, ageSec int64, ok bool) {
	r.fngMu.Lock()
	c := r.fng
	kick := false
	if c != nil {
		age := c.Age()
		if age < 0 || age > fngEvidenceTTL {
			if now := time.Now(); now.Sub(r.fngKickAt) >= fngKickMinInterval {
				r.fngKickAt = now
				kick = true
			}
		}
	}
	r.fngMu.Unlock()
	if kick {
		go func() { _ = c.Refresh() }() // 失败保留旧缓存（客户端纪律），下轮按节流再试
	}
	if c == nil {
		return 0, "", -1, false // 未装配（CRYPTO 链关闭）：闭包在位但零证据零外呼
	}
	s, ok := c.Last()
	if !ok {
		return 0, "", -1, false // 首次取数在途/从未成功：无证据不造数
	}
	return s.Value, s.Classification, int64(c.Age().Round(time.Second) / time.Second), true
}
