Tauri desktop 没有注入 _k2/_platform globals

main.rs 完全没有注入 JavaScript globals 的代码。webapp 启动时检测不到 window._k2，回退到 standalone-k2.ts。

standalone 实现用相对路径 fetch('/core') 调用 daemon，但 Vite dev server 没有配置 proxy 到 k2 daemon (:11777)，导致 VPN 控制全部 404。

CLAUDE.md 描述的设计是 "Tauri: Rust inject -> HTTP 127.0.0.1:1777"，但实际 main.rs 没有实现这个注入。

需要解决：
- 方案A: Tauri Rust 侧通过 initialization script 注入 _k2/_platform，内部用 fetch 到 127.0.0.1:1777
- 方案B: Vite dev 配置 proxy（/core, /ping, /api/device/* → :11777），让 standalone 模式在 dev 下也能工作
- 方案C: 两者都做——生产用 Rust 注入，dev 用 Vite proxy
