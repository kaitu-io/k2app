Center API /app/nodes/batch-matrix 接口需要返回节点的 active/inactive 状态字段。

当前 API 没有 status 字段，导致调用方只能用 tunnelCount == 0 + name 是否为 IP 这种不可靠的启发式来判断节点是否活跃。应该在 Node model 或 API response 中增加明确的状态标记（如 status: "active" | "inactive" | "decommissioned"），让 deploy-compose.sh 和 list_nodes MCP tool 能正确过滤。
