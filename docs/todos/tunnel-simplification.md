Tunnel 精简 + Display URL：删除 deprecated 字段，添加 URL() display method，重命名 SlaveTunnel→Tunnel

Scrum 结论：URL 不作为存储格式，保持结构化字段。精简冗余字段降低复杂度，URL() 方法满足人类可读性。

Action:
1. 删除 SlaveTunnel.SecretToken（已 deprecated）
2. 删除 SlaveTunnel.Name（用 domain 即可）
3. 审查 HasTunnel/HasRelay 语义，简化
4. 添加 URL() string 方法作为 human-readable 标识
5. 重命名 SlaveTunnel → Tunnel（配合 Node 去 Slave 前缀）
6. 存储和传输保持结构化字段
