'use strict';

/**
 * Single-page UI for the time recording API.
 *
 * Plain DOM APIs on purpose: the assets are embedded in the Go binary, so
 * keeping the UI build-step-free means `go build` is the only build there is.
 */

const API = '/api/v1';

/** Cached lookups so tables can show names instead of raw ids. */
const cache = { users: [], projects: [] };

// ---------------------------------------------------------------- transport

/**
 * Calls the API and unwraps GoFr's {data, error} envelope.
 * @throws {Error} with the server-provided message on a non-2xx response.
 */
async function api(path, options = {}) {
  const res = await fetch(API + path, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });

  let body = null;
  try {
    body = await res.json();
  } catch {
    // A 204 or an error page has no JSON body; fall through to the status.
  }

  if (!res.ok) {
    throw new Error(errorMessage(body) || `Fehler ${res.status}`);
  }

  return body ? body.data : null;
}

/** Pulls a readable message out of GoFr's error shape. */
function errorMessage(body) {
  const err = body && body.error;
  if (!err) return '';
  if (typeof err === 'string') return err;
  if (err.message) return err.message;
  if (Array.isArray(err.param)) return `Ungültige Felder: ${err.param.join(', ')}`;
  return JSON.stringify(err);
}

// -------------------------------------------------------------------- utils

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];

let toastTimer;
function toast(message, kind = 'ok') {
  const el = $('#toast');
  el.textContent = message;
  el.className = `toast ${kind}`;
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.hidden = true; }, 4000);
}

/** Builds an element; text is assigned via textContent, never innerHTML. */
function el(tag, props = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (key === 'class') node.className = value;
    else if (key === 'text') node.textContent = value;
    else if (key.startsWith('on')) node.addEventListener(key.slice(2), value);
    else if (value !== null && value !== undefined) node.setAttribute(key, value);
  }
  for (const child of children) {
    if (child !== null && child !== undefined) node.append(child);
  }
  return node;
}

function statusBadge(status) {
  return el('span', { class: `status status-${status}`, text: status });
}

function fmtDate(iso) {
  if (!iso) return '–';
  const [y, m, d] = iso.split('-');
  return `${d}.${m}.${y}`;
}

const userName = (id) => cache.users.find((u) => u.id === id)?.name ?? `#${id}`;
const projectName = (id) => cache.projects.find((p) => p.id === id)?.name ?? `#${id}`;

/** Replaces a tbody with rows, or a single "empty" row when there are none. */
function fillTable(tbody, rows, columnCount, emptyText) {
  tbody.replaceChildren();
  if (rows.length === 0) {
    tbody.append(el('tr', {}, el('td', { class: 'empty', colspan: columnCount, text: emptyText })));
    return;
  }
  tbody.append(...rows);
}

/** Fills a <select> with options, preserving the current selection if possible. */
function fillSelect(select, items, { placeholder } = {}) {
  const previous = select.value;
  select.replaceChildren();
  if (placeholder) select.append(el('option', { value: '', text: placeholder }));
  for (const item of items) {
    select.append(el('option', { value: String(item.id), text: item.name }));
  }
  if (previous && [...select.options].some((o) => o.value === previous)) {
    select.value = previous;
  }
}

/** Reads a form into a plain object, dropping empty optional fields. */
function formData(form) {
  const out = {};
  for (const [key, raw] of new FormData(form).entries()) {
    const value = typeof raw === 'string' ? raw.trim() : raw;
    if (value !== '') out[key] = value;
  }
  return out;
}

// -------------------------------------------------------------------- views

async function loadUsers() {
  cache.users = (await api('/users'))?.items ?? [];

  fillSelect($('#form-timesheet select[name=userId]'), cache.users);
  fillSelect($('#filter-ts-user'), cache.users, { placeholder: 'Alle Mitarbeiter' });

  const rows = cache.users.map((u) => el('tr', {},
    el('td', { text: u.name }),
    el('td', { text: u.email }),
    el('td', { text: u.role }),
    el('td', { class: 'actions' },
      el('button', {
        class: 'link danger',
        text: 'löschen',
        onclick: () => remove(`/users/${u.id}`, `${u.name} gelöscht`, refreshAll),
      })),
  ));

  fillTable($('#table-users tbody'), rows, 4, 'Noch keine Mitarbeiter angelegt.');
}

async function loadProjects() {
  cache.projects = (await api('/projects'))?.items ?? [];

  const bookable = cache.projects.filter((p) => p.status === 'active');
  fillSelect($('#form-timesheet select[name=projectId]'), bookable);
  fillSelect($('#form-report select[name=projectId]'), cache.projects);
  fillSelect($('#filter-ts-project'), cache.projects, { placeholder: 'Alle Projekte' });

  const rows = cache.projects.map((p) => {
    const actions = el('td', { class: 'actions' });

    if (p.status === 'active') {
      actions.append(el('button', {
        class: 'link',
        text: 'abschließen',
        onclick: () => patch(`/projects/${p.id}`, { status: 'completed' }, 'Projekt abgeschlossen', refreshAll),
      }));
    }

    if (p.status === 'completed') {
      actions.append(el('button', {
        class: 'link',
        text: 'archivieren',
        onclick: () => post(`/projects/${p.id}/archive`, null, 'Projekt archiviert', refreshAll),
      }));
    }

    actions.append(el('button', {
      class: 'link danger',
      text: 'löschen',
      onclick: () => remove(`/projects/${p.id}`, `${p.name} gelöscht`, refreshAll),
    }));

    const period = `${fmtDate(p.startDate)} – ${p.endDate ? fmtDate(p.endDate) : 'offen'}`;

    return el('tr', {},
      el('td', { text: p.name }),
      el('td', { text: period }),
      el('td', { text: p.description ?? '–' }),
      el('td', {}, statusBadge(p.status)),
      actions,
    );
  });

  fillTable($('#table-projects tbody'), rows, 5, 'Noch keine Projekte angelegt.');
}

async function loadTimesheets() {
  const params = new URLSearchParams();
  const userId = $('#filter-ts-user').value;
  const projectId = $('#filter-ts-project').value;
  const status = $('#filter-ts-status').value;
  if (userId) params.set('userId', userId);
  if (projectId) params.set('projectId', projectId);
  if (status) params.set('status', status);

  const suffix = params.toString() ? `?${params}` : '';
  const entries = (await api(`/timesheets${suffix}`))?.items ?? [];

  const rows = entries.map((t) => {
    const actions = el('td', { class: 'actions' });

    // The API only allows open -> submitted -> approved/rejected.
    if (t.status === 'open') {
      actions.append(el('button', {
        class: 'link',
        text: 'einreichen',
        onclick: () => patch(`/timesheets/${t.id}`, { status: 'submitted' }, 'Eingereicht', loadTimesheets),
      }));
    }

    if (t.status === 'submitted') {
      actions.append(el('button', {
        class: 'link',
        text: 'genehmigen',
        onclick: () => patch(`/timesheets/${t.id}`, { status: 'approved' }, 'Genehmigt', loadTimesheets),
      }));
      actions.append(el('button', {
        class: 'link danger',
        text: 'ablehnen',
        onclick: () => patch(`/timesheets/${t.id}`, { status: 'rejected' }, 'Abgelehnt', loadTimesheets),
      }));
    }

    if (t.status !== 'approved') {
      actions.append(el('button', {
        class: 'link danger',
        text: 'löschen',
        onclick: () => remove(`/timesheets/${t.id}`, 'Eintrag gelöscht', loadTimesheets),
      }));
    }

    return el('tr', {},
      el('td', { text: fmtDate(t.date) }),
      el('td', { text: userName(t.userId) }),
      el('td', { text: projectName(t.projectId) }),
      el('td', { class: 'num', text: t.durationHours.toFixed(2) }),
      el('td', { text: t.description ?? '–' }),
      el('td', {}, statusBadge(t.status)),
      actions,
    );
  });

  fillTable($('#table-timesheets tbody'), rows, 7, 'Keine Einträge für diesen Filter.');
}

// ------------------------------------------------------------- mutations

/** Wraps a mutating call so every failure surfaces as a toast, not a crash. */
async function mutate(fn, successMessage, after) {
  try {
    await fn();
    if (successMessage) toast(successMessage, 'ok');
    if (after) await after();
  } catch (err) {
    toast(err.message, 'error');
  }
}

const post = (path, body, msg, after) => mutate(
  () => api(path, { method: 'POST', body: body ? JSON.stringify(body) : undefined }), msg, after);

const patch = (path, body, msg, after) => mutate(
  () => api(path, { method: 'PUT', body: JSON.stringify(body) }), msg, after);

const remove = (path, msg, after) => mutate(
  () => api(path, { method: 'DELETE' }), msg, after);

// --------------------------------------------------------------- bootstrap

function switchView(name) {
  $$('.tab').forEach((tab) => tab.setAttribute('aria-current', String(tab.dataset.view === name)));
  $$('.view').forEach((view) => { view.hidden = view.id !== `view-${name}`; });
}

async function refreshAll() {
  // Users and projects first: the timesheet table resolves ids through them.
  await Promise.all([loadUsers(), loadProjects()]);
  await loadTimesheets();
}

function wireForms() {
  $('#form-user').addEventListener('submit', (e) => {
    e.preventDefault();
    const body = formData(e.target);
    mutate(() => api('/users', { method: 'POST', body: JSON.stringify(body) }),
      'Mitarbeiter angelegt',
      async () => { e.target.reset(); await refreshAll(); });
  });

  $('#form-project').addEventListener('submit', (e) => {
    e.preventDefault();
    const body = formData(e.target);
    mutate(() => api('/projects', { method: 'POST', body: JSON.stringify(body) }),
      'Projekt angelegt',
      async () => { e.target.reset(); await refreshAll(); });
  });

  $('#form-timesheet').addEventListener('submit', (e) => {
    e.preventDefault();
    const raw = formData(e.target);
    const body = {
      ...raw,
      userId: Number(raw.userId),
      projectId: Number(raw.projectId),
      durationHours: Number(raw.durationHours),
    };
    mutate(() => api('/timesheets', { method: 'POST', body: JSON.stringify(body) }),
      'Zeit gebucht',
      async () => {
        // Keep user/project/date so booking several entries in a row is quick.
        e.target.elements.durationHours.value = '';
        e.target.elements.description.value = '';
        await loadTimesheets();
      });
  });

  $('#form-report').addEventListener('submit', (e) => {
    e.preventDefault();
    const { projectId, from, to } = formData(e.target);
    const params = new URLSearchParams();
    if (from) params.set('from', from);
    if (to) params.set('to', to);
    const suffix = params.toString() ? `?${params}` : '';

    mutate(async () => {
      const report = await api(`/projects/${projectId}/report${suffix}`);
      const rows = (report.entries ?? []).map((entry) => el('tr', {},
        el('td', { text: userName(entry.userId) }),
        el('td', { class: 'num', text: entry.hours.toFixed(2) }),
      ));
      fillTable($('#table-report tbody'), rows, 2, 'Keine Buchungen in diesem Zeitraum.');
      $('#report-total').textContent = `${report.totalHours.toFixed(2)} h gesamt`;
      $('#report-result').hidden = false;
    }, null, null);
  });

  for (const id of ['#filter-ts-user', '#filter-ts-project', '#filter-ts-status']) {
    $(id).addEventListener('change', () => mutate(loadTimesheets, null, null));
  }

  $('#tabs').addEventListener('click', (e) => {
    const tab = e.target.closest('.tab');
    if (tab) switchView(tab.dataset.view);
  });
}

function init() {
  wireForms();
  $('#form-timesheet').elements.date.value = new Date().toISOString().slice(0, 10);
  mutate(refreshAll, null, null);
}

document.addEventListener('DOMContentLoaded', init);
