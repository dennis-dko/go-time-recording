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

/**
 * Gives every password field a reveal toggle.
 *
 * Done in script rather than markup so any password field added later is
 * covered automatically. Runs once; already-enhanced fields are skipped.
 */
function enhancePasswordFields(root = document) {
  for (const input of $$('input[type="password"]', root)) {
    if (input.parentElement?.classList.contains('pw-wrap')) continue;

    const wrap = el('span', { class: 'pw-wrap' });
    input.replaceWith(wrap);
    wrap.append(input);

    const button = el('button', {
      type: 'button',
      class: 'pw-toggle',
      'aria-label': t('pw.show', 'Passwort anzeigen'),
      'aria-pressed': 'false',
    });

    button.append(eyeIcon(false));

    button.addEventListener('click', () => {
      const revealed = input.type === 'text';
      input.type = revealed ? 'password' : 'text';

      button.setAttribute('aria-pressed', String(!revealed));
      button.setAttribute('aria-label', revealed
        ? t('pw.show', 'Passwort anzeigen')
        : t('pw.hide', 'Passwort verbergen'));

      button.replaceChildren(eyeIcon(!revealed));

      // Keep the caret where the user left it; changing `type` moves it to
      // the end in some browsers.
      const caret = input.value.length;
      input.focus();
      input.setSelectionRange(caret, caret);
    });

    wrap.append(button);
  }
}

/** Builds the eye icon, struck through when the password is visible. */
function eyeIcon(revealed) {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('fill', 'none');
  svg.setAttribute('stroke', 'currentColor');
  svg.setAttribute('stroke-width', '2');
  svg.setAttribute('stroke-linecap', 'round');
  svg.setAttribute('stroke-linejoin', 'round');
  svg.setAttribute('aria-hidden', 'true');

  const paths = ['M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z', 'M12 9a3 3 0 100 6 3 3 0 000-6z'];
  if (revealed) paths.push('M3 3l18 18');

  for (const d of paths) {
    const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    path.setAttribute('d', d);
    svg.append(path);
  }

  return svg;
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

    'pw.show': 'Show password',
    'pw.hide': 'Hide password',

    'nav.calendar': 'Calendar',
    'cal.title': 'Calendar',
    'cal.today': 'Today',
    'cal.close': 'close',
    'cal.monthTotal': 'Month total',
    'cal.weekdays': 'Mon,Tue,Wed,Thu,Fri,Sat,Sun',
    'cal.months':
      'January,February,March,April,May,June,July,August,September,October,November,December',

    'field.projectOptional': 'Project (optional)',
    'ts.noProject': 'no project',
    'filter.noProject': 'Without project',

    'cat.create': 'Create your own category',
    'cat.hint':
      'Categories are private and visible only to you. Use them to split up a day '
      + 'when no shared project fits.',
    'cat.badge': 'private',
    'msg.categoryCreated': 'Category created',

    'nav.admin': 'Settings',
    'admin.branding': 'Appearance',
    'admin.brandingHint': 'Title, banner, logo and footer of this installation.',
    'admin.title': 'Title (browser tab and header)',
    'admin.banner': 'Banner text (empty hides it)',
    'admin.company': 'Company',
    'admin.companyUrl': 'Company website',
    'admin.footer': 'Footer',
    'admin.legal': 'Legal notice',
    'admin.logo': 'Logo (PNG/SVG, max. 256 KB)',
    'admin.logoClear': 'Remove logo',
    'admin.logoTooBig': 'The logo must be smaller than 256 KB.',
    'admin.database': 'Database connection',
    'admin.databaseHint':
      'The connection is saved and used the next time the application starts. '
      + 'Switching it while running would not be safe.',
    'admin.activeConnection': 'Currently connected via',
    'admin.dialect': 'Type',
    'admin.dbName': 'Database / file name',
    'admin.dbHost': 'Host',
    'admin.dbPort': 'Port',
    'admin.dbUser': 'User',
    'admin.dbPassword': 'Password',
    'admin.dbSsl': 'SSL mode',
    'admin.keepStored': 'leave unchanged',
    'admin.ldap': 'LDAP connection',
    'admin.ldapHint':
      'When enabled, passwords are checked against the directory. Unknown users are '
      + 'created locally on their first successful sign-in.',
    'admin.ldapEnabled': 'Enabled',
    'admin.skipVerify': 'Do not verify certificate (unsafe)',
    'admin.baseDn': 'Base DN',
    'admin.bindDn': 'Bind DN (optional)',
    'admin.bindPassword': 'Bind password',
    'admin.userFilter': 'User filter (%s = login name)',
    'admin.nameAttr': 'Name attribute',
    'admin.mailAttr': 'Email attribute',
    'admin.defaultRole': 'Default role for new users',
    'admin.testConnection': 'Test connection',
    'admin.testing': 'Testing the connection …',
    'admin.saved': 'Settings saved',
    'admin.restartNeeded': 'Saved. Applied on the next start.',
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
  fillSelect($('#filter-ts-user'), cache.users, { placeholder: t('filter.allUsers', 'Alle Mitarbeiter') });
  fillSelect($('#form-overtime select[name=userId]'), cache.users);
  fillSelect($('#calendar-user'), cache.users);

  if (me.user) {
    const own = String(me.user.id);
    for (const selector of ['#form-overtime select[name=userId]', '#form-timesheet select[name=userId]', '#calendar-user']) {
      const select = $(selector);
      if (select && !select.value) select.value = own;
    }
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
  // A blank first option is what lets time be booked without a project.
  fillSelect($('#form-timesheet select[name=projectId]'), bookable,
    { placeholder: t('ts.noProject', 'ohne Projekt') });
  fillSelect($('#form-report select[name=projectId]'), cache.projects);
  fillSelect($('#filter-ts-project'), cache.projects, { placeholder: 'Alle Projekte' });

  const rows = cache.projects.map((p) => {
    const actions = el('td', { class: 'actions' });

    // A private category belongs to the signed-in user, so they may manage it
    // whatever their project permissions are.
    const mine = p.private && me.user && p.ownerId === me.user.id;

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

    if (can('projects:delete') || mine) {
      actions.append(el('button', {
        class: 'link danger',
        text: t('action.delete', 'löschen'),
        onclick: () => remove(`/projects/${p.id}`, t('msg.projectDeleted', 'Projekt gelöscht'), refreshAll),
      }));
    }

    const period = `${fmtDate(p.startDate)} – ${p.endDate ? fmtDate(p.endDate) : t('project.open', 'offen')}`;

    const name = el('td', { text: p.name });
    if (p.private) {
      name.append(' ', el('span', { class: 'pill', text: t('cat.badge', 'privat') }));
    }

    return el('tr', { class: mine ? 'self' : '' },
      name,
      // A category has no meaningful period, so the column stays quiet for it.
      el('td', { class: p.private ? 'empty' : '', text: p.private ? '–' : period }),
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
      el('td', {
        class: entry.projectId ? '' : 'empty',
        text: entry.projectId ? projectName(entry.projectId) : t('ts.noProject', 'ohne Projekt'),
      }),
      el('td', { class: 'num', text: entry.durationHours.toFixed(2) }),
      el('td', { text: entry.description ?? '–' }),
      el('td', {}, statusBadge(entry.status)),
      actions,
    );
  });

  fillTable($('#table-timesheets tbody'), rows, 7, t('ts.empty', 'Keine Einträge für diesen Filter.'));
}

// ----------------------------------------------------------------- calendar

/** The month the calendar is showing, as a Date on the first of that month. */
let calendarMonth = new Date(new Date().getFullYear(), new Date().getMonth(), 1);

const ISO_DAY = (d) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;

/**
 * Renders the month grid with the hours booked on each day.
 *
 * The whole month is fetched in one request and grouped client-side; asking
 * per day would be dozens of round trips for one screen.
 */
async function loadCalendar() {
  if (!can('timesheets:read:own', 'timesheets:read:all')) return;

  const first = calendarMonth;
  const last = new Date(first.getFullYear(), first.getMonth() + 1, 0);

  const params = new URLSearchParams({ from: ISO_DAY(first), to: ISO_DAY(last) });
  const userId = $('#calendar-user').value;
  if (userId) params.set('userId', userId);

  const entries = (await api(`/timesheets?${params}`))?.items ?? [];

  const byDay = new Map();
  for (const entry of entries) {
    if (!byDay.has(entry.date)) byDay.set(entry.date, []);
    byDay.get(entry.date).push(entry);
  }

  renderCalendarGrid(first, last, byDay);
}

function renderCalendarGrid(first, last, byDay) {
  const monthNames = t('cal.months', 'Januar,Februar,März,April,Mai,Juni,Juli,August,September,Oktober,November,Dezember').split(',');
  $('#calendar-title').textContent = `${monthNames[first.getMonth()]} ${first.getFullYear()}`;

  const weekdays = t('cal.weekdays', 'Mo,Di,Mi,Do,Fr,Sa,So').split(',');
  $('#calendar-weekdays').replaceChildren(...weekdays.map((d) => el('span', { text: d })));

  // Weeks start on Monday, so Sunday (0) becomes the last column.
  const leading = (first.getDay() + 6) % 7;
  const cells = [];

  for (let i = 0; i < leading; i += 1) {
    cells.push(el('div', { class: 'cal-day outside' }));
  }

  const todayIso = ISO_DAY(new Date());
  const maxHours = Math.max(8, ...[...byDay.values()].map(
    (list) => list.reduce((sum, e) => sum + e.durationHours, 0)));

  let monthTotal = 0;

  for (let d = 1; d <= last.getDate(); d += 1) {
    const date = new Date(first.getFullYear(), first.getMonth(), d);
    const iso = ISO_DAY(date);
    const dayEntries = byDay.get(iso) ?? [];
    const hours = dayEntries.reduce((sum, e) => sum + e.durationHours, 0);
    monthTotal += hours;

    const classes = ['cal-day'];
    if (dayEntries.length) classes.push('has-entries');
    if (iso === todayIso) classes.push('today');

    const cell = el(dayEntries.length ? 'button' : 'div', {
      class: classes.join(' '),
      ...(dayEntries.length ? { type: 'button' } : {}),
    }, el('span', { class: 'cal-daynum', text: String(d) }));

    if (hours > 0) {
      cell.append(el('span', { class: 'cal-hours', text: fmtHours(hours) }));

      const names = [...new Set(dayEntries.map(
        (e) => (e.projectId ? projectName(e.projectId) : t('ts.noProject', 'ohne Projekt'))))];
      cell.append(el('span', { class: 'cal-projects', text: names.join(', ') }));

      const bar = el('div', { class: 'cal-bar' });
      bar.style.width = `${Math.min(100, (hours / maxHours) * 100)}%`;
      cell.append(bar);

      cell.addEventListener('click', () => showCalendarDay(iso, dayEntries));
    }

    cells.push(cell);
  }

  $('#calendar-days').replaceChildren(...cells);
  $('#calendar-summary').textContent =
    `${t('cal.monthTotal', 'Gesamt im Monat')}: ${fmtHours(monthTotal)}`;
  $('#calendar-day-card').hidden = true;
}

/** Shows the entries behind one day. */
function showCalendarDay(iso, entries) {
  $('#calendar-day-title').textContent = fmtDate(iso);

  const rows = entries.map((entry) => el('tr', {},
    el('td', { text: entry.projectId ? projectName(entry.projectId) : t('ts.noProject', 'ohne Projekt') }),
    el('td', { class: 'num', text: entry.durationHours.toFixed(2) }),
    el('td', { text: entry.description ?? '–' }),
    el('td', {}, statusBadge(entry.status)),
  ));

  fillTable($('#table-calendar-day tbody'), rows, 4, t('ot.empty', 'Keine Buchungen in diesem Zeitraum.'));
  $('#calendar-day-card').hidden = false;
}

function wireCalendar() {
  const shift = (months) => {
    calendarMonth = new Date(calendarMonth.getFullYear(), calendarMonth.getMonth() + months, 1);
    mutate(loadCalendar, null, null);
  };

  $('#calendar-prev').addEventListener('click', () => shift(-1));
  $('#calendar-next').addEventListener('click', () => shift(1));
  $('#calendar-today').addEventListener('click', () => {
    const now = new Date();
    calendarMonth = new Date(now.getFullYear(), now.getMonth(), 1);
    mutate(loadCalendar, null, null);
  });
  $('#calendar-user').addEventListener('change', () => mutate(loadCalendar, null, null));
  $('#calendar-day-close').addEventListener('click', () => { $('#calendar-day-card').hidden = true; });
}

function fillSettingsForm() {
  if (!me.user) return;
  const form = $('#form-working-times');
  form.elements.dailyTargetHours.value = me.user.dailyTargetHours || '';
  form.elements.maxDailyHours.value = me.user.maxDailyHours || '';
}

// ------------------------------------------------------------ administration

/** Whether the caller may open the administration screen. */
const isSystemAdmin = () => !me.authEnabled || (me.user?.isSystem ?? false);

/**
 * Applies the instance branding.
 *
 * Fetched separately from /me because the sign-in screen must show the title
 * and logo before anyone has authenticated.
 */
async function loadBranding() {
  const branding = await api('/branding');

  document.title = branding.title || 'Zeiterfassung';
  $('#app-title').textContent = branding.title || 'Zeiterfassung';

  for (const holder of ['#brand-logo', '#login-logo']) {
    const img = $(holder);
    if (!img) continue;

    img.src = branding.logo || '';
    img.hidden = !branding.logo;
    img.alt = branding.title || '';
  }

  // The announcement banner is separate from the "change your password" one.
  const banner = $('#instance-banner');
  banner.textContent = branding.banner ?? '';
  banner.hidden = !branding.banner;

  $('#footer-text').textContent = branding.footerText ?? '';
  $('#footer-legal').textContent = branding.legalNotice ?? '';

  const company = $('#footer-company');
  company.textContent = branding.companyName ?? '';
  company.hidden = !branding.companyName;
  if (branding.companyUrl) company.href = branding.companyUrl;
  else company.removeAttribute('href');

  $('#site-footer').hidden = !(branding.footerText || branding.companyName || branding.legalNotice);

  return branding;
}

async function loadAdmin() {
  $('#tab-admin').hidden = !isSystemAdmin();
  if (!isSystemAdmin()) return;

  const branding = await api('/branding');
  const form = $('#form-branding');
  for (const field of ['title', 'banner', 'companyName', 'companyUrl', 'footerText', 'legalNotice']) {
    form.elements[field].value = branding[field] ?? '';
  }

  setLogoPreview(branding.logo ?? '');

  const ds = await api('/settings/datasource');
  const dsForm = $('#form-datasource');
  for (const field of ['dialect', 'name', 'host', 'port', 'user', 'sslMode']) {
    dsForm.elements[field].value = ds[field] ?? '';
  }

  $('#datasource-active').textContent =
    `${t('admin.activeConnection', 'Aktuell verbunden über')}: ${ds.active}`;

  const ldap = await api('/settings/ldap');
  const ldapForm = $('#form-ldap');
  for (const field of ['host', 'baseDn', 'bindDn', 'userFilter', 'nameAttribute', 'emailAttribute']) {
    ldapForm.elements[field].value = ldap[field] ?? '';
  }
  ldapForm.elements.port.value = ldap.port || 389;
  for (const flag of ['enabled', 'startTls', 'useTls', 'skipVerify']) {
    ldapForm.elements[flag].checked = Boolean(ldap[flag]);
  }

  fillSelect(ldapForm.elements.defaultRole, cache.roles, { labelKey: 'name', valueKey: 'name' });
  ldapForm.elements.defaultRole.value = ldap.defaultRole ?? 'employee';
}

/** The logo travels as a data URI, so it needs no upload endpoint. */
let pendingLogo = '';

function setLogoPreview(dataURI) {
  pendingLogo = dataURI;

  const preview = $('#logo-preview');
  preview.src = dataURI || '';
  preview.hidden = !dataURI;
}

function wireAdmin() {
  $('#logo-file').addEventListener('change', (e) => {
    const file = e.target.files?.[0];
    if (!file) return;

    // 256 KB matches the server-side limit; checking here gives a better
    // message than a rejected request would.
    if (file.size > 256 * 1024) {
      toast(t('admin.logoTooBig', 'Das Logo darf höchstens 256 KB groß sein.'), 'error');
      e.target.value = '';

      return;
    }

    const reader = new FileReader();
    reader.onload = () => setLogoPreview(String(reader.result));
    reader.readAsDataURL(file);
  });

  $('#logo-clear').addEventListener('click', () => {
    setLogoPreview('');
    $('#logo-file').value = '';
  });

  $('#form-branding').addEventListener('submit', (e) => {
    e.preventDefault();
    const body = { ...formData(e.target), logo: pendingLogo };
    mutate(() => api('/settings/branding', { method: 'PUT', body: JSON.stringify(body) }),
      t('admin.saved', 'Einstellungen gespeichert'),
      async () => { await loadBranding(); await loadAdmin(); });
  });

  $('#form-datasource').addEventListener('submit', (e) => {
    e.preventDefault();
    const body = formData(e.target);
    mutate(async () => {
      const result = await api('/settings/datasource', { method: 'PUT', body: JSON.stringify(body) });
      toast(result.message ?? t('admin.restartNeeded', 'Gespeichert. Wird beim nächsten Start übernommen.'), 'ok');
    }, null, loadAdmin);
  });

  $('#form-ldap').addEventListener('submit', (e) => {
    e.preventDefault();
    mutate(() => api('/settings/ldap', { method: 'PUT', body: JSON.stringify(ldapPayload()) }),
      t('admin.saved', 'Einstellungen gespeichert'),
      loadAdmin);
  });

  $('#ldap-test').addEventListener('click', () => {
    const result = $('#ldap-test-result');
    result.textContent = t('admin.testing', 'Verbindung wird geprüft …');

    mutate(async () => {
      const outcome = await api('/settings/ldap/test',
        { method: 'POST', body: JSON.stringify(ldapPayload()) });

      result.textContent = outcome.message;
      result.className = outcome.ok ? 'muted plus' : 'muted minus';
    }, null, null);
  });
}

/** Reads the LDAP form, including its checkboxes and numeric port. */
function ldapPayload() {
  const form = $('#form-ldap');
  const body = {
    host: form.elements.host.value.trim(),
    port: Number(form.elements.port.value || 389),
    baseDn: form.elements.baseDn.value.trim(),
    bindDn: form.elements.bindDn.value.trim(),
    bindPassword: form.elements.bindPassword.value,
    userFilter: form.elements.userFilter.value.trim(),
    nameAttribute: form.elements.nameAttribute.value.trim(),
    emailAttribute: form.elements.emailAttribute.value.trim(),
    defaultRole: form.elements.defaultRole.value,
  };

  for (const flag of ['enabled', 'startTls', 'useTls', 'skipVerify']) {
    body[flag] = form.elements[flag].checked;
  }

  return body;
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
  // After users and projects, so the calendar can resolve names.
  await loadCalendar();
  await loadAdmin();
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

  // A personal category is the same endpoint with "private", which is what
  // makes the caller its owner.
  $('#form-category').addEventListener('submit', (e) => {
    e.preventDefault();
    const body = { ...formData(e.target), private: true };
    mutate(() => api('/projects', { method: 'POST', body: JSON.stringify(body) }),
      t('msg.categoryCreated', 'Kategorie angelegt'),
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
  enhancePasswordFields();

  // Branding is public, so the sign-in screen already carries the instance's
  // own title and logo. A failure here must not block signing in.
  try {
    await loadBranding();
  } catch {
    // Falls back to the built-in title.
  }

  try {
    wireForms();
    wireTOTP();
    wireCalendar();
    wireAdmin();
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
