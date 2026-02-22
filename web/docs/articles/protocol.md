---
status: generated
output: web/content/zh-CN/k2/protocol.md
last_generated: 2026-02-22
eeat_score: High
---

# k2arc 自适应速率控制算法

## Content Brief

| Field | Value |
|-------|-------|
| Primary KW | k2arc 拥塞控制算法 |
| Secondary KW | 自适应速率控制, 效用函数梯度上升, 自适应丢包惩罚, GFW 拥塞控制, PCC Vivace |
| Intent | informational (deep technical understanding of algorithm) |
| Audience | 技术用户、网络工程师、对拥塞控制算法感兴趣的开发者 |
| Tone | 技术性、精确、源码级准确 |
| Angle | 从源码出发的算法深度解析，展示每个参数的精确值和设计理由 |
| Word Count | 3000+ |
| CTA | 使用 k2 体验 k2arc / 查看性能对比 |

## Acceptance Criteria

- AC1: 标题聚焦 k2arc 拥塞控制算法
- AC2: 包含完整效用函数公式及每个分量的精确系数值（α, β, ε）
- AC3: 三阶段状态机（慢启动、决策、速率调整）的转换条件精确到源码值
- AC4: 自适应 α 的完整计算公式及 EWMA 平滑机制
- AC5: k2arc vs 标准 PCC 的参数差异对比表
- AC6: 包含协议其他技术要素（URL格式、身份体系、ECH、传输层等）
- AC7: FAQ section
- AC8: CORE-EEAT score >= Medium

## Internal Links

- /k2/vs-hysteria2
- /k2/stealth
- /k2/quickstart
- /k2/vs-reality

## Notes

- 所有数值直接来自 k2/wire/pcc/pcc.go 源码
- 文章重心从"协议技术详解"转向"k2arc 算法详解 + 协议补充"
- 保留 URL 格式、身份体系、ECH、传输层等段落但作为辅助内容
