# -*- coding: utf-8 -*-
"""qmt_gateway/tests/test_report_token_guard.py — §N2/§N3（2026-09-26 全量审计）启动装配闸（网关侧）。

覆盖两条审计缺陷的网关腿（裁决口径见 docs/AUDIT_FULL_UAT_20260926.md §三 N2/N3）：

  §N2 回报口令双源：Go 侧 /api/qmt/report 归因只比对 rules.qmt.token
    （internal/server/qmt.go:145-156 常量时间比对），网关的 cfg["report_token"] 却可被
    QUANT_GATEWAY_REPORT_TOKEN 独立设置（gateway.load_config），handler 拿它推送。
    配一个不等值的口令 = 全部回报 401 = outbox 死信 = 静默失联。
    现由 gateway.report_token_conflicts 在启动装配处判定，main 拒启（exit 2）。
  §N3 /health 白名单：/health 只对回环与 ALLOWED_IPS 放行，远程引擎靠它驱动熔断，
    白名单漏配 = 探测全 403 = 误熔。现由 gateway.health_whitelist_warnings 在启动时告警。

风格照本目录既有单测：unittest、零第三方依赖、不发真实网络请求。
English: startup-assembly guards — a second report token is rejected at boot (N2) and an
empty ALLOWED_IPS on a non-loopback listener warns about the resulting /health 403 mis-trip (N3).
"""
import contextlib
import inspect  # noqa: E402  # 与 test_gateway/test_order_gates 同法：用源码反射锁住装配接线
import io
import json
import logging
import os
import shutil
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from gateway import (  # noqa: E402
    DEFAULT_CONFIG,
    health_whitelist_warnings,
    load_config,
    main as gateway_main,
    report_token_conflicts,
)


def _cfg(token="tok-A", report_token=""):
    """构造一份最小配置字典（只放本次校验关心的两项，避免被其它默认值牵动）。"""
    return {"token": token, "report_token": report_token}


@contextlib.contextmanager
def _env(**pairs):
    """临时设置/清除环境变量（值为 None 表示本次删除该变量），退出时逐键复原。"""
    saved = {k: os.environ.get(k) for k in pairs}
    try:
        for k, v in pairs.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v
        yield
    finally:
        for k, v in saved.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v


class TestN2ReportTokenGuard(unittest.TestCase):
    """N2：回报口令必须与鉴权口令同源（等值或缺省回退），不等值即拒启。"""

    def setUp(self):
        # 配置文件写临时目录：main 的 PID 隔离日志文件也落在同一目录，测试不污染仓库
        self._tmp = tempfile.mkdtemp(prefix="n2_gw_cfg_")

    def tearDown(self):
        shutil.rmtree(self._tmp, ignore_errors=True)

    def _write_config(self, obj):
        path = os.path.join(self._tmp, "gateway.json")
        with open(path, "w", encoding="utf-8") as f:
            json.dump(obj, f)
        return path

    def test_mismatched_report_token_rejects_startup(self):
        """缺陷原文：配了独立 report_token（≠token）→ 首尔按唯一口令归因，全量 401 静默。
        为何这样修：这类配置不可能工作，启动装配处就该拒绝，而不是让回报流进 outbox 死信。"""
        errs = report_token_conflicts(_cfg(token="tok-A", report_token="tok-B"))
        self.assertTrue(errs, "report_token 与 token 不等值时必须给出错误说明")
        joined = "\n".join(errs)
        # 错误文本必须把「因果 + 处置办法」说清楚（这三处关键词是运维能照做的最小信息量）
        self.assertIn("401", joined)
        self.assertIn("QUANT_GATEWAY_REPORT_TOKEN", joined)
        self.assertIn("rules.qmt.token", joined)
        # 绝不回显口令内容（日志会落盘，口令值不能进日志）
        self.assertNotIn("tok-A", joined)
        self.assertNotIn("tok-B", joined)

    def test_main_exits_2_on_mismatched_report_token(self):
        """main 的接线：校验发生在 load_config 之后、起 HTTP 服务之前，且以 exit(2) 退出。

        为何锁这条：只加函数不加闸等于没修——本用例不真起服务（parse_args 之后、
        绑定端口之前就因口令不符退出），配置文件写在临时目录里，日志文件也跟着落临时目录。"""
        src = inspect.getsource(gateway_main)
        self.assertIn("report_token_conflicts", src, "main 未接入 N2 口令校验闸")
        self.assertIn("sys.exit(2)", src)
        # 顺序断言：先 load_config 再校验（校验必须站在合并完环境变量之后的 cfg 上）
        self.assertLess(src.index("cfg = load_config"), src.index("report_token_conflicts"))
        self.assertLess(src.index("report_token_conflicts"), src.index("ThreadingHTTPServer("))

        # 真闸复现：config 里 token=tok-A、report_token=tok-B 两不相等——main 必须在
        # parse_args/load_config 之后、绑定端口之前以 exit(2) 拒启（N2 的 fail-fast 本体）。
        cfg_path = self._write_config({"token": "tok-A", "report_token": "tok-B"})
        err = None
        captured = io.StringIO()
        root = logging.getLogger()
        handlers_before = list(root.handlers)
        # 环境变量与旧口径一致地覆盖（两 env 同值 tok-A≠cfg 的 tok-B 仍冲突），
        # 并锁 ALLOWED_IPS/None 复位避免本机环境串味；stderr 收集拒启说明文案。
        with _env(QUANT_GATEWAY_TOKEN="tok-A", QUANT_GATEWAY_REPORT_TOKEN="tok-B",
                  ALLOWED_IPS=None, QUANT_GATEWAY_BIND=None):
            with contextlib.redirect_stderr(captured):
                try:
                    gateway_main(["-c", cfg_path])
                except SystemExit as e:
                    err = e
                finally:
                    for h in list(root.handlers):
                        if h not in handlers_before:
                            root.removeHandler(h)
                            h.close()
        self.assertIsNotNone(err, "口令不等值时 main 必须以 sys.exit 退出（不得继续起服务）")
        self.assertEqual(2, err.code)
        self.assertIn("report_token", captured.getvalue())
        self.assertIn("启动被拒", captured.getvalue())

    def test_equal_report_token_passes(self):
        """等值形态行为不变：显式把 report_token 配成与 token 相同 = 放行（向后兼容）。"""
        self.assertEqual([], report_token_conflicts(_cfg(token="tok-A", report_token="tok-A")))
        with _env(QUANT_GATEWAY_TOKEN="tok-A", QUANT_GATEWAY_REPORT_TOKEN="tok-A"):
            cfg = load_config("")
        self.assertEqual(cfg["token"], cfg["report_token"])
        self.assertEqual([], report_token_conflicts(cfg))

    def test_unset_report_token_falls_back_to_token(self):
        """缺省回退不变：不配 report_token 时 load_config 仍自动填成 token（逐字节旧行为），
        且该形态在校验里零错误——修复没有给存量部署增加任何新配置项。"""
        with _env(QUANT_GATEWAY_TOKEN="tok-A", QUANT_GATEWAY_REPORT_TOKEN=None):
            cfg = load_config("")
        self.assertEqual("tok-A", cfg["report_token"])
        self.assertEqual([], report_token_conflicts(cfg))
        # 环境变量显式给了 report_token 才会形成双源（上一条已覆盖拒启），这里再锁默认表口径
        self.assertEqual("", DEFAULT_CONFIG["report_token"])


class TestN3HealthWhitelistWarning(unittest.TestCase):
    """N3：白名单为空 + 非回环监听 = /health 对远程引擎 403，启动必须说清楚。"""

    def test_warns_when_allowed_ips_empty_on_remote_listen(self):
        warnings = health_whitelist_warnings("0.0.0.0:8789", [])
        self.assertTrue(warnings, "非回环监听且白名单为空时必须告警")
        joined = "\n".join(warnings)
        self.assertIn("ALLOWED_IPS", joined)          # 指路：改哪个变量
        self.assertIn("/health", joined)              # 说清受影响的端点
        self.assertIn("403", joined)                  # 说清引擎看到的实况
        self.assertIn("误熔", joined)                 # 说清后果（熔断状态机被探测失败驱动）

    def test_silent_when_allowed_ips_configured(self):
        """白名单已配 → 零告警（不能对正常部署刷屏）。"""
        self.assertEqual([], health_whitelist_warnings("0.0.0.0:8789", ["203.0.113.7"]))

    def test_silent_on_loopback_listen(self):
        """纯回环监听：/health 对本机天然开放，远程引擎本就不该直连 → 不告警。"""
        self.assertEqual([], health_whitelist_warnings("127.0.0.1:8789", []))
        self.assertEqual([], health_whitelist_warnings("::1:8789", []))
        self.assertEqual([], health_whitelist_warnings(":8789", []))

    def test_main_wires_the_warning_after_listen_convergence(self):
        """接线顺序：必须在 listen 收敛（0.0.0.0→127.0.0.1）与 ALLOWED_IPS 解析之后，
        否则会对"已被收敛成本机自用"的部署误报告警。"""
        src = inspect.getsource(gateway_main)
        self.assertIn("health_whitelist_warnings", src)
        self.assertLess(src.index('cfg["listen"] = "127.0.0.1:"'),
                        src.index("health_whitelist_warnings"))
        self.assertLess(src.index("allowed_ips = "), src.index("health_whitelist_warnings"))


if __name__ == "__main__":
    unittest.main()
