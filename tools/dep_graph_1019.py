#!/usr/bin/env python3
# ── 独立依赖环检测 + 拓扑分层（2026-10-19 全栈审计本轮新写）──
# 用途：不依赖项目任何工具/断言，直接扫 internal + cmd 生产代码（排除 _test.go），
# 从 import 字面量建「包级」有向图，DFS 检环 + 拓扑最长路径分层，输出：
#   - 内部包数 / 依赖边数 / 环数量（含环路径前 5 条）
#   - 拓扑层级（L0 顶层 → Ln 底层）+ 每层最大的几个包（带 LOC）
# 用法：python3 tools/dep_graph_1019.py
# 实测（HEAD e7e6b8c）：56 包 / 260 边 / 0 环 / 15 层
# English: standalone package dependency graph builder — cycle detection + topological
# layering over internal/cmd production code (test files excluded).
import os
import re
import collections

SKIP = {'node_modules', '.git', '.uat-data', 'dist', 'test-results',
        'coverage', '__pycache__'}
PREFIX = 'quant-trading-v2/'
BASES = ('internal', 'cmd')


def collect_files():
    files = []
    for base in BASES:
        for dp, dn, fn in os.walk(base):
            dn[:] = [d for d in dn if d not in SKIP]
            for f in fn:
                # 只扫生产代码：_test.go 的 import 会引入测试专用依赖，污染图
                if f.endswith('.go') and not f.endswith('_test.go'):
                    files.append(os.path.join(dp, f))
    return files


def pkg_of(path):
    return PREFIX + os.path.dirname(path)


def build_edges(files):
    edges = collections.defaultdict(set)
    loc = collections.defaultdict(int)
    for p in files:
        src = open(p, encoding='utf-8', errors='replace').read()
        loc[pkg_of(p)] += sum(1 for _ in open(p, 'rb'))
        for m in re.finditer(r'"(%s[^"]+)"' % re.escape(PREFIX), src):
            tgt = m.group(1)
            if tgt != pkg_of(p):  # 自引用不算边
                edges[pkg_of(p)].add(tgt)
    return edges, loc


def find_cycles(edges, nodes):
    WHITE, GRAY, BLACK = 0, 1, 2
    color = collections.defaultdict(int)
    cycles = []

    def dfs(u, stack):
        color[u] = GRAY
        stack.append(u)
        for v in sorted(edges.get(u, ())):
            if color[v] == GRAY:               # 回边 = 环
                cycles.append(stack[stack.index(v):] + [v])
            elif color[v] == WHITE:
                dfs(v, stack)
        stack.pop()
        color[u] = BLACK

    for n in sorted(nodes):
        if color[n] == WHITE:
            dfs(n, [])
    return cycles


def topo_levels(edges, nodes):
    """拓扑最长路径分层：L0 = 无入度（顶层入口），层数越大越底层。"""
    indeg = {p: sum(1 for v in edges.values() if p in v) for p in nodes}
    level = {p: 0 for p in nodes}
    q = [p for p in nodes if indeg[p] == 0]
    seen = 0
    while q:
        u = q.pop(0)
        seen += 1
        for v in sorted(edges.get(u, ())):
            level[v] = max(level[v], level[u] + 1)
            indeg[v] -= 1
            if indeg[v] == 0:
                q.append(v)
    return level, seen == len(nodes)


def main():
    files = collect_files()
    edges, loc = build_edges(files)
    nodes = set(edges) | {t for v in edges.values() for t in v}
    cycles = find_cycles(edges, nodes)
    level, sorted_ok = topo_levels(edges, nodes)

    print('production files scanned : %d' % len(files))
    print('internal packages        : %d' % len(nodes))
    print('dependency edges         : %d' % sum(len(v) for v in edges.values()))
    print('CYCLES                   : %d' % len(cycles))
    for c in cycles[:5]:
        print('   cycle: ' + ' -> '.join(x.replace(PREFIX, '') for x in c[:8]))
    print('topological sort ok      : %s (= no cycle, cross-check)' % sorted_ok)
    print('max level                : %d' % max(level.values()))

    by = collections.defaultdict(list)
    for p, lv in level.items():
        by[lv].append(p)
    print('\n--- layering (L0 top -> Ln bottom) ---')
    for lv in sorted(by):
        ps = sorted(by[lv], key=lambda x: -loc[x])[:5]
        names = ', '.join(
            '%s(%dk)' % (p.replace(PREFIX + 'internal/', '')
                          .replace(PREFIX + 'cmd/', 'cmd/'), loc[p] // 1000)
            for p in ps)
        print('L%-2d %s' % (lv, names))


if __name__ == '__main__':
    main()
