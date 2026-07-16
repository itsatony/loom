// Tier-H audit app. No build step, no framework. Keyboard-first.
const TYPE_OPTIONS = [
  {key: 's', label: 'statement', caption: 'plain fact / policy'},
  {key: 'q', label: 'quote', caption: '"…" attributed speech'},
  {key: 'x', label: 'sarcasm', caption: 'ironic — not really asserted'},
  {key: 'd', label: 'declaration', caption: 'announces a story/view/exercise'},
  {key: 'c', label: 'confirmation', caption: 'verifies an earlier claim'},
];
const ADVANCE_DELAY = 400;

let items = [];
let answers = new Map(); // n -> {context, type, flagged}
let idx = 0;
let advanceTimer = null;
let otherOpen = false;

const $ = (sel) => document.querySelector(sel);
const main = $('#main');

async function boot() {
  const [itemsRes, progRes] = await Promise.all([
    fetch('/api/items'), fetch('/api/progress'),
  ]);
  items = await itemsRes.json();
  const prog = await progRes.json();
  prog.forEach(a => answers.set(a.n, a));
  idx = firstUnanswered();
  render();
  document.addEventListener('keydown', onKey);
  $('#prevBtn').onclick = () => goto(idx - 1);
  $('#nextBtn').onclick = () => goto(idx + 1);
  $('#finishBtn').onclick = finish;
  $('#helpBtn').onclick = openHelp;
  $('#helpCloseBtn').onclick = closeHelp;
  $('#helpOverlay').onclick = (e) => { if (e.target.id === 'helpOverlay') closeHelp(); };
  if (!localStorage.getItem('tierh_help_seen')) openHelp();
}

function openHelp() { $('#helpOverlay').classList.add('show'); }
function closeHelp() {
  $('#helpOverlay').classList.remove('show');
  localStorage.setItem('tierh_help_seen', '1');
}

function firstUnanswered() {
  for (let i = 0; i < items.length; i++) {
    if (!isDone(items[i].n)) return i;
  }
  return 0;
}

function isDone(n) {
  const a = answers.get(n);
  return !!a && (a.flagged || (a.context && a.type));
}

function goto(i) {
  cancelAdvance();
  otherOpen = false;
  idx = Math.max(0, Math.min(items.length - 1, i));
  render();
}

function cancelAdvance() {
  if (advanceTimer) { clearTimeout(advanceTimer); advanceTimer = null; }
}

async function saveCurrent() {
  const it = items[idx];
  const a = answers.get(it.n) || {n: it.n, context: '', type: '', flagged: false};
  answers.set(it.n, a);
  setStatus('saving…');
  try {
    await fetch('/api/answer', {
      method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(a),
    });
    setStatus('saved');
  } catch (e) {
    setStatus('save failed — check server');
  }
}

function setAnswer(patch) {
  const it = items[idx];
  const cur = answers.get(it.n) || {n: it.n, context: '', type: '', flagged: false};
  const next = {...cur, ...patch};
  answers.set(it.n, next);
  render(); // re-render to reflect selection immediately
  saveCurrent();
  cancelAdvance();
  if (next.flagged || (next.context && next.type)) {
    advanceTimer = setTimeout(() => {
      advanceTimer = null;
      if (idx < items.length - 1) goto(idx + 1);
    }, ADVANCE_DELAY);
  }
}

function setStatus(s) {
  const el = $('#status');
  if (el) el.textContent = s;
}

function render() {
  const it = items[idx];
  const a = answers.get(it.n) || {context: '', type: '', flagged: false};
  const doneCount = items.filter(x => isDone(x.n)).length;
  $('#count').textContent = `${idx + 1} / ${items.length}  (${doneCount} done)`;
  $('#progBar').style.width = `${(doneCount / items.length) * 100}%`;
  setStatus(isDone(it.n) ? 'saved' : 'unanswered');

  const decls = it.decls.length
    ? `<details class="decls"><summary>Announcements seen earlier in the log (${it.decls.length})</summary>
        ${it.decls.map(d => `<blockquote>${esc(d)}</blockquote>`).join('')}
       </details>` : '';

  const lines = it.episode_lines.map((t, i) => {
    const n = i + 1;
    const isTarget = n === it.target_line;
    const cls = isTarget ? 'line target' : 'line';
    const marker = isTarget ? '▶ ' : '';
    return `<div class="${cls}"><span class="num">${n}.</span>${marker}${esc(t)}</div>`;
  }).join('');

  const ctxBtns = it.context_options.map((opt, i) => {
    const keyLabel = i < 9 ? String(i + 1) : '';
    const sel = a.context === opt ? 'selected' : '';
    return `<button class="opt ${sel}" data-ctx="${escAttr(opt)}">
      ${keyLabel ? `<span class="key">${keyLabel}</span>` : ''}${esc(opt)}</button>`;
  }).join('');
  const otherIdx = it.context_options.length + 1;
  const otherSel = a.context && !it.context_options.includes(a.context) ? 'selected' : '';
  const otherBtn = `<button class="opt other ${otherSel}" id="otherBtn">
      ${otherIdx <= 9 ? `<span class="key">${otherIdx}</span>` : ''}other…</button>`;

  const typeBtns = TYPE_OPTIONS.map(t => {
    const sel = a.type === t.label ? 'selected' : '';
    return `<button class="opt ${sel}" data-type="${t.label}">
      <span class="key">${t.key}</span>
      <span>${t.label}<span class="type-caption">${t.caption}</span></span>
      </button>`;
  }).join('');

  const flagSel = a.flagged ? 'selected' : '';
  const cheatOpen = localStorage.getItem('tierh_cheat_collapsed') !== '1';

  main.innerHTML = `
    <div class="item-head">
      <span>seed ${it.seed} · ${it.ep} · day ${it.day}</span>
      <span>item ${it.n}</span>
    </div>
    <details class="cheatsheet" id="cheatsheet" ${cheatOpen ? 'open' : ''}>
      <summary>Rules to remember</summary>
      <ul>
        <li>A <b>declaration</b>'s context is the thing being declared
        (story/view/exercise), <b>never actual</b> — even if it reads
        like a desk policy.</li>
        <li>A <b>confirmation</b> is <b>always actual</b>, even about a
        party's earlier projection — the desk is now vouching for it.</li>
        <li><b>sarcasm</b> is always <code>actual | sarcasm</code>.</li>
        <li>Routine feed sources (registry_A, field_report,
        customer_disclosure, audit_note, partner_feed) are
        <b>actual</b>, not a view.</li>
      </ul>
    </details>
    ${decls}
    <p class="hint-line">Decide about the highlighted line below (↓).</p>
    <div class="episode">${lines}</div>

    <div class="group-label">context — whose reality is this line in?</div>
    <p class="hint-line">actual = the log's own claim · story/view/exercise = attributed elsewhere (named below, from what's been introduced so far)</p>
    <div class="btn-row">${ctxBtns}${otherBtn}</div>
    <div class="other-input ${otherOpen ? 'show' : ''}" id="otherWrap">
      <input id="otherInput" placeholder="type context, e.g. view partner_04" value="${a.context && !it.context_options.includes(a.context) ? escAttr(a.context) : ''}">
    </div>

    <div class="group-label">type — what kind of line is it?</div>
    <div class="btn-row">${typeBtns}</div>

    <div class="group-label">&nbsp;</div>
    <div class="btn-row">
      <button class="opt flag ${flagSel}" id="flagBtn"><span class="key">0</span>flag / unsure</button>
    </div>
  `;

  main.querySelectorAll('[data-ctx]').forEach(b => {
    b.onclick = () => { otherOpen = false; setAnswer({context: b.dataset.ctx, flagged: false}); };
  });
  main.querySelectorAll('[data-type]').forEach(b => {
    b.onclick = () => setAnswer({type: b.dataset.type, flagged: false});
  });
  $('#flagBtn').onclick = () => setAnswer({flagged: true, context: '', type: ''});
  $('#cheatsheet').ontoggle = (e) => {
    localStorage.setItem('tierh_cheat_collapsed', e.target.open ? '0' : '1');
  };
  $('#otherBtn').onclick = () => { otherOpen = true; render(); $('#otherInput') && $('#otherInput').focus(); };
  const otherInput = $('#otherInput');
  if (otherInput) {
    otherInput.onkeydown = (e) => {
      if (e.key === 'Enter') {
        e.preventDefault();
        otherOpen = false;
        setAnswer({context: otherInput.value.trim(), flagged: false});
      } else if (e.key === 'Escape') {
        otherOpen = false; render();
      }
      e.stopPropagation();
    };
  }
}

function onKey(e) {
  if (document.activeElement && document.activeElement.id === 'otherInput') return;
  if (e.key >= '1' && e.key <= '9') {
    const i = parseInt(e.key, 10) - 1;
    const it = items[idx];
    if (i < it.context_options.length) {
      otherOpen = false;
      setAnswer({context: it.context_options[i], flagged: false});
    } else if (i === it.context_options.length) {
      otherOpen = true; render(); const el = $('#otherInput'); if (el) el.focus();
    }
    return;
  }
  if (e.key === '0') { setAnswer({flagged: true, context: '', type: ''}); return; }
  const t = TYPE_OPTIONS.find(t => t.key === e.key.toLowerCase());
  if (t) { setAnswer({type: t.label, flagged: false}); return; }
  if (e.key === 'ArrowRight') { goto(idx + 1); return; }
  if (e.key === 'ArrowLeft' || e.key === 'Backspace') { e.preventDefault(); goto(idx - 1); return; }
  if (e.key === 'Enter') { goto(idx + 1); return; }
}

async function finish() {
  if (!confirm('This reveals the ground-truth key and scores your current answers. Continue?')) return;
  cancelAdvance();
  const r = await fetch('/api/finish', {method: 'POST'});
  const j = await r.json();
  if (!j.ok) { alert(j.error || 'scoring failed'); return; }
  renderScore(j.score);
}

function renderScore(s) {
  const misses = s.rows.filter(r => !r.exact);
  main.innerHTML = `
    <div class="score-panel">
      <h2>Score</h2>
      <div class="stat-row">
        <div><div class="big-stat">${(s.exact_rate * 100).toFixed(1)}%</div><div class="muted">exact (context+type)</div></div>
        <div><div class="big-stat">${(s.context_accuracy * 100).toFixed(1)}%</div><div class="muted">context only</div></div>
        <div><div class="big-stat">${(s.type_accuracy * 100).toFixed(1)}%</div><div class="muted">type only</div></div>
      </div>
      <p class="muted">${s.answered}/${s.total} answered · ${s.flagged} flagged as unsure · ${misses.length} misses below</p>
      <table>
        <tr><th>item</th><th>seed/ep</th><th>expected</th><th>given</th></tr>
        ${misses.map(r => `<tr class="miss">
          <td>${r.item}</td><td>${r.seed}/${r.ep} L${r.line}</td>
          <td>${esc(r.expected_context)} / ${esc(r.expected_type)}</td>
          <td>${r.flagged ? '(flagged)' : esc(r.given_context) + ' / ' + esc(r.given_type)}</td>
        </tr>`).join('')}
      </table>
    </div>
    <p><button class="opt" id="backBtn">back to items</button></p>
  `;
  $('#backBtn').onclick = () => render();
}

function esc(s) { return (s || '').replace(/[&<>"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c])); }
function escAttr(s) { return esc(s).replace(/'/g, '&#39;'); }

boot();
