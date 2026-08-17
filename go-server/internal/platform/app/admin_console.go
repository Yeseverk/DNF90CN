package app

import (
	"net/http"

	"longheng.io/server/internal/platform/httpx"
)

func adminConsole(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet)
		httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(adminConsoleHTML))
}

const adminConsoleHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>LongHeng 龙恒 Admin Console</title>
  <style>
    :root { color-scheme: light; --ink:#17202a; --muted:#5f6c7b; --line:#d8dee7; --bg:#f6f8fb; --panel:#ffffff; --accent:#0f766e; --danger:#b42318; }
    * { box-sizing: border-box; }
    body { margin: 0; font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: var(--bg); color: var(--ink); }
    header { min-height: 88px; display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 20px clamp(18px, 4vw, 48px); border-bottom: 1px solid var(--line); background: #ffffff; }
    h1 { margin: 0; font-size: 22px; line-height: 1.2; font-weight: 700; letter-spacing: 0; }
    main { display: grid; grid-template-columns: minmax(220px, 300px) minmax(0, 1fr); gap: 18px; padding: 18px clamp(18px, 4vw, 48px) 32px; }
    .token { display: flex; align-items: center; gap: 8px; min-width: min(100%, 520px); }
    .token input { width: 100%; min-height: 38px; border: 1px solid var(--line); border-radius: 6px; padding: 8px 10px; font: inherit; }
    nav { display: flex; flex-direction: column; gap: 8px; }
    button { min-height: 36px; border: 1px solid var(--line); border-radius: 6px; background: var(--panel); color: var(--ink); padding: 8px 10px; font: inherit; text-align: left; cursor: pointer; }
    button:hover, button.active { border-color: var(--accent); color: var(--accent); }
    section { min-width: 0; }
    .toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 38px; margin-bottom: 10px; }
    .request { display: grid; grid-template-columns: 92px minmax(0, 1fr) 110px; gap: 8px; margin-bottom: 10px; }
    .admin-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; margin-bottom: 10px; }
    .request input, .request select, .admin-fields input, textarea { width: 100%; border: 1px solid var(--line); border-radius: 6px; padding: 8px 10px; font: inherit; background: #fff; }
    textarea { min-height: 96px; resize: vertical; margin-bottom: 10px; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
    .endpoint { width: min(100%, 680px); border: 1px solid var(--line); border-radius: 6px; padding: 8px 10px; color: var(--muted); background: #fff; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 13px; }
    pre { min-height: 420px; margin: 0; padding: 16px; overflow: auto; border: 1px solid var(--line); border-radius: 8px; background: var(--panel); font: 13px/1.5 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
    .error { color: var(--danger); }
    @media (max-width: 760px) { header { align-items: flex-start; flex-direction: column; } main { grid-template-columns: 1fr; } nav, .admin-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); } }
  </style>
</head>
<body>
  <header>
    <h1>LongHeng 龙恒 Admin Console</h1>
    <label class="token"><span>Token</span><input id="token" autocomplete="off" placeholder="X-Admin-Token"></label>
  </header>
  <main>
    <nav id="nav">
      <button data-endpoint="/healthz">Health</button>
      <button data-endpoint="/debug/config">Config</button>
      <button data-endpoint="/debug/metrics">Metrics</button>
      <button data-endpoint="/debug/topology?refresh=true">Topology</button>
      <button data-endpoint="/debug/framework">Framework</button>
      <button data-endpoint="/debug/adapters">Adapters</button>
      <button data-endpoint="/debug/session-contract">Session Contract</button>
      <button data-endpoint="/debug/lifecycle">Lifecycle</button>
      <button data-endpoint="/debug/readiness">Readiness</button>
      <button data-endpoint="/debug/audit">Audit</button>
      <button data-endpoint="/debug/logiclog">Logic Log</button>
      <button data-endpoint="/debug/bilog">BI Log</button>
      <button data-endpoint="/debug/hotupdate">Hot Update</button>
      <button data-endpoint="/debug/statesync">State Sync</button>
      <button data-endpoint="/debug/rpc">RPC</button>
      <button data-endpoint="/debug/i18n">I18N</button>
      <button data-endpoint="/gateway/sessions">Gateway Sessions</button>
      <button data-endpoint="/gateway/presence">Presence</button>
      <button data-endpoint="/onlinepush">Online Push</button>
      <button data-endpoint="/onlinepush/send" data-method="POST">Send Push</button>
      <button data-endpoint="/onlinepush/offline?account_id=">Offline Push</button>
      <button data-endpoint="/gateway/backends">Gateway Backends</button>
      <button data-endpoint="/gateway/transports">Transports</button>
      <button data-endpoint="/gateway/drain">Gateway Drain</button>
      <button data-endpoint="/gateway/drain?enabled=true&close_existing=false" data-method="POST">Start Drain</button>
      <button data-endpoint="/gateway/drain?enabled=false" data-method="POST">Stop Drain</button>
      <button data-endpoint="/gateway/session/kick?session_id=" data-method="POST">Kick Session</button>
      <button data-endpoint="/logic/accounts">Accounts</button>
      <button data-endpoint="/accountcenter">Account Center</button>
      <button data-endpoint="/accountcenter/account?account_id=">Account Query</button>
      <button data-endpoint="/accountcenter/shards" data-method="POST">Account Shards</button>
      <button data-endpoint="/accountcenter/gates" data-method="POST">Account Gates</button>
      <button data-endpoint="/accountcenter/login" data-method="POST">GetGate Login</button>
      <button data-endpoint="/accountcenter/bind" data-method="POST">Account Bind</button>
      <button data-endpoint="/accountcenter/ban" data-method="POST">Account Ban</button>
      <button data-endpoint="/accountcenter/allow" data-method="POST">Account Allow</button>
      <button data-endpoint="/logic/ops/players?account_id=">Ops Player Query</button>
      <button data-endpoint="/logic/ops/profile?account_id=">Ops Profile</button>
      <button data-endpoint="/logic/ops/profile/repair" data-method="POST">Profile Repair</button>
      <button data-endpoint="/logic/ops/events/replay" data-method="POST">Event Replay</button>
      <button data-endpoint="/logic/ops/leaderboard">Leaderboard</button>
      <button data-endpoint="/logic/ops/leaderboard/repair" data-method="POST">Leaderboard Repair</button>
      <button data-endpoint="/logic/ops/bans">Bans</button>
      <button data-endpoint="/logic/notice">Notice</button>
      <button data-endpoint="/logic/notice/publish" data-method="POST">Notice Publish</button>
      <button data-endpoint="/logic/notice/account?account_id=">Notice Account</button>
      <button data-endpoint="/logic/notice/announcements">Announcements</button>
      <button data-endpoint="/logic/playerstore">Storage</button>
      <button data-endpoint="/logic/playerstore/deadletters">Storage Deadletters</button>
      <button data-endpoint="/logic/playerstore/deadletters/requeue?account_id=" data-method="POST">Storage Requeue</button>
      <button data-endpoint="/storage/objects?collection=&index.region=">Storage Object</button>
      <button data-endpoint="/storage/objects/read" data-method="POST">Storage Batch Read</button>
      <button data-endpoint="/storage/objects/write" data-method="POST">Storage Batch Write</button>
      <button data-endpoint="/storage/objects/delete" data-method="POST">Storage Batch Delete</button>
      <button data-endpoint="/storage/object?collection=&key=">Storage Object Read</button>
      <button data-endpoint="/storage/object" data-method="POST">Storage Object Write</button>
      <button data-endpoint="/storage/object?collection=&key=&user_id=&version=" data-method="DELETE">Storage Object Delete</button>
      <button data-endpoint="/logic/idempotency">Idempotency</button>
      <button data-endpoint="/logic/handlers">Handlers</button>
      <button data-endpoint="/eventlog">Event Log</button>
      <button data-endpoint="/eventlog/snapshot">Event Snapshot</button>
      <button data-endpoint="/eventlog/deadletters">Event Deadletters</button>
      <button data-endpoint="/eventlog/deadletters/requeue?id=" data-method="POST">Event Requeue</button>
      <button data-endpoint="/debug/moderation">Moderation</button>
    </nav>
    <section>
      <div class="toolbar"><strong id="title">Health</strong><input class="endpoint" id="endpoint" autocomplete="off" value="/healthz" aria-label="endpoint"></div>
      <div class="request">
        <select id="method"><option>GET</option><option>POST</option><option>DELETE</option></select>
        <input id="confirm" autocomplete="off" placeholder="X-Admin-Confirm">
        <button id="send">Send</button>
      </div>
      <div class="admin-fields">
        <input id="actor" autocomplete="off" placeholder="X-Admin-Actor">
        <input id="idem" autocomplete="off" placeholder="X-Admin-Idempotency-Key">
      </div>
      <textarea id="body" spellcheck="false" placeholder='{"account_id":"acc-1"}'></textarea>
      <pre id="output">{}</pre>
    </section>
  </main>
  <script>
    const token = document.getElementById('token');
    const output = document.getElementById('output');
    const title = document.getElementById('title');
    const endpoint = document.getElementById('endpoint');
    const method = document.getElementById('method');
    const confirm = document.getElementById('confirm');
    const actor = document.getElementById('actor');
    const idem = document.getElementById('idem');
    const body = document.getElementById('body');
    const send = document.getElementById('send');
    const buttons = Array.from(document.querySelectorAll('button[data-endpoint]'));
    const requestBodies = {
      '/gateway/push': { account_id: 'acc-100001', msg_id: 30142, body: '{"notice_id":"notice-1","title":"hello","content":"world"}' },
      '/gateway/drain?enabled=true&close_existing=false': { enabled: true, reason: 'rolling-upgrade', close_existing: false },
      '/gateway/drain?enabled=false': { enabled: false, reason: 'rollback' },
      '/gateway/session/kick?session_id=': { reason: 'admin-kick' },
      '/onlinepush/send': { idempotency_key: 'push-1', account_id: 'acc-100001', packet_id: 1, msg_id: 1, body: 'aGVsbG8=', note: 'admin-smoke' },
      '/accountcenter/shards': { shards: [{ id: '1001', name: '一区', state: 'open', weight: 1 }] },
      '/accountcenter/gates': { gates: [{ node_id: 'gateway-1', address: '127.0.0.1:3341', shard_ids: ['1001'], weight: 1 }] },
      '/accountcenter/login': { identity: { kind: 'device', device_id: 'device-001' }, preferred_shard_id: '1001' },
      '/accountcenter/bind': { account_id: 'acc-100001', identity: { kind: 'email', email: 'user@example.com' } },
      '/accountcenter/ban': { subject: 'acc-100001', scope: 'shard:1001', reason: 'manual-review' },
      '/accountcenter/allow': { subject: 'acc-100001', scope: 'global', reason: 'allowlist' },
      '/logic/ops/profile/repair': { account_id: 'acc-100001', reason: 'operator-repair' },
      '/logic/ops/events/replay': { id: 'event-id' },
      '/logic/ops/leaderboard/repair': { action: 'upsert_record', leaderboard_id: 'power', owner_id: 'acc-100001', score: 100, operator: 'admin' },
      '/logic/notice/publish': { notice: { id: 'notice-1', title: '维护通知', content: '今晚例行维护。', category: 'system' }, recipients: ['acc-100001'], idempotency_key: 'notice-1' },
      '/logic/playerstore/deadletters/requeue?account_id=': { reason: 'operator-requeue' },
      '/storage/objects/read': { keys: [{ collection: 'global', key: 'motd' }] },
      '/storage/objects/write': { writes: [{ object: { key: { collection: 'global', key: 'motd' }, value: { text: 'hello' }, index: { region: 'cn', kind: 'notice' }, permission_read: 2, permission_write: 1 } }] },
      '/storage/objects/delete': { deletes: [{ key: { collection: 'global', key: 'motd' } }] },
      '/storage/object': { object: { key: { collection: 'global', key: 'motd' }, value: { text: 'hello' }, index: { region: 'cn', kind: 'notice' }, permission_read: 2, permission_write: 1 } },
      '/eventlog/deadletters/requeue?id=': { reason: 'operator-requeue' }
    };
    token.value = localStorage.getItem('longheng_admin_token') || '';
    actor.value = localStorage.getItem('longheng_admin_actor') || '';
    token.addEventListener('input', () => localStorage.setItem('longheng_admin_token', token.value));
    actor.addEventListener('input', () => localStorage.setItem('longheng_admin_actor', actor.value));
    function operationID(methodValue, rawEndpoint) {
      return methodValue + ' ' + rawEndpoint.split('?')[0];
    }
    function newIdempotencyKey(op) {
      const safe = op.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
      return safe + '-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 8);
    }
    async function requestCurrent() {
      const selected = buttons.find(item => item.classList.contains('active')) || buttons[0];
      title.textContent = selected.textContent;
      const targetEndpoint = endpoint.value.trim() || selected.dataset.endpoint;
      endpoint.value = targetEndpoint;
      output.className = '';
      output.textContent = 'loading';
      try {
        const headers = {};
        if (token.value.trim()) headers['X-Admin-Token'] = token.value.trim();
        if (actor.value.trim()) headers['X-Admin-Actor'] = actor.value.trim();
        if (confirm.value.trim()) headers['X-Admin-Confirm'] = confirm.value.trim();
        if (idem.value.trim()) headers['X-Admin-Idempotency-Key'] = idem.value.trim();
        const init = { method: method.value, headers };
        if (method.value !== 'GET' && method.value !== 'HEAD' && body.value.trim()) {
          headers['Content-Type'] = 'application/json';
          init.body = body.value.trim();
        }
        const res = await fetch(targetEndpoint, init);
        const text = await res.text();
        let body = text;
        try { body = JSON.stringify(JSON.parse(text), null, 2); } catch (_) {}
        output.className = res.ok ? '' : 'error';
        output.textContent = 'status=' + res.status + '\n' + body;
      } catch (err) {
        output.className = 'error';
        output.textContent = String(err);
      }
    }
    function load(button) {
      buttons.forEach(item => item.classList.toggle('active', item === button));
      method.value = button.dataset.method || 'GET';
      title.textContent = button.textContent;
      endpoint.value = button.dataset.endpoint;
      const op = operationID(method.value, endpoint.value);
      confirm.value = method.value === 'GET' ? '' : op;
      idem.value = method.value === 'GET' ? '' : newIdempotencyKey(op);
      const preset = requestBodies[button.dataset.endpoint];
      body.value = method.value === 'GET' || !preset ? '' : JSON.stringify(preset, null, 2);
      requestCurrent();
    }
    send.addEventListener('click', requestCurrent);
    buttons.forEach(button => button.addEventListener('click', () => load(button)));
    load(buttons[0]);
  </script>
</body>
</html>`
