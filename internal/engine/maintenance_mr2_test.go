// 文件职责：§MR-2 维护节拍入口回归——MaintenanceBinanceOnce 在路由器缺失（未装配币安链）
// 时必须零行为不 panic；挂上路由器后重复调用无害（子步自带节流）。
// English: §MR-2 regression — the 7×24 ticker entry point is nil-safe and idempotent.
package engine

import (
	"testing"
	"time"
)

func TestMaintenanceBinanceOnceNilSafe(t *testing.T) {
	var e Engine
	e.MaintenanceBinanceOnce(time.Now()) // 无 LiveRouter：零行为，不得 panic
	e.SetLiveRouter(nil)
	e.MaintenanceBinanceOnce(time.Now()) // 显式置空同样零行为
}
