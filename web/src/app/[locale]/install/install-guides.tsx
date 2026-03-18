"use client";

import { AlertCircle, CheckCircle, Copy } from 'lucide-react';

// ---------------------------------------------------------------------------
// Reusable wrapper for yellow warning tip cards
// ---------------------------------------------------------------------------

export function DownloadTipCard({ icon, title, children }: { icon?: React.ReactNode; title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-yellow-500/30 bg-yellow-500/5 p-4">
      <p className="text-base font-bold text-yellow-500 mb-3 flex items-center gap-2">
        {icon || <AlertCircle className="w-5 h-5 shrink-0" />}
        {title}
      </p>
      {children}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Inline guide illustrations (dynamic, themed, no static images needed)
// ---------------------------------------------------------------------------

export function GuideIframe({ srcdoc, height }: { srcdoc: string; height: number }) {
  return (
    <iframe
      srcDoc={srcdoc}
      className="w-full rounded-lg border-0 overflow-hidden"
      style={{ height, display: 'block' }}
      scrolling="no"
    />
  );
}

function EdgeBlockedGuide({ filename }: { filename: string }) {
  const html = `<!DOCTYPE html><html><head><meta charset="utf-8"><style>
*{margin:0;padding:0;box-sizing:border-box}
body{background:#f0f0f0;padding:8px;font-family:'Segoe UI',sans-serif;position:relative;min-height:310px}
/* Download panel — left side */
.panel{background:#fff;border-radius:10px;box-shadow:0 8px 32px rgba(0,0,0,0.12);width:55%;position:absolute;left:8px;top:8px;overflow:hidden}
.header{padding:12px 16px;display:flex;align-items:center;justify-content:space-between}
.htitle{font-size:14px;font-weight:600;color:#1a1a1a}
.hicons{display:flex;gap:12px;color:#555;font-size:13px}
/* Warning file row */
.warn-row{padding:12px 14px;display:flex;align-items:flex-start;gap:10px;background:#fef3e2;border-top:1px solid #eee;border-bottom:1px solid #eee}
.warn-ico{width:30px;height:30px;border-radius:50%;background:#ea580c;display:flex;align-items:center;justify-content:center;flex-shrink:0}
.warn-ico svg{width:18px;height:18px;fill:#fff}
.warn-text{flex:1;font-size:11.5px;color:#1a1a1a;line-height:1.45}
.warn-actions{display:flex;gap:4px;flex-shrink:0;align-items:flex-start;margin-top:2px}
.act-btn{width:26px;height:26px;display:flex;align-items:center;justify-content:center;border-radius:4px;color:#555;font-size:13px;cursor:pointer}
.act-btn:hover{background:#eee}
.dots-hl{border:2px solid #e94560;color:#e94560;position:relative}
.dots-hl::after{content:'← 点这里';position:absolute;left:calc(100% + 4px);top:50%;transform:translateY(-50%);font-size:9px;color:#e94560;white-space:nowrap;font-weight:700}
/* View more */
.view-more{padding:10px 14px;font-size:13px;color:#1a1a1a;font-weight:500;cursor:pointer}
/* Floating context menu — overlapping right side */
.ctx-menu{position:absolute;right:8px;top:80px;width:48%;background:#fff;border-radius:10px;box-shadow:0 8px 32px rgba(0,0,0,0.18);overflow:hidden;z-index:10}
.mi{padding:10px 16px;font-size:13px;color:#1a1a1a;display:flex;align-items:center;gap:10px;cursor:pointer}
.mi:hover{background:#f5f5f5}
.mi .ico{width:20px;text-align:center;font-size:14px;color:#555}
.mi.keep{background:#f0f0f0;font-weight:600;border:2px solid #e94560;border-radius:8px;margin:3px 6px}
.keep-arrow{color:#e94560;font-size:11px;margin-left:auto;font-weight:700}
.msep{height:1px;background:#e5e5e5;margin:3px 0}
.mi-bottom{border-top:1px solid #e5e5e5;padding:10px 16px;font-size:13px;color:#1a1a1a;display:flex;align-items:center;gap:10px}
.mi-bottom .ico{width:20px;text-align:center;font-size:14px;color:#555}
</style></head><body>
<div class="panel">
  <div class="header">
    <span class="htitle">下载</span>
    <span class="hicons">📂&nbsp;&nbsp;🔍&nbsp;&nbsp;···</span>
  </div>
  <div class="warn-row">
    <div class="warn-ico"><svg viewBox="0 0 24 24"><path d="M1 21h22L12 2 1 21zm12-3h-2v-2h2v2zm0-4h-2v-4h2v4z"/></svg></div>
    <div class="warn-text">通常不会下载 ${filename}。请在打开前信任 ${filename}。</div>
    <div class="warn-actions">
      <div class="act-btn">🗑</div>
      <div class="act-btn dots-hl">···</div>
    </div>
  </div>
  <div class="view-more">查看更多</div>
</div>
<div class="ctx-menu">
  <div class="mi"><span class="ico">🗑</span>删除</div>
  <div class="mi keep"><span class="ico">📂</span>保留<span class="keep-arrow">👈</span></div>
  <div class="msep"></div>
  <div class="mi"><span class="ico">🛡</span>将此文件报告为安全</div>
  <div class="mi"><span class="ico">ℹ️</span>了解更多信息</div>
  <div class="msep"></div>
  <div class="mi-bottom"><span class="ico">🔗</span>复制下载链接</div>
</div>
</body></html>`;
  return <GuideIframe srcdoc={html} height={320} />;
}

function ChromeBlockedGuide({ filename }: { filename: string }) {
  const html = `<!DOCTYPE html><html><head><meta charset="utf-8"><style>*{margin:0;padding:0;box-sizing:border-box}body{background:#2b2b2b;padding:12px 12px 8px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif}.bar{background:#3a3a3a;border-radius:8px;padding:10px 14px;display:flex;align-items:center;gap:10px}.icon{color:#fbbf24;font-size:16px;flex-shrink:0}.text{color:#ccc;font-size:11px;flex:1}strong{color:#fff;font-weight:600}.keep{background:#4285f4;color:#fff;padding:5px 14px;border-radius:4px;font-size:11px;font-weight:500;border:2px solid #ff4444;flex-shrink:0;white-space:nowrap}.keep::after{content:' 👈';font-size:12px}.discard{background:transparent;color:#888;padding:5px 14px;border-radius:4px;font-size:11px;border:1px solid #555;flex-shrink:0}</style></head><body><div class="bar"><span class="icon">⚠️</span><span class="text"><strong>${filename}</strong> 不是常见的下载文件，可能存在危险。</span><span class="keep">保留</span><span class="discard">丢弃</span></div></body></html>`;
  return <GuideIframe srcdoc={html} height={70} />;
}

export function BrowserBlockedGuide({ filename, browser }: { filename: string; browser?: string | null }) {
  if (browser === 'edge') {
    return <EdgeBlockedGuide filename={filename} />;
  }
  if (browser === 'chrome') {
    return <ChromeBlockedGuide filename={filename} />;
  }
  // Fallback: show both
  return (
    <div className="space-y-3">
      <EdgeBlockedGuide filename={filename} />
      <ChromeBlockedGuide filename={filename} />
    </div>
  );
}

export function SmartScreenGuide({ filename, publisher }: { filename: string; publisher: string }) {
  const step1 = `<!DOCTYPE html><html><head><meta charset="utf-8"><style>*{margin:0;padding:0;box-sizing:border-box}body{background:#0078d4;padding:24px 28px;font-family:'Segoe UI',sans-serif;color:#fff}h1{font-size:20px;font-weight:300;margin-bottom:14px}p{font-size:12px;color:rgba(255,255,255,0.8);line-height:1.6;margin-bottom:2px}a{color:rgba(255,255,255,0.9);font-size:12px;text-decoration:underline;display:inline-block;margin-top:8px;border:2px solid #ff4444;border-radius:3px;padding:1px 6px}a::after{content:' 👈 点击这里';color:#ffcc00;font-size:11px;font-weight:600;text-decoration:none}.spacer{height:60px}.row{text-align:right}.btn{background:rgba(255,255,255,0.1);color:#fff;border:1px solid rgba(255,255,255,0.3);padding:5px 24px;font-size:12px}</style></head><body><h1>Windows 已保护你的电脑</h1><p>Microsoft Defender SmartScreen 已阻止一个未识别的应用启动。</p><p>运行此应用可能会使你的电脑面临风险。</p><a>更多信息</a><div class="spacer"></div><div class="row"><button class="btn">我知道了</button></div></body></html>`;
  const step2 = `<!DOCTYPE html><html><head><meta charset="utf-8"><style>*{margin:0;padding:0;box-sizing:border-box}body{background:#0078d4;padding:24px 28px;font-family:'Segoe UI',sans-serif;color:#fff}h1{font-size:20px;font-weight:300;margin-bottom:14px}p{font-size:12px;color:rgba(255,255,255,0.8);line-height:1.6;margin-bottom:2px}.info{font-size:11px;color:rgba(255,255,255,0.5);margin-top:10px}.spacer{height:30px}.row{display:flex;justify-content:flex-end;gap:8px}.btn{background:rgba(255,255,255,0.1);color:#fff;border:1px solid rgba(255,255,255,0.3);padding:5px 24px;font-size:12px}.hl{border:2px solid #ff4444}.hl::after{content:' 👈';color:#ffcc00;font-size:11px;font-weight:600}</style></head><body><h1>Windows 已保护你的电脑</h1><p>Microsoft Defender SmartScreen 已阻止一个未识别的应用启动。</p><p>运行此应用可能会使你的电脑面临风险。</p><p class="info">应用: ${filename}</p><p class="info">发布者: ${publisher}</p><div class="spacer"></div><div class="row"><button class="btn">不运行</button><button class="btn hl">仍要运行</button></div></body></html>`;

  return (
    <div className="space-y-3">
      <div>
        <p className="text-sm text-muted-foreground mb-1.5">第 1 步：点击「更多信息」</p>
        <GuideIframe srcdoc={step1} height={280} />
      </div>
      <div>
        <p className="text-sm text-muted-foreground mb-1.5">第 2 步：点击「仍要运行」</p>
        <GuideIframe srcdoc={step2} height={260} />
      </div>
    </div>
  );
}

export function MacOSAllowGuide({ publisher }: { publisher: string }) {
  const html = `<!DOCTYPE html><html><head><meta charset="utf-8"><style>*{margin:0;padding:0;box-sizing:border-box}body{background:#1c1c1e;font-family:-apple-system,BlinkMacSystemFont,sans-serif;font-size:11px}.tb{background:#2c2c2e;padding:8px 12px;display:flex;align-items:center;gap:6px;border-bottom:1px solid #3a3a3c}.d{width:10px;height:10px;border-radius:50%}.r{background:#ff5f57}.y{background:#febc2e}.g{background:#28c840}.main{display:flex}.sb{width:150px;background:#262628;border-right:1px solid #3a3a3c;padding:10px}.search{background:#3a3a3c;border-radius:12px;padding:5px 8px;display:flex;align-items:center;gap:4px;margin-bottom:8px;border:2px solid #ff4444;box-shadow:0 0 6px rgba(255,68,68,0.4)}.search span{color:#98989d;font-size:9px}.search .t{color:#e5e5e7;flex:1}.si{background:#3478f6;border-radius:5px;padding:5px 8px;display:flex;align-items:center;gap:5px}.si span{font-size:9px;color:#fff}.ct{flex:1;padding:14px}.ch{padding:8px 0 10px;display:flex;align-items:center;gap:6px;border-bottom:1px solid #3a3a3c;margin-bottom:12px}.ch span{color:#3478f6;font-size:14px}.ch .t{flex:1;font-size:11px;font-weight:600;color:#e5e5e7}.st{font-size:10px;font-weight:600;color:#e5e5e7;margin-bottom:8px}.bx{background:#2c2c2e;border-radius:8px;padding:10px;margin-bottom:8px}.rl{color:#e5e5e7;font-size:10px;margin-bottom:6px}.rd{display:flex;align-items:center;gap:5px;margin-bottom:3px}.roff{width:12px;height:12px;border-radius:50%;border:1.5px solid #58585a}.ron{width:12px;height:12px;border-radius:50%;background:#3478f6;position:relative}.ron::after{content:'';position:absolute;top:3px;left:3px;width:6px;height:6px;border-radius:50%;background:#fff}.rl2{font-size:9px;color:#98989d}.rl2.sel{color:#e5e5e7}.at{color:#98989d;font-size:10px;line-height:1.5}strong{color:#e5e5e7}.ba{float:right;background:#48484a;color:#e5e5e7;border:none;padding:3px 12px;border-radius:5px;font-size:9px;margin-top:6px;border:2px solid #ff4444}.ba::after{content:' 👈';color:#ffcc00;font-size:9px;font-weight:600}.cf::after{content:'';display:table;clear:both}</style></head><body><div class="tb"><div class="d r"></div><div class="d y"></div><div class="d g"></div></div><div class="main"><div class="sb"><div class="search"><span>🔍</span><span class="t">隐私与安全</span></div><div class="si"><span>🤚</span><span>隐私与安全性</span></div></div><div class="ct"><div class="ch"><span>‹</span><span>›</span><span class="t">隐私与安全性</span></div><div class="st">安全性</div><div class="bx"><div class="rl">允许从以下位置下载的应用程序</div><div class="rd"><div class="roff"></div><span class="rl2">App Store</span></div><div class="rd"><div class="ron"></div><span class="rl2 sel">App Store 和被认可的开发者</span></div></div><div class="bx cf"><p class="at">来自开发者 <strong>"${publisher}"</strong> 的系统软件已被阻止载入。</p><button class="ba">允许</button></div></div></div></body></html>`;

  return (
    <div>
      <p className="text-sm text-muted-foreground mb-1.5">在系统设置中搜索「隐私与安全」，找到底部的安全性提示：</p>
      <GuideIframe srcdoc={html} height={340} />
    </div>
  );
}

export function AndroidInstallGuide() {
  const html = `<!DOCTYPE html><html><head><meta charset="utf-8"><style>*{margin:0;padding:0;box-sizing:border-box}body{background:#1a1a2e;padding:14px;font-family:-apple-system,BlinkMacSystemFont,sans-serif;color:#e0e0e0}.steps{display:flex;flex-direction:column;gap:10px}.step{display:flex;align-items:flex-start;gap:10px;background:#16213e;border-radius:8px;padding:10px 12px}.num{background:#0f3460;color:#e94560;font-weight:700;font-size:13px;width:26px;height:26px;border-radius:50%;display:flex;align-items:center;justify-content:center;flex-shrink:0}.content{flex:1}.title{font-size:12px;font-weight:600;color:#fff;margin-bottom:2px}.desc{font-size:10px;color:#999;line-height:1.4}.hl{background:#e94560;color:#fff;padding:1px 5px;border-radius:3px;font-size:10px;font-weight:600}</style></head><body><div class="steps"><div class="step"><div class="num">1</div><div class="content"><div class="title">📥 下载 APK 安装包</div><div class="desc">浏览器可能提示「有害文件」，选择 <span class="hl">保留</span></div></div></div><div class="step"><div class="num">2</div><div class="content"><div class="title">✈️ 开启飞行模式</div><div class="desc">安装前开启飞行模式，避免手机安全中心联网云检测拦截安装</div></div></div><div class="step"><div class="num">3</div><div class="content"><div class="title">📲 安装 APK</div><div class="desc">打开 APK → 允许「安装未知应用」→ 点击 <span class="hl">安装</span></div></div></div><div class="step"><div class="num">4</div><div class="content"><div class="title">📶 关闭飞行模式</div><div class="desc">安装完成后关闭飞行模式，恢复网络</div></div></div><div class="step"><div class="num">5</div><div class="content"><div class="title">🚀 打开开途</div><div class="desc">首次启动会请求 VPN 权限，点击 <span class="hl">允许</span> 即可</div></div></div></div></body></html>`;
  return <GuideIframe srcdoc={html} height={310} />;
}

export function DesktopUsbInstallGuide() {
  // Step 1: Desktop app Account page → "其他设备安装"
  const step1 = `<!DOCTYPE html><html><head><meta charset="utf-8"><style>
*{margin:0;padding:0;box-sizing:border-box}
body{background:#e8e8ec;padding:12px;font-family:-apple-system,BlinkMacSystemFont,sans-serif}
.app{background:#2a2a3a;border-radius:10px;overflow:hidden;max-width:320px;margin:0 auto;display:flex;flex-direction:column}
.header{background:#3b3bbd;padding:14px 16px;text-align:center}
.header h2{color:#fff;font-size:15px;font-weight:700}
.header p{color:rgba(255,255,255,0.6);font-size:10px;margin-top:2px}
.menu{padding:4px 0;flex:1}
.item{padding:11px 16px;display:flex;align-items:center;gap:10px;border-bottom:1px solid #333}
.item .ico{width:22px;height:22px;display:flex;align-items:center;justify-content:center;font-size:14px;color:#888}
.item .label{flex:1;font-size:12px;color:#ccc}
.item .arrow{color:#555;font-size:12px}
.item.hl{background:rgba(229,57,53,0.08);border:3px solid #e53935;border-radius:8px;margin:4px 8px}
.item.hl .label{color:#fff;font-weight:600}
.item.hl .arrow{color:#e53935}
.hl-hint{color:#e53935;font-size:9px;font-weight:700;margin-left:auto;margin-right:4px}
.dots{text-align:center;padding:6px 0;color:#555;font-size:14px;letter-spacing:4px}
.tabbar{display:flex;border-top:1px solid #444;background:#222230}
.tab{flex:1;padding:8px 0 6px;text-align:center;position:relative}
.tab .tab-ico{font-size:16px;display:block;margin-bottom:2px}
.tab .tab-label{font-size:9px;color:#666}
.tab.active{border:3px solid #e53935;border-radius:8px;margin:2px;background:rgba(229,57,53,0.08)}
.tab.active .tab-label{color:#e53935;font-weight:700}
.tab.active .tab-ico{filter:none}
</style></head><body>
<div class="app">
<div class="header"><h2>Kaitu.io 开途</h2><p>越拥堵，越从容</p></div>
<div class="menu">
<div class="item"><span class="ico">✉️</span><span class="label">x***y@qq.com</span><span class="arrow" style="color:#4a7cba;font-size:11px;border:1px solid #4a7cba;padding:2px 8px;border-radius:4px">修改</span></div>
<div class="item"><span class="ico">🖥</span><span class="label">我的设备</span><span class="arrow">›</span></div>
<div class="dots">···</div>
<div class="item hl"><span class="ico">📱</span><span class="label">其他设备安装</span><span class="hl-hint">👈 点这里</span><span class="arrow">›</span></div>
</div>
<div class="tabbar">
<div class="tab"><span class="tab-ico">📊</span><span class="tab-label">仪表板</span></div>
<div class="tab"><span class="tab-ico">🛒</span><span class="tab-label">购买</span></div>
<div class="tab"><span class="tab-ico">🎁</span><span class="tab-label">邀请</span></div>
<div class="tab"><span class="tab-ico">🧭</span><span class="tab-label">发现</span></div>
<div class="tab active"><span class="tab-ico">👤</span><span class="tab-label">账户</span></div>
</div>
</div>
</body></html>`;

  // Step 2: "其他设备安装" page → "安卓手机安装" USB card
  const step2 = `<!DOCTYPE html><html><head><meta charset="utf-8"><style>
*{margin:0;padding:0;box-sizing:border-box}
body{background:#e8e8ec;padding:12px;font-family:-apple-system,BlinkMacSystemFont,sans-serif}
.page{max-width:320px;margin:0 auto}
.usb-card{background:#2a2a3a;border:3px solid #e53935;border-radius:10px;padding:14px;display:flex;align-items:center;gap:12px;margin-bottom:12px;cursor:pointer}
.usb-ico{width:40px;height:40px;background:rgba(76,175,80,0.15);border-radius:50%;display:flex;align-items:center;justify-content:center;flex-shrink:0}
.usb-ico svg{width:22px;height:22px;fill:#4caf50}
.usb-text{flex:1}
.usb-title{font-size:13px;font-weight:700;color:#fff;display:flex;align-items:center;gap:6px}
.usb-badge{background:#e65100;color:#fff;font-size:9px;padding:1px 6px;border-radius:4px;font-weight:600}
.usb-desc{font-size:10px;color:#999;margin-top:3px;line-height:1.4}
.usb-arrow{color:#e53935;font-size:18px;font-weight:700}
.hl-hint{color:#e94560;font-size:10px;font-weight:700;text-align:center;margin-bottom:8px}
.wizard{background:#f5f5dc;border-radius:10px;padding:16px;margin-top:4px}
.wizard h3{font-size:14px;font-weight:800;color:#1a1a1a;margin-bottom:4px}
.wizard p{font-size:10px;color:#666;margin-bottom:12px}
.step{display:flex;align-items:flex-start;gap:10px;margin-bottom:10px}
.step:last-child{margin-bottom:0}
.snum{width:22px;height:22px;background:#42a5f5;color:#fff;font-size:11px;font-weight:700;border-radius:50%;display:flex;align-items:center;justify-content:center;flex-shrink:0}
.snum.dim{background:#999}
.sinfo{flex:1}
.sinfo strong{font-size:11px;color:#1a1a1a;display:block}
.sinfo span{font-size:10px;color:#888;display:block;margin-top:2px}
.line{width:2px;height:12px;background:#ddd;margin:2px 0 2px 10px}
</style></head><body>
<div class="page">
<div class="hl-hint">👇 点击进入安卓 USB 安装向导</div>
<div class="usb-card">
  <div class="usb-ico"><svg viewBox="0 0 24 24"><path d="M15 7v4h1v2h-3V5h2l-3-4-3 4h2v8H8v-2.07A1.993 1.993 0 007 5a2 2 0 00-1 3.75V11c0 1.1.9 2 2 2h3v2.05A1.993 1.993 0 0012 21a2 2 0 001-3.75V13h3c1.1 0 2-.9 2-2V9h1V7h-4z"/></svg></div>
  <div class="usb-text">
    <div class="usb-title">安卓手机安装 <span class="usb-badge">实验性</span></div>
    <div class="usb-desc">USB 连接电脑，保姆级引导，一键自动安装到手机</div>
  </div>
  <div class="usb-arrow">›</div>
</div>
<div class="wizard">
  <h3>安卓 USB 安装</h3>
  <p>通过 USB 数据线将应用安装到安卓手机</p>
  <div class="step"><div class="snum">1</div><div class="sinfo"><strong>开启开发者选项和 USB 调试</strong><span>选择手机品牌，按图片指引操作</span></div></div>
  <div class="line"></div>
  <div class="step"><div class="snum dim">2</div><div class="sinfo"><strong>USB 连接并授权</strong><span>用数据线连接手机和电脑</span></div></div>
  <div class="line"></div>
  <div class="step"><div class="snum dim">3</div><div class="sinfo"><strong>自动安装</strong><span>开途自动推送 APK 到手机</span></div></div>
</div>
</div>
</body></html>`;

  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground mb-1">
        在电脑上打开开途客户端，进入「账户」→「其他设备安装」：
      </p>
      <GuideIframe srcdoc={step1} height={380} />
      <p className="text-sm text-muted-foreground mb-1">
        点击「安卓手机安装」，按照向导用 USB 数据线一键安装：
      </p>
      <GuideIframe srcdoc={step2} height={420} />
    </div>
  );
}

// ---------------------------------------------------------------------------
// CLI install block
// ---------------------------------------------------------------------------

export function CliBlock({ onCopy, copied }: { onCopy: () => void; copied: boolean }) {
  return (
    <div className="bg-card rounded-lg border font-mono text-sm p-4">
      <div className="flex items-center gap-2">
        <span className="text-muted-foreground">$</span>
        <code className="flex-1 text-foreground break-all">curl -fsSL https://kaitu.io/i/k2 | sudo bash</code>
        <button
          onClick={onCopy}
          className="shrink-0 p-1 hover:text-foreground transition-colors text-muted-foreground"
        >
          {copied ? <CheckCircle className="w-5 h-5 text-green-500" /> : <Copy className="w-5 h-5" />}
        </button>
      </div>
    </div>
  );
}
