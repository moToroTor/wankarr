'use strict';

const $ = (sel) => document.querySelector(sel);

function humanBytes(n) {
  if (!n) return '—';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(v >= 100 ? 0 : 1)} ${units[i]}`;
}

async function fetchJSON(path, opts) {
  const res = await fetch(path, opts);
  if (!res.ok) throw new Error(`${path}: ${res.status}`);
  return res.json();
}

function setStatus(msg) { $('#status').textContent = msg || ''; }

// Titles and tags arrive from third-party feeds: escape everything
// interpolated into markup.
function esc(s) {
  return String(s == null ? '' : s).replace(/[&<>"']/g, (c) =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function variantRow(v) {
  const badges = [];
  if (v.resolution) badges.push(`<span class="badge">${esc(v.resolution)}</span>`);
  if (v.codec) badges.push(`<span class="badge">${esc(v.codec)}</span>`);
  if (v.hbr) badges.push('<span class="badge hbr">HBR</span>');
  if (v.freeleech) badges.push('<span class="badge pick">FL</span>');
  if (v.pick) badges.push('<span class="badge pick">profile pick</span>');
  const seeds = v.seeders ? ` · ${v.seeders} seed${v.seeders === 1 ? '' : 's'}` : '';
  return `<tr class="${v.pick ? 'pick' : ''}">
    <td>${badges.join('')}</td>
    <td>${humanBytes(v.size_bytes)}${esc(seeds)} <span class="src">${esc(v.source || '')}</span></td>
    <td>${v.pub_date ? esc(v.pub_date.slice(0, 10)) : '—'}</td>
    <td>${esc(v.premium_note || '')}</td>
    <td><button data-send="${esc(v.group_id)}" title="Queue in Transmission">Send</button></td>
  </tr>`;
}

async function sendTorrent(groupID, btn) {
  btn.disabled = true;
  setStatus(`Queueing ${groupID} in Transmission…`);
  try {
    const r = await fetchJSON('/api/send', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ group_id: groupID }),
    });
    setStatus(`Queued: ${r.queued}`);
    btn.textContent = 'Queued';
  } catch (e) {
    setStatus(`Send failed: ${e.message}`);
    btn.disabled = false;
  }
}

document.addEventListener('click', (e) => {
  const btn = e.target.closest('[data-send]');
  if (btn) sendTorrent(btn.dataset.send, btn);
  const sbtn = e.target.closest('[data-search-query]');
  if (sbtn) searchTitle(sbtn.dataset.searchQuery, sbtn.dataset.searchTitle, sbtn);
});

function groupCard(g) {
  const wanted = g.wanted_scene
    ? `<div class="wanted">Wanted: ${esc(g.wanted_scene)} (score ${Number(g.wanted_score).toFixed(2)})</div>` : '';
  const owned = g.owned_height
    ? `<div class="owned">In library: ${esc(String(g.owned_height))}p${g.owned_title ? ` — ${esc(g.owned_title)}` : ''}</div>` : '';
  const cover = g.cover
    ? `<img class="cover" src="${esc(g.cover)}" alt="" loading="lazy" onerror="this.remove()">` : '';
  // The group key is the scene identity minus variant parentheticals
  // and bare resolution tokens, so it doubles as the Jackett query
  // to find other versions.
  return `<article class="group">
    <div class="group-head">${cover}<div><h2>${esc(g.title)}</h2>${wanted}${owned}
    <button data-search-query="${esc(g.key)}" data-search-title="${esc(g.title)}" title="Search Jackett for other versions of this scene">Find versions</button>
    </div></div>
    <table><thead><tr><th>Variant</th><th>Size</th><th>Published</th><th>Note</th><th></th></tr></thead>
    <tbody>${g.variants.map(variantRow).join('')}</tbody></table>
  </article>`;
}

let activeQuery = '';
let activePage = 1;
// A search result summary parked here survives the groups reload that
// follows every search (showGroups owns the status line and would
// otherwise clobber it before it can be read).
let statusNote = '';

const VIEWS = ['groups', 'wishlist', 'matches'];

// The URL hash is the single source of truth for navigation state, so
// browser back/forward works: every view, search, filter, and page
// change writes one history entry, and popstate re-renders from it.
function defaultState() {
  return { view: 'groups', q: '', page: 1, vrOnly: $('#vr-only').checked };
}

function readURLState() {
  const s = defaultState();
  const m = /^#\/([^?]*)(\?(.*))?$/.exec(location.hash || '');
  if (!m) return s;
  if (VIEWS.includes(m[1])) s.view = m[1];
  if (m[3]) {
    const params = new URLSearchParams(m[3]);
    s.q = params.get('q') || '';
    s.page = Math.max(1, parseInt(params.get('page') || '1', 10) || 1);
    s.vrOnly = params.get('vr') !== '0';
  }
  return s;
}

function serializeState(s) {
  const params = new URLSearchParams();
  if (s.q) params.set('q', s.q);
  if (s.page > 1) params.set('page', String(s.page));
  if (!s.vrOnly) params.set('vr', '0');
  const qs = params.toString();
  return `#/${s.view}${qs ? `?${qs}` : ''}`;
}

let currentState = null;

// One entry per change: back walks views, searches, filters, and pages
// in the order they happened. Identical states are skipped so
// double-clicks don't stack dead entries.
function navigate(patch) {
  const next = { ...(currentState || defaultState()), ...patch };
  if (currentState && serializeState(next) === serializeState(currentState)) return;
  history.pushState(next, '', serializeState(next));
  applyState(next);
}

function applyState(s) {
  currentState = s;
  activeQuery = s.q;
  activePage = s.page;
  $('#vr-only').checked = s.vrOnly;
  $('#search-q').value = s.q;
  renderView(s.view);
}

async function showGroups() {
  setStatus('Loading groups…');
  const note = statusNote;
  statusNote = '';
  const suffix = note ? ` — ${note}` : '';
  try {
    const params = new URLSearchParams();
    if (!$('#vr-only').checked) params.set('vr', '0');
    if (activeQuery) params.set('q', activeQuery);
    params.set('page', String(activePage));
    const res = await fetchJSON(`/api/groups?${params.toString()}`);
    activePage = res.page;
    const pages = Math.max(1, Math.ceil(res.total / res.per_page));
    const filterNote = activeQuery
      ? `<p>Filtered to “${esc(activeQuery)}” <button id="clear-q">show all</button></p>` : '';
    const pager = pages > 1
      ? `<p><button id="prev-p"${res.page <= 1 ? ' disabled' : ''}>← Prev</button> Page ${res.page} of ${pages} · ${res.total} groups <button id="next-p"${res.page >= pages ? ' disabled' : ''}>Next →</button></p>`
      : '';
    $('#groups-view').innerHTML = filterNote + (res.groups.length
      ? res.groups.map(groupCard).join('')
      : '<p>No matching groups. Poll an RSS feed or run a search.</p>') + pager;
    const clear = $('#clear-q');
    if (clear) clear.addEventListener('click', () => navigate({ q: '', page: 1 }));
    const prev = $('#prev-p');
    if (prev) prev.addEventListener('click', () => { if (activePage > 1) navigate({ page: activePage - 1 }); });
    const next = $('#next-p');
    if (next) next.addEventListener('click', () => navigate({ page: activePage + 1 }));
    setStatus(`Page ${res.page} of ${pages} · ${res.total} scene group(s).${suffix}`);
  } catch (e) { setStatus(`Error: ${e.message}${suffix}`); }
}

async function showMatches() {
  setStatus('Loading matches…');
  try {
    const matches = await fetchJSON('/api/matches');
    $('#match-count').textContent = matches.length ? `(${matches.length})` : '';
    $('#matches-view').innerHTML = matches.length
      ? matches.map((m) => `<div class="match"><strong>${esc(m.GroupKey)}</strong><br>wanted scene ${esc(m.SceneID)} — score ${Number(m.Score).toFixed(2)}</div>`).join('')
      : '<p>No wishlist matches yet.</p>';
    setStatus('');
  } catch (e) { setStatus(`Error: ${e.message}`); }
}

function renderView(name) {
  document.querySelectorAll('nav button').forEach((b) =>
    b.classList.toggle('active', b.dataset.view === name));
  $('#groups-view').hidden = name !== 'groups';
  $('#wishlist-view').hidden = name !== 'wishlist';
  $('#matches-view').hidden = name !== 'matches';
  if (name === 'groups') showGroups();
  else if (name === 'wishlist') showWishlist();
  else showMatches();
}

// Back/forward restores the recorded state object; the URL parse is a
// fallback for entries this script didn't create.
window.addEventListener('popstate', (e) => {
  applyState(e.state && e.state.view ? e.state : readURLState());
});

async function showWishlist() {
  setStatus('Loading wishlist from XBVR…');
  try {
    const items = await fetchJSON('/api/wishlist');
    $('#wishlist-view').innerHTML = items.length
      ? items.map(wishlistCard).join('')
      : '<p>Wishlist is empty.</p>';
    setStatus(`${items.length} wishlisted scene(s).`);
  } catch (e) { setStatus(`Error: ${e.message}`); }
}

function wishlistCard(it) {
  const who = (it.performers || []).join(', ');
  // Narrow the Emp search with the first known performer: cuts the
  // clown-fetish noise without losing the scene when the title matches.
  const query = it.performers && it.performers.length
    ? `${it.title} ${it.performers[0]}` : it.title;
  const known = it.groups && it.groups.length
    ? `<p>${it.groups.length} known Emp group(s):</p>${it.groups.map(groupCard).join('')}`
    : '<p>No Emp results known yet — search to check.</p>';
  return `<article class="group"><div class="group-head">
    ${it.cover ? `<img class="cover" src="${esc(it.cover)}" alt="" loading="lazy" onerror="this.remove()">` : ''}
    <div><h2>${esc(it.title)}</h2>
    <div class="wanted">${esc(it.site || '')}${who ? ` · ${esc(who)}` : ''}</div>
    <button data-search-query="${esc(query)}" data-search-title="${esc(it.title)}">Search Emp</button>
    </div></div>${known}</article>`;
}

async function searchTitle(query, title, btn) {
  if (btn) btn.disabled = true;
  setStatus(`Searching Emp for “${title}”… (one Jackett query, results cached)`);
  try {
    const r = await fetchJSON(`/api/search?q=${encodeURIComponent(query)}`);
    statusNote = `Search complete: ${r.results} result(s), ${r.new} new.`;
    navigate({ view: 'groups', q: query, page: 1 });
  } catch (e) {
    setStatus(`Search error: ${e.message}`);
    if (btn) btn.disabled = false;
  }
}

document.querySelectorAll('nav button').forEach((b) =>
  b.addEventListener('click', () => navigate({ view: b.dataset.view })));

$('#vr-only').addEventListener('change', () => navigate({ view: 'groups', page: 1, vrOnly: $('#vr-only').checked }));

$('#rematch').addEventListener('click', async () => {
  setStatus('Re-matching index against wishlist…');
  try {
    await fetchJSON('/api/rematch', { method: 'POST' });
    setStatus('Rematch running — check New matches in a few seconds.');
  } catch (e) { setStatus(`Rematch error: ${e.message}`); }
});

$('#search-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const q = $('#search-q').value.trim();
  if (!q) return;
  searchTitle(q, q, null);
});

// Normalize the URL to a recorded entry (no extra history item), so
// the first back press leaves the app instead of landing nowhere.
currentState = readURLState();
history.replaceState(currentState, '', serializeState(currentState));
applyState(currentState);
showMatches();
