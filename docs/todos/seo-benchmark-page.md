# SEO: 性能基准测试页面

## 优先级: P1

## 目标
在 kaitu.io 建立 `/benchmarks` 页面，用实测数据展示 k2 在不同丢包率下的性能优势。

## 动机
- "30% 丢包满速" 需要公开可验证的数据支撑
- 基准测试页面自带外链吸引力（技术社区喜欢引用数据）
- 满足 E-E-A-T 可信度标准

## 内容规划
1. 使用 iperf3 在 5%、10%、20%、30% 丢包率下测试：
   - k2 (k2arc/k2cc)
   - WireGuard
   - Hysteria2
   - 裸 TCP (baseline)
2. 生成吞吐量曲线对比图表
3. 测试环境描述（VPS 配置、tc netem 丢包模拟参数）
4. 可复现的测试脚本

## 技术实现
- Next.js 页面 `/benchmarks`
- 图表用 recharts 或静态 SVG
- 数据来源：实测后硬编码或 JSON 数据文件
