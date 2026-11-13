'use strict';
// Keys, freebies, profile and status, trades.

// ---------------------------------------------------------------- keys

function KeysSec(id, info) {
  const B = binder(id);
  let q = '';
  const total = h('span.dim');
  info.append(total);
  const one = field({ class: 'wide mono', placeholder: 'XXXXX-XXXXX-XXXXX', spellcheck: 'false', autocapitalize: 'characters' });
  const pack = field({ area: true, class: 'wide', rows: 4, placeholder: t('keys.packHint') });
  const found = h('span.dim');
  const queue = h('div');
  const hist = h('div.col');
  const send = async (text, clear) => {
    const n = await act('keys', { id, text }).catch(() => null);
    if (n == null) return;
    toast(n ? t('keys.queued', n) : t('keys.noneNew'));
    if (n) clear();
  };
  one.addEventListener('keydown', e => { if (e.key === 'Enter') send(one.value, () => { one.value = ''; }); });
  const countKeys = s => (s.toUpperCase().match(/\b[A-Z0-9]{5}(?:-[A-Z0-9]{5}){2,4}\b/g) || []).length;
  pack.addEventListener('input', () => { found.textContent = t('keys.found', countKeys(pack.value)); });
  found.textContent = t('keys.found', 0);
  const el = h('div.col', { style: 'gap:14px' },
    h('div.split', null,
      group(t('keys.one'), h('div.row', null, one, btn(t('keys.activate'), () => send(one.value, () => { one.value = ''; }), 'primary')),
        B.switch('keys.clipboard', t('keys.clipboard'))),
      group(t('keys.pack'), pack, h('div.row', null, found, h('span.grow'), btn(t('keys.activateAll'), () => send(pack.value, () => { pack.value = ''; found.textContent = t('keys.found', 0); }))))),
    queue,
    h('div.toolbar', null, h('h3.sect', { text: t('keys.history') }), h('span.grow'),
      search(t('keys.search'), v => { q = v; m([]); draw(); }),
      btn(t('keys.copyCSV'), () => {
        const rows = (S.bots[id].keys || []).map(k => [fmtDate(k.at), k.game, k.key, t('keyres.' + k.result)].join(';'));
        copy(['date;game;key;result', ...rows].join('\n'));
      }, 'sm', 'copy')),
    hist);
  const m = memo();
  function draw() {
    const b = S.bots[id];
    const keys = (b.keys || []).filter(k => !q || (k.game || '').toLowerCase().includes(q) || k.key.toLowerCase().includes(q));
    fill(hist, table([
      { cls: 'c-d hide-sm', label: t('keys.date') }, { cls: 'c-k', label: t('keys.key') }, { cls: 'c-n', label: t('keys.game') }, { cls: 'c-h', label: t('keys.result') },
    ], keys.map(k => h('div.tr', null,
      h('div.c-d.mono.hide-sm', { text: fmtDate(k.at) }),
      h('div.c-k.mono', null, h('a', { title: t('keys.reveal'), text: k.key, onclick: async () => {
        const full = await act('revealKey', { id, code: k.key, at: k.at }).catch(() => '');
        if (full) copy(full);
      } })),
      h('div.c-n', null, h('span.ellipsis', { text: k.game || '' })),
      h('div.c-h', null, h('span', { class: k.result === 'ok' ? 'ok' : k.result === 'owned' ? 'dim' : 'err', text: t('keyres.' + k.result) })))), t('keys.empty')));
  }
  return {
    el,
    update(b) {
      B.sync();
      total.textContent = t('keys.total', (b.keys || []).filter(k => k.result === 'ok').length);
      const qn = (b.keyQueue || []).length;
      fill(queue, ...(qn ? [h('div.raised.row', { style: 'padding:10px 14px' }, h('span.led.idle'),
        h('span', { text: t('keys.queue') + ' ' + t('keys.left', qn) }),
        b.keyPause ? h('span.warn', { text: t('keys.paused', fmtTime(b.keyPause)) }) : null,
        h('span.grow'), h('span.mono.dim.ellipsis', { text: b.keyQueue.slice(0, 3).join('  ') }))] : []));
      m([b.keys?.length, b.keys?.[0]?.at, S.lang], draw);
    },
  };
}

// ---------------------------------------------------------------- freebies

function FreeSec(id) {
  const B = binder(id);
  const deals = h('div.col', { style: 'gap:4px' });
  const claimed = h('div.col');
  const el = h('div.col', { style: 'gap:14px' },
    h('div.grid2', null,
      group(t('free.catcher'), B.switch('free.promos', t('free.promos')), hint(t('free.catcherHint'))),
      group(t('free.sales'), B.switch('free.queue', t('free.queue')), B.switch('free.sticker', t('free.sticker')), hint(t('free.salesHint')))),
    group(t('free.wishlist'), B.switch('free.wishlistOn', t('free.wishOn')),
      h('div.row.wrap', null, h('span', { text: t('free.notifyFrom') }),
        B.select('free.wishlistPct', [10, 25, 50, 75, 90, 100].map(p => [p, p + '%'])), h('span.dim', { text: t('free.notifyWhere') })),
      deals),
    h('h3.sect', { text: t('free.claimed') }), claimed);
  const m1 = memo(), m2 = memo();
  return {
    el,
    update(b) {
      B.sync();
      m1([b.deals, S.lang], () => {
        const d = b.deals || [];
        fill(deals, ...(d.length ? d.map(x => h('div.tr.sunken', { style: 'padding:0 6px;border-radius:4px;cursor:pointer', onclick: () => openURL('https://store.steampowered.com/app/' + x.appid) },
          h('div.c-n', null, h('span.ellipsis', { text: x.name })), h('div', null, h('span.pill.y', { text: '-' + x.discount + '%' })),
          h('div.mono', { text: x.price }), h('div', null, ic('globe')))) : [hint(t('free.noDeals'))]));
      });
      m2([b.claimed, S.lang], () => {
        fill(claimed, table([
          { cls: 'c-d', label: t('keys.date') }, { cls: 'c-n', label: t('free.what') }, { cls: 'c-h', label: t('free.type') },
        ], (b.claimed || []).map(c => h('div.tr', null, h('div.c-d.mono', { text: fmtDate(c.at) }),
          h('div.c-n', null, h('span.ellipsis', { text: c.kind === 'sticker' ? t('free.stickerItem') : c.kind === 'queue' ? t('free.queueItem', c.name) : c.name })),
          h('div.c-h', null, h('span.chip', { text: t('free.kind.' + c.kind) })))), t('free.empty')));
      });
    },
  };
}

// ---------------------------------------------------------------- profile

function ProfileSec(id) {
  const B = binder(id);
  const preview = h('div.preview.sunken');
  let fq = '';
  const exCount = h('span.pill');
  const friendList = h('div.flist.sunken');
  const friendSearch = search(t('auto.search'), v => { fq = v; drawFriends(S.bots[id]); });
  const el = h('div.col', { style: 'gap:14px' },
    group(t('profile.persona'), h('div.row', null, B.select('persona', ['online', 'away', 'invisible', 'offline'].map(p => [p, t('persona.' + p)])))),
    h('div.grid2', null,
      group(t('profile.slides'), B.switch('profile.slideOn', t('profile.slideOn')),
        h('div.label', { text: t('profile.names') }), B.lines('profile.names', { rows: 4, placeholder: 'Serious Sam: The Third Encounter (beta)' }),
        h('div.row.wrap', null, h('span', { text: t('profile.every') }), B.num('profile.interval', 1, 1440), h('span', { text: t('u.min') }),
          h('span.grow'), B.check('profile.shuffle', t('profile.shuffle'))),
        h('div.label', { text: t('profile.friendsSee') }), preview, hint(t('profile.slideHint'))),
      group(t('profile.nick'), B.switch('profile.nickOn', t('profile.nickOn')),
        h('div.label', { text: t('profile.day') }), B.input('profile.nickDay', { class: 'wide' }),
        h('div.label', { text: t('profile.night') }), B.input('profile.nickNight', { class: 'wide' }),
        h('div.row.wrap', null, h('span', { text: t('profile.nightFrom') }), B.time('profile.nickFrom'), h('span', { text: t('profile.to') }), B.time('profile.nickTo')),
        hint(t('profile.nickHint')))),
    group(t('profile.auto'),
      hint(t('auto.what')),
      h('div.auto', null,
        h('div.col', null,
          B.switch('profile.autoOn', t('profile.autoOn')),
          h('div.label', { text: t('profile.autoText') }), B.input('profile.autoText', { class: 'wide', placeholder: 'AFK' }),
          h('div.row.wrap', null, h('span', { text: t('auto.once') }), B.num('profile.autoCooldown', 1, 1440), h('span', { text: t('u.min') }))),
        h('div.col', null,
          h('div.row', null, h('span.label', { style: 'margin:0', text: t('auto.except') }), h('span.grow'), exCount),
          friendSearch, friendList))));
  function drawFriends(b) {
    const ex = new Set(b.cfg.profile.autoExcept || []);
    const fr = (b.friends || []).filter(f => !fq || (f.name || f.id).toLowerCase().includes(fq));
    exCount.textContent = ex.size;
    const toggleEx = sid => {
      const next = ex.has(sid) ? [...ex].filter(x => x !== sid) : [...ex, sid];
      patch(id, { profile: { autoExcept: next } }).then(() => drawFriends(S.bots[id]));
    };
    fill(friendList, fr.length ? fr.map(f => h('div.fitem' + (ex.has(f.id) ? '.on' : ''), { onclick: () => toggleEx(f.id) },
      h('span.box' + (ex.has(f.id) ? '.on' : '')), h('span.ellipsis', { text: f.name || f.id })))
      : h('div.empty', { style: 'padding:18px', text: (b.friends || []).length ? t('hours.nothing') : t('profile.noFriends') }));
  }
  const m = memo();
  return {
    el,
    update(b) {
      B.sync();
      const g = b.custom || (b.cfg.profile.names || [])[0] || '';
      const real = (S.games[id] || []).find(x => (b.playingIDs || []).includes(x.appid));
      fill(preview, avatar(h('div.ava.sm'), b),
        h('div', { style: 'min-width:0' }, h('div.n.ellipsis', { text: b.name || b.account }),
          h('div.g.ellipsis', { text: g ? t('profile.nonSteam', g) : real ? t('profile.playing', real.name) : t('st.online') })));
      m([b.cfg.profile.autoExcept, b.friends, S.lang], () => drawFriends(b));
    },
  };
}

// ---------------------------------------------------------------- trades

function TradeSec(id) {
  const B = binder(id);
  const crafted = h('div.col');
  const el = h('div.col', { style: 'gap:14px' },
    h('div.grid2', null,
      group(t('trade.craftTitle'), B.switch('trade.craft', t('trade.craft')), B.check('trade.foil', t('trade.foil')), hint(t('trade.craftHint'))),
      group(t('trade.giftsTitle'), B.switch('trade.gifts', t('trade.gifts')), B.check('trade.friendsOnly', t('trade.friendsOnly')), hint(t('trade.giftsHint')))),
    h('h3.sect', { text: t('trade.crafted') }), crafted,
    h('div.row.hint', null, ic('shield'), h('span', { text: t('trade.never') })));
  const m = memo();
  return {
    el,
    update(b) {
      B.sync();
      m([b.crafted, S.lang], () => fill(crafted, table([
        { cls: 'c-d', label: t('keys.date') }, { cls: 'c-n', label: t('trade.badge') }, { cls: 'c-h', label: t('free.type') },
      ], (b.crafted || []).map(c => h('div.tr', null, h('div.c-d.mono', { text: fmtDate(c.at) }), h('div.c-n', null, h('span.ellipsis', { text: c.game })),
        h('div.c-h', null, h('span.chip' + (c.foil ? '.on' : ''), { text: c.foil ? t('trade.foilBadge') : t('trade.normalBadge') })))), t('trade.empty'))));
    },
  };
}
