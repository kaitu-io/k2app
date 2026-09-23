-- UAT 套餐：一个 app 套餐 + 路由器版两档（硬件 / 续费），并给路由器套餐挂 PrivateNodePlanSpec。
-- 线上 plans 表里 product=router 的行数为 0 —— 路由器版代码在没有这两行之前等于未执行代码，
-- 所以 UAT 必须自己把它们种出来，否则整条路由器链路根本进不去。
DELETE FROM private_node_plan_specs WHERE plan_id IN (SELECT id FROM plans WHERE pid IN ('uat-app-1m','router-std-1y','router-svc-1y'));
DELETE FROM plans WHERE pid IN ('uat-app-1m','router-std-1y','router-svc-1y');
INSERT INTO plans (created_at,updated_at,pid,label,price,origin_price,month,highlight,is_active,tier,product,hardware_sku,brand) VALUES
 (NOW(),NOW(),'uat-app-1m','开途会员月付',1999,2999,1,0,1,'pro','app','','kaitu'),
 (NOW(),NOW(),'router-std-1y','开途路由器版（含路由器）',39900,39900,12,1,1,'pro','router','redmi-ax6s','kaitu'),
 (NOW(),NOW(),'router-svc-1y','开途路由器版服务续费',29900,29900,12,0,1,'pro','router','','kaitu');
INSERT INTO private_node_plan_specs (plan_id, ip_type, allowed_regions, traffic_total_bytes)
 SELECT id,'non_residential','["ap-northeast-1","us-west-2"]',2199023255552 FROM plans WHERE product='router';
