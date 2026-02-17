Tauri WebView 无法 fetch 外部 HTTPS URL

在 dev 模式下 (http://localhost:1420)，WebKit WebView 对 CloudFront 和 52j.me 的 fetch 请求都返回 "Load failed (TypeError)"。

现象：
- fetch('https://d1l0lk9fcyd6r8.cloudfront.net/api/tunnels') → Load failed
- fetch('https://w.app.52j.me/api/tunnels') → Load failed
- 终端 curl 同样的 URL 正常（返回 404，说明网络通）

影响：
- Cloud API 完全不可用
- 服务器列表加载失败
- 用户信息获取失败
- 所有依赖 cloudApi.request() 的功能都无法工作

可能原因：
- Tauri v2 WebView 安全限制（需要配置 HTTP 权限或 allowlist）
- WebKit 对 HTTP origin (localhost:1420) 发起 HTTPS 请求的限制
- 系统 VPN 服务 (kaitu-service) 可能影响网络路由
- 需要 tauri-plugin-http 或在 Tauri capabilities 中配置外部 URL 访问权限
