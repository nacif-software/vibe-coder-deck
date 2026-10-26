(() => {
  const $ = (id) => document.getElementById(id);

  /* ---- light / dark ------------------------------------------------------- */
  // Follows the system until the header button picks one; the pick is kept
  // in localStorage (a per-viewer convenience, so failures are ignored).
  const systemDark = matchMedia('(prefers-color-scheme: dark)');
  let themePick = null;
  try { themePick = localStorage.getItem('theme'); } catch { /* storage blocked */ }
  function applyTheme() {
    const root = document.documentElement;
    if (themePick) root.dataset.theme = themePick;
    else delete root.dataset.theme;
    const dark = themePick ? themePick === 'dark' : systemDark.matches;
    root.dataset.dark = dark ? '1' : '0';
    $('theme').setAttribute('aria-label', dark ? 'Switch to light mode' : 'Switch to dark mode');
  }
  $('theme').addEventListener('click', () => {
    themePick = document.documentElement.dataset.dark === '1' ? 'light' : 'dark';
    try { localStorage.setItem('theme', themePick); } catch { /* storage blocked */ }
    applyTheme();
  });
  systemDark.addEventListener('change', applyTheme);
  applyTheme();

  const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const fmt = (iso) => iso ? new Date(iso).toLocaleString() : 'never';
  const svg = (d, w = 18) => `<svg aria-hidden="true" viewBox="0 0 24 24" width="${w}" height="${w}" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${d}</svg>`;

  async function api(method, path, body) {
    const res = await fetch(path, { method, headers: { 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || `${res.status} ${res.statusText}`);
    return data;
  }

  /* ---- dialogs ------------------------------------------------------------------ */
  // The app's webview (Wails) shows no confirm(), prompt() or alert(): they
  // return at once. These are the page's own, and wait for the answer.
  // dialog resolves with the typed text (input), true (OK) or null (Cancel / Esc).
  function dialog({ title = '', message = '', input = null, ok = 'OK', cancel = 'Cancel', danger = false }) {
    const d = $('ui-dialog');
    $('ui-dialog-title').textContent = title;
    $('ui-dialog-message').textContent = message;
    $('ui-dialog-input').classList.toggle('hidden', input === null);
    $('ui-dialog-input').value = input ?? '';
    $('ui-dialog-ok').textContent = ok;
    $('ui-dialog-ok').classList.toggle('danger', danger);
    $('ui-dialog-cancel').classList.toggle('hidden', cancel === null);
    $('ui-dialog-cancel').textContent = cancel || '';
    return new Promise((resolve) => {
      const finish = (value) => {
        $('ui-dialog-form').onsubmit = null;
        $('ui-dialog-cancel').onclick = null;
        d.onclose = null;
        if (d.open) d.close();
        resolve(value);
      };
      $('ui-dialog-form').onsubmit = (e) => {
        e.preventDefault();
        if (input !== null && !$('ui-dialog-input').value.trim()) return;
        finish(input !== null ? $('ui-dialog-input').value.trim() : true);
      };
      $('ui-dialog-cancel').onclick = () => finish(null);
      d.onclose = () => finish(null);
      d.showModal();
      (input !== null ? $('ui-dialog-input') : $('ui-dialog-ok')).focus();
    });
  }
  const ask = (message, ok = 'OK', danger = false) => dialog({ message, ok, danger }).then((v) => v === true);
  const askText = (title, value, ok = 'OK') => dialog({ title, input: value, ok });
  const tell = (message) => dialog({ message, cancel: null });

  const keyRows = [['key1', 'key2', 'key3'], ['key4', 'key5', 'key6']];
  // Media and system keys the pad can send itself (pad action names).
  const systemKeys = [
    ['play', 'Play / pause', '<path d="M4 5l9 7-9 7z"/><path d="M16 5v14M20 5v14"/>'],
    ['next', 'Next track', '<path d="M5 5l10 7-10 7z"/><path d="M19 5v14"/>'],
    ['prev', 'Previous track', '<path d="M19 5L9 12l10 7z"/><path d="M5 5v14"/>'],
    ['mute', 'Mute', '<path d="M4 9h4l5-4v14l-5-4H4z"/><path d="M17 9l5 6M22 9l-5 6"/>'],
    ['volume-up', 'Volume up', '<path d="M4 9h4l5-4v14l-5-4H4z"/><path d="M17 9a4 4 0 0 1 0 6M19.5 6.5a8 8 0 0 1 0 11"/>'],
    ['volume-down', 'Volume down', '<path d="M4 9h4l5-4v14l-5-4H4z"/><path d="M17 10a3 3 0 0 1 0 4"/>'],
    ['brightness-up', 'Brightness up', '<circle cx="12" cy="12" r="4"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3"/>'],
    ['brightness-down', 'Brightness down', '<circle cx="12" cy="12" r="4"/>'],
  ];
  const systemNames = new Set(systemKeys.map((s) => s[0]).concat(['play-pause', 'previous', 'vol-up', 'vol-down', 'stop']));
  const macroKinds = [
    ['once', 'No repeat', 'Plays once per press.', '<path d="M4 12h15M13 6l6 6-6 6"/>'],
    ['repeat', 'Repeat while holding', 'Plays again and again while the key is held.', '<path d="M20 12a8 8 0 1 1-3-6.2"/><path d="M20 4v5h-5"/>'],
    ['toggle', 'Toggle', 'A press starts repeating it, the next press stops it.', '<rect x="2" y="7" width="20" height="10" rx="5"/><circle cx="16" cy="12" r="3" fill="currentColor"/>'],
    ['sequence', 'Sequence', 'Steps when pressed, others repeated while held, others when released.', '<rect x="2" y="7" width="8" height="8" rx="1.5" fill="currentColor"/><rect x="8" y="9" width="7" height="9" rx="1.5"/><rect x="14" y="10" width="7" height="9" rx="1.5"/>'],
  ];
  const kindName = Object.fromEntries(macroKinds.map((k) => [k[0], k[1]]));

  let state = { keys: [], modes: [], icons: [] };
  let macros = [];
  let selected = 'key1';
  let atab = null; // the open assignment tab; null = the one matching the key
  let editingProfile = 0; // the profile shown and edited; 0 = the persistent one

  const bySlot = (slot) => state.keys.find((k) => k.slot === slot);
  const macroById = (id) => macros.find((m) => m.id === id);
  const basename = (p) => String(p || '').split('/').filter(Boolean).pop() || p;

  // Which assignment tab a key's current setting belongs to.
  function kindOf(k) {
    if (k.mode === 'macro') return 'macros';
    if (k.mode === 'keys') return systemNames.has((k.action || '').trim().toLowerCase()) ? 'system' : 'keys';
    if (k.mode === 'script' || k.mode === 'shell') return 'commands';
    return null;
  }

  function assignmentName(k) {
    switch (k.mode) {
      case 'macro': return (k.macro && k.macro.name) || 'Macro';
      case 'keys': {
        const sys = systemKeys.find((s) => s[0] === (k.action || '').trim().toLowerCase());
        return sys ? sys[1] : (k.action || '');
      }
      case 'script': return basename(k.action);
      case 'shell': return k.action || '';
      default: return '';
    }
  }
  // An off key shows as unassigned even if it kept a label.
  const calloutText = (k) => (k.mode === 'off' ? 'Not assigned' : k.label || assignmentName(k) || 'Not assigned');

  async function load() {
    const q = editingProfile ? `?profile=${editingProfile}` : '';
    const [keys, lib] = await Promise.all([api('GET', `/api/keys${q}`), api('GET', `/api/macros${q}`)]);
    state = keys;
    macros = lib.macros;
    renderAll();
  }
  function renderAll() {
    renderDevice();
    renderAssign();
    renderSync();
    requestAnimationFrame(renderCallouts);
  }

  // A key's icon: icon:<name> (built-in drawing from the server), a data URL
  // (an uploaded picture, shown as an image so an SVG's scripts never run) or
  // a short text / emoji.
  function iconHTML(icon, cls = 'key-icon') {
    if (!icon) return '';
    if (icon.startsWith('icon:')) {
      const ic = (state.icons || []).find((i) => i.name === icon.slice(5));
      return ic ? `<svg class="${cls}" aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${ic.svg}</svg>` : '';
    }
    if (icon.startsWith('data:image/')) return `<img class="${cls}" src="${esc(icon)}" alt="">`;
    return `<span class="${cls} icon-text">${esc(icon)}</span>`;
  }

  // What the dot on a key means: the result of its last run by the pad.
  function dotTitle(k) {
    if (!k.dispatched) return k.mode === 'off' ? 'Off' : 'The pad sends this itself; this app does not run it';
    if (!k.last_run) return 'Not run yet';
    return `Last run: ${k.last_run.status} at ${fmt(k.last_run.started_at)}`;
  }

  /* ---- the device ----------------------------------------------------------- */
  function renderDevice() {
    $('key-grid').innerHTML = keyRows.flat().map((slot) => {
      const k = bySlot(slot);
      return `<button type="button" class="key ${slot === selected ? 'selected' : ''}" data-slot="${slot}" title="${esc(k.name)}: ${esc(assignmentName(k) || 'not assigned')}">
        <span class="cap">
          <span class="top"><span class="name">${esc(k.name)}</span><span class="dot ${k.last_run ? esc(k.last_run.status) : ''}" title="${esc(dotTitle(k))}"></span></span>
          ${iconHTML(k.icon)}
          <span class="label">${esc(k.mode === 'off' ? '' : k.label || assignmentName(k))}</span>
          <span class="badge ${esc(k.mode)}">${esc(k.mode)}</span>
        </span>
      </button>`;
    }).join('');
    const knobBtn = (slot, cls, text) => {
      const k = bySlot(slot);
      return `<button type="button" class="${cls} ${slot === selected ? 'selected' : ''} ${k.mode === 'off' ? '' : 'set'}" data-slot="${slot}" title="${esc(k.name)}: ${esc(assignmentName(k) || 'not assigned')}">${text}</button>`;
    };
    $('knob-area').innerHTML = knobBtn('knob-press', 'knob-cap', '<span class="knob-mark"></span>') +
      `<div class="knob-turns">${knobBtn('knob-left', 'knob-turn', '↺')}${knobBtn('knob-right', 'knob-turn', '↻')}</div>`;
  }

  function selectSlot(slot) {
    if (slot === selected) return;
    selected = slot;
    atab = null;
    openPicker(false);
    renderAll();
  }
  document.querySelector('.stage').addEventListener('click', (e) => {
    const el = e.target.closest('[data-slot]');
    if (el) selectSlot(el.dataset.slot);
  });

  /* ---- callouts: a dot on each control, a line to its name (like G HUB) ----- */
  // Where each control's name goes, so no line crosses a key: keys 1 and 4 to
  // the left, 2 and 3 above, 5 and 6 below (each growing away from the
  // other), the knob to the right. The dot sits on the edge the line comes from.
  const calloutPlan = {
    key1: { side: 'left', edge: 'left' }, key4: { side: 'left', edge: 'left' },
    key2: { side: 'top', align: 'end' }, key3: { side: 'top', align: 'start' },
    key5: { side: 'bottom', align: 'end' }, key6: { side: 'bottom', align: 'start' },
    'knob-press': { side: 'right' }, 'knob-left': { side: 'right' }, 'knob-right': { side: 'right' },
  };

  function renderCallouts() {
    const stage = document.querySelector('.stage');
    const sr = stage.getBoundingClientRect();
    const dr = $('device').getBoundingClientRect();
    const lines = $('callout-lines');
    const box = $('callouts');
    if (!state.keys.length || sr.width < dr.width + 360) { lines.innerHTML = ''; box.innerHTML = ''; return; }
    const rel = (r) => ({ left: r.left - sr.left, right: r.right - sr.left, top: r.top - sr.top, bottom: r.bottom - sr.top, cx: r.left + r.width / 2 - sr.left, cy: r.top + r.height / 2 - sr.top });
    const dev = rel(dr);
    const margin = 22;
    // Names first, so their widths can be measured for the underlines.
    box.innerHTML = Object.keys(calloutPlan).map((slot) => {
      const k = bySlot(slot);
      const cls = ['callout', calloutPlan[slot].side, k.mode === 'off' ? 'unassigned' : '', slot === selected ? 'selected' : ''].join(' ');
      return `<button type="button" class="${cls}" data-slot="${slot}" title="${esc(k.name)}">${iconHTML(k.icon, 'callout-icon')}<span>${esc(calloutText(k))}</span></button>`;
    }).join('');
    let out = '';
    let rightPrev = -Infinity;
    const rightOrder = ['knob-press', 'knob-left', 'knob-right'];
    for (const slot of [...Object.keys(calloutPlan).filter((s) => !rightOrder.includes(s)), ...rightOrder]) {
      const plan = calloutPlan[slot];
      const el = box.querySelector(`[data-slot="${slot}"]`);
      const t = rel($('device').querySelector(`[data-slot="${slot}"]`).getBoundingClientRect());
      const w = Math.min(el.offsetWidth, 220);
      let dot, under; // under: [x1, x2, y] of the line under the name
      if (plan.side === 'left') {
        dot = [t.left + 8, t.cy];
        el.style.cssText = `left:${margin}px;top:${t.cy - 26}px;max-width:${dev.left - margin - 40}px`;
        under = [margin, dev.left - 26, t.cy];
      } else if (plan.side === 'right') {
        dot = [t.cx, t.cy];
        const y = Math.max(t.cy, rightPrev + 34);
        rightPrev = y;
        el.style.cssText = `right:${margin}px;top:${y - 26}px;max-width:${sr.width - dev.right - margin - 40}px`;
        under = [dev.right + 26, sr.width - margin, y];
      } else {
        const top = plan.side === 'top';
        dot = [t.cx, top ? t.top + 8 : t.bottom - 8];
        const y = top ? dev.top - 36 : dev.bottom + 50;
        const x = plan.align === 'end' ? t.cx - w : t.cx;
        el.style.cssText = `left:${x}px;top:${y - 26}px;max-width:220px`;
        under = plan.align === 'end' ? [t.cx - w, t.cx, y] : [t.cx, t.cx + w, y];
      }
      const [x1, x2, y] = under;
      const joint = plan.side === 'left' ? [x2, y] : plan.side === 'right' ? [x1, y] : [t.cx, y];
      out += `<polyline points="${x1},${y} ${x2},${y}"/><polyline points="${joint[0]},${joint[1]} ${dot[0]},${dot[1]}"/><circle cx="${dot[0]}" cy="${dot[1]}" r="5"/>`;
    }
    lines.innerHTML = out;
  }
  new ResizeObserver(() => renderCallouts()).observe(document.querySelector('.stage'));

  /* ---- assignments panel ------------------------------------------------------ */
  function renderAssign() {
    const k = bySlot(selected);
    if (!k) return;
    $('target-name').textContent = k.name;
    document.querySelectorAll('.target-ref').forEach((el) => { el.textContent = k.name; });
    if (document.activeElement !== $('label')) $('label').value = k.label || '';
    setIcon(k.icon || '');
    const what = assignmentName(k);
    $('current').innerHTML = {
      macro: () => `Plays the macro <b>${esc(what)}</b> (${esc(kindName[k.macro && k.macro.kind] || 'macro')}).`,
      keys: () => `The pad sends <b>${esc(k.action)}</b> itself.`,
      script: () => `Runs the script <b>${esc(what)}</b>.`,
      shell: () => `Runs <b>${esc(what)}</b>.`,
    }[k.mode]?.() || 'Not assigned: pick something below.';
    const kind = kindOf(k);
    const tab = atab || kind || 'macros';
    document.querySelectorAll('.atab').forEach((t) => {
      t.classList.toggle('active', t.dataset.atab === tab);
      t.classList.toggle('current-kind', t.dataset.atab === kind);
      t.setAttribute('aria-selected', t.dataset.atab === tab);
    });
    $('atab-rules').classList.toggle('hidden', !k.dispatched);
    document.querySelectorAll('.apanel').forEach((p) => p.classList.toggle('hidden', p.dataset.apanel !== tab));
    $('unassign').classList.toggle('hidden', k.mode === 'off');
    renderMacroList();
    if (document.activeElement !== $('keys-action')) $('keys-action').value = kind === 'keys' ? k.action : '';
    if (!cmdKind || kind === 'commands') cmdKind = k.mode === 'shell' ? 'shell' : 'script';
    renderCommand(kind === 'commands' ? k.action : null);
    $('system-grid').innerHTML = systemKeys.map(([name, label, path]) => `<button type="button" class="system-btn ${kind === 'system' && k.action === name ? 'selected' : ''}" data-system="${name}">${svg(path)}<span>${esc(label)}</span></button>`).join('');
    $('cooldown').value = k.cooldown_seconds;
    $('single_instance').checked = k.single_instance;
    $('last-run').textContent = k.last_run ? `Last run: ${k.last_run.status}${k.last_run.exit_code == null ? '' : ` (exit ${k.last_run.exit_code})`} at ${fmt(k.last_run.started_at)}${k.last_run.output ? ` — ${k.last_run.output.slice(-200)}` : ''}` : 'Not run yet.';
  }

  document.querySelectorAll('.atab').forEach((t) => t.addEventListener('click', () => { atab = t.dataset.atab; renderAssign(); }));

  function renderMacroList() {
    const q = $('macro-search').value.trim().toLowerCase();
    const k = bySlot(selected);
    const rows = macros.filter((m) => !q || m.name.toLowerCase().includes(q));
    $('macro-list').innerHTML = rows.length ? rows.map((m) => {
      const assigned = k.mode === 'macro' && k.macro_id === m.id;
      const where = m.assigned_to.map((s) => (bySlot(s) || {}).name || s).join(', ');
      return `<div class="macro-row ${assigned ? 'assigned' : ''}" role="listitem" data-macro="${m.id}" title="Assign to ${esc(k.name)}">
        <span class="macro-badge" style="${m.color ? `background:${esc(m.color)}` : ''}">M</span>
        <span class="macro-name">${esc(m.name)}</span>
        <span class="macro-meta">${esc(where || kindName[m.kind])}</span>
        <button type="button" class="btn btn-ghost btn-sm macro-edit" data-edit="${m.id}" title="Edit macro" aria-label="Edit ${esc(m.name)}">${svg('<path d="M4 20h4L19 9l-4-4L4 16z"/>', 16)}</button>
      </div>`;
    }).join('') : `<p class="muted">${macros.length ? 'No macro matches.' : 'No macros yet: create one.'}</p>`;
  }
  $('macro-search').addEventListener('input', renderMacroList);
  $('macro-list').addEventListener('click', (e) => {
    const edit = e.target.closest('[data-edit]');
    if (edit) { openEditor(macroById(Number(edit.dataset.edit))); return; }
    const row = e.target.closest('[data-macro]');
    if (row) saveKey({ mode: 'macro', macro_id: Number(row.dataset.macro) });
  });
  $('macro-new').addEventListener('click', () => openEditor(null));

  // Keys: what the pad types itself.
  $('keys-examples').innerHTML = ['cmd+shift+4', 'cmd+space', 'cmd+c', 'cmd+a, cmd+c', 'esc', 'f5'].map((x) => `<button type="button" class="chip-btn" data-example="${esc(x)}">${esc(x)}</button>`).join('');
  $('keys-examples').addEventListener('click', (e) => { const b = e.target.closest('[data-example]'); if (b) $('keys-action').value = b.dataset.example; });
  $('keys-assign').addEventListener('click', () => saveKey({ mode: 'keys', action: $('keys-action').value.trim() }));
  $('keys-action').addEventListener('keydown', (e) => { if (e.key === 'Enter') $('keys-assign').click(); });

  // Commands: a script file or a shell command.
  let cmdKind = null;
  const cmdInfo = {
    script: ['Script file', '/Users/you/scripts/handoff.sh', 'Run with bash unless it is executable. Output is kept with the run.'],
    shell: ['Shell command', 'open -a Spotify', 'Run with bash -c (cmd /C on Windows).'],
  };
  function renderCommand(value) {
    document.querySelectorAll('[data-cmd]').forEach((b) => b.setAttribute('aria-checked', b.dataset.cmd === cmdKind));
    const [label, placeholder, hint] = cmdInfo[cmdKind];
    $('cmd-label').textContent = label;
    $('cmd-action').placeholder = placeholder;
    $('cmd-hint').textContent = hint;
    if (value != null && document.activeElement !== $('cmd-action')) $('cmd-action').value = value;
  }
  document.querySelectorAll('[data-cmd]').forEach((b) => b.addEventListener('click', () => { cmdKind = b.dataset.cmd; renderCommand(null); }));
  $('cmd-assign').addEventListener('click', () => saveKey({ mode: cmdKind, action: $('cmd-action').value.trim() }));

  // System: media keys the pad sends.
  $('system-grid').addEventListener('click', (e) => { const b = e.target.closest('[data-system]'); if (b) saveKey({ mode: 'keys', action: b.dataset.system }); });

  // When it runs, label: saved as soon as they change.
  ['cooldown', 'single_instance', 'label'].forEach((id) => $(id).addEventListener('change', () => saveKey({})));

  $('unassign').addEventListener('click', async () => {
    const k = bySlot(selected);
    if (!(await ask(`Unassign ${k.name}? It does nothing until you assign something again.`, 'Unassign', true))) return;
    saveKey({ mode: 'off', action: '', macro_id: 0, label: '' });
  });

  function status(text, kind) {
    $('status').textContent = text;
    $('status').className = `status ${kind || 'muted'}`;
  }

  // saveKey stores the selected key with patch over its current settings;
  // the server writes the pad too.
  async function saveKey(patch, slot = selected) {
    const k = bySlot(slot);
    const body = {
      label: slot === selected ? $('label').value : k.label, icon: slot === selected ? currentIcon : k.icon,
      mode: k.mode, action: k.action, steps: [], macro_id: k.macro_id || 0,
      app_scope: '',
      cooldown_seconds: Number((slot === selected ? $('cooldown').value : k.cooldown_seconds) || 0),
      single_instance: slot === selected ? $('single_instance').checked : k.single_instance,
      ...patch,
    };
    if (body.mode !== 'macro') body.macro_id = 0;
    status('Saving…');
    try {
      const out = await api('PUT', `/api/keys/${slot}${editingProfile ? `?profile=${editingProfile}` : ''}`, body);
      atab = null;
      await load();
      if (out.synced) status('Saved and written to the pad.', 'ok');
      else status(`Saved, but the pad was not written: ${out.sync_error}`, 'error');
    } catch (err) {
      status(err.message, 'error');
    }
  }

  function renderSync() {
    const behind = !state.synced_at || (state.changed_at && state.changed_at > state.synced_at);
    $('sync-banner').classList.toggle('hidden', !behind);
    $('sync-text').textContent = `Last written: ${fmt(state.synced_at)}.`;
  }
  $('sync-button').addEventListener('click', async () => {
    $('sync-button').disabled = true;
    try { await api('POST', '/api/sync'); await load(); status('Written to the pad.', 'ok'); } catch (err) { status(err.message, 'error'); }
    $('sync-button').disabled = false;
  });

  /* ---- icon picker -------------------------------------------------------- */
  let currentIcon = '';
  function setIcon(icon) {
    currentIcon = icon;
    $('icon-button').innerHTML = icon ? iconHTML(icon, 'icon-preview') : '<span class="muted">Icon</span>';
  }
  function pickIcon(icon) { setIcon(icon); openPicker(false); saveKey({}); }
  function openPicker(open) {
    $('icon-picker').classList.toggle('hidden', !open);
    $('icon-button').setAttribute('aria-expanded', open);
    $('icon-error').classList.add('hidden');
    if (!open) return;
    $('icon-grid').innerHTML = (state.icons || []).map((ic) => `<button type="button" class="icon-choice ${currentIcon === 'icon:' + ic.name ? 'selected' : ''}" data-icon="icon:${esc(ic.name)}" title="${esc(ic.name)}" role="option" aria-selected="${currentIcon === 'icon:' + ic.name}">${iconHTML('icon:' + ic.name, 'icon-preview')}</button>`).join('');
    $('icon-text').value = currentIcon && !currentIcon.includes(':') ? currentIcon : '';
  }
  $('icon-button').addEventListener('click', () => openPicker($('icon-picker').classList.contains('hidden')));
  $('icon-grid').addEventListener('click', (e) => { const b = e.target.closest('[data-icon]'); if (b) pickIcon(b.dataset.icon); });
  $('icon-text-use').addEventListener('click', () => pickIcon($('icon-text').value.trim()));
  $('icon-text').addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); $('icon-text-use').click(); } });
  $('icon-none').addEventListener('click', () => pickIcon(''));
  document.addEventListener('click', (e) => { if (!e.target.closest('.icon-field')) openPicker(false); });
  // Pictures are shrunk to 128 px PNG (SVG is kept as is) so they stay small.
  $('icon-file').addEventListener('change', async () => {
    const file = $('icon-file').files[0];
    $('icon-file').value = '';
    if (!file) return;
    const fail = (msg) => { $('icon-error').textContent = msg; $('icon-error').classList.remove('hidden'); };
    const read = (f) => new Promise((ok, no) => { const r = new FileReader(); r.onload = () => ok(r.result); r.onerror = no; r.readAsDataURL(f); });
    try {
      let url = await read(file);
      if (file.type === 'image/svg+xml') {
        if (file.size > 256 * 1024) return fail('The SVG is over 256 KB.');
      } else {
        const img = new Image();
        await new Promise((ok, no) => { img.onload = ok; img.onerror = no; img.src = url; });
        const scale = Math.min(1, 128 / Math.max(img.width, img.height));
        const canvas = document.createElement('canvas');
        canvas.width = Math.max(1, Math.round(img.width * scale));
        canvas.height = Math.max(1, Math.round(img.height * scale));
        canvas.getContext('2d').drawImage(img, 0, 0, canvas.width, canvas.height);
        url = canvas.toDataURL('image/png');
      }
      pickIcon(url);
    } catch {
      fail('Could not read that picture.');
    }
  });

  /* ---- views: Devices | Profiles ---------------------------------------------- */
  document.querySelectorAll('.view-tab').forEach((t) => t.addEventListener('click', () => showView(t.dataset.view)));
  function showView(view) {
    document.querySelectorAll('.view-tab').forEach((t) => t.classList.toggle('active', t.dataset.view === view));
    $('view-devices').classList.toggle('hidden', view !== 'devices');
    $('view-profiles').classList.toggle('hidden', view !== 'profiles');
    if (view === 'devices') requestAnimationFrame(renderCallouts);
    else renderProfiles();
  }

  /* ---- profiles ------------------------------------------------------------------ */
  let prof = { apps: [], switching: false, persistent_profile_id: 0, active_profile_id: 0 };
  let appFilter = 'all';
  let openApp = null; // app id whose profiles are shown
  const allProfiles = () => prof.apps.flatMap((a) => a.profiles.map((p) => ({ ...p, app: a })));
  const profileName = (p) => `${p.app.name}: ${p.name}`;
  const appArt = (a) => a.kind === 'desktop'
    ? svg('<rect x="2" y="4" width="20" height="13" rx="2"/><path d="M8 21h8M12 17v4"/>', 56)
    : (a.path ? `<img src="/api/app-icon?path=${encodeURIComponent(a.path)}" alt="">` : svg('<rect x="4" y="4" width="16" height="16" rx="4"/>', 56));

  async function loadProfiles() {
    prof = await api('GET', '/api/profiles');
    if (!editingProfile) editingProfile = prof.switching ? prof.active_profile_id : prof.persistent_profile_id;
    renderProfileSelect();
    if (!$('view-profiles').classList.contains('hidden')) renderProfiles();
  }
  function renderProfileSelect() {
    const groups = prof.apps.map((a) => `<optgroup label="${esc(a.name)}">${a.profiles.map((p) => {
      const mark = p.id === prof.active_profile_id ? ' ●' : '';
      return `<option value="${p.id}" ${p.id === editingProfile ? 'selected' : ''}>${esc(a.name)}: ${esc(p.name)}${mark}</option>`;
    }).join('')}</optgroup>`).join('');
    if ($('profile-select').innerHTML !== groups) $('profile-select').innerHTML = groups;
    $('profile-select').value = String(editingProfile);
  }
  $('profile-select').addEventListener('change', () => editProfile(Number($('profile-select').value)));
  async function editProfile(id) {
    editingProfile = id;
    renderProfileSelect();
    await load();
    status(`Editing ${profileName(allProfiles().find((p) => p.id === id))}.`);
  }

  function renderProfiles() {
    const active = prof.apps.filter((a) => a.enabled), disabled = prof.apps.filter((a) => !a.enabled);
    $('count-all').textContent = prof.apps.length;
    $('count-active').textContent = active.length;
    $('count-disabled').textContent = disabled.length;
    document.querySelectorAll('.pv-filter').forEach((b) => b.classList.toggle('active', b.dataset.filter === appFilter));
    $('switching').checked = prof.switching;
    $('persistent-select').innerHTML = allProfiles().map((p) => `<option value="${p.id}" ${p.id === prof.persistent_profile_id ? 'selected' : ''}>${esc(profileName(p))}</option>`).join('');
    const q = $('app-search').value.trim().toLowerCase();
    const list = (appFilter === 'active' ? active : appFilter === 'disabled' ? disabled : prof.apps).filter((a) => !q || a.name.toLowerCase().includes(q));
    $('app-cards').innerHTML = list.map((a) => {
      const inUse = a.profiles.some((p) => p.id === prof.active_profile_id);
      return `<button type="button" class="app-card ${a.enabled ? '' : 'disabled'} ${openApp === a.id ? 'open' : ''}" data-app="${a.id}">
        <span class="app-art">${appArt(a)}</span>
        <span class="app-name">${esc(a.name)}</span>
        <span class="app-meta"><span>${a.profiles.length} profile${a.profiles.length === 1 ? '' : 's'}</span>${inUse ? '<span class="in-use">● In use</span>' : ''}</span>
      </button>`;
    }).join('') || '<p class="muted">No apps here.</p>';
    renderAppDetail();
  }
  document.querySelectorAll('.pv-filter').forEach((b) => b.addEventListener('click', () => { appFilter = b.dataset.filter; renderProfiles(); }));
  $('app-search').addEventListener('input', renderProfiles);
  $('app-cards').addEventListener('click', (e) => {
    const card = e.target.closest('[data-app]');
    if (!card) return;
    openApp = openApp === Number(card.dataset.app) ? null : Number(card.dataset.app);
    renderProfiles();
  });

  function renderAppDetail() {
    const a = prof.apps.find((x) => x.id === openApp);
    $('app-detail').classList.toggle('hidden', !a);
    if (!a) return;
    $('app-detail').innerHTML = `<h3>${esc(a.name)}</h3>
      <p class="muted">${a.kind === 'desktop' ? 'Used with any app that has no profile of its own.' : `Used while ${esc(a.name)} is in front${a.bundle_id ? '' : ' (matched by name until it is seen)'}.`} The selected profile is the one it switches to.</p>
      ${a.profiles.map((p) => `<div class="profile-row" data-profile="${p.id}">
        <input type="radio" name="app-active" ${p.id === a.active_profile_id ? 'checked' : ''} data-use="${p.id}" aria-label="Use ${esc(p.name)} for ${esc(a.name)}" title="Use this profile for ${esc(a.name)}">
        <input type="text" value="${esc(p.name)}" data-rename="${p.id}" aria-label="Profile name" maxlength="60">
        <span class="row-actions">
          <button type="button" class="btn btn-sm" data-edit-profile="${p.id}">Edit keys</button>
          <button type="button" class="btn btn-ghost btn-sm" data-dup="${p.id}">Duplicate</button>
          ${a.profiles.length > 1 ? `<button type="button" class="btn btn-ghost btn-sm btn-danger" data-del-profile="${p.id}">Delete</button>` : ''}
        </span>
      </div>`).join('')}
      <div class="detail-actions">
        <button type="button" class="btn btn-sm" data-new-profile="${a.id}">+ New profile</button>
        ${a.kind === 'desktop' ? '' : `<label class="check"><input type="checkbox" data-enable="${a.id}" ${a.enabled ? 'checked' : ''}> Enabled</label>
        <button type="button" class="btn btn-ghost btn-sm btn-danger" data-remove-app="${a.id}">Remove app</button>`}
      </div>`;
  }

  async function profileCall(method, path, body, done) {
    try { await api(method, path, body); await loadProfiles(); if (done) await done(); } catch (err) { tell(err.message); }
  }
  $('app-detail').addEventListener('change', (e) => {
    const a = prof.apps.find((x) => x.id === openApp);
    const use = e.target.closest('[data-use]');
    if (use) return profileCall('PATCH', `/api/apps/${a.id}`, { active_profile_id: Number(use.dataset.use) });
    const ren = e.target.closest('[data-rename]');
    if (ren) return profileCall('PATCH', `/api/profiles/${ren.dataset.rename}`, { name: ren.value });
    const en = e.target.closest('[data-enable]');
    if (en) return profileCall('PATCH', `/api/apps/${en.dataset.enable}`, { enabled: en.checked });
  });
  $('app-detail').addEventListener('click', async (e) => {
    const a = prof.apps.find((x) => x.id === openApp);
    const b = e.target.closest('button');
    if (!b || !a) return;
    if (b.dataset.editProfile) { editProfile(Number(b.dataset.editProfile)); showView('devices'); return; }
    if (b.dataset.dup) { const p = a.profiles.find((x) => x.id === Number(b.dataset.dup)); profileCall('POST', '/api/profiles', { app_id: a.id, name: `${p.name} copy`, copy_from: p.id }); return; }
    if (b.dataset.newProfile) { const name = await askText(`New profile for ${a.name}`, 'New profile', 'Create'); if (name) profileCall('POST', '/api/profiles', { app_id: a.id, name, copy_from: a.active_profile_id }); return; }
    if (b.dataset.delProfile && await ask(`Delete the profile “${a.profiles.find((p) => p.id === Number(b.dataset.delProfile)).name}” and its keys?`, 'Delete', true)) {
      const id = Number(b.dataset.delProfile);
      profileCall('DELETE', `/api/profiles/${id}`, null, () => (id === editingProfile ? editProfile(prof.persistent_profile_id) : null));
      return;
    }
    if (b.dataset.removeApp && await ask(`Remove ${a.name} and its profiles?`, 'Remove', true)) {
      openApp = null;
      profileCall('DELETE', `/api/apps/${a.id}`, null, () => (a.profiles.some((p) => p.id === editingProfile) ? editProfile(prof.persistent_profile_id) : null));
    }
  });
  $('switching').addEventListener('change', () => profileCall('PUT', '/api/profiles/settings', { switching: $('switching').checked }, load));
  $('persistent-select').addEventListener('change', () => profileCall('PUT', '/api/profiles/settings', { persistent_profile_id: Number($('persistent-select').value) }, load));

  // Add apps: the installed ones, or the OS's own picker.
  let installed = [];
  $('add-apps').addEventListener('click', async () => {
    $('add-apps-error').textContent = '';
    $('add-apps-dialog').showModal();
    $('installed-list').innerHTML = '<p class="muted">Looking for apps…</p>';
    try { installed = (await api('GET', '/api/installed-apps')).apps; } catch { installed = []; }
    renderInstalled();
  });
  function renderInstalled() {
    const have = new Set(prof.apps.map((a) => a.bundle_id).filter(Boolean));
    const q = $('installed-search').value.trim().toLowerCase();
    $('installed-list').innerHTML = installed.filter((a) => !have.has(a.bundle_id) && (!q || a.name.toLowerCase().includes(q))).map((a) => `<label class="installed-row" role="listitem">
      <input type="checkbox" data-path="${esc(a.path)}"><img src="/api/app-icon?path=${encodeURIComponent(a.path)}" alt="" loading="lazy"><span>${esc(a.name)}</span>
    </label>`).join('') || '<p class="muted">No apps to add.</p>';
  }
  $('installed-search').addEventListener('input', renderInstalled);
  $('add-apps-cancel').addEventListener('click', () => $('add-apps-dialog').close());
  $('add-apps-ok').addEventListener('click', async () => {
    const paths = [...$('installed-list').querySelectorAll('input:checked')].map((c) => c.dataset.path);
    try {
      for (const path of paths) await api('POST', '/api/apps', { path });
      $('add-apps-dialog').close();
      await loadProfiles();
    } catch (err) { $('add-apps-error').textContent = err.message; }
  });
  $('browse-app').addEventListener('click', async () => {
    try {
      const out = await api('POST', '/api/apps/browse');
      if (out.cancelled) return;
      $('add-apps-dialog').close();
      await loadProfiles();
      openApp = out.id;
      renderProfiles();
    } catch (err) { $('add-apps-error').textContent = err.message; }
  });

  // The profile in use follows the app in front: keep the ● and "In use" current.
  setInterval(() => { if (!editorOpen()) loadProfiles().catch(() => {}); }, 2000);

  /* ---- macro editor (full window, like G HUB) ---------------------------------- */
  // Name → type → editor. Steps show as blocks: a key going down has ▼ under
  // it, going up ▲ over it; the milliseconds between steps sit between them.
  let ed = null;        // the macro being edited
  let edSaved = '';     // JSON of it as loaded, for Reset and "discard?"
  let edAssign = null;  // slot to assign a new macro to once saved
  let edLane = 'steps'; // lane the add menu / recorder writes into
  let showUpDown = true;
  try { showUpDown = localStorage.getItem('showUpDown') !== '0'; } catch { /* storage blocked */ }

  const editorOpen = () => !$('macro-editor').classList.contains('hidden');
  const clone = (x) => JSON.parse(JSON.stringify(x));
  const keyLabel = { cmd: 'Command', rcmd: 'Command', opt: 'Option', ropt: 'Option', ctrl: 'Control', rctrl: 'Control', shift: 'Shift', rshift: 'Shift', esc: 'Esc', enter: 'Enter', space: 'Space', backspace: 'Delete', tab: 'Tab', capslock: 'Caps lock', fn: 'fn', left: '←', right: '→', up: '↑', down: '↓' };
  const isMedia = (v) => systemNames.has(String(v).toLowerCase()) || /^media:\d+$/.test(v) || /^(brightness|keyboard-light|volume)-/.test(v) || ['eject', 'fast-forward', 'rewind'].includes(v);

  function showStep(name) {
    ['me-name-step', 'me-type-step', 'me-edit-step'].forEach((id) => $(id).classList.toggle('hidden', id !== name));
  }

  function openEditor(macro) {
    closeMenus();
    edAssign = macro ? null : selected;
    ed = macro ? clone(macro) : { id: 0, name: '', kind: 'once', steps: [], hold_steps: [], release_steps: [], standard_delay_ms: 0, color: '' };
    ['steps', 'hold_steps', 'release_steps'].forEach((l) => { ed[l] = ed[l] || []; });
    edSaved = JSON.stringify(ed);
    edLane = 'steps';
    $('macro-editor').classList.remove('hidden');
    $('me-error').textContent = '';
    if (macro) { renderEditor(); showStep('me-edit-step'); return; }
    $('me-name-input').value = '';
    $('me-name-hint').classList.add('hidden');
    showStep('me-name-step');
    $('me-name-input').focus();
  }

  async function closeEditor(force) {
    if (!force && ed && JSON.stringify(ed) !== edSaved && !(await ask('Discard the changes to this macro?', 'Discard', true))) return;
    if (recording) stopRecording();
    closeMenus();
    $('macro-editor').classList.add('hidden');
    ed = null;
  }

  $('me-back').addEventListener('click', () => {
    if (!$('me-type-step').classList.contains('hidden')) { showStep('me-name-step'); $('me-name-input').focus(); return; }
    closeEditor(false);
  });
  $('me-name-input').addEventListener('input', () => $('me-name-hint').classList.toggle('hidden', !$('me-name-input').value.trim()));
  $('me-name-input').addEventListener('keydown', (e) => {
    if (e.key !== 'Enter' || !$('me-name-input').value.trim()) return;
    e.preventDefault(); // or the same Enter would press the first type card
    ed.name = $('me-name-input').value.trim();
    $('me-type-title').textContent = ed.name;
    $('me-type-cards').innerHTML = macroKinds.map(([kind, name, hint, path]) => `<button type="button" class="me-type-card" data-kind="${kind}"><span class="box">${svg(path, 44)}</span><span class="me-caps">${esc(name)}</span><small>${esc(hint)}</small></button>`).join('');
    showStep('me-type-step');
    $('me-type-cards').querySelector('button').focus();
  });
  $('me-type-cards').addEventListener('click', (e) => {
    const card = e.target.closest('[data-kind]');
    if (!card) return;
    ed.kind = card.dataset.kind;
    renderEditor();
    showStep('me-edit-step');
  });

  function renderEditor() {
    if (document.activeElement !== $('me-title')) $('me-title').value = ed.name;
    $('me-type-buttons').innerHTML = macroKinds.map(([kind, name, , path]) => `<button type="button" class="me-type-btn" role="radio" aria-checked="${ed.kind === kind}" data-kind="${kind}" title="${esc(name)}" aria-label="${esc(name)}">${svg(path, 26)}</button>`).join('');
    $('me-type-hint').textContent = macroKinds.find((k) => k[0] === ed.kind)[2];
    $('me-std').checked = ed.standard_delay_ms > 0;
    if (ed.standard_delay_ms > 0 && document.activeElement !== $('me-std-ms')) $('me-std-ms').value = ed.standard_delay_ms;
    $('me-std-hint').textContent = ed.standard_delay_ms > 0 ? `Currently using a standard delay of ${ed.standard_delay_ms} ms between actions.` : 'Currently using the recorded delays between actions.';
    $('me-updown').checked = showUpDown;
    $('me-color').value = ed.color || '#7c6cf5';
    $('me-color').style.opacity = ed.color ? 1 : .35;
    $('me-delete').classList.toggle('hidden', !ed.id);
    const lanes = ed.kind === 'sequence'
      ? [['steps', 'When pressed'], ['hold_steps', 'While held (repeats)'], ['release_steps', 'When released']]
      : [['steps', '']];
    $('me-lanes').innerHTML = lanes.map(([lane, title]) => `<div class="me-lane" data-lane="${lane}">
      ${title ? `<span class="me-caps me-lane-title">${esc(title)}</span>` : ''}
      <div class="me-flow">${laneHTML(lane)}</div>
    </div>`).join('');
  }

  // What a step looks like as a block: class (color), text, ▼/▲.
  function chipOf(s) {
    switch (s.type) {
      case 'keydown': case 'keyup':
        if (isMedia(s.value)) return ['k-system', s.value.replace(/-/g, ' '), s.type === 'keydown' ? 'down' : 'up'];
        return ['k-key', keyLabel[s.value] || s.value, s.type === 'keydown' ? 'down' : 'up'];
      case 'keys': return isMedia(s.value) ? ['k-system', s.value.replace(/-/g, ' '), ''] : ['k-action', s.value, ''];
      case 'text': return ['k-text', `“${s.value}”`, ''];
      case 'launch': return ['k-launch', `↗ ${basename(s.value)}`, ''];
      case 'click': return ['k-click', `${s.value} click`, ''];
      default: return ['k-system', s.value, ''];
    }
  }

  function laneHTML(lane) {
    const steps = ed[lane];
    const std = ed.standard_delay_ms;
    let html = '';
    let shown = 0;
    steps.forEach((s, i) => {
      if (s.type === 'delay') {
        if (!std) html += `<button type="button" class="ms-pill" data-ms="${i}" data-lane="${lane}" title="Wait — click to change">${esc(s.value)} ms</button>`;
        return;
      }
      if (!showUpDown && s.type === 'keyup' && !isMedia(s.value)) return; // folded into its key down
      if (std && shown > 0) html += `<span class="ms-pill fixed" title="Standard delay">${std} ms</span>`;
      const [cls, text, mark] = chipOf(s);
      const marker = showUpDown ? mark : '';
      html += `<span class="step-wrap ${marker}" draggable="true" data-block="${i}" data-lane="${lane}"><button type="button" class="step-chip ${cls}" data-chip="${i}" data-lane="${lane}" title="${esc(s.type)}: ${esc(s.value)} — drag to reorder">${esc(text)}</button></span>`;
      shown++;
    });
    const plus = `<button type="button" class="me-plus" data-add="${lane}" aria-label="Add a step">${svg('<path d="M12 5v14M5 12h14"/>', 26)}</button>`;
    return steps.length ? html + plus : `<span class="me-start">${plus}<span class="me-caps">Start now</span></span>`;
  }

  $('me-title').addEventListener('input', () => { ed.name = $('me-title').value; });
  $('me-type-buttons').addEventListener('click', (e) => { const b = e.target.closest('[data-kind]'); if (b) { ed.kind = b.dataset.kind; renderEditor(); } });
  $('me-std').addEventListener('change', () => { ed.standard_delay_ms = $('me-std').checked ? Math.max(1, Number($('me-std-ms').value) || 50) : 0; renderEditor(); });
  $('me-std-ms').addEventListener('change', () => { if ($('me-std').checked) { ed.standard_delay_ms = Math.max(1, Number($('me-std-ms').value) || 50); renderEditor(); } });
  $('me-updown').addEventListener('change', () => {
    showUpDown = $('me-updown').checked;
    try { localStorage.setItem('showUpDown', showUpDown ? '1' : '0'); } catch { /* storage blocked */ }
    renderEditor();
  });
  $('me-color').addEventListener('input', () => { ed.color = $('me-color').value; renderEditor(); });
  $('me-color-none').addEventListener('click', () => { ed.color = ''; renderEditor(); });

  // Click on a ms: edit it in place.
  $('me-lanes').addEventListener('click', (e) => {
    const pill = e.target.closest('.ms-pill[data-ms]');
    if (pill) {
      const lane = pill.dataset.lane, i = Number(pill.dataset.ms);
      const input = document.createElement('input');
      input.type = 'number'; input.min = '0'; input.className = 'ms-input'; input.value = ed[lane][i].value;
      input.setAttribute('aria-label', 'Milliseconds');
      pill.replaceWith(input);
      input.focus(); input.select();
      const done = () => { ed[lane][i].value = String(Math.max(0, Math.round(Number(input.value) || 0))); renderEditor(); };
      input.addEventListener('blur', done);
      input.addEventListener('keydown', (ev) => { if (ev.key === 'Enter') input.blur(); if (ev.key === 'Escape') { input.value = ed[lane][i].value; input.blur(); } });
      return;
    }
    const chip = e.target.closest('[data-chip]');
    if (chip) { openChipMenu(chip, chip.dataset.lane, Number(chip.dataset.chip)); return; }
    const add = e.target.closest('[data-add]');
    if (add) openAddMenu(add, add.dataset.add);
  });

  function placeNear(el, anchor) {
    const r = anchor.getBoundingClientRect();
    el.classList.remove('hidden');
    const w = el.offsetWidth, h = el.offsetHeight;
    el.style.left = `${Math.min(window.innerWidth - w - 12, Math.max(12, r.left))}px`;
    el.style.top = `${r.bottom + h + 12 > window.innerHeight ? Math.max(12, r.top - h - 8) : r.bottom + 8}px`;
  }
  function closeMenus() { ['me-add-menu', 'me-composer', 'me-chip-menu'].forEach((id) => $(id).classList.add('hidden')); }

  const addItems = [
    ['record', 'Record keystrokes', '●'], ['text', 'Text & emojis', 'T'], ['action', 'Action', '⚡'],
    ['launch', 'Launch application', '↗'], ['system', 'System', 'S'], ['delay', 'Delay', '→'],
  ];
  function openAddMenu(anchor, lane) {
    closeMenus();
    edLane = lane;
    $('me-add-menu').innerHTML = addItems.map(([id, label, ic]) => `<button type="button" class="add-item a-${id}" data-item="${id}" role="menuitem"><span class="add-ic">${ic}</span>${esc(label)}</button>`).join('');
    // Beside the +, bottom aligned with it, like G HUB.
    const menu = $('me-add-menu');
    const r = anchor.getBoundingClientRect();
    menu.classList.remove('hidden');
    menu.style.left = `${Math.min(window.innerWidth - menu.offsetWidth - 12, r.right + 20)}px`;
    menu.style.top = `${Math.max(12, Math.min(window.innerHeight - menu.offsetHeight - 12, r.bottom - menu.offsetHeight))}px`;
    $('me-add-menu').querySelector('button').focus();
  }
  $('me-add-menu').addEventListener('click', (e) => {
    const b = e.target.closest('[data-item]');
    if (!b) return;
    const anchor = b.getBoundingClientRect();
    closeMenus();
    if (b.dataset.item === 'record') { startRecording(); return; }
    openComposer(b.dataset.item, anchor);
  });

  const composers = {
    text: ['Text & emojis', 'Typed as is', 'Hello 👋', 'text'],
    action: ['Action', 'A shortcut in pad syntax', 'cmd+shift+4  ·  cmd+a, cmd+c', 'keys'],
    launch: ['Launch application', 'An app name or a path', 'Safari  ·  /Applications/Notes.app', 'launch'],
    delay: ['Delay', 'Milliseconds', '300', 'delay'],
  };
  function openComposer(item, rect, editIndex) {
    const c = $('me-composer');
    if (item === 'system') {
      c.innerHTML = `<strong>System</strong><div class="media-grid">${systemKeys.map(([name, label, path]) => `<button type="button" class="system-btn" data-media="${name}">${svg(path, 16)}<span>${esc(label)}</span></button>`).join('')}</div>`;
    } else {
      const [title, hint, placeholder] = composers[item];
      const value = editIndex != null ? ed[edLane][editIndex].value : '';
      c.innerHTML = `<strong>${esc(title)}</strong><span class="muted">${esc(hint)}</span>
        <div class="row"><input id="me-comp-input" ${item === 'delay' ? 'type="number" min="0"' : ''} placeholder="${esc(placeholder)}" value="${esc(value)}"><button type="button" class="btn btn-primary btn-sm" id="me-comp-add">${editIndex != null ? 'Save' : 'Add'}</button></div>`;
    }
    c.dataset.item = item;
    c.dataset.edit = editIndex != null ? editIndex : '';
    c.classList.remove('hidden');
    c.style.left = `${Math.min(window.innerWidth - c.offsetWidth - 12, Math.max(12, rect.left))}px`;
    c.style.top = `${Math.min(window.innerHeight - c.offsetHeight - 12, rect.top)}px`;
    const input = $('me-comp-input');
    if (input) { input.focus(); input.select(); }
  }
  $('me-composer').addEventListener('click', (e) => {
    const media = e.target.closest('[data-media]');
    if (media) { addStep({ type: 'keys', value: media.dataset.media }); closeMenus(); return; }
    if (e.target.closest('#me-comp-add')) commitComposer();
  });
  $('me-composer').addEventListener('keydown', (e) => { if (e.key === 'Enter' && e.target.id === 'me-comp-input') commitComposer(); });
  function commitComposer() {
    const c = $('me-composer');
    const value = $('me-comp-input').value.trim();
    if (!value) return;
    const step = { type: composers[c.dataset.item][3], value: c.dataset.item === 'delay' ? String(Math.max(0, Math.round(Number(value) || 0))) : value };
    if (c.dataset.edit !== '') ed[edLane][Number(c.dataset.edit)] = step;
    else addStep(step);
    closeMenus();
    renderEditor();
  }
  function addStep(step) {
    ed[edLane].push(step);
    renderEditor();
  }

  // A block's menu: edit, move, add a wait after it, delete.
  function openChipMenu(anchor, lane, i) {
    closeMenus();
    edLane = lane;
    const s = ed[lane][i];
    const editable = { text: 'text', keys: isMedia(s.value) ? null : 'action', launch: 'launch' }[s.type];
    $('me-chip-menu').innerHTML = [
      editable ? `<button type="button" data-do="edit" data-what="${editable}">Edit</button>` : '',
      `<button type="button" data-do="left">Move left</button>`,
      `<button type="button" data-do="right">Move right</button>`,
      ed.standard_delay_ms ? '' : `<button type="button" data-do="wait">Add a wait after</button>`,
      `<button type="button" data-do="delete" class="btn-danger">Delete</button>`,
    ].join('');
    $('me-chip-menu').dataset.i = i;
    placeNear($('me-chip-menu'), anchor);
  }
  $('me-chip-menu').addEventListener('click', (e) => {
    const b = e.target.closest('[data-do]');
    if (!b) return;
    const steps = ed[edLane];
    const i = Number($('me-chip-menu').dataset.i);
    const rect = $('me-chip-menu').getBoundingClientRect();
    closeMenus();
    // The neighbor block in a direction, skipping waits.
    const neighbor = (dir) => { for (let j = i + dir; j >= 0 && j < steps.length; j += dir) if (steps[j].type !== 'delay') return j; return -1; };
    switch (b.dataset.do) {
      case 'edit': openComposer(b.dataset.what, rect, i); return;
      case 'left': case 'right': {
        const j = neighbor(b.dataset.do === 'left' ? -1 : 1);
        if (j >= 0) [steps[i], steps[j]] = [steps[j], steps[i]];
        break;
      }
      case 'wait': steps.splice(i + 1, 0, { type: 'delay', value: '100' }); break;
      case 'delete': {
        const s = steps[i];
        steps.splice(i, 1);
        if (steps[i] && steps[i].type === 'delay' && (i === 0 || steps[i - 1].type === 'delay')) steps.splice(i, 1);
        // With key down/up folded, deleting a key removes its release too.
        if (!showUpDown && s.type === 'keydown') {
          const up = steps.findIndex((x, j) => j >= i && x.type === 'keyup' && x.value === s.value);
          if (up >= 0) steps.splice(up, 1);
        }
        break;
      }
    }
    renderEditor();
  });

  // Drag and drop of blocks: drop a block before or after another (left or
  // right half), or on the + / empty space to put it last; across lanes too.
  // With key down/up folded, a key moves with its release. The waits stay
  // where they are; two left side by side collapse into the first.
  let drag = null; // { lane, i } of the block being dragged
  function blockUnit(steps, i) {
    const idx = [i];
    if (!showUpDown && steps[i].type === 'keydown') {
      for (let j = i + 1; j < steps.length; j++) {
        if (steps[j].type === 'delay') continue;
        if (steps[j].type === 'keyup' && steps[j].value === steps[i].value) for (let k = i + 1; k <= j; k++) idx.push(k);
        break;
      }
    }
    return idx;
  }
  function moveBlock(fromLane, i, toLane, before) {
    const src = ed[fromLane], dst = ed[toLane];
    const idx = blockUnit(src, i);
    const last = idx[idx.length - 1];
    if (fromLane === toLane && before >= i && before <= last + 1) return false; // onto itself
    const unit = idx.map((k) => src[k]);
    const anchor = before >= 0 && before < dst.length ? dst[before] : null; // the step it goes before (null: last)
    for (let k = idx.length - 1; k >= 0; k--) src.splice(idx[k], 1);
    if (idx[0] === 0 && src[0] && src[0].type === 'delay') src.shift(); // a wait with nothing before it, like delete
    dst.splice(anchor ? dst.indexOf(anchor) : dst.length, 0, ...unit);
    for (let k = dst.length - 1; k > 0; k--) if (dst[k].type === 'delay' && dst[k - 1].type === 'delay') dst.splice(k, 1);
    return true;
  }
  const clearDropMarks = () => $('me-lanes').querySelectorAll('.drop-before, .drop-after, .drop-end').forEach((el) => el.classList.remove('drop-before', 'drop-after', 'drop-end'));
  // Where a drop at this point lands: [lane, index to insert before (-1 = end)].
  function dropTarget(e) {
    const w = e.target.closest('.step-wrap');
    if (w) {
      const lane = w.dataset.lane, t = Number(w.dataset.block);
      const r = w.getBoundingClientRect();
      if (e.clientX < r.left + r.width / 2) return [lane, t, w, 'drop-before'];
      const unit = blockUnit(ed[lane], t);
      return [lane, unit[unit.length - 1] + 1, w, 'drop-after'];
    }
    const flow = e.target.closest('.me-flow');
    if (flow) return [flow.closest('.me-lane').dataset.lane, -1, flow, 'drop-end'];
    return null;
  }
  $('me-lanes').addEventListener('dragstart', (e) => {
    const w = e.target.closest('.step-wrap');
    if (!w) return;
    closeMenus();
    drag = { lane: w.dataset.lane, i: Number(w.dataset.block) };
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', `${drag.lane}:${drag.i}`);
    w.classList.add('dragging');
  });
  $('me-lanes').addEventListener('dragover', (e) => {
    if (!drag) return;
    const target = dropTarget(e);
    if (!target) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    clearDropMarks();
    target[2].classList.add(target[3]);
  });
  $('me-lanes').addEventListener('dragleave', (e) => { if (drag && !$('me-lanes').contains(e.relatedTarget)) clearDropMarks(); });
  $('me-lanes').addEventListener('drop', (e) => {
    if (!drag) return;
    const target = dropTarget(e);
    if (!target) return;
    e.preventDefault();
    const moved = moveBlock(drag.lane, drag.i, target[0], target[1]);
    drag = null;
    if (moved) renderEditor(); else clearDropMarks();
  });
  $('me-lanes').addEventListener('dragend', () => {
    drag = null;
    clearDropMarks();
    $('me-lanes').querySelectorAll('.dragging').forEach((el) => el.classList.remove('dragging'));
  });

  document.addEventListener('click', (e) => {
    if (!editorOpen()) return;
    if (!e.target.closest('#me-add-menu, #me-composer, #me-chip-menu, [data-add], [data-chip], .ms-pill')) closeMenus();
  });
  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape' || !editorOpen()) return;
    if (!$('me-add-menu').classList.contains('hidden') || !$('me-composer').classList.contains('hidden') || !$('me-chip-menu').classList.contains('hidden')) closeMenus();
  });

  // Recording: the app listens system-wide (an event tap) and the page polls.
  let recording = false;
  let recSeen = 0;
  let recLastAt = 0;
  let recTimer = null;
  async function startRecording() {
    closeMenus();
    recSeen = 0;
    recLastAt = 0;
    $('me-recording').classList.remove('hidden');
    $('me-rec-allow').classList.add('hidden');
    $('me-rec-status').textContent = '';
    try {
      await api('POST', `/api/record/start?mouse=${$('me-rec-mouse').checked ? 1 : 0}`);
      recording = true;
      recTimer = setInterval(pollRecording, 150);
    } catch (err) {
      $('me-rec-status').textContent = err.message;
      $('me-rec-allow').classList.remove('hidden');
    }
  }
  async function pollRecording() {
    try {
      const out = await api('GET', `/api/record/events?since=${recSeen}`);
      out.events.forEach((ev) => {
        recSeen += 1;
        const lane = ed[edLane];
        if (lane.length && recLastAt && !ed.standard_delay_ms) {
          const ms = Math.round(ev.at - recLastAt);
          if (ms > 0) lane.push({ type: 'delay', value: String(ms) });
        }
        recLastAt = ev.at;
        lane.push({ type: ev.type, value: ev.value });
      });
      if (out.events.length) renderEditor();
      if (!out.recording && recording) stopRecording();
    } catch (err) {
      stopRecording();
      $('me-rec-status').textContent = err.message;
    }
  }
  function stopRecording() {
    if (recTimer) clearInterval(recTimer);
    recTimer = null;
    if (recording) api('POST', '/api/record/stop').catch(() => {});
    recording = false;
    if (!$('me-rec-status').textContent) $('me-recording').classList.add('hidden');
  }
  $('me-rec-stop').addEventListener('click', () => { $('me-rec-status').textContent = ''; stopRecording(); $('me-recording').classList.add('hidden'); });
  $('me-rec-allow').addEventListener('click', () => api('POST', '/api/permissions/input-monitoring').catch(() => {}));

  $('me-reset').addEventListener('click', () => { ed = JSON.parse(edSaved); renderEditor(); });
  $('me-save').addEventListener('click', async () => {
    $('me-error').textContent = '';
    const body = {
      name: ed.name.trim(), kind: ed.kind, steps: ed.steps,
      hold_steps: ed.kind === 'sequence' ? ed.hold_steps : [], release_steps: ed.kind === 'sequence' ? ed.release_steps : [],
      standard_delay_ms: ed.standard_delay_ms, color: ed.color,
    };
    $('me-save').disabled = true;
    try {
      const saved = await api(ed.id ? 'PUT' : 'POST', ed.id ? `/api/macros/${ed.id}` : '/api/macros', body);
      const assign = edAssign;
      closeEditor(true);
      if (assign) await saveKey({ mode: 'macro', macro_id: saved.id }, assign);
      else { await load(); status(`Saved “${saved.name}”.`, 'ok'); }
    } catch (err) {
      $('me-error').textContent = err.message;
    } finally {
      $('me-save').disabled = false;
    }
  });
  $('me-delete').addEventListener('click', async () => {
    if (!(await ask(`Delete the macro “${ed.name}”? Keys that use it are turned off.`, 'Delete', true))) return;
    try {
      await api('DELETE', `/api/macros/${ed.id}`);
      closeEditor(true);
      await load();
      status('Macro deleted.', 'ok');
    } catch (err) {
      $('me-error').textContent = err.message;
    }
  });

  /* ---- the pad: connection and presses ----------------------------------- */
  // The app follows the pad over USB: whether it is plugged in, and each
  // keystroke it sends, traced back to the slots that send it.
  let padNext = null;

  function flash(slot) {
    document.querySelectorAll(`[data-slot="${slot}"]`).forEach((el) => {
      el.classList.remove('pressed');
      void el.offsetWidth; // restart the animation on repeated presses
      el.classList.add('pressed');
    });
  }

  function renderPadStatus(pad) {
    const status = $('pad-status');
    status.classList.toggle('connected', pad.connected);
    $('device').classList.toggle('unplugged', !pad.connected);
    const hex = (n) => n.toString(16).padStart(4, '0');
    let text = 'Not connected: plug the pad in.';
    if (pad.connected) {
      text = `Connected: ${pad.product || 'vibe-coder-keyboard'} (${hex(pad.vendor_id)}:${hex(pad.product_id)})`;
      if (!pad.input_allowed) text += ' · allow Input Monitoring to see which key you press';
    }
    $('pad-status-text').textContent = text;
  }

  async function pollPad() {
    try {
      const pad = await api('GET', `/api/pad?since=${padNext ?? 0}`);
      renderPadStatus(pad);
      if (padNext !== null) {
        pad.presses.forEach((p) => {
          // Open the pressed key unless that would throw away edits.
          if (p.slots.length && !editorOpen() && !p.slots.includes(selected)) selectSlot(p.slots[0]);
          p.slots.forEach(flash);
        });
      }
      padNext = pad.next; // the first poll only learns where the list is
    } catch { /* the app is restarting; try again */ }
    setTimeout(pollPad, 300);
  }

  /* ---- macOS permissions ------------------------------------------------- */
  async function pollPermissions() {
    try {
      const { permissions } = await api('GET', '/api/permissions');
      const missing = permissions.filter((p) => !p.granted);
      $('perm-banner').classList.toggle('hidden', !missing.length);
      $('perm-list').innerHTML = missing.map((p) => `<div class="perm">
        <span><strong>${esc(p.label)}</strong> <span class="muted">for ${esc(p.why)}</span></span>
        <button type="button" class="btn btn-primary btn-sm" data-allow="${esc(p.name)}">Allow</button>
      </div>`).join('');
    } catch { /* try again */ }
    setTimeout(pollPermissions, 2000);
  }

  $('perm-list').addEventListener('click', async (e) => {
    const btn = e.target.closest('[data-allow]');
    if (!btn) return;
    btn.disabled = true;
    try { await api('POST', `/api/permissions/${btn.dataset.allow}`); } catch (err) { tell(err.message); }
  });

  /* ---- menu drawer -------------------------------------------------------- */
  // A modal <dialog>: Esc closes it natively, a click on the dimmed backdrop
  // (the dialog itself, outside its content) closes it too.
  const drawer = $('drawer');
  $('menu').addEventListener('click', () => {
    drawer.showModal();
    $('menu').setAttribute('aria-expanded', 'true');
  });
  $('drawer-close').addEventListener('click', () => drawer.close());
  drawer.addEventListener('click', (e) => { if (e.target === drawer) drawer.close(); });
  drawer.addEventListener('close', () => $('menu').setAttribute('aria-expanded', 'false'));

  /* ---- logs and debug mode (in the drawer) ------------------------------- */
  // While the drawer is open the log tail refreshes every 2 s; it stays at
  // the bottom unless you scrolled up to read.
  let logTimer = null;
  async function loadLogs() {
    try {
      const out = await api('GET', '/api/logs?lines=300');
      const view = $('log-view');
      const atBottom = view.scrollHeight - view.scrollTop - view.clientHeight < 24;
      view.textContent = out.lines.join('\n') || 'Nothing logged yet.';
      if (atBottom) view.scrollTop = view.scrollHeight;
      $('debug').checked = out.debug;
      $('log-path').textContent = out.path;
    } catch { /* the app is restarting */ }
    if (drawer.open) logTimer = setTimeout(loadLogs, 2000);
  }
  $('menu').addEventListener('click', () => { clearTimeout(logTimer); loadLogs().then(() => { $('log-view').scrollTop = $('log-view').scrollHeight; }); });
  drawer.addEventListener('close', () => clearTimeout(logTimer));
  $('debug').addEventListener('change', async () => {
    try { await api('PUT', '/api/debug', { on: $('debug').checked }); } catch (err) { tell(err.message); }
    clearTimeout(logTimer);
    loadLogs();
  });
  $('log-copy').addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText($('log-view').textContent);
      $('log-copy').textContent = 'Copied';
    } catch {
      getSelection().selectAllChildren($('log-view'));
      $('log-copy').textContent = 'Selected: press cmd+C';
    }
    setTimeout(() => { $('log-copy').textContent = 'Copy'; }, 1500);
  });
  $('log-open').addEventListener('click', () => api('POST', '/api/logs/open').catch((err) => tell(err.message)));

  /* ---- restart (desktop app only) ---------------------------------------- */
  api('GET', '/api/app').then((info) => {
    if (!info.can_restart) return;
    document.querySelector('.restart-section').classList.remove('hidden');
    document.querySelectorAll('.restart').forEach((btn) => {
      btn.classList.remove('hidden');
      btn.addEventListener('click', async () => {
        btn.disabled = true;
        btn.textContent = 'Restarting…';
        try { await api('POST', '/api/app/restart'); } catch (err) { tell(err.message); btn.disabled = false; }
      });
    });
  }).catch(() => {});

  /* ---- updates (the drawer's Updates section and a banner) --------------- */
  // The app looks for newer releases by itself; the page shows where it is
  // and offers the next step. Nothing is installed without a click.
  let upd = null;        // the last /api/update answer
  let updTimer = null;
  let updDismissed = ''; // the version whose banner was closed with Later
  const vlabel = (v) => (v ? (v.startsWith('v') ? v : 'v' + v) : '');
  const busyStates = ['checking', 'downloading', 'installing'];
  function updateWords(st) {
    const latest = st.latest ? vlabel(st.latest.version) : '';
    switch (st.state) {
      case 'unavailable': return ['This build cannot look for updates.', ''];
      case 'checking': return ['Checking…', ''];
      case 'up-to-date': return [`Up to date${st.checked_at ? ' (checked ' + fmt(st.checked_at) + ')' : ''}.`, ''];
      case 'available': return [`${latest} is available.`, st.target ? `Download ${latest}` : ''];
      case 'downloading': return [`Downloading ${latest}… ${Math.round((st.progress || 0) * 100)}%`, ''];
      case 'ready': return [`${latest} is downloaded and verified.`, 'Restart to update'];
      case 'installing': return [`Installing ${latest}…`, ''];
      case 'installed': return [`${latest} is installed.`, 'Restart now'];
      case 'error': return [st.error || 'Something went wrong.', st.latest && st.target ? 'Try again' : ''];
      default: return [st.checked_at ? `Checked ${fmt(st.checked_at)}.` : 'Not checked yet.', ''];
    }
  }
  function renderUpdate() {
    if (!upd) return;
    const st = upd.status;
    const [text, action] = updateWords(st);
    $('update-version').textContent = `Version ${st.current || 'dev'}${st.release === false && st.state !== 'unavailable' ? ' (development build)' : ''}`;
    $('update-text').textContent = text;
    $('update-action').textContent = action;
    $('update-action').classList.toggle('hidden', !action);
    $('update-check').disabled = busyStates.includes(st.state) || st.state === 'unavailable';
    $('update-auto').checked = !!st.auto;
    $('update-auto').disabled = st.state === 'unavailable';
    const note = st.state === 'unavailable' ? ''
      : !st.target ? 'Updates install only in the installed app (Vibe Coder Deck.app or vibe-coder-deck.exe).'
        : st.release === false ? 'Development builds are not downloaded automatically; use the buttons.' : '';
    $('update-note').textContent = note;
    $('update-note').classList.toggle('hidden', !note);
    const latest = st.latest ? vlabel(st.latest.version) : '';
    const show = ['available', 'ready', 'installed'].includes(st.state) && !!st.target && latest !== updDismissed;
    $('update-banner').classList.toggle('hidden', !show);
    if (show) {
      $('update-banner-text').textContent = st.state === 'available' ? `Vibe Coder Deck ${latest} is available.`
        : st.state === 'ready' ? `Vibe Coder Deck ${latest} is ready to install.` : `Vibe Coder Deck ${latest} is installed.`;
      $('update-banner-action').textContent = action;
    }
  }
  async function loadUpdate() {
    try { upd = await api('GET', '/api/update'); renderUpdate(); } catch { /* the app is restarting */ }
    const st = upd && upd.status;
    clearTimeout(updTimer);
    updTimer = setTimeout(loadUpdate, st && busyStates.includes(st.state) ? 500 : 60000);
  }
  async function updateAct() {
    const st = upd && upd.status;
    if (!st) return;
    try {
      if (st.state === 'ready') {
        const ok = await dialog({ title: 'Install the update', message: `Install Vibe Coder Deck ${vlabel(st.latest.version)} and restart the app? Nothing you set up changes.`, ok: 'Install and restart' });
        if (!ok) return;
        await api('POST', '/api/update/install');
      } else if (st.state === 'installed') {
        await api('POST', '/api/app/restart');
      } else if (st.latest && (st.state === 'available' || st.state === 'error')) {
        await api('POST', '/api/update/download');
      } else {
        await api('POST', '/api/update/check');
      }
    } catch (err) { tell(err.message); }
    loadUpdate();
  }
  $('update-check').addEventListener('click', async () => {
    $('update-check').disabled = true;
    $('update-text').textContent = 'Checking…';
    try { await api('POST', '/api/update/check'); } catch (err) { tell(err.message); }
    loadUpdate();
  });
  $('update-action').addEventListener('click', updateAct);
  $('update-banner-action').addEventListener('click', updateAct);
  $('update-banner-close').addEventListener('click', () => {
    updDismissed = upd && upd.status.latest ? vlabel(upd.status.latest.version) : '';
    renderUpdate();
  });
  $('update-auto').addEventListener('change', async () => {
    try { await api('PUT', '/api/update/auto', { on: $('update-auto').checked }); } catch (err) { tell(err.message); }
    loadUpdate();
  });

  // Patch notes: a small markdown renderer for release notes (headings,
  // lists, bold, code, links); everything else is text.
  function md(src) {
    const inline = (s) => esc(s)
      .replace(/`([^`]+)`/g, '<code>$1</code>')
      .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
      .replace(/\[([^\]]+)\]\((https?:[^)\s]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>');
    const out = [];
    let list = null;
    const closeList = () => { if (list) { out.push(`</${list}>`); list = null; } };
    for (const raw of String(src || '').split('\n')) {
      const line = raw.trimEnd();
      let m;
      if ((m = line.match(/^(#{1,6})\s+(.*)$/))) { closeList(); const n = Math.min(m[1].length + 2, 6); out.push(`<h${n}>${inline(m[2])}</h${n}>`); }
      else if ((m = line.match(/^\s*[-*]\s+(.*)$/))) { if (list !== 'ul') { closeList(); list = 'ul'; out.push('<ul>'); } out.push(`<li>${inline(m[1])}</li>`); }
      else if ((m = line.match(/^\s*\d+[.)]\s+(.*)$/))) { if (list !== 'ol') { closeList(); list = 'ol'; out.push('<ol>'); } out.push(`<li>${inline(m[1])}</li>`); }
      else if (!line.trim()) closeList();
      else { closeList(); out.push(`<p>${inline(line)}</p>`); }
    }
    closeList();
    return out.join('');
  }
  let releasesLoaded = false;
  async function loadReleases(force) {
    if (releasesLoaded && !force) return;
    $('release-list').innerHTML = '<p class="muted">Loading…</p>';
    try {
      const out = await api('GET', '/api/releases');
      releasesLoaded = true;
      const cur = vlabel((upd && upd.status.current) || out.current || '');
      const items = out.releases.map((r, i) => {
        const v = vlabel(r.version);
        const mine = cur === v || cur.startsWith(v + '-');
        const date = r.published_at && !r.published_at.startsWith('0001') ? r.published_at.slice(0, 10) : ''; // the day as published, no time zone shift
        return `<article class="release${mine ? ' current' : ''}${i > 0 ? ' more' : ''}">` +
          `<h3>${esc(v)}${mine ? ' <span class="tag">this version</span>' : ''} <small class="muted">${esc(date)}</small></h3>` +
          (md(r.notes) || '<p class="muted">No notes.</p>') +
          (r.url ? `<p><a href="${esc(r.url)}" target="_blank" rel="noopener">Release page</a></p>` : '') +
          '</article>';
      });
      // Only the newest release shows at first; View more unfolds the rest (the
      // drawer is the one thing that scrolls).
      $('release-list').classList.remove('expanded');
      $('release-list').innerHTML = (items.join('') || '<p class="muted">No releases yet.</p>') +
        (items.length > 1 ? `<button type="button" id="release-more" class="btn btn-outline btn-sm release-more">View ${items.length - 1} more release${items.length > 2 ? 's' : ''}</button>` : '') +
        (out.source === 'changelog' ? '<p class="muted">From the changelog built into the app (GitHub was not reached).</p>' : '');
      const more = $('release-more');
      if (more) more.addEventListener('click', () => { $('release-list').classList.add('expanded'); more.remove(); });
    } catch (err) { $('release-list').innerHTML = `<p class="muted">${esc(err.message)}</p>`; }
  }
  $('whats-new').addEventListener('toggle', () => { if ($('whats-new').open) loadReleases(); });
  $('update-banner-notes').addEventListener('click', () => {
    $('menu').click();
    $('whats-new').open = true;
    loadReleases();
    $('whats-new').scrollIntoView({ block: 'nearest' });
  });
  // The first start of a new version opens its notes once.
  loadUpdate().then(() => {
    if (!upd || !upd.whats_new) return;
    $('menu').click();
    $('whats-new').open = true;
    loadReleases(true);
    api('POST', '/api/update/seen').catch(() => {});
  });

  /* ---- MCP install ------------------------------------------------------- */
  async function loadMCP() {
    let info;
    try { info = await api('GET', '/api/mcp'); } catch { return; }
    $('mcp').classList.remove('hidden');
    $('mcp-buttons').innerHTML = info.clients.map((c) => `<button type="button" class="btn btn-outline btn-sm" data-client="${esc(c.id)}" title="${esc(c.note)}">Install in ${esc(c.name)}</button>`).join('');
    $('mcp-snippet').textContent =
      `HTTP (while the app runs): ${info.url}\n` +
      `stdio: ${info.executable} mcp\n\n` +
      `Claude Code: claude mcp add -s user vibe-coder-deck -- "${info.executable}" mcp\n\n` +
      `JSON (Claude Desktop, Cursor, Windsurf):\n` +
      JSON.stringify({ mcpServers: { 'vibe-coder-deck': { command: info.executable, args: ['mcp'] } } }, null, 2);
    $('mcp-buttons').querySelectorAll('[data-client]').forEach((btn) => btn.addEventListener('click', async () => {
      btn.disabled = true;
      try {
        const out = await api('POST', '/api/mcp/install', { client: btn.dataset.client });
        $('mcp-result').textContent = out.message;
      } catch (err) {
        $('mcp-result').textContent = err.message;
      } finally {
        $('mcp-result').classList.remove('hidden');
        btn.disabled = false;
      }
    }));
  }
  loadMCP();
  pollPermissions();

  loadProfiles().then(load).then(pollPad).catch((err) => status(err.message, 'error'));
})();

