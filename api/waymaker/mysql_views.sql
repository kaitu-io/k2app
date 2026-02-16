-- WayMaker 兼容层 - MySQL 跨库 VIEW
-- 用途：让 wgcenter 通过 VIEW 读取 kaitu 数据库的节点和隧道信息
-- 前提：kaitu 和 wgcenter 必须在同一 MySQL 实例
--
-- 使用方法：
-- 1. 确保 MySQL 用户对 kaitu 和 wgcenter 两个数据库都有权限
-- 2. 在 wgcenter 数据库中执行此脚本
-- 3. 修改 wgcenter 代码使用 VIEW（或重命名表并将 VIEW 命名为原表名）
--
-- 清理方法：当 WayMaker 旧客户端下线后，删除这些 VIEW

-- ============================================
-- VIEW 1: v_host_nodes
-- 映射 kaitu.slave_nodes -> host_nodes 结构
-- ============================================
CREATE OR REPLACE VIEW v_host_nodes AS
SELECT
    id,
    CONCAT('kaitu-', ipv4) AS uuid,              -- 生成唯一 UUID
    secret_token AS secret,
    'waymaker' AS node_type,                      -- 固定为 waymaker 类型
    name,
    country,
    JSON_ARRAY(JSON_OBJECT('ip', ipv4)) AS ipv4s, -- 转换为 JSON 格式
    0 AS support_chatgpt_web,
    0 AS support_chatgpt_app,
    created_at,
    updated_at,
    deleted_at
FROM kaitu.slave_nodes
WHERE deleted_at IS NULL;

-- ============================================
-- VIEW 2: v_host_services
-- 映射 kaitu.slave_tunnels -> host_services 结构
-- 端口固定为 10001（WayMaker 使用端口）
-- ============================================
CREATE OR REPLACE VIEW v_host_services AS
SELECT
    t.id,
    t.name,
    t.domain,
    t.node_id,
    10001 AS node_port,                           -- 固定端口 10001
    0 AS is_http,
    1 AS is_global,
    t.created_at,
    t.updated_at
FROM kaitu.slave_tunnels t
JOIN kaitu.slave_nodes n ON t.node_id = n.id
WHERE t.deleted_at IS NULL
  AND n.deleted_at IS NULL
  AND t.protocol = 'k2oc';            -- 只映射兼容协议

-- ============================================
-- 验证 VIEW 创建成功
-- ============================================
-- SELECT * FROM v_host_nodes LIMIT 5;
-- SELECT * FROM v_host_services LIMIT 5;

-- ============================================
-- 可选：替换原表（谨慎操作）
-- ============================================
-- 如果要让 wgcenter 无需修改代码即可使用 VIEW：
-- 1. 备份原表
--    RENAME TABLE host_nodes TO host_nodes_legacy;
--    RENAME TABLE host_services TO host_services_legacy;
-- 2. 将 VIEW 命名为原表名
--    CREATE OR REPLACE VIEW host_nodes AS SELECT * FROM v_host_nodes;
--    CREATE OR REPLACE VIEW host_services AS SELECT * FROM v_host_services;
