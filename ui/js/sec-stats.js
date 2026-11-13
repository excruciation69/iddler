'use strict';
// Statistics and account settings.

// ---------------------------------------------------------------- stats

const CURRENCY = { 1: '$', 2: '£', 3: '€', 5: '₽', 6: 'zł', 18: '₴', 37: '₸' };
function StatsSec(id, info) {
  const refresh = btn(t('stats.refresh'), () => act('refreshStats', { id }).catch(() => {}), 'sm', 'refresh');
  info.append(refresh);
  const kpis = h('div.kpis.k4');
  const chart = h('div.chart');
  const games = h('div.col.glist');
  const level = h('div.col');
  const inv = h('div.col');
  const el = h('div.col', { style: 'gap:14px' }, kpis,
    h('div.split', null, group(t('stats.byDay'), chart), group(t('stats.byGame'), games)),
    h('div.split', null, group(t('stats.level'), level), group(t('stats.inventory'), inv)));
  let asked = false;
  const m = memo();
  return {
    el,
    update(b) {
      if (!asked && b.status === 'online' && !b.level?.level) { asked = true; act('refreshStats', { id }).catch(() => {}); }
      refresh.disabled = b.status !== 'online';
      m([b.days, b.perGame, b.playingIDs, b.drops, b.level, b.inventory, S.lang], () => {
        const days = b.days || [];
        const total = days.reduce((n, d) => n + d.sec, 0);
        const best = days.reduce((a, d) => (d.sec > a.sec ? d : a), { sec: 0 });
        const drops = (b.drops || []).reduce((n, d) => n + d.count, 0);
        fill(kpis, 
          kpi(fmtH(total) + ' ' + t('u.h'), t('stats.total', days.length)),
          kpi(best.sec ? fmtH(best.sec) + ' ' + t('u.h') : '0', t('stats.best')),
          kpi(drops, t('stats.cards')),
          kpi(b.level?.level || '…', t('stats.level')));
        const max = Math.max(3600, ...days.map(d => d.sec));
        fill(chart, ...(total ? days.map(d => {
          const day = d.day.slice(8).replace(/^0/, '');
          return h('div.bcol', { title: `${d.day}: ${fmtH(d.sec)} ${t('u.h')}` },
            h('div.bv', { style: `height:${Math.max(2, d.sec / max * 100)}%` }), h('span', { text: day }));
        }) : [hint(t('stats.noData'))]));
        const pg = b.perGame || [], top = Math.max(1, ...pg.map(g => g.sec));
        const now = new Set(b.playingIDs || []);
        // one row per game; the clock turns for games that are being idled right now
        fill(games, ...(pg.length ? pg.map(g => h('div.gstat' + (now.has(g.appid) ? '.live' : ''), null,
          ic('spin', now.has(g.appid) ? 'spin' : ''), h('span.nm.ellipsis', { text: g.name }), h('span.hv', { text: fmtH(g.sec) + ' ' + t('u.h') }))) : [hint(t('stats.noData'))]));
        const L = b.level || {};
        if (!L.level) fill(level, hint(b.status === 'online' ? t('stats.loading') : t('stats.offline')));
        else {
          const span = (L.xp - L.xpThisLevel) + L.xpToNext;
          fill(level, h('div.row', null, h('span.lvl', { text: L.level }), h('div.col.grow', { style: 'gap:6px' },
            h('div.dim', { text: t('stats.xp', L.xp) }),
            h('div.bar', null, h('i', { style: `width:${span ? (L.xp - L.xpThisLevel) / span * 100 : 0}%` })),
            h('div.faint', { style: 'font-size:12px', text: t('stats.toNext', L.xpToNext) }))));
        }
        const I = b.inventory || {};
        const cur = CURRENCY[I.currency] || '';
        fill(inv, 
          h('div.bignum', { text: I.at || I.priced ? (cur === '$' || cur === '£' || cur === '€' ? cur + I.value.toFixed(2) : I.value.toFixed(2) + ' ' + cur) : '…' }),
          h('div.dim', { text: I.busy ? t('stats.pricing', I.priced, I.total) : I.at ? t('stats.items', I.items) : t('stats.invHint') }),
          I.busy ? h('div.bar', null, h('i', { style: `width:${I.total ? I.priced / I.total * 100 : 0}%` })) : null,
          I.error && !I.busy ? h('div.err', { text: tr(I.error) }) : null);
      });
    },
  };
}
const kpi = (v, l) => h('div.kpi.sunken', null, h('b', { text: v }), h('span', { text: l }));

// ---------------------------------------------------------------- account settings

function AcctSec(id) {
  const B = binder(id);
  const acc = h('div.col');
  const guard = h('div.col');
  const prio = h('div.col', { style: 'gap:6px' });
  const file = h('input', { type: 'file', accept: '.maFile,.mafile,application/json', hidden: true, onchange: async () => {
    const f = file.files[0];
    file.value = '';
    if (!f) return;
    const bytes = new Uint8Array(await f.arrayBuffer());
    let bin = '';
    for (const x of bytes) bin += String.fromCharCode(x);
    act('maFile', { id, file: btoa(bin) }).then(() => toast(t('acc.maFileOk'))).catch(() => {});
  } });
  const el = h('div.col', { style: 'gap:14px' },
    group(t('acc.account'), acc),
    h('div.grid2', null,
      group(t('acc.guard'), guard, file),
      group(t('acc.onPlay'), B.radios('onPlay', [['pause', t('acc.onPlayPause')], ['kick', t('acc.onPlayKick')]]))),
    group(t('acc.priority'), prio, hint(t('acc.priorityHint'))));
  let code = '', timer = 0;
  const codeEl = h('span.bignum.acc');
  async function tick() {
    if (!S.bots[id]?.hasTOTP) return;
    code = await call('guardCode', { id }).catch(() => '');
    codeEl.textContent = code || '-----';
  }
  const m1 = memo(), m2 = memo(), m3 = memo();
  return {
    el,
    destroy() { clearInterval(timer); },
    update(b) {
      B.sync();
      m1([b.status, b.name, b.steamID, b.tokenExpiry, b.hasToken, b.avatar, S.lang], () => {
        const online = b.status === 'online';
        fill(acc, 
          h('div.row', null, avatar(h('div.ava', { title: t('ava.title'), onclick: e => profileMenu(e.currentTarget, id) }), b),
            h('div.col', { style: 'gap:2px;min-width:0' }, h('b.ellipsis', { text: b.name || b.account }),
              h('span.mono.dim', { text: b.account + (b.steamID ? ' · ' + b.steamID : '') }),
              b.tokenExpiry ? h('span.dim', { style: 'font-size:12px', text: t('acc.session', fmtDay(b.tokenExpiry)) }) : null)),
          h('div.row.wrap', null,
            online || b.status !== 'offline'
              ? btn(t('acc.logout'), () => act('logout', { id, forget: false }).catch(() => {}), 'sm', 'logout')
              : btn(t('btn.signIn'), () => go(`#/a/${encodeURIComponent(id)}/hours`), 'sm primary', 'logout'),
            b.hasToken ? btn(t('acc.forget'), () => confirmDialog(t('acc.forget'), t('acc.forgetText'), t('acc.forget'),
              () => act('logout', { id, forget: true }).catch(() => {})), 'sm ghost') : null,
            h('span.grow'),
            btn(t('acc.remove'), () => confirmDialog(t('acc.remove'), t('acc.removeText', b.account), t('btn.remove'),
              () => act('removeAccount', { id }).then(() => go('#/')).catch(() => {})), 'sm ghost danger', 'trash')));
      });
      m2([b.hasTOTP, S.lang], () => {
        clearInterval(timer);
        if (b.hasTOTP) {
          fill(guard, h('div.row', null, h('span.ok', null, ic('check')), h('span', { text: t('acc.maFileOn') })),
            h('div.row', null, h('span.dim', { text: t('acc.currentCode') }), codeEl, btn('', () => copy(code), 'xs ghost', 'copy')),
            h('div.row.wrap', null, btn(t('acc.replaceMaFile'), () => file.click(), 'sm', 'upload'),
              btn(t('acc.removeMaFile'), () => act('removeMaFile', { id }).catch(() => {}), 'sm ghost')),
            hint(t('acc.maFileHint')));
          tick();
          timer = setInterval(tick, 5000);
        } else {
          fill(guard, h('div.row', null, h('span.dim', null, ic('shield')), h('span', { text: t('acc.manual') })),
            h('div.row', null, btn(t('acc.loadMaFile'), () => file.click(), 'sm', 'upload')), hint(t('acc.maFileHint')));
        }
      });
      m3([b.cfg.priority, S.lang], () => {
        const p = b.cfg.priority || ['cards', 'goals', 'hours'];
        const save = n => patch(id, { priority: n }).then(() => { m3(null, () => {}); this.update(S.bots[id]); });
        fill(prio, sortable(p, (k, i) => h('div.prio.sunken', null,
          h('span.grip', null, ic('grip')), h('span.num', { text: i + 1 }),
          h('div.col', { style: 'gap:1px;min-width:0;flex:1' }, h('span', { text: t('prio.' + k) }), h('span.faint', { style: 'font-size:12px', text: t('prioHint.' + k) }))), save));
      });
    },
  };
}
