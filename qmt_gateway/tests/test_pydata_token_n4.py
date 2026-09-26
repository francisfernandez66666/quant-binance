# -*- coding: utf-8 -*-
"""qmt_gateway/tests/test_pydata_token_n4.py — §N4（2026-09-26 全量审计）pydata sidecar 可选口令。

缺陷原文：cmd/pydata/server.py 全文没有任何 token/Authorization 校验（审计实核 grep 计数 0），
防线只有"绑 127.0.0.1"这一条——同机任意进程都能匿名拉取全市场研究数据；
私有化多租户交付时是暗雷（且 dataload --token 是 Tushare 专用、易误读）。

为何这样修（并这样测）：加**可选** --token，缺省空串 = 匿名放行 = 现网行为逐字节不变；
非空时所有路径都要带 X-Pydata-Token 且逐字相等，否则 401。
拒绝响应刻意用 **"error: unauthorized" 纯文本 + HTTP 401**，不是 JSON：
Go 侧客户端 internal/data/baostock.go 判错只看响应体是否以 "error:" 前缀开头
（baostock.go:69-71），它根本不读 status——JSON 会被当成 CSV 解析成"空表 + 无错误"，
那等于把鉴权失败静默成取数为空，比现在更糟。

三态用例（本文件）：未配置口令（匿名放行，兼容锁）/ 配了但口令错（401）/ 口令对（200）。
English: the optional shared-secret gate on the research sidecar — off by default (byte-identical
to today), and a mismatch answers 401 with the "error:" text protocol the Go client understands.
"""
import os
import sys
import threading
import types
import unittest
import urllib.error
import urllib.request
from http.server import ThreadingHTTPServer

_TESTS_DIR = os.path.dirname(os.path.abspath(__file__))
_GATEWAY_DIR = os.path.dirname(_TESTS_DIR)
_ROOT = os.path.dirname(_GATEWAY_DIR)
sys.path.insert(0, _GATEWAY_DIR)

SERVER_PATH = os.path.join(_ROOT, "cmd", "pydata", "server.py")


def _load_server_module():
    """按文件路径装载 cmd/pydata/server.py（它不是包内模块，不能普通 import）。

    装载前保证 `baostock` 可导入：本机装了就用真的（本测试绝不触发它的网络调用，
    只在 import 期需要名字存在）；没装就注入一个空壳，保证测试在任意机器上可跑。
    """
    try:
        import baostock  # noqa: F401
    except ImportError:
        stub = types.ModuleType("baostock")
        stub.login = lambda *a, **k: types.SimpleNamespace(error_code="0", error_msg="")
        stub.logout = lambda *a, **k: None
        sys.modules["baostock"] = stub
    import importlib.util
    spec = importlib.util.spec_from_file_location("pydata_server_under_test", SERVER_PATH)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


srv = _load_server_module()


def _get(port, token_header=None, path="/health"):
    """发一条 GET，返回 (status, body_text)。401 也算正常返回（urllib 会抛 HTTPError）。"""
    url = "http://127.0.0.1:%d%s" % (port, path)
    req = urllib.request.Request(url, method="GET")
    if token_header is not None:
        req.add_header(srv.TOKEN_HEADER, token_header)
    try:
        with urllib.request.urlopen(req, timeout=5) as resp:
            return resp.status, resp.read().decode("utf-8")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8")


class TestN4PydataTokenGate(unittest.TestCase):
    """sidecar 口令闸三态：未配置=匿名放行 / 配了但不匹配=401 / 匹配=200。"""

    def setUp(self):
        # 每个用例独立端口 + 独立口令，避免类级状态在用例间串味
        self.token_backup = srv._Handler.token
        self.srv = ThreadingHTTPServer(("127.0.0.1", 0), srv._Handler)
        self.port = self.srv.server_address[1]
        self.t = threading.Thread(target=self.srv.serve_forever, daemon=True)
        self.t.start()

    def tearDown(self):
        self.srv.shutdown()
        self.t.join(timeout=5)
        self.srv.server_close()
        srv._Handler.token = self.token_backup

    def test_unset_token_keeps_anonymous_access(self):
        """未配置口令（现网形态）：不带任何头也能取数，逐字节等价于修复前。"""
        srv._Handler.token = ""
        status, body = _get(self.port)
        self.assertEqual(200, status)
        self.assertEqual("ok", body)
        # 配了错误头也不该被拒（口令未启用时头是无意义的，不能反过来把人挡在门外）
        status2, _ = _get(self.port, token_header="whatever")
        self.assertEqual(200, status2)

    def test_wrong_token_rejected_with_error_prefix(self):
        """口令不匹配 → 401，且响应体必须是 "error:" 前缀（Go 客户端唯一的错误协议形态）。"""
        srv._Handler.token = "s3cr3t"
        for bad in ("", "wrong", "s3cr3t ", "S3CR3T"):
            status, body = _get(self.port, token_header=bad)
            self.assertEqual(401, status, "口令 %r 应被拒" % bad)
            self.assertTrue(body.startswith("error:"),
                            "拒绝响应必须是 error: 前缀文本，否则 Go 侧会当成空表静默通过：%r" % body)
            self.assertIn("unauthorized", body)

    def test_correct_token_passes(self):
        """口令匹配 → 200 正常回数据。"""
        srv._Handler.token = "s3cr3t"
        status, body = _get(self.port, token_header="s3cr3t")
        self.assertEqual(200, status)
        self.assertEqual("ok", body)

    def test_all_paths_gated_including_health(self):
        """/health 也不例外：探测端点同样会泄露"这台机器上有研究数据服务"。"""
        srv._Handler.token = "s3cr3t"
        for path in ("/health", "/trade_days", "/kline", "/no_such_route"):
            status, body = _get(self.port, path=path)
            self.assertEqual(401, status, "%s 未过口令闸" % path)
            self.assertTrue(body.startswith("error:"), "%s 响应形态不兼容 Go 客户端" % path)

    def test_token_error_body_unit(self):
        """token_error_body 的纯函数口径：空口令放行、匹配放行、不匹配回错误文本。"""
        self.assertIsNone(srv.token_error_body("", ""))
        self.assertIsNone(srv.token_error_body("", "anything"))
        self.assertIsNone(srv.token_error_body("s3cr3t", "s3cr3t"))
        self.assertEqual("error: unauthorized", srv.token_error_body("s3cr3t", "nope"))
        self.assertEqual("error: unauthorized", srv.token_error_body("s3cr3t", ""))

    def test_main_wires_the_flag(self):
        """接线锁：main 必须解析 --token 并注入 _Handler（只加函数不接入口等于没修）。"""
        import inspect
        src = inspect.getsource(srv.main)
        self.assertIn('"--token"', src)
        self.assertIn("_Handler.token", src)
        # 缺省值取 env QUANT_PYDATA_TOKEN：与 Go 侧来源链同名，配一次两侧对齐
        self.assertIn("QUANT_PYDATA_TOKEN", src)


if __name__ == "__main__":
    unittest.main()
