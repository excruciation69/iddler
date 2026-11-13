'use strict';
// Account page: section navigation, sign-in panel and the game art helpers.

// ================================================================ account

const SECTIONS = [
  ['nav.g.farm', [['hours', 'clock'], ['cards', 'cards'], ['ach', 'trophy']]],
  ['nav.g.tools', [['inventory', 'box'], ['keys', 'key'], ['free', 'gift'], ['profile', 'user'], ['trade', 'swap'], ['stats', 'chart']]],
  ['nav.g.account', [['acct', 'id']]],
];
const SEC_ICON = Object.fromEntries(SECTIONS.flatMap(g => g[1]));
const secLabel = s => t(s === 'acct' ? 'mod.account' : 'mod.' + s);
const OFFLINE_OK = new Set(['acct', 'stats']);

function AcctPage(id, sec) {
  const info = h('span.row', { style: 'gap:8px;color:var(--dim);font-size:12px' });
  const body = h('div.mbody');
  const el = h('div.page', null, h('div.main', null, body));
  let cur = null, mode = '';
  function mount() {
    const b = S.bots[id];
    if (!b) return;
    const want = b.status === 'online' || OFFLINE_OK.has(sec) ? 'sec:' + sec : 'session';
    if (want === mode) return;
    mode = want;
    cur?.destroy?.();
    fill(info);
    S.onSearch = null;
    cur = want === 'session' ? SessionPanel(id) : SECTION_VIEWS[sec](id, info);
    fill(body, cur.el);
    body.style.animation = 'none';
    void body.offsetWidth;
    body.style.animation = '';
    body.scrollTop = 0;
  }
  function update() {
    mount();
    cur?.update?.(S.bots[id]);
  }
  return {
    el, id, info,
    get sec() { return sec; },
    setSection(s) { sec = s; mode = ''; update(); },
    update,
    destroy() { cur?.destroy?.(); },
  };
}
const anyOn = (o, keys) => !!o && keys.some(k => o[k]);

// Sign-in, Steam Guard and connecting states share one panel.
function SessionPanel(id) {
  const el = h('div.session.raised');
  const body = h('div.body');
  el.append(body);
  let key = '', err = null, pw = null;
  function errorText(b) {
    const since = S.attempt[id];
    if (!since || b.status !== 'offline') return '';
    const l = [...S.logs].reverse().find(l => l.account === id && l.kind === 'err' && l.at >= since);
    return l ? t(l.key, ...(l.args || []).map(tr)) : '';
  }
  function build(b) {
    const cancel = btn(t('btn.cancel'), () => act('logout', { id, forget: false }).catch(() => {}), 'ghost');
    err = h('div.error');
    if (b.status === 'connecting') {
      fill(body, h('h3', { text: t('conn.title') }), h('div.progress', null, h('i')), hint(t('conn.text')), h('div.row', null, h('span.grow'), cancel));
      return;
    }
    if (b.status === 'guard') {
      const kids = [h('h3', { text: t('st.guard') })];
      if (b.guardKind === 'confirm') {
        kids.push(hint(t('guard.confirm')), h('div.progress', null, h('i')));
      } else {
        const code = field({ class: 'code', maxlength: 5, autocomplete: 'one-time-code', spellcheck: 'false', autocapitalize: 'characters' });
        const send = () => { if (code.value.trim()) act('guard', { id, code: code.value.trim() }).catch(() => {}); };
        code.addEventListener('keydown', e => { if (e.key === 'Enter') send(); });
        code.addEventListener('input', () => { if (code.value.trim().length === 5) send(); });
        kids.push(hint(b.guardKind === 'email' ? t('guard.email', b.guardDomain || '') : t('guard.code')),
          h('div.row', { style: 'justify-content:center;padding:6px 0' }, code));
        if (b.guardWrong) kids.push(h('div.error', { text: t('guard.wrong') }));
        kids.push(h('div.row', null, h('span.grow'), cancel, btn(t('guard.submit'), send, 'primary')));
        setTimeout(() => code.focus(), 40);
        fill(body, ...kids);
        return;
      }
      kids.push(h('div.row', null, h('span.grow'), cancel));
      fill(body, ...kids);
      return;
    }
    // offline
    pw = field({ type: 'password', placeholder: t('login.password'), autocomplete: 'current-password' });
    const rem = check(t('login.remember'), b.cfg.remember, () => {});
    const submit = () => {
      if (!pw.value) { err.textContent = t('err.loginEmpty'); return; }
      S.attempt[id] = Math.floor(Date.now() / 1000) - 1;
      err.textContent = '';
      act('login', { id, password: pw.value, remember: rem.input.checked }).catch(() => {});
      pw.value = '';
    };
    pw.addEventListener('keydown', e => { if (e.key === 'Enter') submit(); });
    const kids = [h('h3', { text: t('login.signInAs', b.account) })];
    if (b.hasToken) {
      kids.push(hint(t('login.savedSession')),
        btn(t('login.useSaved'), () => { S.attempt[id] = Math.floor(Date.now() / 1000) - 1; act('loginSaved', { id }).catch(() => {}); }, 'primary'),
        h('div.hint', { style: 'margin-top:6px', text: t('login.orPassword') }));
    }
    kids.push(pw, rem, err, h('div.row', null, hint(t('login.hint')), btn(t('login.submit'), submit, b.hasToken ? '' : 'primary')));
    fill(body, ...kids);
    setTimeout(() => { if (!b.hasToken) pw.focus(); }, 40);
  }
  return {
    el,
    update(b) {
      const k = [b.status, b.guardKind, b.guardWrong, b.hasToken].join('|');
      if (k !== key) { key = k; build(b); }
      if (b.status === 'offline' && err) err.textContent = errorText(b) || err.textContent;
    },
  };
}
