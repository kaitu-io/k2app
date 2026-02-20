分层硬删除 + 统一 OperationLog：统一删除策略，移除 DeletedAt，扩展 CloudOperationLog 为通用 OperationLog

Scrum 结论：节点数据 source of truth 在节点自身（自注册），DB 是缓存。软删除对这类数据没有恢复价值。

Action:
1. Node/Tunnel: 硬删除 + OperationLog 元数据
2. NodeCloudMeta: 硬删除 + admin 二次确认 + OperationLog 详细
3. 运行时数据（SlaveNodeLoad, SessionAcct）: 硬删除无日志
4. 扩展 CloudOperationLog → OperationLog（entity_type + entity_id + action + metadata JSON）
5. 移除 Node/Tunnel model 的 DeletedAt 字段
6. OperationLog 按月归档或设置 TTL（保留 180 天）
