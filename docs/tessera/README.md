# Tessera（`k2t`）

抗主动探测的传输层伪装协议，用于替代 k2v5 当前基于 ECH 的伪装层。

**状态**：设计中（Design Draft）。尚无实现，尚未定案。

## 名字

罗马的 *tessera hospitalis* 是一枚掰成两半的信物：两个素不相识的人各持一半，合得上就认作自己人；旁观者只看到一块普通陶片。

这就是本协议的判别机制——客户端出示的"那一半"藏在 ClientHello 里本来就该是随机数的字段中，只有持有另一半（节点私钥）的服务端能对上。观察者既看不出它在场，也无法伪造它。

## 文档

| 文档 | 内容 |
|---|---|
| [spec.md](spec.md) | 完整协议规范：威胁模型、判别机制、借壳转发、一致性要求、迁移路径 |

设计中的每一处"可行"都由可执行测试背书，而不是推理。`go test ./docs/tessera/spike/`：

| 证据 | 回答的问题 |
|---|---|
| [spike/feasibility_test.go](spike/feasibility_test.go) | TCP 判别能否在现有 uTLS 上实现（§12） |
| [spike/quic_test.go](spike/quic_test.go) | QUIC 上判别载体与借壳转发怎么落地（§6） |
| [spike/timing_test.go](spike/timing_test.go) | 两条路径的时序差有多可分、缓解是否有效（§13） |
| [spike/residual_test.go](spike/residual_test.go) | 缓解后的残余在大样本下会不会被攒出来（§13.4） |
| [spike/shape_test.go](spike/shape_test.go) | 流量形状这条轴有多大，以及为什么它的零假设取不到（§14） |
| [tools/ech_survey.py](tools/ech_survey.py) | 现实世界到底有多少域名部署了 ECH（§1.2） |
| [tools/front_survey.py](tools/front_survey.py) | 候选 front 在流量类别与 IP 分散度上的实测评分（§14.5） |

## 一句话概括

**判别靠密码学秘密而非协议标志位；任何认证失败的连接——包括所有主动探测——被整条透明转发给一个真实网站，探测者拿到真证书真内容，无法证实节点身份。**

## 与 k2v5 的关系

Tessera 是**新增传输类型**，不替换、不修改 k2v5。两者在节点上并存，由 Center 按客户端版本决定下发哪一种。存量客户端零感知。

隧道层（stream 复用、metadata 设备认证、k2cc 拥塞控制）完全复用 k2v5 的现有实现——Tessera 只替换"如何建立一条不被识别的加密通道"这一层。
