'use strict';

/**
 * Single-page UI for the time recording API.
 *
 * Plain DOM APIs on purpose: the assets are embedded in the Go binary, so
 * keeping the UI build-step-free means `go build` is the only build there is.
 *
 * Permission checks here only decide what to *show*. Every rule is enforced
 * again by the server, so hiding a control is convenience, never security.
 */

const API = '/api/v1';

/** Cached lookups so tables can show names instead of raw ids. */
const cache = { users: [], projects: [], roles: [], permissions: [] };

/** The signed-in user, their permissions, and whether auth is on at all. */
let me = { user: null, permissions: [], authEnabled: false };

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
    throw new Error(errorMessage(body) || `${t('msg.error', 'Fehler')} ${res.status}`);
  }

  return body ? body.data : null;
}

/** Pulls a readable message out of GoFr's error shape. */
function errorMessage(body) {
  const err = body && body.error;
  if (!err) return '';
  if (typeof err === 'string') return err;
  if (err.message) return err.message;
  if (Array.isArray(err.param)) return `${t('msg.invalidFields', 'Ungültige Felder')}: ${err.param.join(', ')}`;
  return JSON.stringify(err);
}

// -------------------------------------------------------------------- utils

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];

/** Whether the signed-in user holds at least one of the given permissions. */
function can(...permissions) {
  if (!me.authEnabled) return true;
  return permissions.some((p) => me.permissions.includes(p));
}

let toastTimer;
function toast(message, kind = 'ok') {
  const el = $('#toast');
  el.textContent = message;
  el.className = `toast ${kind}`;
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.hidden = true; }, 5000);
}

/** Builds an element; text is assigned via textContent, never innerHTML. */
function el(tag, props = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (key === 'class') node.className = value;
    else if (key === 'text') node.textContent = value;
    else if (key === 'checked') node.checked = value;
    else if (key.startsWith('on')) node.addEventListener(key.slice(2), value);
    else if (value !== null && value !== undefined) node.setAttribute(key, value);
  }
  for (const child of children) {
    if (child !== null && child !== undefined) node.append(child);
  }
  return node;
}

function statusBadge(status) {
  // The class keeps the raw status so the colour rules still match; only the
  // label is translated.
  return el('span', { class: `status status-${status}`, text: t(`status.${status}`, status) });
}

function fmtDate(iso) {
  if (!iso) return '–';
  const [y, m, d] = iso.split('-');
  return `${d}.${m}.${y}`;
}

const fmtHours = (n) => `${n.toFixed(2)} h`;

/** Renders a signed balance, coloured as a credit or a debt. */
function balanceCell(value) {
  const sign = value > 0 ? '+' : '';
  return el('td', {
    class: `num ${value > 0 ? 'plus' : value < 0 ? 'minus' : ''}`,
    text: `${sign}${value.toFixed(2)} h`,
  });
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
function fillSelect(select, items, { placeholder, labelKey = 'name', valueKey = 'id' } = {}) {
  if (!select) return;
  const previous = select.value;
  select.replaceChildren();
  if (placeholder) select.append(el('option', { value: '', text: placeholder }));
  for (const item of items) {
    select.append(el('option', { value: String(item[valueKey]), text: item[labelKey] }));
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

/** Shows or hides every element that declares a data-perm requirement. */
function applyPermissionVisibility(root = document) {
  for (const node of $$('[data-perm]', root)) {
    node.hidden = !can(...node.dataset.perm.split(','));
  }
}

// ---------------------------------------------------------------------- i18n

/**
 * Translations for the interface.
 *
 * German is the source language and lives in the markup itself, so the `de`
 * dictionary is intentionally empty: switching back to German restores the
 * original text nodes. Every user-visible string has a key here, including the
 * ones this script generates at run time.
 */
const TRANSLATIONS = {
  en: {
    'app.title': 'Time tracking',
    'app.language': 'Language',

    'nav.timesheets': 'Time entries',
    'nav.overtime': 'Overtime',
    'nav.projects': 'Projects',
    'nav.users': 'Staff',
    'nav.roles': 'Roles',
    'nav.report': 'Reports',
    'nav.settings': 'My account',
    'nav.logout': 'Sign out',

    'login.title': 'Sign in',
    'login.hint': 'Please sign in with your email address and password.',
    'login.email': 'Email',
    'login.password': 'Password',
    'login.totp': 'Code from your authenticator app',
    'login.submit': 'Sign in',
    'login.failed': 'Email address or password is not correct.',
    'login.totpNeeded': 'Please enter the code from your authenticator app.',

    'banner.password':
      'The initial password is still in place. Please change it under "My account" — '
      + 'changes stay blocked until you do.',

    // Field labels and table headings, shared across views.
    'field.user': 'Staff member',
    'field.project': 'Project',
    'field.date': 'Date',
    'field.hours': 'Hours',
    'field.description': 'Description',
    'field.status': 'Status',
    'field.action': 'Action',
    'field.name': 'Name',
    'field.email': 'Email',
    'field.role': 'Role',
    'field.password': 'Password',
    'field.start': 'Start',
    'field.end': 'End',
    'field.period': 'Period',
    'field.from': 'From',
    'field.to': 'To',
    'field.optional': 'optional',
    'field.default': 'default',
    'field.targetPerDay': 'Target h/day',
    'field.maxPerDay': 'Max h/day',

    'action.book': 'Book',
    'action.create': 'Create',
    'action.save': 'Save',
    'action.new': 'New',
    'action.evaluate': 'Evaluate',
    'action.calculate': 'Calculate',
    'action.delete': 'delete',
    'action.edit': 'edit',
    'action.submit': 'submit',
    'action.approve': 'approve',
    'action.reject': 'reject',
    'action.complete': 'complete',
    'action.archive': 'archive',

    'filter.allUsers': 'All staff',
    'filter.allProjects': 'All projects',
    'filter.allStatus': 'All statuses',

    'status.open': 'open',
    'status.submitted': 'submitted',
    'status.approved': 'approved',
    'status.rejected': 'rejected',
    'status.active': 'active',
    'status.completed': 'completed',
    'status.archived': 'archived',

    'ts.book': 'Book time',
    'ts.entries': 'Entries',
    'ts.empty': 'No entries for this filter.',

    'ot.balance': 'Balance',
    'ot.booked': 'Booked',
    'ot.target': 'Target',
    'ot.team': 'Team balance (same period)',
    'ot.total': 'total',
    'ot.perDay': '/day',
    'ot.of': 'of',
    'ot.empty': 'No bookings in this period.',

    'project.create': 'Create project',
    'project.empty': 'No projects yet.',
    'project.open': 'open',

    'user.create': 'Add staff member',
    'user.initialPassword': 'empty = initial password',
    'user.empty': 'No staff members yet.',
    'user.systemAccount': 'System account',

    'role.create': 'Create role',
    'role.edit': 'Edit role',
    'role.permissions': 'Permissions',
    'role.rights': 'Rights',
    'role.empty': 'No roles available.',
    'role.systemRole': 'System role',
    'role.noRole': 'no role',

    'report.title': 'Project report',
    'report.result': 'Result',
    'report.empty': 'No bookings in this period.',

    'settings.workingTimes': 'My working hours',
    'settings.workingTimesHint':
      'The daily target is the basis for overtime. The daily maximum limits how much '
      + 'may be booked on a single day.',
    'settings.targetHours': 'Target hours per day',
    'settings.maxHours': 'Maximum hours per day',
    'settings.changePassword': 'Change password',
    'settings.currentPassword': 'Current password',
    'settings.newPassword': 'New password',

    'totp.title': 'Two-factor authentication',
    'totp.instructions': 'Add this key to your authenticator app and confirm the code it shows:',
    'totp.code': 'Code',
    'totp.enable': 'Enable',
    'totp.confirm': 'Confirm',
    'totp.disable': 'Disable',
    'totp.on': 'Two-factor authentication is enabled.',
    'totp.off': 'Two-factor authentication is not enabled.',
    'totp.enabled': 'Two-factor authentication enabled',
    'totp.disabled': 'Two-factor authentication disabled',

    'msg.authDisabled': 'Authentication is disabled',
    'msg.userCreated': 'Staff member created',
    'msg.userDeleted': 'Staff member deleted',
    'msg.roleCreated': 'Role created',
    'msg.roleSaved': 'Role saved',
    'msg.roleDeleted': 'Role deleted',
    'msg.roleChanged': 'Role changed',
    'msg.projectCreated': 'Project created',
    'msg.projectDeleted': 'Project deleted',
    'msg.projectCompleted': 'Project completed',
    'msg.projectArchived': 'Project archived',
    'msg.booked': 'Time booked',
    'msg.submitted': 'Submitted',
    'msg.approved': 'Approved',
    'msg.rejected': 'Rejected',
    'msg.entryDeleted': 'Entry deleted',
    'msg.workingTimesSaved': 'Working hours saved',
    'msg.passwordChanged': 'Password changed. Please sign in again.',
    'msg.initFailed': 'Initialisation failed',
    'msg.invalidFields': 'Invalid field(s)',
    'msg.error': 'Error',
  },
  de: {},
};

/**
 * Applies the active language.
 *
 * Elements keep their German text in the markup, so switching back to German
 * simply finds no translation and leaves the original in place.
 */
function applyLanguage(language) {
  const dict = TRANSLATIONS[language] ?? {};
  document.documentElement.lang = language;

  for (const node of $$('[data-i18n]')) {
    const translated = dict[node.dataset.i18n];
    if (translated === undefined) {
      // Restore the source language from the copy taken on first run.
      if (language === 'de' && node.dataset.i18nSource !== undefined) {
        setLeadingText(node, node.dataset.i18nSource);
      }

      continue;
    }

    if (node.dataset.i18nSource === undefined) {
      node.dataset.i18nSource = leadingText(node);
    }

    setLeadingText(node, translated);
  }

  for (const node of $$('[data-i18n-placeholder]')) {
    const translated = dict[node.dataset.i18nPlaceholder];
    if (node.dataset.i18nPlaceholderSource === undefined) {
      node.dataset.i18nPlaceholderSource = node.placeholder;
    }

    node.placeholder = translated ?? node.dataset.i18nPlaceholderSource;
  }

  for (const node of $$('[data-i18n-aria]')) {
    const translated = dict[node.dataset.i18nAria];
    if (translated) node.setAttribute('aria-label', translated);
  }
}

/**
 * Reads the label text of a node, ignoring any nested elements.
 *
 * Labels wrap their input, so the text lives in the first text node; using
 * textContent would pull the field's own content in as well.
 */
function leadingText(node) {
  const first = node.firstChild;

  return first && first.nodeType === Node.TEXT_NODE ? first.nodeValue : node.textContent;
}

function setLeadingText(node, value) {
  const first = node.firstChild;

  if (first && first.nodeType === Node.TEXT_NODE) first.nodeValue = value;
  else node.textContent = value;
}

/** Translates one key for use in code-generated text. */
function t(key, fallback) {
  const language = me.user?.language ?? 'de';
  return TRANSLATIONS[language]?.[key] ?? fallback;
}

// -------------------------------------------------------------------- views

async function loadMe() {
  me = await api('/me');
  me.permissions = me.permissions ?? [];

  const who = $('#who');
  if (me.authEnabled) {
    who.replaceChildren(
      el('strong', { text: me.user.name }),
      el('span', { text: ` · ${me.user.role || 'ohne Rolle'}` }),
    );
  } else {
    who.replaceChildren(el('span', { text: 'Authentifizierung deaktiviert' }));
  }

  $("#password-banner").hidden = !me.user.mustChangePassword;
  $("#logout").hidden = !me.authEnabled;

  applyLanguage(me.user.language ?? "de");
  applyPermissionVisibility();
  renderTOTPState();
}

async function loadUsers() {
  // Everyone can see themselves, but listing all users is a permission.
  if (!can('users:read')) {
    cache.users = me.user ? [me.user] : [];
  } else {
    cache.users = (await api('/users'))?.items ?? [];
  }

  fillSelect($('#form-timesheet select[name=userId]'), cache.users);
  fillSelect($('#filter-ts-user'), cache.users, { placeholder: 'Alle Mitarbeiter' });
  fillSelect($('#form-overtime select[name=userId]'), cache.users);

  if (me.user) {
    const own = String(me.user.id);
    const overtimeSelect = $('#form-overtime select[name=userId]');
    if (overtimeSelect && !overtimeSelect.value) overtimeSelect.value = own;
    const bookFor = $('#form-timesheet select[name=userId]');
    if (bookFor && !bookFor.value) bookFor.value = own;
  }

  if (!can('users:read')) return;

  const rows = cache.users.map((u) => {
    const actions = el('td', { class: 'actions' });

    if (can('users:delete') && !u.isSystem) {
      actions.append(el('button', {
        class: 'link danger',
        text: t('action.delete', 'löschen'),
        onclick: () => remove(`/users/${u.id}`, t('msg.userDeleted', 'Mitarbeiter gelöscht'), refreshAll),
      }));
    }

    if (u.isSystem) actions.append(el('span', { class: 'muted', text: t('user.systemAccount', 'Systemkonto') }));

    const roleCell = el('td', {});
    if (can('users:write') && can('roles:read')) {
      // Changing a role is a select rather than a form: it is the one field
      // that is changed on its own often enough to deserve it.
      const select = el('select', {
        onchange: (e) => patch(`/users/${u.id}/role`, { role: e.target.value },
          t('msg.roleChanged', 'Rolle geändert'), refreshAll),
      });
      fillSelect(select, cache.roles, { labelKey: 'name', valueKey: 'name' });
      select.value = u.role;
      if (u.isSystem) select.disabled = true;
      roleCell.append(select);
    } else {
      roleCell.textContent = u.role || '–';
    }

    return el('tr', { class: me.user && u.id === me.user.id ? 'self' : '' },
      el('td', { text: u.name }),
      el('td', { text: u.email }),
      roleCell,
      el('td', { class: 'num', text: u.dailyTargetHours ? u.dailyTargetHours.toFixed(1) : 'Standard' }),
      el('td', { class: 'num', text: u.maxDailyHours ? u.maxDailyHours.toFixed(1) : 'Standard' }),
      actions,
    );
  });

  fillTable($('#table-users tbody'), rows, 6, t('user.empty', t('user.empty', 'Noch keine Mitarbeiter angelegt.')));
}

async function loadRoles() {
  if (!can('roles:read')) return;

  cache.roles = (await api('/roles'))?.items ?? [];
  cache.permissions = (await api('/permissions'))?.permissions ?? [];

  fillSelect($('#form-user select[name=role]'), cache.roles, { labelKey: 'name', valueKey: 'name' });
  renderPermissionCheckboxes();

  const rows = cache.roles.map((role) => {
    const actions = el('td', { class: 'actions' });

    if (can('roles:write')) {
      actions.append(el('button', {
        class: 'link',
        text: t('action.edit', 'bearbeiten'),
        onclick: () => editRole(role),
      }));

      if (!role.isSystem) {
        actions.append(el('button', {
          class: 'link danger',
          text: t('action.delete', 'löschen'),
          onclick: () => remove(`/roles/${role.id}`, t('msg.roleDeleted', 'Rolle gelöscht'), refreshAll),
        }));
      }
    }

    if (role.isSystem) actions.append(el('span', { class: 'muted', text: t('role.systemRole', 'Systemrolle') }));

    return el('tr', {},
      el('td', { text: role.name }),
      el('td', { text: role.description || '–' }),
      el('td', { class: 'num', text: String(role.permissions.length) }),
      actions,
    );
  });

  fillTable($('#table-roles tbody'), rows, 4, t('role.empty', t('role.empty', 'Keine Rollen vorhanden.')));
}

function renderPermissionCheckboxes(selected = []) {
  const list = $('#permission-list');
  list.replaceChildren();
  for (const permission of cache.permissions) {
    list.append(el('label', {},
      el('input', {
        type: 'checkbox',
        name: 'permissions',
        value: permission,
        checked: selected.includes(permission),
      }),
      el('span', { text: permission }),
    ));
  }
}

function editRole(role) {
  const form = $('#form-role');
  form.elements.id.value = String(role.id);
  form.elements.name.value = role.name;
  form.elements.description.value = role.description ?? '';
  form.elements.name.readOnly = role.isSystem;
  renderPermissionCheckboxes(role.permissions);
  $('#role-form-title').textContent = `Rolle „${role.name}" bearbeiten`;
  switchView('roles');
}

function resetRoleForm() {
  const form = $('#form-role');
  form.reset();
  form.elements.id.value = '';
  form.elements.name.readOnly = false;
  renderPermissionCheckboxes();
  $('#role-form-title').textContent = 'Rolle anlegen';
}

async function loadProjects() {
  if (!can('projects:read')) return;

  cache.projects = (await api('/projects'))?.items ?? [];

  const bookable = cache.projects.filter((p) => p.status === 'active');
  fillSelect($('#form-timesheet select[name=projectId]'), bookable);
  fillSelect($('#form-report select[name=projectId]'), cache.projects);
  fillSelect($('#filter-ts-project'), cache.projects, { placeholder: 'Alle Projekte' });

  const rows = cache.projects.map((p) => {
    const actions = el('td', { class: 'actions' });

    if (can('projects:write') && p.status === 'active') {
      actions.append(el('button', {
        class: 'link',
        text: t('action.complete', 'abschließen'),
        onclick: () => patch(`/projects/${p.id}`, { status: 'completed' }, t('msg.projectCompleted', 'Projekt abgeschlossen'), refreshAll),
      }));
    }

    if (can('projects:archive') && p.status === 'completed') {
      actions.append(el('button', {
        class: 'link',
        text: t('action.archive', 'archivieren'),
        onclick: () => post(`/projects/${p.id}/archive`, null, t('msg.projectArchived', 'Projekt archiviert'), refreshAll),
      }));
    }

    if (can('projects:delete')) {
      actions.append(el('button', {
        class: 'link danger',
        text: t('action.delete', 'löschen'),
        onclick: () => remove(`/projects/${p.id}`, t('msg.projectDeleted', 'Projekt gelöscht'), refreshAll),
      }));
    }

    const period = `${fmtDate(p.startDate)} – ${p.endDate ? fmtDate(p.endDate) : 'offen'}`;

    return el('tr', {},
      el('td', { text: p.name }),
      el('td', { text: period }),
      el('td', { text: p.description ?? '–' }),
      el('td', {}, statusBadge(p.status)),
      actions,
    );
  });

  fillTable($('#table-projects tbody'), rows, 5, t('project.empty', t('project.empty', 'Noch keine Projekte angelegt.')));
}

async function loadTimesheets() {
  if (!can('timesheets:read:own', 'timesheets:read:all')) return;

  const params = new URLSearchParams();
  const userId = $('#filter-ts-user').value;
  const projectId = $('#filter-ts-project').value;
  const status = $('#filter-ts-status').value;
  if (userId) params.set('userId', userId);
  if (projectId) params.set('projectId', projectId);
  if (status) params.set('status', status);

  const suffix = params.toString() ? `?${params}` : '';
  const entries = (await api(`/timesheets${suffix}`))?.items ?? [];

  // Named `entry` rather than `t`, which would shadow the translation helper.
  const rows = entries.map((entry) => {
    const actions = el('td', { class: 'actions' });
    const mine = me.user && entry.userId === me.user.id;
    const mayEdit = can('timesheets:write:all') || (mine && can('timesheets:write:own'));

    // The API only allows open -> submitted -> approved/rejected.
    if (mayEdit && entry.status === 'open') {
      actions.append(el('button', {
        class: 'link',
        text: t('action.submit', 'einreichen'),
        onclick: () => patch(`/timesheets/${entry.id}`, { status: 'submitted' },
          t('msg.submitted', 'Eingereicht'), loadTimesheets),
      }));
    }

    if (can('timesheets:approve') && entry.status === 'submitted') {
      actions.append(el('button', {
        class: 'link',
        text: t('action.approve', 'genehmigen'),
        onclick: () => patch(`/timesheets/${entry.id}`, { status: 'approved' },
          t('msg.approved', 'Genehmigt'), loadTimesheets),
      }));
      actions.append(el('button', {
        class: 'link danger',
        text: t('action.reject', 'ablehnen'),
        onclick: () => patch(`/timesheets/${entry.id}`, { status: 'rejected' },
          t('msg.rejected', 'Abgelehnt'), loadTimesheets),
      }));
    }

    if (mayEdit && entry.status !== 'approved') {
      actions.append(el('button', {
        class: 'link danger',
        text: t('action.delete', 'löschen'),
        onclick: () => remove(`/timesheets/${entry.id}`,
          t('msg.entryDeleted', 'Eintrag gelöscht'), loadTimesheets),
      }));
    }

    return el('tr', { class: mine ? 'self' : '' },
      el('td', { text: fmtDate(entry.date) }),
      el('td', { text: userName(entry.userId) }),
      el('td', { text: projectName(entry.projectId) }),
      el('td', { class: 'num', text: entry.durationHours.toFixed(2) }),
      el('td', { text: entry.description ?? '–' }),
      el('td', {}, statusBadge(entry.status)),
      actions,
    );
  });

  fillTable($('#table-timesheets tbody'), rows, 7, t('ts.empty', 'Keine Einträge für diesen Filter.'));
}

function fillSettingsForm() {
  if (!me.user) return;
  const form = $('#form-working-times');
  form.elements.dailyTargetHours.value = me.user.dailyTargetHours || '';
  form.elements.maxDailyHours.value = me.user.maxDailyHours || '';
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

// ------------------------------------------------------------------ sign-in

/** Shows the sign-in overlay and hides the application behind it. */
function showLogin(message) {
  $('#login-screen').hidden = false;
  const error = $('#login-error');
  error.textContent = message ?? '';
  error.hidden = !message;
}

function hideLogin() {
  $('#login-screen').hidden = true;
  $('#login-error').hidden = true;
  $('#login-totp-field').hidden = true;
  $('#form-login').reset();
}

async function submitLogin(e) {
  e.preventDefault();

  const form = e.target;
  const body = {
    email: form.elements.email.value.trim(),
    password: form.elements.password.value,
    totp: form.elements.totp.value.trim(),
  };

  try {
    const result = await api('/auth/login', { method: 'POST', body: JSON.stringify(body) });

    // The password was right but the account has a second factor: ask for the
    // code instead of reporting a failed sign-in.
    if (result.totpRequired) {
      $('#login-totp-field').hidden = false;
      $('#login-error').textContent = t('login.totpNeeded',
        'Bitte den Code aus der Authenticator-App eingeben.');
      $('#login-error').hidden = false;
      form.elements.totp.focus();

      return;
    }

    hideLogin();
    await refreshAll();
    switchView(firstVisibleView());
  } catch {
    // The server deliberately does not say which part was wrong, so neither
    // does the interface.
    showLogin(t('login.failed', 'E-Mail-Adresse oder Passwort ist nicht korrekt.'));
    form.elements.password.value = '';
  }
}

async function doLogout() {
  try {
    await api('/auth/logout', { method: 'POST' });
  } catch {
    // Even a failed call should drop the client back to the sign-in screen.
  }

  me = { user: null, permissions: [], authEnabled: true };
  showLogin();
}

// ------------------------------------------------------------- two-factor

function renderTOTPState() {
  const enabled = me.user?.totpEnabled ?? false;

  $('#totp-state').textContent = enabled
    ? t('totp.on', 'Zwei-Faktor-Authentifizierung ist aktiviert.')
    : t('totp.off', 'Zwei-Faktor-Authentifizierung ist nicht aktiviert.');

  $('#totp-begin').hidden = enabled;
  $('#totp-disable').hidden = !enabled;
  $('#totp-confirm').hidden = true;
  $('#totp-setup').hidden = true;
  // Disabling also needs a current code, so the field stays visible for it.
  $('#totp-code-field').hidden = !enabled;
}

function wireTOTP() {
  $('#totp-begin').addEventListener('click', () => mutate(async () => {
    const setup = await api('/me/totp', { method: 'POST' });
    $('#totp-secret').textContent = setup.secret;
    $('#totp-uri').textContent = setup.uri;
    $('#totp-setup').hidden = false;
    $('#totp-code-field').hidden = false;
    $('#totp-confirm').hidden = false;
    $('#totp-begin').hidden = true;
  }, null, null));

  $('#totp-confirm').addEventListener('click', () => {
    const code = $('#totp-code').value.trim();
    mutate(() => api('/me/totp', { method: 'PUT', body: JSON.stringify({ code }) }),
      t('totp.enabled', 'Zwei-Faktor-Authentifizierung aktiviert'),
      async () => { $('#totp-code').value = ''; await refreshAll(); });
  });

  $('#totp-disable').addEventListener('click', () => {
    const code = $('#totp-code').value.trim();
    mutate(() => api(`/me/totp?code=${encodeURIComponent(code)}`, { method: 'DELETE' }),
      t('totp.disabled', 'Zwei-Faktor-Authentifizierung deaktiviert'),
      async () => { $('#totp-code').value = ''; await refreshAll(); });
  });
}

async function loadLanguages() {
  const languages = (await api('/languages'))?.languages ?? ['de'];
  const picker = $('#language-picker');

  picker.replaceChildren();
  for (const language of languages) {
    picker.append(el('option', { value: language, text: language.toUpperCase() }));
  }

  picker.value = me.user?.language ?? 'de';
}

// --------------------------------------------------------------- bootstrap

function switchView(name) {
  $$('.tab').forEach((tab) => tab.setAttribute('aria-current', String(tab.dataset.view === name)));
  $$('.view').forEach((view) => { view.hidden = view.id !== `view-${name}`; });
}

/** Picks the first tab the user is actually allowed to see. */
function firstVisibleView() {
  const tab = $$('.tab').find((candidate) => !candidate.hidden);
  return tab ? tab.dataset.view : 'settings';
}

async function refreshAll() {
  await loadMe();
  await loadLanguages();
  // Roles first: the user table renders a role picker from them.
  await loadRoles();
  await Promise.all([loadUsers(), loadProjects()]);
  await loadTimesheets();
  fillSettingsForm();
}

function wireForms() {
  $('#form-user').addEventListener('submit', (e) => {
    e.preventDefault();
    const raw = formData(e.target);
    const body = { ...raw };
    for (const key of ['dailyTargetHours', 'maxDailyHours']) {
      if (body[key] !== undefined) body[key] = Number(body[key]);
    }
    mutate(() => api('/users', { method: 'POST', body: JSON.stringify(body) }),
      t('msg.userCreated', 'Mitarbeiter angelegt'),
      async () => { e.target.reset(); await refreshAll(); });
  });

  $('#form-role').addEventListener('submit', (e) => {
    e.preventDefault();
    const form = e.target;
    const id = form.elements.id.value;
    const permissions = $$('input[name=permissions]:checked', form).map((i) => i.value);
    const body = {
      name: form.elements.name.value.trim(),
      description: form.elements.description.value.trim(),
      permissions,
    };

    const request = id
      ? api(`/roles/${id}`, { method: 'PUT', body: JSON.stringify(body) })
      : api('/roles', { method: 'POST', body: JSON.stringify(body) });

    mutate(() => request, id ? t('msg.roleSaved', 'Rolle gespeichert') : t('msg.roleCreated', 'Rolle angelegt'),
      async () => { resetRoleForm(); await refreshAll(); });
  });

  $('#role-reset').addEventListener('click', resetRoleForm);

  $('#form-project').addEventListener('submit', (e) => {
    e.preventDefault();
    const body = formData(e.target);
    mutate(() => api('/projects', { method: 'POST', body: JSON.stringify(body) }),
      t('msg.projectCreated', 'Projekt angelegt'),
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
      t('msg.booked', 'Zeit gebucht'),
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
      fillTable($('#table-report tbody'), rows, 2, t('ot.empty', t('ot.empty', 'Keine Buchungen in diesem Zeitraum.')));
      $('#report-total').textContent = `${report.totalHours.toFixed(2)} h gesamt`;
      $('#report-result').hidden = false;
    }, null, null);
  });

  $('#form-overtime').addEventListener('submit', (e) => {
    e.preventDefault();
    const { userId, from, to } = formData(e.target);
    const params = new URLSearchParams();
    if (from) params.set('from', from);
    if (to) params.set('to', to);
    const suffix = params.toString() ? `?${params}` : '';

    mutate(async () => {
      const balance = await api(`/users/${userId}/overtime${suffix}`);
      const rows = (balance.days ?? []).map((d) => el('tr', {},
        el('td', { text: fmtDate(d.date) }),
        el('td', { class: 'num', text: fmtHours(d.booked) }),
        el('td', { class: 'num', text: fmtHours(d.target) }),
        balanceCell(d.balance),
      ));
      fillTable($('#table-overtime tbody'), rows, 4, t('ot.empty', t('ot.empty', 'Keine Buchungen in diesem Zeitraum.')));

      const total = balance.totalBalance;
      const pill = $('#overtime-total');
      pill.textContent = `${total > 0 ? '+' : ''}${total.toFixed(2)} h`;
      pill.className = `pill ${total > 0 ? 'plus' : total < 0 ? 'minus' : ''}`;
      $('#overtime-meta').textContent =
        `${balance.userName} · Soll ${fmtHours(balance.dailyTarget)}/Tag · `
        + `gebucht ${fmtHours(balance.totalBooked)} von ${fmtHours(balance.totalTarget)}`;
      $('#overtime-result').hidden = false;

      if (can('reports:read')) await loadTeamOvertime(suffix);
    }, null, null);
  });

  $('#form-working-times').addEventListener('submit', (e) => {
    e.preventDefault();
    const form = e.target;
    // Empty means "use the instance default", which the API expresses as 0.
    const body = {
      dailyTargetHours: Number(form.elements.dailyTargetHours.value || 0),
      maxDailyHours: Number(form.elements.maxDailyHours.value || 0),
    };
    mutate(() => api(`/users/${me.user.id}/working-times`,
      { method: 'PUT', body: JSON.stringify(body) }),
      t('msg.workingTimesSaved', 'Arbeitszeiten gespeichert'),
      refreshAll);
  });

  $('#form-password').addEventListener('submit', (e) => {
    e.preventDefault();
    const body = formData(e.target);
    mutate(() => api('/me/password', { method: 'PUT', body: JSON.stringify(body) }),
      t('msg.passwordChanged', 'Passwort geändert. Bitte neu anmelden.'),
      async () => { e.target.reset(); await refreshAll(); });
  });

  for (const id of ['#filter-ts-user', '#filter-ts-project', '#filter-ts-status']) {
    $(id).addEventListener('change', () => mutate(loadTimesheets, null, null));
  }

  $('#tabs').addEventListener('click', (e) => {
    const tab = e.target.closest('.tab');
    if (tab) switchView(tab.dataset.view);
  });
}

async function loadTeamOvertime(suffix) {
  const balances = (await api(`/overtime${suffix}`))?.items ?? [];
  const rows = balances.map((b) => el('tr', { class: me.user && b.userId === me.user.id ? 'self' : '' },
    el('td', { text: b.userName }),
    el('td', { class: 'num', text: fmtHours(b.totalBooked) }),
    el('td', { class: 'num', text: fmtHours(b.totalTarget) }),
    balanceCell(b.totalBalance),
  ));
  fillTable($('#table-overtime-team tbody'), rows, 4, t('ot.empty', t('ot.empty', 'Keine Buchungen in diesem Zeitraum.')));
  $('#overtime-team-card').hidden = false;
}

async function init() {
  // The sign-in form is wired first and on its own: if anything below fails,
  // the user must still be able to sign in rather than face a form whose
  // submit handler was never attached, which would silently reload the page.
  $('#form-login').addEventListener('submit', submitLogin);

  try {
    wireForms();
    wireTOTP();
    $('#logout').addEventListener('click', doLogout);
    $('#language-picker').addEventListener('change', (e) => mutate(
      () => api('/me/language', { method: 'PUT', body: JSON.stringify({ language: e.target.value }) }),
      null,
      refreshAll));

    $('#form-timesheet').elements.date.value = new Date().toISOString().slice(0, 10);
  } catch (err) {
    toast(`${t('msg.initFailed', 'Initialisierung fehlgeschlagen')}: ${err.message}`, 'error');
  }

  try {
    await refreshAll();
    hideLogin();
    switchView(firstVisibleView());
  } catch {
    // No usable session: the sign-in screen is the whole interface until
    // there is one.
    showLogin();
  }
}

document.addEventListener('DOMContentLoaded', init);
