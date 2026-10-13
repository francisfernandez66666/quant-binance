#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""针对性诊断：IBV1 报的 3 条 401（低权应 403 / 非法 JSON 应 400）到底是
产品缺陷、登录频控、还是脚手架问题。只读 + 最小副作用。"""
import json, os, sys, time, urllib.request, urllib.error
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BASE = os.environ.get("E2E_API", "http://127.0.0.1:18080")
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def http(method, path, token=None, body=None, raw_body=None, timeout=15):
    data = None
    if raw_body is not None:
        data = raw_body.encode() if isinstance(raw_body, str) else raw_body
    elif body is not None:
        data = json.dumps(body, ensure_ascii=False).encode()
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("Accept", "application/json")
    if data is not None:
        req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with OPENER.open(req, timeout=timeout) as r:
            return r.status, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()
    except Exception as e:
        return 0, str(e).encode()


def login(u, p):
    st, b = http("POST", "/api/auth/login", body={"username": u, "password": p})
    try:
        j = json.loads(b.decode())
    except Exception:
        j = {}
    return st, j.get("token", ""), b[:120]


print("=== 诊断 1：单个 tester 登录后立即访问 admin 端点 ===")
st, tok, raw = login("tester", "Tester!2026")
print(f"  login http={st} token_len={len(tok)} body={raw}")
if tok:
    st2, b2 = http("GET", "/api/admin/users", token=tok)
    print(f"  GET /api/admin/users (低权) -> http={st2}  body={b2[:100]}")
    st3, b3 = http("GET", "/api/research/backtest-config", token=tok)
    print(f"  GET /api/research/backtest-config (perm) -> http={st3}  body={b3[:100]}")
    st4, b4 = http("POST", "/api/watchlist", token=tok, raw_body="{not-json")
    print(f"  POST /api/watchlist 非法JSON (低权) -> http={st4}  body={b4[:100]}")
    st5, b5 = http("POST", "/api/watchlist", token=tok, raw_body=json.dumps({"code": "600519"}))
    print(f"  POST /api/watchlist 合法JSON (低权) -> http={st5}  body={b5[:100]}")
    http("DELETE", "/api/watchlist", token=tok, raw_body=json.dumps({"code": "600519"}))

print("\n=== 诊断 2：登录频控边界（同 IP 连续登录）===")
for i in range(12):
    st, tok, raw = login("tester", "Tester!2026")
    print(f"  第{i+1:2d} 次登录 -> http={st} token_len={len(tok)} {raw[:70]}")
    if st == 429:
        print("  >>> 触发 429 频控，后续 login() 会拿到空 token → 表现为 401")
        break

print("\n=== 诊断 3：频控冷却后恢复 ===")
time.sleep(62)
st, tok, raw = login("tester", "Tester!2026")
print(f"  冷却 62s 后登录 -> http={st} token_len={len(tok)}")
if tok:
    st2, b2 = http("GET", "/api/admin/users", token=tok)
    print(f"  GET /api/admin/users (低权) -> http={st2}  body={b2[:100]}")
