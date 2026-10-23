// 独立校验 research_format.js（vitest worker 在本沙箱无法启动，用纯 Node ESM 等价验证行为）。
import {
  fmtPctGlobal, pct, fmtRows, fmt, signClass, signColor, fmtNum,
  weightList, depthSummary, kindLabel, factorDirs, parseReason,
  fmtPct, verdict, plainLines, btTested,
} from './src/pages/research_format.js'

let pass = 0, fail = 0
function eq(actual, expected, msg) {
  const a = JSON.stringify(actual), e = JSON.stringify(expected)
  if (a === e) { pass++ } else { fail++; console.log(`FAIL ${msg}: got ${a} want ${e}`) }
}
function ok(cond, msg) { if (cond) pass++; else { fail++; console.log(`FAIL ${msg}`) } }
function close(actual, expected, msg) { ok(Math.abs(actual - expected) < 1e-9, `${msg} (${actual} ~ ${expected})`) }

// fmtPctGlobal
eq(fmtPctGlobal(null), '-', 'fmtPctGlobal null')
eq(fmtPctGlobal(undefined), '-', 'fmtPctGlobal undefined')
eq(fmtPctGlobal(NaN), '-', 'fmtPctGlobal NaN')
eq(fmtPctGlobal(12.345), '12.35%', 'fmtPctGlobal 12.345')
eq(fmtPctGlobal(0), '0.00%', 'fmtPctGlobal 0')

// pct
eq(pct(null), 0, 'pct null')
eq(pct(undefined), 0, 'pct undefined')
eq(pct(NaN), 0, 'pct NaN')
eq(pct(0.3456), 35, 'pct 0.3456')
eq(pct(1.5), 100, 'pct clamp 100')
eq(pct(0.9999), 100, 'pct clamp 99.99')

// fmtRows
eq(fmtRows(null), '-', 'fmtRows null')
eq(fmtRows(undefined), '-', 'fmtRows undefined')
eq(fmtRows(NaN), '-', 'fmtRows NaN')
eq(fmtRows(1234567), '1,234,567', 'fmtRows thousands')

// fmt
eq(fmt(null), '-', 'fmt null')
eq(fmt(NaN), '-', 'fmt NaN')
eq(fmt(0.123456), '0.1235', 'fmt 4dp')
eq(fmt(-0.1), '-0.1000', 'fmt neg')

// signClass
eq(signClass(null), '', 'signClass null')
eq(signClass(NaN), '', 'signClass NaN')
eq(signClass(0), 'pos', 'signClass 0')
eq(signClass(1.2), 'pos', 'signClass pos')
eq(signClass(-0.5), 'neg', 'signClass neg')

// signColor
eq(signColor(undefined), 'var(--app-faint)', 'signColor undef')
eq(signColor(3), 'var(--app-up)', 'signColor up')
eq(signColor(-3), 'var(--app-down)', 'signColor down')

// fmtNum
eq(fmtNum(null), '-', 'fmtNum null')
eq(fmtNum(NaN), '-', 'fmtNum NaN')
eq(fmtNum(5), '5', 'fmtNum default')
eq(fmtNum(5.678, 2), '5.68', 'fmtNum 2dp')

// weightList
eq(weightList({ weights: '{"weights":{"f1":0.6,"f2":0.4},"directions":{"f1":1}}' }), [['f1',0.6],['f2',0.4]], 'weightList nested')
eq(weightList({ weights: '{"f1":0.9,"f2":0.1}' }), [['f1',0.9],['f2',0.1]], 'weightList flat')
eq(weightList({ weights: '{"f1":0.6,"f2":"x"}' }), [['f1',0.6]], 'weightList filter')
eq(weightList({ weights: '{bad' }), [], 'weightList bad json')
eq(weightList({}), [], 'weightList empty')

// depthSummary
eq(depthSummary({ weights: '{"a":1}' }), { a: 1 }, 'depthSummary ok')
eq(depthSummary({ weights: 'xxx' }), {}, 'depthSummary bad')

// kindLabel
eq(kindLabel('factor'), '因子战法', 'kindLabel factor')
eq(kindLabel('pattern'), '形态战法', 'kindLabel pattern')
eq(kindLabel('weights'), '权重优化', 'kindLabel weights')
eq(kindLabel('depth'), '盘口扫描', 'kindLabel depth')
eq(kindLabel('unknown'), 'unknown', 'kindLabel unknown')

// factorDirs
eq(factorDirs({ weights: '{"directions":{"f1":1,"f2":-1}}' }), { f1: 1, f2: -1 }, 'factorDirs ok')
eq(factorDirs({ weights: '{"a":1}' }), {}, 'factorDirs no dirs')
eq(factorDirs({ weights: 'bad' }), {}, 'factorDirs bad')
eq(factorDirs({}), {}, 'factorDirs empty')

// parseReason
const cR = { reason: '样本内IR=0.52;样本外IR=0.41;反推超额=0.07' }
close(parseReason(cR, '样本内IR'), 0.52, 'parseReason 样本内IR')
close(parseReason(cR, '样本外IR'), 0.41, 'parseReason 样本外IR')
close(parseReason(cR, '反推超额'), 0.07, 'parseReason 反推超额')
eq(parseReason({ reason: '样本内IR=0.5' }, '样本外IR'), null, 'parseReason missing')

// fmtPct
eq(fmtPct(null), '-', 'fmtPct null')
eq(fmtPct(NaN), '-', 'fmtPct NaN')
eq(fmtPct(0.07), '+7.0%', 'fmtPct 0.07')
eq(fmtPct(-0.1), '-10.0%', 'fmtPct -0.1')

// verdict
const passR = { reason: '样本内IR=0.52;样本外IR=0.41;反推超额=0.07' }
eq(verdict(passR).ok, true, 'verdict pass ok')
ok(verdict(passR).text.includes('不是碰运气'), 'verdict pass text')
eq(verdict({ reason: '样本内IR=0.2;样本外IR=0.41;反推超额=0.07' }).ok, false, 'verdict low insample')
ok(verdict({ reason: '样本内IR=0.2;样本外IR=0.41;反推超额=0.07' }).text.includes('不建议直接拿来实盘'), 'verdict fail text')
eq(verdict({ reason: '样本内IR=0.5;样本外IR=0.5;反推超额=-0.1' }).ok, false, 'verdict neg gen')
eq(verdict({}).ok, true, 'verdict empty default pass')

// plainLines
eq(plainLines({ reason: '样本内IR=0.52' }), ['先拿前半段历史行情回放：这套打分的选股效果明显（稳定度 0.5200，越高越靠谱）。'], 'plainLines insample')
ok(plainLines({ reason: '反推超额=0.07' })[0].includes('多赚'), 'plainLines gen 多赚')
ok(plainLines({ reason: '反推超额=0.07' })[0].includes('+7.0%'), 'plainLines gen +7.0%')
eq(plainLines({ reason: '通过护栏测试' }), ['电脑验证通过，这条规律可以试试。'], 'plainLines guard')
eq(plainLines({ reason: '随便写' }), ['这条规律未通过验证。'], 'plainLines notpass')

// btTested
eq(btTested({ backtest_done: true }), true, 'btTested done true')
eq(btTested({ backtest_done: false }), false, 'btTested done false')
eq(btTested({ avg_excess: 0.5 }), true, 'btTested avg>0')
eq(btTested({ avg_excess: 0 }), false, 'btTested avg 0')
eq(btTested({}), true, 'btTested empty (undefined!==0)')

console.log(`\nresearch_format 校验：pass=${pass} fail=${fail}`)
process.exit(fail === 0 ? 0 : 1)
