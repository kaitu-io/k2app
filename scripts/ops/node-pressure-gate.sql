-- 节点压力校验门（进入第三阶段前人工确认）。只读：本文件只含 SELECT。
-- 规格：docs/superpowers/specs/2026-11-01-node-pressure-design.md「校验门」
-- 运行：mysql -h <host> -u <user> -p kaitu < scripts/ops/node-pressure-gate.sql
-- 库：MariaDB 10.6（无 CORR，R² 用 Pearson 公式 + SUM/COUNT 聚合）。
--
-- 口径：窗口 14 天；metrics_version >= 1（新 sidecar）；steal_percent <= 5（宿主机抢占会扭曲 busy%）；
--       Mbps = bandwidth_up_mbps + bandwidth_down_mbps；busy = cpu_percent。
--       拟合只用 Mbps >= 20 的样本（低流量时 CPU 被基线噪声主导）。
-- 表为空时每条语句都返回结果（分母为 0 时比例为 NULL，不报错）。

-- ---------------------------------------------------------------------
-- 0. 逐节点明细：样本数、R²、斜率/截距、观测峰值、峰值时 busy%、拟合预测与偏差、M 与来源
-- ---------------------------------------------------------------------
WITH s AS (
  SELECT ipv4, created_at, (bandwidth_up_mbps + bandwidth_down_mbps) AS x, cpu_percent AS y
  FROM slave_node_loads
  WHERE created_at >= NOW() - INTERVAL 14 DAY
    AND metrics_version >= 1
    AND steal_percent <= 5
),
q AS (SELECT * FROM s WHERE x >= 20),
agg AS (
  SELECT ipv4,
         COUNT(*) AS n,
         SUM(x) AS sx, SUM(y) AS sy, SUM(x*x) AS sxx, SUM(y*y) AS syy, SUM(x*y) AS sxy
  FROM q GROUP BY ipv4
),
fit AS (
  SELECT ipv4, n,
         (n*sxy - sx*sy) / NULLIF(n*sxx - sx*sx, 0) AS slope,
         (sy - (n*sxy - sx*sy) / NULLIF(n*sxx - sx*sx, 0) * sx) / n AS intercept,
         CASE WHEN (n*sxx - sx*sx) > 0 AND (n*syy - sy*sy) > 0
              THEN POW(n*sxy - sx*sy, 2) / ((n*sxx - sx*sx) * (n*syy - sy*sy))
         END AS r2
  FROM agg
),
ranked AS (
  SELECT ipv4, x, y, created_at,
         ROW_NUMBER() OVER (PARTITION BY ipv4 ORDER BY x DESC, created_at DESC) AS rn
  FROM s
),
peak AS (SELECT ipv4, x AS peak_mbps, y AS busy_at_peak, created_at AS peak_at FROM ranked WHERE rn = 1)
SELECT p.ipv4,
       COALESCE(f.n, 0)                         AS samples_ge20mbps,
       ROUND(f.r2, 3)                           AS r2,
       ROUND(f.slope, 4)                        AS slope_busy_per_mbps,
       ROUND(f.intercept, 2)                    AS intercept,
       ROUND(p.peak_mbps, 1)                    AS peak_mbps,
       p.peak_at,
       ROUND(p.busy_at_peak, 1)                 AS busy_at_peak,
       ROUND(f.intercept + f.slope * p.peak_mbps, 1)               AS fit_busy_at_peak,
       ROUND(p.busy_at_peak - (f.intercept + f.slope * p.peak_mbps), 1) AS busy_minus_fit,
       u.capacity_mbps,
       u.capacity_source
FROM peak p
LEFT JOIN fit f ON f.ipv4 = p.ipv4
LEFT JOIN node_usages u ON u.ipv4 = p.ipv4
ORDER BY p.peak_mbps DESC;

-- ---------------------------------------------------------------------
-- 1. 门 1：有 >=30 个 Mbps>=20 样本的节点中，R² >= 0.6 的比例（目标 >= 60%）
-- ---------------------------------------------------------------------
WITH q AS (
  SELECT ipv4, (bandwidth_up_mbps + bandwidth_down_mbps) AS x, cpu_percent AS y
  FROM slave_node_loads
  WHERE created_at >= NOW() - INTERVAL 14 DAY
    AND metrics_version >= 1
    AND steal_percent <= 5
    AND (bandwidth_up_mbps + bandwidth_down_mbps) >= 20
),
agg AS (
  SELECT ipv4, COUNT(*) AS n,
         SUM(x) AS sx, SUM(y) AS sy, SUM(x*x) AS sxx, SUM(y*y) AS syy, SUM(x*y) AS sxy
  FROM q GROUP BY ipv4
),
r AS (
  SELECT ipv4, n,
         CASE WHEN (n*sxx - sx*sx) > 0 AND (n*syy - sy*sy) > 0
              THEN POW(n*sxy - sx*sy, 2) / ((n*sxx - sx*sx) * (n*syy - sy*sy))
         END AS r2
  FROM agg WHERE n >= 30
)
SELECT COUNT(*)                                   AS qualified_nodes,
       COALESCE(SUM(r2 >= 0.6), 0)                AS nodes_r2_ge_0_6,
       ROUND(100 * SUM(r2 >= 0.6) / NULLIF(COUNT(*), 0), 1) AS pct_r2_ge_0_6,
       '目标 >= 60'                               AS target,
       COALESCE(ROUND(100 * SUM(r2 >= 0.6) / NULLIF(COUNT(*), 0), 1) >= 60, 0) AS pass
FROM r;

-- ---------------------------------------------------------------------
-- 2. 门 2：出现过 >=100 Mbps 峰值的节点数（目标 >= 3）
--    峰值时 busy% 与该节点拟合预测的偏差见语句 0 的 busy_minus_fit（要求 |偏差| <= 15 个百分点）。
--    本语句同时给出满足偏差条件的节点数。
-- ---------------------------------------------------------------------
WITH s AS (
  SELECT ipv4, created_at, (bandwidth_up_mbps + bandwidth_down_mbps) AS x, cpu_percent AS y
  FROM slave_node_loads
  WHERE created_at >= NOW() - INTERVAL 14 DAY
    AND metrics_version >= 1
    AND steal_percent <= 5
),
agg AS (
  SELECT ipv4, COUNT(*) AS n,
         SUM(x) AS sx, SUM(y) AS sy, SUM(x*x) AS sxx, SUM(y*y) AS syy, SUM(x*y) AS sxy
  FROM s WHERE x >= 20 GROUP BY ipv4
),
fit AS (
  SELECT ipv4,
         (n*sxy - sx*sy) / NULLIF(n*sxx - sx*sx, 0) AS slope,
         (sy - (n*sxy - sx*sy) / NULLIF(n*sxx - sx*sx, 0) * sx) / n AS intercept
  FROM agg
),
ranked AS (
  SELECT ipv4, x, y,
         ROW_NUMBER() OVER (PARTITION BY ipv4 ORDER BY x DESC, created_at DESC) AS rn
  FROM s
),
peak AS (SELECT ipv4, x AS peak_mbps, y AS busy_at_peak FROM ranked WHERE rn = 1 AND x >= 100)
SELECT COUNT(*)                                            AS nodes_peak_ge_100mbps,
       COALESCE(SUM(ABS(p.busy_at_peak - (f.intercept + f.slope * p.peak_mbps)) <= 15), 0)
                                                           AS nodes_within_15pp_of_fit,
       '目标 >= 3（且均在 15pp 内）'                         AS target,
       COALESCE(SUM(ABS(p.busy_at_peak - (f.intercept + f.slope * p.peak_mbps)) <= 15) >= 3, 0) AS pass
FROM peak p
LEFT JOIN fit f ON f.ipv4 = p.ipv4;

-- ---------------------------------------------------------------------
-- 3. 门 3：在线节点中近 1 小时内有 metrics_version >= 1 样本的比例（目标 >= 80%）
--    在线节点 = slave_nodes 中未软删除的行。
-- ---------------------------------------------------------------------
SELECT COUNT(*)                                            AS live_nodes,
       COALESCE(SUM(l.ipv4 IS NOT NULL), 0)                AS nodes_on_new_sidecar,
       ROUND(100 * SUM(l.ipv4 IS NOT NULL) / NULLIF(COUNT(*), 0), 1) AS pct_new_sidecar,
       '目标 >= 80'                                        AS target,
       COALESCE(ROUND(100 * SUM(l.ipv4 IS NOT NULL) / NULLIF(COUNT(*), 0), 1) >= 80, 0) AS pass
FROM slave_nodes n
LEFT JOIN (
  SELECT DISTINCT ipv4
  FROM slave_node_loads
  WHERE created_at >= NOW() - INTERVAL 1 HOUR AND metrics_version >= 1
) l ON l.ipv4 = n.ipv4
WHERE n.deleted_at IS NULL;
