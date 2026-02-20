Node 统一模型：重命名 SlaveNode→Node, CloudInstance→NodeCloudMeta, API 聚合视图, 前端合并 /manager/nodes 入口

Scrum 结论：不做物理合并表，保持两表独立生命周期和写入路径。通过命名重构 + API 聚合视图解决 admin 体验割裂。

Action:
1. 重命名 SlaveNode → Node
2. 重命名 CloudInstance → NodeCloudMeta（明确从属关系）
3. API 层 /app/nodes 返回聚合视图（LEFT JOIN cloud meta）
4. 前端合并 /manager/cloud 和 /manager/nodes 为一个页面
5. 不合并表、不改写入路径
