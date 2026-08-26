// Proves the desktop UI actually RENDERED, by reading the live DOM out of the
// running WebView2 over CDP.
//
// Why the DOM and not a log line: 0.4.8 shipped a blank window to every
// desktop user (4579cb8a). Every signal a cheaper gate would have watched
// stayed green through it — the Rust side logged a clean boot, the
// `ui_boot_ok` handshake fired (it only proves the bundle's JS ran, not that
// React rendered anything), web-OTA rollback therefore never engaged, and the
// bridge/stores/pollers kept logging healthy traffic. The only trace anywhere
// was one react-router line. So the gate has to assert the thing that was
// actually false: #root has children and real innerHTML.
//
// Usage: node assert-ui-rendered.mjs <cdp-port> <timeout-seconds>
// Exits 0 on a rendered UI, 1 otherwise, printing what it saw either way.

const port = Number(process.argv[2] || 9222);
const timeoutSec = Number(process.argv[3] || 90);
const deadline = Date.now() + timeoutSec * 1000;

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function listTargets() {
  const res = await fetch(`http://127.0.0.1:${port}/json/list`);
  if (!res.ok) throw new Error(`/json/list -> HTTP ${res.status}`);
  return res.json();
}

// The shell boots the webapp at the custom scheme's origin root. On Windows
// that is http://kaitu-ui.localhost/ (see ui_protocol.rs ui_boot_url).
function pickPage(targets) {
  return targets.find(
    (t) => t.type === 'page' && typeof t.url === 'string' && t.url.includes('kaitu-ui'),
  );
}

async function waitForPage() {
  let lastErr = 'no attempt made';
  let lastTargets = [];
  while (Date.now() < deadline) {
    try {
      lastTargets = await listTargets();
      const page = pickPage(lastTargets);
      if (page) return page;
      lastErr = `no kaitu-ui page among ${lastTargets.length} target(s)`;
    } catch (e) {
      lastErr = e.message;
    }
    await sleep(2000);
  }
  console.error(`FAIL: CDP page never appeared on port ${port}: ${lastErr}`);
  console.error('Last target list:', JSON.stringify(lastTargets, null, 2));
  process.exit(1);
}

// Minimal CDP client over the global WebSocket (Node >= 22).
function connect(wsUrl) {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(wsUrl);
    const pending = new Map();
    let id = 0;
    ws.addEventListener('open', () =>
      resolve({
        send(method, params) {
          const msgId = ++id;
          return new Promise((res, rej) => {
            pending.set(msgId, { res, rej });
            ws.send(JSON.stringify({ id: msgId, method, params }));
          });
        },
        close: () => ws.close(),
      }),
    );
    ws.addEventListener('error', (e) => reject(new Error(`websocket error: ${e.message ?? e}`)));
    ws.addEventListener('message', (ev) => {
      const msg = JSON.parse(ev.data);
      const p = pending.get(msg.id);
      if (!p) return;
      pending.delete(msg.id);
      msg.error ? p.rej(new Error(JSON.stringify(msg.error))) : p.res(msg.result);
    });
  });
}

const PROBE = `(() => {
  const root = document.getElementById('root');
  return JSON.stringify({
    href: location.href,
    title: document.title,
    rootPresent: !!root,
    children: root ? root.children.length : -1,
    innerHTMLLen: root ? root.innerHTML.length : -1,
    // Emotion injects these once MUI actually renders; both were length 0
    // during the 0.4.8 blank-window failure.
    styleLens: Array.from(document.querySelectorAll('style')).map((s) => s.innerHTML.length),
    bodyText: (document.body.innerText || '').slice(0, 200),
  });
})()`;

const page = await waitForPage();
console.log(`CDP page found: ${page.url}`);

const client = await connect(page.webSocketDebuggerUrl);

let last = null;
while (Date.now() < deadline) {
  const r = await client.send('Runtime.evaluate', {
    expression: PROBE,
    returnByValue: true,
    awaitPromise: false,
  });
  if (r.exceptionDetails) {
    console.error('probe threw:', JSON.stringify(r.exceptionDetails));
  } else {
    last = JSON.parse(r.result.value);
    if (last.children > 0 && last.innerHTMLLen > 0) {
      console.log('UI RENDERED:', JSON.stringify(last, null, 2));
      client.close();
      process.exit(0);
    }
  }
  await sleep(2000);
}

console.error('FAIL: the window is up but #root never rendered anything.');
console.error('This is the 0.4.8 blank-window shape — the app "started fine" and');
console.error('every log stayed healthy while the user saw nothing.');
console.error('Last probe:', JSON.stringify(last, null, 2));
client.close();
process.exit(1);
