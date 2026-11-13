'use strict';
// App-wide pages: settings and support.

// ================================================================ settings

function SettingsPage() {
  const st = () => S.hub.settings;
  const items = [];
  const sw = (key, label, invert) => {
    const el = toggle(label, invert ? !st()[key] : st()[key], v => patchSettings({ [key]: invert ? !v : v }));
    items.push(() => el.set(invert ? !st()[key] : st()[key]));
    return el;
  };
  const mac = S.hub.platform === 'mac';
  const tg = h('div.col');
  const lan = h('div.col');
  const langBox = h('div.row');
  const accentBox = h('div.swatches');
  const drawAccents = () => fill(accentBox, ACCENTS.map(c => h('span.swatch' + ((st().accent || ACCENTS[0]) === c ? '.on' : ''),
    { style: 'background:' + c, title: c, onclick: () => { patchSettings({ accent: c === ACCENTS[0] ? '' : c }); applyAccent(); drawAccents(); } })));
  drawAccents();
  const el = h('div.page', null, h('div.scroll', null,
    h('div.ptitle', null, h('h1', { text: t('nav.settings') }), h('span.sub', { text: t('set.subtitle') })),
    h('div.grid2', null,
      group(t('set.background'),
        sw('loginAtStartup', t('set.loginAtStartup')),
        sw('keepAwake', t('set.keepAwake')),
        sw('menuTime', mac ? t('set.menuTime') : t('set.trayTime')),
        mac ? sw('hideDock', t('set.dock'), true) : null,
        hint(mac ? t('set.bgHintMac') : t('set.bgHintWin'))),
      group('Telegram', tg),
      group(t('set.phone'), lan),
      group(t('set.look'), hint(t('set.accentHint')), accentBox),
      group(t('set.language'), langBox,
        h('div.label', { style: 'margin-top:8px', text: t('set.about') }),
        h('div.dim', { text: 'SteamIdler by excruciation · ' + t('set.version', S.hub.version) }),
        h('div.row', null, btn('GitHub', () => openURL(GITHUB), 'sm', 'star'))))));
  langBox.append(...[['ru', 'Русский'], ['en', 'English']].map(([l, n]) => {
    const r = makeToggle('radio', n, S.lang === l, on => on && setLang(l, true));
    r.input.name = 'lang';
    return r;
  }));
  let tgCode = '';
  const m1 = memo(), m2 = memo();
  function drawTG() {
    const T = st().telegram || {};
    if (!T.token) {
      const tok = field({ class: 'wide mono', placeholder: t('set.tgToken'), spellcheck: 'false' });
      const err = h('div.error');
      fill(tg, hint(t('set.tgHow')), h('div.row', null, tok, btn(t('set.tgConnect'), async () => {
        err.textContent = '';
        try { tgCode = await call('tgConnect', { token: tok.value }); drawTG(); }
        catch (e) { err.textContent = tr(e.message); }
      }, 'primary', 'send')), err);
    } else if (!T.chatID) {
      const cmd = '/start ' + (tgCode || '…');
      if (!tgCode) call('tgCode').then(c => { if (c) { tgCode = c; drawTG(); } }).catch(() => {});
      fill(tg, hint(t('set.tgSend', '@' + (T.bot || 'bot'))),
        h('div.row', null, h('code.addr.sunken.grow', { text: cmd }), btn('', () => copy(cmd), 'icon', 'copy')),
        h('div.row.wrap', null, btn(t('set.tgOpen'), () => openURL(`https://t.me/${T.bot}?start=${tgCode}`), 'sm primary', 'send'),
          btn(t('set.tgOff'), () => act('tgDisconnect').catch(() => {}), 'sm ghost')),
        hint(t('set.tgWait')));
    } else {
      const N = T.notify || {};
      fill(tg, h('div.row', null, h('span.led.ok'), h('span', { text: t('set.tgBound', '@' + T.bot, T.user || '') })),
        h('div.label', { text: t('set.notify') }),
        h('div.tgn', null, ['cards', 'goals', 'pause', 'keys', 'free', 'chat'].map(k =>
          check(t('tg.n.' + k), N[k], v => patchSettings({ telegram: { notify: { [k]: v } } })))),
        hint(t('set.tgSafe')),
        h('div.row', null, btn(t('set.tgOff'), () => act('tgDisconnect').catch(() => {}), 'sm ghost')));
    }
  }
  function drawLAN() {
    const url = S.hub.lanURL;
    fill(lan, sw('lan', t('set.lan')),
      url ? h('div.row', { style: 'align-items:flex-start;gap:14px' },
        h('div.qr.small', null, qrSVG(url, { badge: false })),
        h('div.col', { style: 'min-width:0' }, hint(t('set.lanHint')),
          h('div.row', null, btn(t('btn.copy'), () => copy(url), 'sm', 'copy'))))
        : hint(st().lan ? t('stats.loading') : t('set.lanOff')));
  }
  return {
    el,
    update() {
      for (const f of items) f();
      const T = st().telegram || {};
      if (!tg.contains(document.activeElement)) m1([!!T.token, T.chatID, T.bot, T.user, T.notify, tgCode, S.lang], drawTG);
      m2([S.hub.lanURL, st().lan, S.lang], drawLAN);
    },
  };
}

// ================================================================ support

const SOCIALS = () => [
  { url: STEAM_GROUP, icon: 'users', cls: '', title: t('soc.group'), sub: 'steamcommunity.com/groups/iddle2003' },
  { url: STEAM_PROFILE, icon: 'user', cls: 'pf', title: t('soc.profile'), sub: 'excruciation' },
  { url: TELEGRAM, icon: 'send', cls: 'tg', title: 'Telegram', sub: '@excruciation69' },
];

// Support: wallet cards. The USDT card turns over to show its QR code.
function SupportPage() {
  const stop = fn => e => { e.stopPropagation(); fn(); };
  const usdt = h('div.flip', { onclick: () => usdt.classList.toggle('on') }, h('div.inner', null,
    h('div.face.card-usdt', null, h('span.shine'),
      h('div.top-row', null, h('span.coin', { text: '₮' }), h('div', null, h('div.nm', { text: 'Tether USDT' }), h('div.net', { text: 'TRON · TRC20' })), h('span.chip-ic')),
      h('div.addr', null, TRC20.match(/.{1,4}/g).map(g => h('span', { text: g }))),
      h('div.foot-row', null, h('span.flip-hint', null, ic('refresh'), t('sup.flip')), btn(t('sup.copyAddr'), stop(() => copy(TRC20)), 'sm primary', 'copy'))),
    h('div.face.back', null, h('div.qr', null, qrSVG(TRC20)), h('div.cap', { text: t('sup.scan') }))));
  const bnb = h('div.flip', null, h('div.inner', null,
    h('div.face.card-bnb', null, h('span.shine'),
      h('div.top-row', null, h('span.coin', { text: 'B' }), h('div', null, h('div.nm', { text: 'Binance Pay' }), h('div.net', { text: 'Pay ID' })), h('span.chip-ic')),
      h('div.big-id', { text: BINANCE_ID.replace(/(\d{3})(?=\d)/g, '$1 ') }),
      h('div.foot-row', null, h('span', { text: t('sup.binance') }), btn(t('btn.copy'), stop(() => copy(BINANCE_ID)), 'sm', 'copy')))));
  const el = h('div.page', null, h('div.scroll', null, h('div.support', null,
    h('div.intro', null, h('h1', { text: t('sup.title') }), h('p', { text: t('sup.text') })),
    h('div.wallets', null, usdt, bnb),
    h('p.hint.warn', null, ic('info'), ' ', t('sup.network')),
    h('h3.sect', { text: t('soc.title') }),
    h('div.socials', null, SOCIALS().map(x => h('a.social.raised', { href: x.url, onclick: e => { e.preventDefault(); openURL(x.url); } },
      h('span.b', null, ic(x.icon)), h('div', { style: 'min-width:0' }, h('b', { text: x.title }), h('small.ellipsis', { style: 'display:block', text: x.sub }))))),
    h('div.row.wrap', { style: 'padding:4px 2px' }, h('span.dim', { text: t('sup.star') }), h('span.grow'), btn('GitHub', () => openURL(GITHUB), 'sm', 'star')))));
  return { el, update() {} };
}
