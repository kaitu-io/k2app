App 伪装与隐蔽保护系统

背景：在某些国家（如中国）使用 VPN 应用开始违法，需要帮助用户通过各种方式伪装 app，保护用户安全。

威胁模型：
- L1 肉眼检查：街上抽查、边检看桌面图标
- L2 手动翻查：打开 app 看内容、翻 VPN 设置
- L3 工具扫描：检测工具扫包名、进程名
- L4 专业取证：手机接电脑提取数据库

组合拳方案（四层防护）：

层级 1: 视觉伪装（对抗 L1）
- Build-time 多马甲分发：构建时产出不同 app variant
- Android 不同 applicationId/图标/名字，iOS 不同 Bundle ID + alternate icons
- Desktop 不同 productName/exe名/安装路径/Dock图标
- 每个 variant 都是真的能用的工具 app（计算器/备忘录/电池管理）

层级 2: 交互伪装（对抗 L2）
- Fake-first UI：打开就是正常工具界面
- 隐蔽入口进入 VPN（计算器输特定序列、备忘录长按标题栏等）
- VPN 在后台运行，按 Home 自动切回假界面
- 多任务切换器显示假 app 截图

层级 3: 紧急响应（对抗被迫打开手机）
- Duress 双密码：正常 PIN 进 VPN，胁迫 PIN 擦除数据+显示干净假 app
- 快速紧急操作：连按电源键、摇晃模式、远程推送指令、Widget 假按钮
- 擦除后 app 功能正常（plausible deniability）

层级 4: 数据隐蔽（对抗 L3~L4）
- AES-256 加密存储，密钥 = PIN + 设备指纹（PBKDF2）
- 擦除时只删 key，密文无法解密
- 无明文 VPN 相关字符串，配置文件名用 hash
- VPN profile 名改成无害名字（System Update Service）
- Desktop 进程名伪装

用户原始想法：
1. 自毁能力：Android 被强迫打开屏幕时触发自毁
2. 伪装能力：伪装成极其冷门的现有应用
3. Desktop 也需要这个能力
