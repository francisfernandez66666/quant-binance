// 文件职责：§CN-MASTER 前端导航锁（源码级静态锁，App.jsx 无整树渲染 harness，
// 装配真值由 npm run build + 后端契约锁兜底）。三条：
//  1. /api/status 回传的 cn_master 只认显式布尔（undefined=旧服务端→维持全显，向后兼容）；
//  2. CN 专属入口（信号/自选/热点/情绪回看/消息/股票咨询/自动研究）必须带 !cnOff 前置——
//     关链时侧栏隐藏；
//  3. 负向锁：仪表盘/持仓/量化交易三个承载币安链观测与账本的路由不得被 cnOff 误藏
//     （根路径重定向依赖仪表盘）。
// English: static source locks for §CN-MASTER nav hiding.
import { readFileSync } from 'fs'
import { resolve } from 'path'

const src = readFileSync(resolve(__dirname, '../App.jsx'), 'utf8')

const CN_ONLY = ['/signals', '/watchlist', '/hotspot', '/emotion', '/msgcenter', '/consult', '/research']
const MUST_STAY = ['/dashboard', '/positions', '/quant']

describe('§CN-MASTER 导航隐藏接线', () => {
  it('cn_master 只认显式布尔，缺字段维持 null（旧服务端不误藏）', () => {
    expect(src).toMatch(/setCnMaster\(typeof st\.cn_master === 'boolean' \? st\.cn_master : null\)/)
    expect(src).toMatch(/const cnOff = cnMaster === false/)
  })

  it.each(CN_ONLY)('CN 专属入口 %s 带 !cnOff 前置', (route) => {
    // 宽松窗口匹配：'/research' 一项是 `!cnOff && (canResearch ? {...})` 双层条件，
    // 严格相邻式正则会把这种合法写法误判为缺失（假红）。
    const re = new RegExp(`!cnOff && .{0,40}to: '${route.replace('/', '\\/')}'`)
    expect(re.test(src)).toBe(true)
  })

  it.each(MUST_STAY)('保留入口 %s 未被 cnOff 误藏', (route) => {
    const re = new RegExp(`!cnOff && .{0,40}to: '${route.replace('/', '\\/')}'`)
    expect(re.test(src)).toBe(false)
  })
})
