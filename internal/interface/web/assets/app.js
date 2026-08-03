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
 * Reads a cookie the server left for this page.
 *
 * Only non-HttpOnly cookies are visible here, which is exactly the CSRF token
 * and never the session token.
 */
function readCookie(name) {
  const prefix = `${name}=`;

  for (const part of document.cookie.split(';')) {
    const entry = part.trim();
    if (entry.startsWith(prefix)) return decodeURIComponent(entry.slice(prefix.length));
  }

  return '';
}

/** Requests that cannot change anything need no token. */
const SAFE_METHODS = new Set(['GET', 'HEAD', 'OPTIONS']);

/**
 * Calls the API and unwraps GoFr's {data, error} envelope.
 *
 * State-changing calls echo the CSRF cookie back in a header. Another site can
 * make the browser send the session cookie, but it can neither read this cookie
 * nor set a custom header, so the echo is what proves the call came from here.
 *
 * @throws {Error} with the server-provided message on a non-2xx response.
 */
async function api(path, options = {}) {
  const method = (options.method ?? 'GET').toUpperCase();
  const headers = { 'Content-Type': 'application/json', ...(options.headers ?? {}) };

  if (!SAFE_METHODS.has(method)) {
    headers['X-CSRF-Token'] = readCookie('gtr_csrf');
  }

  const res = await fetch(API + path, {
    ...options,
    headers,
    // Without this a cross-origin deployment would drop the cookies entirely.
    credentials: 'same-origin',
  });

  let body = null;
  try {
    body = await res.json();
  } catch {
    // A 204 or an error page has no JSON body; fall through to the status.
  }

  if (!res.ok) {
    throw new Error(errorMessage(body) || `${t('msg.error', 'Error')} ${res.status}`);
  }

  return body ? body.data : null;
}

/** Pulls a readable message out of GoFr's error shape. */
function errorMessage(body) {
  const err = body && body.error;
  if (!err) return '';
  if (typeof err === 'string') return err;
  if (err.message) return err.message;
  if (Array.isArray(err.param)) return `${t('msg.invalidFields', 'Invalid field(s)')}: ${err.param.join(', ')}`;
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
      'aria-label': t('pw.show', 'Show password'),
      'aria-pressed': 'false',
    });

    button.append(eyeIcon(false));

    button.addEventListener('click', () => {
      const revealed = input.type === 'text';
      input.type = revealed ? 'password' : 'text';

      button.setAttribute('aria-pressed', String(!revealed));
      button.setAttribute('aria-label', revealed
        ? t('pw.show', 'Show password')
        : t('pw.hide', 'Hide password'));

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
  // English is the source language: it lives in the markup and in the
  // fallbacks passed to t(), so a key missing from a translation below
  // degrades to English rather than to a language nobody asked for.
  en: {},

  de: {
    // Entries that read the same in both languages are still listed, so the
    // dictionary stays a complete translation of the markup rather than
    // something a reader has to diff against it.
    'admin.baseDn': 'Base DN',
    'admin.bindDn': 'Bind DN (optional)',
    'admin.dbHost': 'Host',
    'admin.dbPort': 'Port',
    'admin.logo': 'Logo (PNG/SVG, max. 256 KB)',
    'field.name': 'Name',
    'field.optional': 'optional',
    'field.start': 'Start',
    'field.status': 'Status',
    'totp.code': 'Code',

    'setup.title': 'Einrichtung',
    'setup.progress': 'Schritt {n} von {total} — {done} erledigt',
    'setup.next': 'Weiter',
    'setup.back': 'Zurück',
    'setup.skip': 'Diesen Schritt überspringen',
    'setup.openSettings': 'Stattdessen die Einstellungen öffnen',
    'setup.later': 'Später abschließen',
    'setup.finish': 'Fertigstellen',
    'setup.done': 'Einrichtung abgeschlossen.',
    'setup.stillRequired': 'Diese Schritte müssen zuerst erledigt werden; bis dahin erscheint der Assistent immer wieder.',
    'setup.passwordChanged': 'Passwort geändert. Bitte erneut anmelden.',
    'setup.password.title': 'Administrator-Passwort ändern',
    'setup.password.text': 'Dieses Konto hat noch das Passwort aus der Dokumentation, das jeder nachlesen kann. Bis zur Änderung lässt sich nichts anderes nutzen.',
    'setup.timezone.title': 'Zeitzone wählen',
    'setup.timezone.text': 'Sie bestimmt, auf welchen Kalendertag eine Buchung fällt. Ohne Angabe gilt UTC, was Abendbuchungen für den Großteil der Welt auf den falschen Tag legt.',
    'tour.card.title': 'Geführte Tour',
    'tour.card.text': 'Ein kurzer Rundgang durch die Bereiche der Anwendung. Wird bei der ersten Anmeldung einmal gezeigt; jederzeit erneut startbar.',
    'tour.card.start': 'Tour anzeigen',
    'tour.count': 'Schritt {n} von {total}',
    'tour.next': 'Weiter',
    'tour.back': 'Zurück',
    'tour.end': 'Tour überspringen',
    'tour.finish': 'Fertig',
    'tour.nav.title': 'Alles liegt hier oben',
    'tour.nav.text': 'Diese Reiter sind die ganze Anwendung. Sichtbar ist nur, was deine Rolle erlaubt — bei manchen ist diese Leiste also kürzer als bei anderen.',
    'tour.book.title': 'Zeit buchen',
    'tour.book.text': 'Datum wählen, Stunden eintragen, fertig. Ein Projekt ist optional — Zeiten lassen sich jetzt erfassen und später einsortieren.',
    'tour.entries.title': 'Deine Einträge',
    'tour.entries.text': 'Alles Gebuchte, filterbar nach Person, Projekt und Status. Ein Eintrag bleibt änderbar, bis du ihn zur Genehmigung einreichst.',
    'tour.calendar.title': 'Der Monat auf einen Blick',
    'tour.calendar.text': 'Welche Tage Stunden haben und wie viele. Ein Klick auf einen Tag zeigt, was dahintersteckt.',
    'tour.overtime.title': 'Dein Überstundensaldo',
    'tour.overtime.text': 'Gebuchte Stunden gegen dein Tagesziel. Nur Tage mit Buchungen zählen, damit Wochenenden und freie Tage sich nicht stillschweigend als Minus aufsummieren.',
    'tour.projects.title': 'Projekte, auch eigene',
    'tour.projects.text': 'Gemeinsame Projekte werden zentral angelegt. Du kannst zusätzlich private anlegen, die nur du siehst, um einen Tag aufzuteilen, wenn kein gemeinsames Projekt passt.',
    'tour.account.title': 'Mein Konto',
    'tour.account.text': 'Tagesziel, Zeitzone, Zwei-Faktor-Authentifizierung und API-Token. Das Tagesziel ist der Maßstab für den Überstundensaldo.',
    'tour.admin.title': 'Einstellungen',
    'tour.admin.text': 'Nur für dich als eingebauten Administrator: Darstellung, Datenbank, Verzeichnis, Zeitzone und die Betriebsgrenzwerte.',
    'tour.theme.title': 'Darstellung und Sprache',
    'tour.theme.text': 'Hell oder dunkel — automatisch richtet sich nach der Tageszeit. Die Sprache folgt dem Browser, bis du eine wählst. Das war alles, viel Erfolg.',

    'setup.decideFirst': 'Bitte diesen Schritt zuerst erledigen.',
    'setup.database.title': 'Datenbank wählen',
    'setup.database.text': 'Dieser Schritt kommt zuerst, weil alles Weitere in der hier gewählten Datenbank gespeichert wird. Ein späterer Wechsel richtet die Anwendung auf eine leere Datenbank: Passwort, Zeitzone und Titel bleiben in der alten zurück, und die Installation kommt mit dem Initialpasswort aus der Dokumentation wieder hoch.',
    'setup.database.current': 'Läuft derzeit auf: {dialect}',
    'setup.database.keep': 'Bei dieser Datenbank bleiben',
    'setup.database.configure': 'Andere Datenbank einrichten',
    'setup.database.gotoHint': 'Verbindung einrichten, dann neu starten. Der Assistent läuft in der neuen Datenbank weiter.',
    'setup.branding.title': 'Installation benennen',
    'setup.branding.text': 'Der Titel erscheint im Browser-Tab und in der Kopfzeile. Optional, aber genau das unterscheidet auf einen Blick eine Testinstanz von der echten.',
    'setup.directory.title': 'Verzeichnis anbinden (optional)',
    'setup.directory.text': 'Mit angebundenem LDAP oder Active Directory melden sich alle mit ihrem vorhandenen Konto an, und hier werden keine Passwörter gespeichert. Überspringen, um Konten lokal zu verwalten.',

    'ops.title': 'Betrieb und Grenzwerte',
    'ops.hint': 'Leer lassen, um den Wert aus der Konfigurationsdatei zu behalten. Änderungen wirken innerhalb weniger Sekunden, ohne Neustart.',
    'ops.sessionLifetime': 'Sitzungsdauer (Stunden)',
    'ops.maxDailyHours': 'Maximale Stunden pro Tag (systemweit)',
    'ops.rateLimit': 'Ratenbegrenzung (Anfragen)',
    'ops.rateWindow': 'Zeitfenster der Ratenbegrenzung (Sekunden)',
    'ops.autoClose': 'Offene Einträge einreichen nach (Tagen)',
    'ops.deleteRatio': 'Verzeichnis-Abgleich: Löschgrenze (0–1)',
    'ops.reset': 'Alle Werte auf die Konfigurationsdatei zurücksetzen',
    'ops.saved': 'Grenzwerte gespeichert',
    'ops.reset.done': 'Alle Werte folgen wieder der Konfigurationsdatei',
    'ops.effective': 'Aktuell wirksam',
    'ops.sessionShort': 'Sitzung',
    'ops.maxShort': 'max./Tag',
    'ops.rateShort': 'Rate',
    'ops.autoShort': 'Auto-Einreichen',
    'ops.ratioShort': 'Löschgrenze',

    'user.directoryAccount': 'aus dem Verzeichnis',
    'user.directoryHint': 'Wird im LDAP verwaltet. Das Passwort liegt dort, und das Entfernen des Eintrags dort entfernt auch dieses Konto.',

    'passkey.title': 'Passkeys',
    'passkey.hint': 'Mit Fingerabdruck, Gesicht oder Geräte-PIN anmelden, statt ein Passwort einzutippen. Der Schlüssel verlässt das Gerät nie, kann also weder abgephisht noch aus einem Datenleck gelesen werden. Dein Passwort funktioniert weiterhin.',
    'passkey.name': 'Name für dieses Gerät',
    'passkey.namePlaceholder': 'Arbeitslaptop',
    'passkey.add': 'Passkey hinzufügen',
    'passkey.added': 'Hinzugefügt',
    'passkey.lastUsed': 'Zuletzt benutzt',
    'passkey.never': 'nie',
    'passkey.empty': 'Noch keine Passkeys. Füge einen hinzu, um dich ohne Passwort anzumelden.',
    'passkey.signIn': 'Mit Passkey anmelden',
    'passkey.added.done': 'Passkey hinzugefügt',
    'passkey.removed': 'Passkey entfernt',
    'passkey.cancelled': 'Der Passkey wurde nicht verwendet.',
    'passkey.failed': 'Der Passkey wurde nicht akzeptiert.',

    'tz.title': 'Zeitzone',
    'tz.hint': 'Bestimmt, auf welchen Kalendertag eine Buchung fällt — für alle, die unter „Mein Konto" keine eigene gesetzt haben.',
    'tz.myTitle': 'Meine Zeitzone',
    'tz.myHint': 'Bestimmt, auf welchen Kalendertag deine Buchungen fallen. Lass es auf „Systemeinstellung folgen", außer du arbeitest aus einem anderen Land.',
    'tz.timezone': 'Zeitzone',
    'tz.inherit': 'Systemeinstellung folgen',
    'tz.saved': 'Zeitzone gespeichert',

    'app.theme': 'Darstellung',
    'theme.auto': 'Automatisch (nach Tageszeit)',
    'theme.light': 'Hell',
    'theme.dark': 'Dunkel',

    'action.approve': 'genehmigen',
    'action.archive': 'archivieren',
    'action.book': 'Buchen',
    'action.calculate': 'Berechnen',
    'action.complete': 'abschließen',
    'action.create': 'Anlegen',
    'action.delete': 'löschen',
    'action.edit': 'bearbeiten',
    'action.evaluate': 'Auswerten',
    'action.new': 'Neu',
    'action.reject': 'ablehnen',
    'action.save': 'Speichern',
    'action.submit': 'einreichen',
    'admin.activeConnection': 'Aktuell verbunden über',
    'admin.banner': 'Banner-Text (leer = ausgeblendet)',
    'admin.bindPassword': 'Bind-Passwort',
    'admin.branding': 'Erscheinungsbild',
    'admin.brandingHint': 'Titel, Banner, Logo und Fußzeile dieser Installation.',
    'admin.company': 'Firma',
    'admin.companyUrl': 'Firmen-Website',
    'admin.database': 'Datenbank-Anbindung',
    'admin.databaseHint': 'Die Verbindung wird gespeichert und beim nächsten Start der Anwendung verwendet. Ein Wechsel im laufenden Betrieb wäre nicht gefahrlos möglich.',
    'admin.dbName': 'Datenbank / Dateiname',
    'admin.dbPassword': 'Passwort',
    'admin.dbSsl': 'SSL-Modus',
    'admin.dbUser': 'Benutzer',
    'admin.defaultRole': 'Standardrolle für neue Benutzer',
    'admin.dialect': 'Typ',
    'admin.footer': 'Fußzeile',
    'admin.idAttr': 'Eindeutiges ID-Attribut (entryUUID, Active Directory: objectGUID)',
    'admin.keepStored': 'unverändert lassen',
    'admin.ldap': 'LDAP-Anbindung',
    'admin.ldapEnabled': 'Aktiviert',
    'admin.ldapHint': 'Bei aktivierter Anbindung wird das Passwort gegen das Verzeichnis geprüft. Unbekannte Benutzer werden beim ersten erfolgreichen Login lokal angelegt.',
    'admin.legal': 'Rechtlicher Hinweis',
    'admin.logoClear': 'Logo entfernen',
    'admin.logoTooBig': 'Das Logo darf höchstens 256 KB groß sein.',
    'admin.mailAttr': 'E-Mail-Attribut',
    'admin.nameAttr': 'Namens-Attribut',
    'admin.restartNeeded': 'Gespeichert. Wird beim nächsten Start übernommen.',
    'admin.saved': 'Einstellungen gespeichert',
    'admin.skipVerify': 'Zertifikat nicht prüfen (unsicher)',
    'admin.testConnection': 'Verbindung testen',
    'admin.testing': 'Verbindung wird geprüft …',
    'admin.title': 'Titel (Browser-Tab und Kopfzeile)',
    'admin.userFilter': 'Benutzer-Filter (%s = Anmeldename)',
    'app.language': 'Sprache',
    'banner.password': 'Das Initialpasswort ist noch aktiv. Bitte unter „Mein Konto" ändern — bis dahin bleibt die übrige Anwendung gesperrt.',
    'cal.close': 'schließen',
    'cal.monthTotal': 'Gesamt im Monat',
    'cal.months': 'Januar,Februar,März,April,Mai,Juni,Juli,August,September,Oktober,November,Dezember',
    'cal.title': 'Kalender',
    'cal.today': 'Heute',
    'cal.weekdays': 'Mo,Di,Mi,Do,Fr,Sa,So',
    'cat.badge': 'privat',
    'cat.create': 'Eigenes Projekt anlegen',
    'cat.hint': 'Eigene Projekte sind privat und nur für Sie sichtbar. Damit lässt sich Zeit innerhalb eines Tages aufteilen, wenn kein gemeinsames Projekt passt.',
    'field.action': 'Aktion',
    'field.date': 'Datum',
    'field.default': 'Standard',
    'field.description': 'Beschreibung',
    'field.email': 'E-Mail',
    'field.end': 'Ende',
    'field.from': 'Von',
    'field.hours': 'Stunden',
    'field.maxPerDay': 'Max/Tag',
    'field.password': 'Passwort',
    'field.period': 'Zeitraum',
    'field.project': 'Projekt',
    'field.projectOptional': 'Projekt (optional)',
    'field.role': 'Rolle',
    'field.targetPerDay': 'Soll/Tag',
    'field.to': 'Bis',
    'field.user': 'Mitarbeiter',
    'filter.allProjects': 'Alle Projekte',
    'filter.allStatus': 'Alle Status',
    'filter.allUsers': 'Alle Mitarbeiter',
    'login.email': 'E-Mail',
    'login.failed': 'E-Mail-Adresse oder Passwort ist nicht korrekt.',
    'login.hint': 'Bitte mit E-Mail-Adresse und Passwort anmelden.',
    'login.password': 'Passwort',
    'login.submit': 'Anmelden',
    'login.title': 'Anmelden',
    'login.totp': 'Code der Authenticator-App',
    'login.totpNeeded': 'Bitte den Code aus der Authenticator-App eingeben.',
    'msg.approved': 'Genehmigt',
    'msg.booked': 'Zeit gebucht',
    'msg.categoryCreated': 'Projekt angelegt',
    'msg.entryDeleted': 'Eintrag gelöscht',
    'msg.error': 'Fehler',
    'msg.initFailed': 'Initialisierung fehlgeschlagen',
    'msg.loadFailed': 'Konnte nicht alles laden',
    'msg.invalidFields': 'Ungültige Felder',
    'msg.passwordChanged': 'Passwort geändert. Bitte neu anmelden.',
    'msg.projectArchived': 'Projekt archiviert',
    'msg.projectCompleted': 'Projekt abgeschlossen',
    'msg.projectCreated': 'Projekt angelegt',
    'msg.projectDeleted': 'Projekt gelöscht',
    'msg.rejected': 'Abgelehnt',
    'msg.roleChanged': 'Rolle geändert',
    'msg.roleCreated': 'Rolle angelegt',
    'msg.roleDeleted': 'Rolle gelöscht',
    'msg.roleSaved': 'Rolle gespeichert',
    'msg.submitted': 'Eingereicht',
    'msg.userCreated': 'Mitarbeiter angelegt',
    'msg.userDeleted': 'Mitarbeiter gelöscht',
    'msg.workingTimesSaved': 'Arbeitszeiten gespeichert',
    'nav.admin': 'Einstellungen',
    'nav.calendar': 'Kalender',
    'nav.logout': 'Abmelden',
    'nav.overtime': 'Überstunden',
    'nav.projects': 'Projekte',
    'nav.report': 'Auswertung',
    'nav.roles': 'Rollen',
    'nav.settings': 'Mein Konto',
    'nav.timesheets': 'Zeiteinträge',
    'nav.users': 'Mitarbeiter',
    'ot.balance': 'Saldo',
    'ot.booked': 'Gebucht',
    'ot.empty': 'Keine Buchungen in diesem Zeitraum.',
    'ot.target': 'Soll',
    'ot.team': 'Team-Saldo (gleicher Zeitraum)',
    'project.create': 'Projekt anlegen',
    'project.empty': 'Noch keine Projekte angelegt.',
    'project.open': 'offen',
    'pw.hide': 'Passwort verbergen',
    'pw.show': 'Passwort anzeigen',
    'report.result': 'Ergebnis',
    'report.title': 'Projektauswertung',
    'role.create': 'Rolle anlegen',
    'role.empty': 'Keine Rollen vorhanden.',
    'role.permissions': 'Berechtigungen',
    'role.rights': 'Rechte',
    'role.systemRole': 'Systemrolle',
    'settings.changePassword': 'Passwort ändern',
    'settings.currentPassword': 'Aktuelles Passwort',
    'settings.maxHours': 'Maximale Stunden pro Tag',
    'settings.newPassword': 'Neues Passwort',
    'settings.targetHours': 'Soll-Stunden pro Tag',
    'settings.workingTimes': 'Meine Arbeitszeiten',
    'settings.workingTimesHint': 'Das Tagessoll ist die Grundlage der Überstunden. Das Tagesmaximum begrenzt, wie viel an einem Tag gebucht werden darf.',
    'status.approved': 'genehmigt',
    'status.open': 'offen',
    'status.rejected': 'abgelehnt',
    'status.submitted': 'eingereicht',
    'sync.aborted': 'Abgebrochen',
    'sync.confirm': 'ACHTUNG: {n} Konto/Konten und {h} Zeiteinträge werden unwiderruflich gelöscht. Fortfahren?',
    'sync.created': 'Angelegt',
    'sync.deleted': 'Gelöscht',
    'sync.directoryUsers': 'Im Verzeichnis',
    'sync.entries': 'Zeiteinträge',
    'sync.hint': 'Gleicht die Benutzer mit dem Verzeichnis ab. Das LDAP wird ausschließlich gelesen und nie verändert.',
    'sync.localExternal': 'Lokal aus dem Verzeichnis',
    'sync.none': 'Keine Konten fehlen im Verzeichnis.',
    'sync.preview': 'Vorschau',
    'sync.run': 'Abgleich ausführen',
    'sync.running': 'Wird geprüft …',
    'sync.title': 'Verzeichnis-Abgleich',
    'sync.warning': 'Achtung: Konten, die im Verzeichnis fehlen, werden hier gelöscht — mitsamt ihren erfassten Zeiten, eigenen Projekten und Tokens. Das lässt sich nicht rückgängig machen. Bitte immer zuerst die Vorschau ansehen.',
    'sync.wouldCreate': 'Würden angelegt',
    'sync.wouldDelete': 'Würden gelöscht',
    'token.copyNow': 'Jetzt kopieren — dieser Wert wird nicht wieder angezeigt:',
    'token.create': 'Token erstellen',
    'token.created': 'Erstellt',
    'token.docs': 'API-Dokumentation &#8599;',
    'token.empty': 'Noch keine Tokens angelegt.',
    'token.expires': 'Gültig für (Tage, 0 = unbegrenzt)',
    'token.expiresAt': 'Läuft ab',
    'token.hint': 'Ein Token ersetzt bei API-Aufrufen Ihr Passwort und hat immer genau die Rechte Ihrer aktuellen Rolle. Der Wert wird nur einmal angezeigt.',
    'token.lastUsed': 'Zuletzt genutzt',
    'token.name': 'Bezeichnung',
    'token.never': 'unbegrenzt',
    'token.prefix': 'Präfix',
    'token.revoke': 'widerrufen',
    'token.revoked': 'Token widerrufen',
    'token.title': 'API-Tokens',
    'token.unused': 'nie',
    'totp.confirm': 'Bestätigen',
    'totp.disable': 'Deaktivieren',
    'totp.disabled': 'Zwei-Faktor-Authentifizierung deaktiviert',
    'totp.enable': 'Aktivieren',
    'totp.enabled': 'Zwei-Faktor-Authentifizierung aktiviert',
    'totp.instructions': 'Diesen Schlüssel in der Authenticator-App eintragen und den angezeigten Code bestätigen:',
    'totp.off': 'Zwei-Faktor-Authentifizierung ist nicht aktiviert.',
    'totp.on': 'Zwei-Faktor-Authentifizierung ist aktiviert.',
    'totp.title': 'Zwei-Faktor-Authentifizierung',
    'ts.book': 'Zeit buchen',
    'ts.empty': 'Keine Einträge für diesen Filter.',
    'ts.entries': 'Einträge',
    'ts.noProject': 'ohne Projekt',
    'user.create': 'Mitarbeiter anlegen',
    'user.empty': 'Noch keine Mitarbeiter angelegt.',
    'user.initialPassword': 'leer = Initialpasswort',
    'user.systemAccount': 'Systemkonto',
  },
};

/**
 * Applies the active language.
 *
 * Elements carry their English text in the markup, so a key with no
 * translation - including every key when English itself is selected - leaves
 * the English in place. That is what makes English the fallback rather than
 * merely one more translation.
 */
function applyLanguage(language) {
  const dict = TRANSLATIONS[language] ?? {};
  document.documentElement.lang = language;

  for (const node of $$('[data-i18n]')) {
    const translated = dict[node.dataset.i18n];
    if (translated === undefined) {
      // Restore the English source from the copy taken on first run.
      if (node.dataset.i18nSource !== undefined) {
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

/**
 * The language to render in.
 *
 * A signed-in user's stored choice wins. Before anyone has signed in - which
 * is exactly the sign-in screen - the browser's own preference decides, so a
 * first-time visitor is not greeted in a language they may not read.
 */
function activeLanguage() {
  if (me.user?.language) return me.user.language;

  return detectBrowserLanguage();
}

/** The language shown when nothing better is known. */
const FALLBACK_LANGUAGE = 'en';

/**
 * Picks the best supported language from the browser's preference list.
 *
 * Only the primary subtag is compared, so "en-GB" and "en-US" both match "en".
 * A browser asking for something this application does not speak gets English,
 * which is the language the markup itself is written in.
 */
function detectBrowserLanguage() {
  const supported = Object.keys(TRANSLATIONS);
  const preferences = navigator.languages?.length
    ? navigator.languages
    : [navigator.language ?? FALLBACK_LANGUAGE];

  for (const preference of preferences) {
    const primary = String(preference).toLowerCase().split('-')[0];
    if (supported.includes(primary)) return primary;
  }

  return FALLBACK_LANGUAGE;
}

/** Translates one key for use in code-generated text. */
function t(key, fallback) {
  return TRANSLATIONS[activeLanguage()]?.[key] ?? fallback;
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

  applyLanguage(activeLanguage());
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
  fillSelect($('#filter-ts-user'), cache.users, { placeholder: t('filter.allUsers', 'All staff') });
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
        text: t('action.delete', 'delete'),
        onclick: () => remove(`/users/${u.id}`, t('msg.userDeleted', 'Staff member deleted'), refreshAll),
      }));
    }

    if (u.isSystem) actions.append(el('span', { class: 'muted', text: t('user.systemAccount', 'System account') }));

    // A directory account shown as an ordinary one invites two useless
    // actions: setting a password that is never checked, and a deletion the
    // next synchronisation undoes.
    if (u.isExternal) {
      actions.append(el('span', {
        class: 'muted',
        text: t('user.directoryAccount', 'from the directory'),
        title: t('user.directoryHint',
          'Managed in LDAP. The password lives there, and removing the entry there removes this account.'),
      }));
    }

    const roleCell = el('td', {});
    if (can('users:write') && can('roles:read')) {
      // Changing a role is a select rather than a form: it is the one field
      // that is changed on its own often enough to deserve it.
      const select = el('select', {
        onchange: (e) => patch(`/users/${u.id}/role`, { role: e.target.value },
          t('msg.roleChanged', 'Role changed'), refreshAll),
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
      el('td', { class: 'num', text: u.dailyTargetHours ? u.dailyTargetHours.toFixed(1) : t('field.default', 'default') }),
      el('td', { class: 'num', text: u.maxDailyHours ? u.maxDailyHours.toFixed(1) : t('field.default', 'default') }),
      actions,
    );
  });

  fillTable($('#table-users tbody'), rows, 6, t('user.empty', 'No staff members yet.'));
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
        text: t('action.edit', 'edit'),
        onclick: () => editRole(role),
      }));

      if (!role.isSystem) {
        actions.append(el('button', {
          class: 'link danger',
          text: t('action.delete', 'delete'),
          onclick: () => remove(`/roles/${role.id}`, t('msg.roleDeleted', 'Role deleted'), refreshAll),
        }));
      }
    }

    if (role.isSystem) actions.append(el('span', { class: 'muted', text: t('role.systemRole', 'System role') }));

    return el('tr', {},
      el('td', { text: role.name }),
      el('td', { text: role.description || '–' }),
      el('td', { class: 'num', text: String(role.permissions.length) }),
      actions,
    );
  });

  fillTable($('#table-roles tbody'), rows, 4, t('role.empty', 'No roles available.'));
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
    { placeholder: t('ts.noProject', 'no project') });
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
        text: t('action.complete', 'complete'),
        onclick: () => patch(`/projects/${p.id}`, { status: 'completed' }, t('msg.projectCompleted', 'Project completed'), refreshAll),
      }));
    }

    if (can('projects:archive') && p.status === 'completed') {
      actions.append(el('button', {
        class: 'link',
        text: t('action.archive', 'archive'),
        onclick: () => post(`/projects/${p.id}/archive`, null, t('msg.projectArchived', 'Project archived'), refreshAll),
      }));
    }

    if (can('projects:delete') || mine) {
      actions.append(el('button', {
        class: 'link danger',
        text: t('action.delete', 'delete'),
        onclick: () => remove(`/projects/${p.id}`, t('msg.projectDeleted', 'Project deleted'), refreshAll),
      }));
    }

    const period = `${fmtDate(p.startDate)} – ${p.endDate ? fmtDate(p.endDate) : t('project.open', 'open')}`;

    const name = el('td', { text: p.name });
    if (p.private) {
      name.append(' ', el('span', { class: 'pill', text: t('cat.badge', 'private') }));
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

  fillTable($('#table-projects tbody'), rows, 5, t('project.empty', 'No projects yet.'));
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
        text: t('action.submit', 'submit'),
        onclick: () => patch(`/timesheets/${entry.id}`, { status: 'submitted' },
          t('msg.submitted', 'Submitted'), loadTimesheets),
      }));
    }

    if (can('timesheets:approve') && entry.status === 'submitted') {
      actions.append(el('button', {
        class: 'link',
        text: t('action.approve', 'approve'),
        onclick: () => patch(`/timesheets/${entry.id}`, { status: 'approved' },
          t('msg.approved', 'Approved'), loadTimesheets),
      }));
      actions.append(el('button', {
        class: 'link danger',
        text: t('action.reject', 'reject'),
        onclick: () => patch(`/timesheets/${entry.id}`, { status: 'rejected' },
          t('msg.rejected', 'Rejected'), loadTimesheets),
      }));
    }

    if (mayEdit && entry.status !== 'approved') {
      actions.append(el('button', {
        class: 'link danger',
        text: t('action.delete', 'delete'),
        onclick: () => remove(`/timesheets/${entry.id}`,
          t('msg.entryDeleted', 'Entry deleted'), loadTimesheets),
      }));
    }

    return el('tr', { class: mine ? 'self' : '' },
      el('td', { text: fmtDate(entry.date) }),
      el('td', { text: userName(entry.userId) }),
      el('td', {
        class: entry.projectId ? '' : 'empty',
        text: entry.projectId ? projectName(entry.projectId) : t('ts.noProject', 'no project'),
      }),
      el('td', { class: 'num', text: entry.durationHours.toFixed(2) }),
      el('td', { text: entry.description ?? '–' }),
      el('td', {}, statusBadge(entry.status)),
      actions,
    );
  });

  fillTable($('#table-timesheets tbody'), rows, 7, t('ts.empty', 'No entries for this filter.'));
}

// ----------------------------------------------------------------- calendar

/** The month the calendar is showing, as a Date on the first of that month. */
let calendarMonth = new Date(new Date().getFullYear(), new Date().getMonth(), 1);

const ISO_DAY = (d) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;

/**
 * The calendar day it is right now, in the zone that applies to this user.
 *
 * Not `new Date().toISOString().slice(0, 10)`, which is the day in UTC: for
 * anyone east of Greenwich that is yesterday for part of every evening, and for
 * anyone west it is tomorrow for part of every morning. Booking hours against
 * the wrong day is the kind of error nobody notices until a month-end total is
 * short.
 *
 * Intl is used rather than date arithmetic because it is the only thing in the
 * platform that knows about daylight saving.
 */
/** The date this code last filled in, so a date the user typed is recognisable. */
let autofilledDate = '';

/**
 * Points the booking form at today, leaving a date the user chose alone.
 *
 * Overwriting their choice would be worse than a stale default: someone
 * catching up on last week's hours would silently book them against today.
 */
function resetBookingDate() {
  const field = $('#form-timesheet')?.elements.date;
  if (!field) return;

  if (field.value === '' || field.value === autofilledDate) {
    autofilledDate = todayISO();
    field.value = autofilledDate;
  }
}

function todayISO() {
  const timeZone = me.user?.effectiveTimezone;

  if (!timeZone) {
    // Before sign-in there is no zone to apply, so the browser's own is the
    // best guess available - and still better than UTC.
    return ISO_DAY(new Date());
  }

  try {
    // en-CA renders as YYYY-MM-DD, which is the format the API expects.
    return new Intl.DateTimeFormat('en-CA', {
      timeZone, year: 'numeric', month: '2-digit', day: '2-digit',
    }).format(new Date());
  } catch {
    // A zone the browser does not know must not break booking entirely.
    return ISO_DAY(new Date());
  }
}

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
  const monthNames = t('cal.months', 'January,February,March,April,May,June,July,August,September,October,November,December').split(',');
  $('#calendar-title').textContent = `${monthNames[first.getMonth()]} ${first.getFullYear()}`;

  const weekdays = t('cal.weekdays', 'Mon,Tue,Wed,Thu,Fri,Sat,Sun').split(',');
  $('#calendar-weekdays').replaceChildren(...weekdays.map((d) => el('span', { text: d })));

  // Weeks start on Monday, so Sunday (0) becomes the last column.
  const leading = (first.getDay() + 6) % 7;
  const cells = [];

  for (let i = 0; i < leading; i += 1) {
    cells.push(el('div', { class: 'cal-day outside' }));
  }

  const todayIso = todayISO();
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
        (e) => (e.projectId ? projectName(e.projectId) : t('ts.noProject', 'no project'))))];
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
    `${t('cal.monthTotal', 'Month total')}: ${fmtHours(monthTotal)}`;
  $('#calendar-day-card').hidden = true;
}

/** Shows the entries behind one day. */
function showCalendarDay(iso, entries) {
  $('#calendar-day-title').textContent = fmtDate(iso);

  const rows = entries.map((entry) => el('tr', {},
    el('td', { text: entry.projectId ? projectName(entry.projectId) : t('ts.noProject', 'no project') }),
    el('td', { class: 'num', text: entry.durationHours.toFixed(2) }),
    el('td', { text: entry.description ?? '–' }),
    el('td', {}, statusBadge(entry.status)),
  ));

  fillTable($('#table-calendar-day tbody'), rows, 4, t('ot.empty', 'No bookings in this period.'));
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
  fillMyTimezone();
}

// --------------------------------------------------------------- API tokens

async function loadTokens() {
  const tokens = (await api('/me/tokens'))?.items ?? [];

  const rows = tokens.map((token) => el('tr', { class: token.expired ? 'empty' : '' },
    el('td', { text: token.name }),
    el('td', {}, el('code', { text: `${token.prefix}…` })),
    el('td', { text: fmtDate(token.createdAt) }),
    el('td', { text: token.expiresAt ? fmtDate(token.expiresAt) : t('token.never', 'unlimited') }),
    el('td', { text: token.lastUsedAt ? fmtDate(token.lastUsedAt) : t('token.unused', 'never') }),
    el('td', { class: 'actions' }, el('button', {
      class: 'link danger',
      text: t('token.revoke', 'revoke'),
      onclick: () => remove(`/me/tokens/${token.id}`, t('token.revoked', 'Token revoked'), loadTokens),
    })),
  ));

  fillTable($('#table-tokens tbody'), rows, 6, t('token.empty', 'No tokens yet.'));
}

function wireTokens() {
  $('#form-token').addEventListener('submit', (e) => {
    e.preventDefault();

    const raw = formData(e.target);
    const body = { name: raw.name, expiresInDays: Number(raw.expiresInDays ?? 0) };

    mutate(async () => {
      const created = await api('/me/tokens', { method: 'POST', body: JSON.stringify(body) });

      // The secret exists only in this response, so it is shown until the
      // user navigates away rather than in a toast that disappears.
      $('#token-secret-value').textContent = created.secret;
      $('#token-secret').hidden = false;
      e.target.reset();
      await loadTokens();
    }, null, null);
  });
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

  document.title = branding.title || 'Time Recording';
  $('#app-title').textContent = branding.title || 'Time Recording';

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

  await loadOperational();

  const timezone = await api('/settings/timezone');
  const instanceSelect = $('#instance-timezone');

  fillTimezoneSelect(instanceSelect, timezone.timezone ?? 'UTC');
  showTimeIn(instanceSelect, $('#instance-timezone-now'), 'UTC');

  const ds = await api('/settings/datasource');
  const dsForm = $('#form-datasource');
  for (const field of ['dialect', 'name', 'host', 'port', 'user', 'sslMode']) {
    dsForm.elements[field].value = ds[field] ?? '';
  }

  $('#datasource-active').textContent =
    `${t('admin.activeConnection', 'Currently connected via')}: ${ds.active}`;

  const ldap = await api('/settings/ldap');
  const ldapForm = $('#form-ldap');
  const ldapFields = [
    'host', 'baseDn', 'bindDn', 'userFilter',
    'nameAttribute', 'emailAttribute', 'idAttribute',
  ];
  for (const field of ldapFields) {
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
      toast(t('admin.logoTooBig', 'The logo must be smaller than 256 KB.'), 'error');
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
      t('admin.saved', 'Settings saved'),
      async () => { await loadBranding(); await loadAdmin(); });
  });

  $('#form-datasource').addEventListener('submit', (e) => {
    e.preventDefault();
    const body = formData(e.target);
    mutate(async () => {
      const result = await api('/settings/datasource', { method: 'PUT', body: JSON.stringify(body) });
      toast(result.message ?? t('admin.restartNeeded', 'Saved. Applied on the next start.'), 'ok');
    }, null, loadAdmin);
  });

  $('#form-ldap').addEventListener('submit', (e) => {
    e.preventDefault();
    mutate(() => api('/settings/ldap', { method: 'PUT', body: JSON.stringify(ldapPayload()) }),
      t('admin.saved', 'Settings saved'),
      loadAdmin);
  });

  $('#datasource-test').addEventListener('click', () => {
    const result = $('#datasource-test-result');
    result.textContent = t('admin.testing', 'Testing the connection …');
    result.className = 'muted';

    mutate(async () => {
      const outcome = await api('/settings/datasource/test',
        { method: 'POST', body: JSON.stringify(formData($('#form-datasource'))) });

      result.textContent = outcome.message;
      result.className = outcome.ok ? 'muted plus' : 'muted minus';
    }, null, null);
  });

  $('#ldap-test').addEventListener('click', () => {
    const result = $('#ldap-test-result');
    result.textContent = t('admin.testing', 'Testing the connection …');

    mutate(async () => {
      const outcome = await api('/settings/ldap/test',
        { method: 'POST', body: JSON.stringify(ldapPayload()) });

      result.textContent = outcome.message;
      result.className = outcome.ok ? 'muted plus' : 'muted minus';
    }, null, null);
  });
}

/**
 * Runs a directory reconciliation, or previews one.
 *
 * The real run asks for confirmation naming the exact number of accounts and
 * recorded entries at stake, because it cannot be undone.
 */
function wireDirectorySync() {
  const status = $('#sync-status');
  const result = $('#sync-result');

  const show = (report) => {
    const rows = report.candidates.map((c) => el('tr', {},
      el('td', { text: c.name }),
      el('td', { text: c.email }),
      el('td', { class: `num ${c.timesheets > 0 ? 'minus' : ''}`, text: String(c.timesheets) }),
    ));

    fillTable($('#table-sync tbody'), rows, 3, t('sync.none', 'No accounts are missing from the directory.'));
    result.hidden = false;

    if (report.aborted) {
      status.textContent = `${t('sync.aborted', 'Aborted')}: ${report.aborted}`;
      status.className = 'muted minus';

      return;
    }

    const parts = [
      `${t('sync.directoryUsers', 'In the directory')}: ${report.directoryUsers}`,
      `${t('sync.localExternal', 'Local, from the directory')}: ${report.localExternal}`,
    ];

    if (report.dryRun) {
      parts.push(`${t('sync.wouldDelete', 'Would be deleted')}: ${report.candidates.length}`);
      parts.push(`${t('sync.wouldCreate', 'Would be created')}: ${report.created.length}`);
    } else {
      parts.push(`${t('sync.deleted', 'Deleted')}: ${report.deleted.length}`);
      parts.push(`${t('sync.created', 'Created')}: ${report.created.length}`);
    }

    status.textContent = parts.join(' · ');
    status.className = 'muted';
  };

  $('#sync-preview').addEventListener('click', () => {
    status.textContent = t('sync.running', 'Checking …');
    status.className = 'muted';

    mutate(async () => show(await api('/settings/ldap/sync/preview', { method: 'POST' })), null, null);
  });

  $('#sync-run').addEventListener('click', () => {
    mutate(async () => {
      // Preview first, so the confirmation can state the real damage instead
      // of a vague warning.
      const preview = await api('/settings/ldap/sync/preview', { method: 'POST' });
      show(preview);

      if (preview.aborted) return;

      const hours = preview.candidates.reduce((sum, c) => sum + c.timesheets, 0);

      if (preview.candidates.length > 0) {
        const question = t('sync.confirm', 'WARNING: {n} account(s) and {h} time entries will be deleted irreversibly. Continue?')
          .replace('{n}', String(preview.candidates.length))
          .replace('{h}', String(hours));

        if (!window.confirm(question)) return;
      }

      show(await api('/settings/ldap/sync', { method: 'POST' }));
      await refreshAll();
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
    idAttribute: form.elements.idAttribute.value.trim(),
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

  // Only the sign-in call itself decides whether the credentials were wrong.
  //
  // Wrapping the loading that follows in the same catch made a correct password
  // look like a wrong one: with the initial password still in place the server
  // refuses the rest of the API, refreshAll threw, and the handler put the
  // sign-in screen back with "email address or password is not correct" - on
  // the one account that had just authenticated successfully. There was no way
  // through to the screen that would have fixed it.
  let result;

  try {
    result = await api('/auth/login', { method: 'POST', body: JSON.stringify(body) });
  } catch {
    // The server deliberately does not say which part was wrong, so neither
    // does the interface.
    showLogin(t('login.failed', 'Email address or password is not correct.'));
    form.elements.password.value = '';

    return;
  }

  // The password was right but the account has a second factor: ask for the
  // code instead of reporting a failed sign-in.
  if (result.totpRequired) {
    $('#login-totp-field').hidden = false;
    $('#login-error').textContent = t('login.totpNeeded', 'Please enter the code from your authenticator app.');
    $('#login-error').hidden = false;
    form.elements.totp.focus();

    return;
  }

  hideLogin();

  try {
    await refreshAll();
    switchView(firstVisibleView());
  } catch (err) {
    // Signed in, but something behind it would not load. Staying on the
    // application with an explanation beats being thrown back to a sign-in
    // screen that will accept the same password and do this again.
    toast(`${t('msg.loadFailed', 'Could not load everything')}: ${err.message}`, 'error');
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
    ? t('totp.on', 'Two-factor authentication is enabled.')
    : t('totp.off', 'Two-factor authentication is not enabled.');

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
      t('totp.enabled', 'Two-factor authentication enabled'),
      async () => { $('#totp-code').value = ''; await refreshAll(); });
  });

  $('#totp-disable').addEventListener('click', () => {
    const code = $('#totp-code').value.trim();
    mutate(() => api(`/me/totp?code=${encodeURIComponent(code)}`, { method: 'DELETE' }),
      t('totp.disabled', 'Two-factor authentication disabled'),
      async () => { $('#totp-code').value = ''; await refreshAll(); });
  });
}

async function loadLanguages() {
  const languages = (await api('/languages'))?.languages ?? [FALLBACK_LANGUAGE];
  const picker = $('#language-picker');

  picker.replaceChildren();
  for (const language of languages) {
    picker.append(el('option', { value: language, text: language.toUpperCase() }));
  }

  // Nobody signed in yet means the picker should show what is actually on
  // screen, which is whatever the browser preference resolved to.
  picker.value = me.user?.language ?? activeLanguage();
}

// ------------------------------------------------------------- guided tour

/**
 * The tour, in the order someone meets the application.
 *
 * Each step names a real selector rather than describing a control in prose,
 * so a step whose target has been removed can be detected and dropped instead
 * of pointing at nothing. `view` switches to the tab first, because explaining
 * the calendar while the timesheet is on screen helps nobody.
 *
 * `permission` keeps the tour honest about what this person can actually do:
 * an employee has no Roles tab, and a tour that mentions one is a tour that
 * lies.
 */
const TOUR_STEPS = [
  {
    target: '#tabs',
    view: null,
    title: () => t('tour.nav.title', 'Everything lives up here'),
    text: () => t('tour.nav.text',
      'These tabs are the whole application. You only see the ones your role allows, '
      + 'so this bar is shorter for some people than for others.'),
  },
  {
    target: '#form-timesheet',
    view: 'timesheets',
    permission: 'timesheets:write:own,timesheets:write:all',
    title: () => t('tour.book.title', 'Booking time'),
    text: () => t('tour.book.text',
      'Pick a date, enter the hours, done. A project is optional — hours can be recorded '
      + 'now and sorted later.'),
  },
  {
    target: '#table-timesheets',
    view: 'timesheets',
    permission: 'timesheets:read:own,timesheets:read:all',
    title: () => t('tour.entries.title', 'Your entries'),
    text: () => t('tour.entries.text',
      'Everything you booked, filterable by person, project and status. An entry stays '
      + 'editable until you submit it for approval.'),
  },
  {
    target: '#calendar-days',
    view: 'calendar',
    permission: 'timesheets:read:own,timesheets:read:all',
    title: () => t('tour.calendar.title', 'The month at a glance'),
    text: () => t('tour.calendar.text',
      'Which days have hours on them, and how many. Click a day to see what is behind it.'),
  },
  {
    target: '#form-overtime',
    view: 'overtime',
    permission: 'timesheets:read:own,timesheets:read:all',
    title: () => t('tour.overtime.title', 'Your overtime balance'),
    text: () => t('tour.overtime.text',
      'Booked hours against your daily target. Only days with bookings count, so weekends '
      + 'and time off do not quietly pile up as a deficit.'),
  },
  {
    target: '#form-project',
    view: 'projects',
    permission: 'projects:write:own,projects:write',
    title: () => t('tour.projects.title', 'Projects, including your own'),
    text: () => t('tour.projects.text',
      'Shared projects are set up centrally. You can also create private ones, visible only '
      + 'to you, to split up a day when no shared project fits.'),
  },
  {
    target: '#form-working-times',
    view: 'settings',
    title: () => t('tour.account.title', 'My account'),
    text: () => t('tour.account.text',
      'Your daily target, timezone, two-factor authentication and API tokens. The daily '
      + 'target is what the overtime balance is measured against.'),
  },
  {
    target: '#tab-admin',
    view: null,
    adminOnly: true,
    title: () => t('tour.admin.title', 'Settings'),
    text: () => t('tour.admin.text',
      'Yours alone as the built-in administrator: appearance, database, directory, timezone '
      + 'and the operational limits.'),
  },
  {
    target: '#theme-picker',
    view: null,
    title: () => t('tour.theme.title', 'Appearance and language'),
    text: () => t('tour.theme.text',
      'Light or dark — automatic follows the time of day. The language follows your browser '
      + 'until you choose one. That is everything; enjoy.'),
  },
];

/** Where we are in the tour, and the steps that apply to this person. */
let tour = { steps: [], index: 0, active: false };

/** Drops steps this person cannot reach, so the tour never points at nothing. */
function applicableTourSteps() {
  return TOUR_STEPS.filter((step) => {
    if (step.adminOnly && !isSystemAdmin()) return false;
    if (step.permission && !can(...step.permission.split(','))) return false;

    // A target that is not in the document, or is hidden for this person, is
    // not something to explain.
    const node = $(step.target);

    return node && node.offsetParent !== null;
  });
}

/** Positions the spotlight and the bubble around one element. */
function placeTour(node) {
  const rect = node.getBoundingClientRect();
  const pad = 6;
  const spotlight = $('#tour-spotlight');
  const bubble = $('#tour-bubble');

  // Page coordinates, not viewport ones: both elements are absolutely
  // positioned in the document, so they stay put while it scrolls.
  const top = rect.top + window.scrollY - pad;
  const left = rect.left + window.scrollX - pad;

  spotlight.style.top = `${top}px`;
  spotlight.style.left = `${left}px`;
  spotlight.style.width = `${rect.width + pad * 2}px`;
  spotlight.style.height = `${rect.height + pad * 2}px`;
  spotlight.hidden = false;

  bubble.hidden = false;

  // Below the target where there is room, above it otherwise, and never off
  // the right edge on a narrow screen.
  const bubbleRect = bubble.getBoundingClientRect();
  const below = rect.bottom + 14;
  const fitsBelow = below + bubbleRect.height < window.innerHeight;

  bubble.style.top = fitsBelow
    ? `${below + window.scrollY}px`
    : `${rect.top + window.scrollY - bubbleRect.height - 14}px`;

  const maxLeft = window.scrollX + window.innerWidth - bubbleRect.width - 12;
  bubble.style.left = `${Math.max(window.scrollX + 12, Math.min(left, maxLeft))}px`;
}

async function renderTourStep() {
  const step = tour.steps[tour.index];

  if (step.view) switchView(step.view);

  const node = $(step.target);
  if (!node) {
    // The target went away between building the list and getting here.
    await advanceTour();

    return;
  }

  node.scrollIntoView({ block: 'center', behavior: 'smooth' });

  $('#tour-count').textContent = t('tour.count', 'Step {n} of {total}')
    .replace('{n}', String(tour.index + 1))
    .replace('{total}', String(tour.steps.length));
  $('#tour-title').textContent = step.title();
  $('#tour-text').textContent = step.text();

  $('#tour-back').disabled = tour.index === 0;
  $('#tour-next').textContent = tour.index === tour.steps.length - 1
    ? t('tour.finish', 'Finish')
    : t('tour.next', 'Next');

  // After the layout has settled from the view switch and the scroll, or the
  // rectangle measured would be the one from before it moved.
  requestAnimationFrame(() => requestAnimationFrame(() => placeTour(node)));
}

async function advanceTour() {
  if (tour.index >= tour.steps.length - 1) {
    await endTour();

    return;
  }

  tour.index += 1;
  await renderTourStep();
}

/** Starts the tour, from the beginning. */
async function startTour() {
  tour.steps = applicableTourSteps();

  if (tour.steps.length === 0) return;

  tour.index = 0;
  tour.active = true;
  await renderTourStep();
}

/**
 * Ends the tour and records it as seen.
 *
 * Skipping counts as seen: someone who dismissed it made a decision, and
 * greeting them with it again on the next sign-in would override that.
 */
async function endTour() {
  tour.active = false;
  $('#tour-spotlight').hidden = true;
  $('#tour-bubble').hidden = true;

  if (me.user?.tourSeen) return;

  try {
    await api('/me/tour', { method: 'PUT', body: JSON.stringify({ seen: true }) });
    me.user.tourSeen = true;
  } catch {
    // Worst case it is offered again next time, which is a small annoyance
    // and not worth an error message over.
  }
}

function wireTour() {
  $('#tour-next').addEventListener('click', advanceTour);

  $('#tour-back').addEventListener('click', async () => {
    if (tour.index > 0) {
      tour.index -= 1;
      await renderTourStep();
    }
  });

  $('#tour-end').addEventListener('click', endTour);

  // Escape is what people press to get out of a modal, and a tour that traps
  // someone is worse than no tour.
  document.addEventListener('keydown', (e) => {
    if (tour.active && e.key === 'Escape') endTour();
  });

  // The highlight is drawn from a measured rectangle, so it has to be redrawn
  // when the layout changes underneath it.
  for (const event of ['resize', 'scroll']) {
    window.addEventListener(event, () => {
      if (!tour.active) return;

      const node = $(tour.steps[tour.index].target);
      if (node) placeTour(node);
    }, { passive: true });
  }

  $('#tour-restart').addEventListener('click', startTour);
}

/**
 * Offers the tour on a first sign-in.
 *
 * Never while the setup wizard is up: being walked through the application by
 * two things at once is worse than either alone, and the wizard is the one
 * that has to happen first.
 */
async function maybeStartTour() {
  if (!me.user || me.user.tourSeen) return;
  if (!$('#setup-wizard').hidden) return;
  if (me.user.mustChangePassword) return;

  await startTour();
}

// ------------------------------------------------------------ setup wizard

/**
 * What each step says and does.
 *
 * The server decides which steps exist and which are done; this only supplies
 * the words and the form. Keeping the two apart means a step cannot quietly
 * complete because the interface thinks it should have.
 *
 * `fields` renders inside the wizard for the things worth settling on the spot.
 * `tab` sends the administrator to the real screen for the rest - duplicating
 * the LDAP form here would mean two forms to keep in step, and the second one
 * always drifts.
 */
const SETUP_STEPS = {
  password: {
    title: () => t('setup.password.title', 'Change the administrator password'),
    text: () => t('setup.password.text',
      'This account still has the password from the documentation, which anyone can look up. '
      + 'Nothing else can be used until it is changed.'),
    fields: () => [
      passwordField('currentPassword', t('settings.currentPassword', 'Current password'), 'changeme123'),
      passwordField('newPassword', t('settings.newPassword', 'New password'), ''),
    ],
    submit: async (values) => {
      await api('/me/password', {
        method: 'PUT',
        body: JSON.stringify({
          currentPassword: values.currentPassword,
          newPassword: values.newPassword,
        }),
      });

      // The server ends every session on a password change, so staying on the
      // wizard would only produce 401s on the next step.
      return { signOut: true };
    },
  },

  timezone: {
    title: () => t('setup.timezone.title', 'Choose the timezone'),
    text: () => t('setup.timezone.text',
      'This decides which calendar day a booking falls on. Left unset it is UTC, '
      + 'which puts evening bookings on the wrong day for most of the world.'),
    fields: () => {
      const select = el('select', { name: 'timezone' });
      fillTimezoneSelect(select, guessTimezone());

      return [el('label', { text: t('tz.timezone', 'Timezone') }, select)];
    },
    submit: async (values) => {
      await api('/settings/timezone', {
        method: 'PUT', body: JSON.stringify({ timezone: values.timezone }),
      });
    },
  },

  database: {
    title: () => t('setup.database.title', 'Choose the database'),
    text: () => t('setup.database.text',
      'This comes first because everything else is stored in the database chosen here. '
      + 'Switching later points the application at an empty one: the password, the timezone '
      + 'and the title stay behind in the old database, and this installation comes back up '
      + 'with the initial password from the documentation.'),
    fields: () => [
      el('p', {
        class: 'muted',
        text: t('setup.database.current', 'Currently running on: {dialect}')
          .replace('{dialect}', setup.activeDialect ?? 'sqlite'),
      }),
      el('div', { class: 'row' },
        el('button', {
          type: 'button',
          class: 'secondary',
          text: t('setup.database.keep', 'Stay on this database'),
          onclick: keepDatabaseAndAdvance,
        }),
        el('button', {
          type: 'button',
          text: t('setup.database.configure', 'Configure another database'),
          onclick: () => {
            // The real form rather than a copy of it: the connection screen
            // already tests a connection before committing to it, which is the
            // part that matters most here.
            $('#setup-wizard').hidden = true;
            switchView('admin');
            toast(t('setup.database.gotoHint',
              'Configure the connection, then restart. The wizard continues in the new database.'), 'ok');
          },
        })),
    ],
  },

  branding: {
    title: () => t('setup.branding.title', 'Name this installation'),
    text: () => t('setup.branding.text',
      'The title appears in the browser tab and the header. Optional, but it is what tells '
      + 'a test instance apart from the real one at a glance.'),
    fields: () => [
      el('label', { text: t('admin.title', 'Title (browser tab and header)') },
        el('input', { type: 'text', name: 'title', maxlength: '80' })),
    ],
    submit: async (values) => {
      if (!values.title?.trim()) return;

      const branding = await api('/branding');
      await api('/settings/branding', {
        method: 'PUT', body: JSON.stringify({ ...branding, title: values.title.trim() }),
      });
      await loadBranding();
    },
  },

  directory: {
    title: () => t('setup.directory.title', 'Connect a directory (optional)'),
    text: () => t('setup.directory.text',
      'With LDAP or Active Directory connected, people sign in with the account they already have '
      + 'and no passwords are kept here. Skip this to manage accounts locally.'),
    tab: 'admin',
  },
};

/** A labelled password field for the wizard's own forms. */
function passwordField(name, label, placeholder) {
  return el('label', { text: label },
    el('input', { type: 'password', name, placeholder, autocomplete: 'off' }));
}

/** The browser's own zone, as the opening suggestion. */
function guessTimezone() {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
}

/** Server state, where in it we are, and what this process is running on. */
let setup = { state: null, index: 0, activeDialect: null };

/** Confirms the current database and moves on. */
async function keepDatabaseAndAdvance() {
  try {
    setup.state = await api('/setup/keep-database', { method: 'POST' });
  } catch (err) {
    setupError(err.message);

    return;
  }

  setup.index += 1;
  renderSetup();
}

/** Loads the wizard state and shows it if anything is outstanding. */
async function loadSetup() {
  if (!isSystemAdmin()) {
    $('#setup-wizard').hidden = true;

    return;
  }

  try {
    setup.state = await api('/setup');

    // Which database this process actually connected to, so the step can say
    // what "stay on this one" means rather than leaving it to be guessed.
    setup.activeDialect = (await api('/settings/datasource'))?.active ?? null;
  } catch {
    // Not fatal: an installation is usable without the wizard, and failing
    // here would block the interface over a hint.
    $('#setup-wizard').hidden = true;

    return;
  }

  if (!shouldShowSetup(setup.state)) {
    $('#setup-wizard').hidden = true;

    return;
  }

  // Open on the first thing still to do rather than always at step one, so
  // coming back does not mean clicking through what is already settled.
  setup.index = Math.max(0, setup.state.steps.findIndex((s) => !s.done));
  renderSetup();
  $('#setup-wizard').hidden = false;
}

/**
 * Mirrors the server's rule: dismissing the wizard settles the optional steps,
 * not the required ones. Anything required and undone brings it back.
 */
function shouldShowSetup(state) {
  const outstanding = state.steps.some((s) => s.required && !s.done);

  return !state.completed || outstanding;
}

function renderSetup() {
  const { state, index } = setup;
  const step = state.steps[index];
  const definition = SETUP_STEPS[step.id];

  const done = state.steps.filter((s) => s.done).length;
  $('#setup-progress').textContent = t('setup.progress', 'Step {n} of {total} — {done} done')
    .replace('{n}', String(index + 1))
    .replace('{total}', String(state.steps.length))
    .replace('{done}', String(done));

  $('#setup-steps').replaceChildren(...state.steps.map((s, i) => el('li', {
    class: [s.done ? 'done' : '', s.required ? 'required' : '', i === index ? 'current' : '']
      .filter(Boolean).join(' '),
    text: SETUP_STEPS[s.id]?.title() ?? s.id,
  })));

  $('#setup-step-title').textContent = definition.title();
  $('#setup-step-text').textContent = definition.text();
  $('#setup-error').hidden = true;

  const fields = el('div', {});
  if (!step.done && definition.fields) fields.append(...definition.fields());
  $('#setup-step-fields').replaceChildren(fields);

  $('#setup-back').disabled = index === 0;

  const last = index === state.steps.length - 1;
  $('#setup-next').textContent = last
    ? t('setup.finish', 'Finish')
    : t('setup.next', 'Next');

  // A required step cannot be skipped; that is what "required" means here.
  $('#setup-skip').hidden = step.required && !step.done;
  $('#setup-skip').textContent = definition.tab
    ? t('setup.openSettings', 'Open Settings instead')
    : t('setup.skip', 'Skip this step');
}

function setupError(message) {
  const box = $('#setup-error');
  box.textContent = message;
  box.hidden = false;
}

/** Reads the wizard's own fields. */
function setupValues() {
  const values = {};
  for (const input of $$('#setup-step-fields input, #setup-step-fields select')) {
    values[input.name] = input.value;
  }

  return values;
}

async function advanceSetup() {
  const step = setup.state.steps[setup.index];
  const definition = SETUP_STEPS[step.id];

  // A required step with no form of its own - the database question - would
  // otherwise be walked straight past by the Next button, which is exactly the
  // skipping this ordering exists to prevent.
  if (step.required && !step.done && !definition.submit) {
    setupError(t('setup.decideFirst', 'Please settle this step before continuing.'));

    return;
  }

  if (!step.done && definition.submit) {
    try {
      const result = await definition.submit(setupValues());

      if (result?.signOut) {
        // Changing the password invalidates this session, so the only correct
        // next screen is the sign-in one.
        toast(t('setup.passwordChanged', 'Password changed. Please sign in again.'), 'ok');
        $('#setup-wizard').hidden = true;
        await doLogout();

        return;
      }
    } catch (err) {
      setupError(err.message);

      return;
    }
  }

  await nextSetupStep();
}

/** Re-reads the state, so a step completed elsewhere is reflected too. */
async function nextSetupStep() {
  const wasLast = setup.index >= setup.state.steps.length - 1;
  const current = setup.state.steps[setup.index];

  setup.state = await api('/setup');

  // The server is the judge of whether the step took. A submit that returned
  // without error but left the step outstanding - a blank title, a connection
  // that was not actually saved - must not be walked past.
  const after = setup.state.steps[setup.index];

  if (current.required && !after.done) {
    setupError(t('setup.decideFirst', 'Please settle this step before continuing.'));
    renderSetup();

    return;
  }

  if (wasLast) {
    await finishSetup();

    return;
  }

  setup.index += 1;
  renderSetup();
}

async function finishSetup() {
  try {
    await api('/setup/complete', { method: 'POST' });
  } catch (err) {
    setupError(err.message);

    return;
  }

  $('#setup-wizard').hidden = true;
  toast(t('setup.done', 'Setup complete.'), 'ok');
  await refreshAll();
}

function wireSetup() {
  $('#setup-next').addEventListener('click', advanceSetup);

  $('#setup-back').addEventListener('click', () => {
    if (setup.index > 0) {
      setup.index -= 1;
      renderSetup();
    }
  });

  $('#setup-skip').addEventListener('click', async () => {
    const step = setup.state.steps[setup.index];
    const definition = SETUP_STEPS[step.id];

    if (definition.tab) {
      // Sends them to the real screen rather than a copy of it, and gets out
      // of the way so they can use it.
      $('#setup-wizard').hidden = true;
      switchView(definition.tab);

      return;
    }

    await nextSetupStep();
  });

  $('#setup-dismiss').addEventListener('click', async () => {
    const outstanding = setup.state.steps.filter((s) => s.required && !s.done);

    if (outstanding.length > 0) {
      setupError(t('setup.stillRequired',
        'These steps have to be settled first; the wizard will keep coming back until they are.'));

      return;
    }

    await finishSetup();
  });
}

// ------------------------------------------------------- operation & limits

/** The fields of the operational form, in the order they appear. */
const OPERATIONAL_FIELDS = [
  'sessionLifetimeHours', 'maxDailyHours', 'rateLimit',
  'rateLimitWindowSeconds', 'autoCloseAfterDays', 'ldapSyncMaxDeleteRatio',
];

/**
 * Fills the operational form.
 *
 * An overridden value goes in the field; anything still following the
 * configuration file leaves the field empty and shows that file's value as the
 * placeholder. Prefilling every field instead would turn "not set here" into a
 * deliberate-looking choice the moment anyone pressed Save.
 */
function fillOperationalForm(data) {
  const form = $('#form-operational');

  for (const field of OPERATIONAL_FIELDS) {
    const input = form.elements[field];
    const override = data.configured?.[field];

    input.value = override ?? '';
    input.placeholder = String(data.defaults?.[field] ?? '');
  }

  const effective = data.effective ?? {};
  $('#operational-effective').textContent = `${t('ops.effective', 'Currently in force')}: `
    + `${t('ops.sessionShort', 'session')} ${effective.sessionLifetimeHours} h, `
    + `${t('ops.maxShort', 'max/day')} ${effective.maxDailyHours} h, `
    + `${t('ops.rateShort', 'rate')} ${effective.rateLimit}/${effective.rateLimitWindowSeconds} s, `
    + `${t('ops.autoShort', 'auto-submit')} ${effective.autoCloseAfterDays} d, `
    + `${t('ops.ratioShort', 'delete limit')} ${effective.ldapSyncMaxDeleteRatio}`;
}

/** Reads the form, omitting empty fields so they keep following the file. */
function operationalPayload() {
  const form = $('#form-operational');
  const body = {};

  for (const field of OPERATIONAL_FIELDS) {
    const raw = form.elements[field].value.trim();
    if (raw !== '') body[field] = Number(raw);
  }

  return body;
}

function wireOperational() {
  $('#form-operational').addEventListener('submit', (e) => {
    e.preventDefault();
    mutate(
      () => api('/settings/operational', {
        method: 'PUT', body: JSON.stringify(operationalPayload()),
      }),
      t('ops.saved', 'Limits saved'),
      loadOperational);
  });

  $('#operational-reset').addEventListener('click', () => {
    mutate(
      () => api('/settings/operational', { method: 'PUT', body: JSON.stringify({}) }),
      t('ops.reset.done', 'All values follow the configuration file again'),
      loadOperational);
  });
}

async function loadOperational() {
  fillOperationalForm(await api('/settings/operational'));
}

// ---------------------------------------------------------------- passkeys

/**
 * WebAuthn speaks ArrayBuffers; JSON speaks strings. Everything crossing that
 * boundary is base64url, and getting one of these backwards produces a
 * signature that verifies against nothing, with no useful error - so they live
 * here, once, rather than inline at four call sites.
 */
function b64urlToBytes(value) {
  const padded = value.replace(/-/g, '+').replace(/_/g, '/')
    .padEnd(value.length + (4 - value.length % 4) % 4, '=');
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);

  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);

  return bytes.buffer;
}

function bytesToB64url(buffer) {
  const bytes = new Uint8Array(buffer);
  let binary = '';

  for (const byte of bytes) binary += String.fromCharCode(byte);

  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

/** Whether this browser and this connection can do passkeys at all. */
let passkeysAvailable = false;

/**
 * Asks the server whether to offer passkeys.
 *
 * Two conditions have to hold: the browser has to implement WebAuthn, and the
 * connection has to be a secure context. Chrome does not define
 * PublicKeyCredential at all over plain HTTP, so the first check covers the
 * second - but the server knows why, and can be asked before showing a button
 * that could only fail.
 */
async function loadPasskeySupport() {
  if (!window.PublicKeyCredential) {
    passkeysAvailable = false;

    return;
  }

  try {
    passkeysAvailable = Boolean((await api('/auth/passkey'))?.available);
  } catch {
    passkeysAvailable = false;
  }

  $('#login-passkey').hidden = !passkeysAvailable;
}

/** Registers a new credential for the signed-in user. */
async function registerPasskey(name) {
  const started = await api('/me/passkeys/register', { method: 'POST' });
  const options = started.options.publicKey;

  // The server sends base64url because JSON cannot hold bytes; the browser
  // wants the bytes back.
  options.challenge = b64urlToBytes(options.challenge);
  options.user.id = b64urlToBytes(options.user.id);

  for (const credential of options.excludeCredentials ?? []) {
    credential.id = b64urlToBytes(credential.id);
  }

  const created = await navigator.credentials.create({ publicKey: options });
  if (!created) throw new Error(t('passkey.cancelled', 'The passkey was not created.'));

  return api('/me/passkeys/register', {
    method: 'PUT',
    body: JSON.stringify({
      token: started.token,
      name,
      credential: {
        id: created.id,
        rawId: bytesToB64url(created.rawId),
        type: created.type,
        response: {
          clientDataJSON: bytesToB64url(created.response.clientDataJSON),
          attestationObject: bytesToB64url(created.response.attestationObject),
        },
        // What the device is: a phone, a security key, the laptop itself. The
        // server keeps it so the next prompt can ask for the right thing.
        transports: created.response.getTransports?.() ?? [],
      },
    }),
  });
}

/**
 * Signs in with a passkey, without a username.
 *
 * The browser offers whichever credentials it holds for this site, and the one
 * that signs names its own owner - so there is nothing to type. That is the
 * point: a password never typed cannot be phished, reused, or read out of
 * somebody else's breach.
 */
async function signInWithPasskey() {
  const started = await api('/auth/passkey/login', { method: 'POST' });
  const options = started.options.publicKey;

  options.challenge = b64urlToBytes(options.challenge);

  for (const credential of options.allowCredentials ?? []) {
    credential.id = b64urlToBytes(credential.id);
  }

  const assertion = await navigator.credentials.get({ publicKey: options });
  if (!assertion) throw new Error(t('passkey.cancelled', 'The passkey was not used.'));

  return api('/auth/passkey/login', {
    method: 'PUT',
    body: JSON.stringify({
      token: started.token,
      credential: {
        id: assertion.id,
        rawId: bytesToB64url(assertion.rawId),
        type: assertion.type,
        response: {
          clientDataJSON: bytesToB64url(assertion.response.clientDataJSON),
          authenticatorData: bytesToB64url(assertion.response.authenticatorData),
          signature: bytesToB64url(assertion.response.signature),
          userHandle: assertion.response.userHandle
            ? bytesToB64url(assertion.response.userHandle)
            : null,
        },
      },
    }),
  });
}

async function loadPasskeys() {
  const card = $('#passkey-card');

  // The built-in administrator keeps its password on purpose, so it is never
  // offered a passkey it might then come to rely on.
  card.hidden = !passkeysAvailable || !me.user || me.user.isSystem;

  if (card.hidden) return;

  const passkeys = (await api('/me/passkeys'))?.items ?? [];

  const rows = passkeys.map((passkey) => el('tr', {},
    el('td', { text: passkey.name }),
    el('td', { text: fmtDate(passkey.createdAt) }),
    el('td', {
      text: passkey.lastUsedAt ? fmtDate(passkey.lastUsedAt) : t('passkey.never', 'never'),
    }),
    el('td', { class: 'actions' }, el('button', {
      class: 'link danger',
      text: t('action.delete', 'delete'),
      onclick: () => remove(`/me/passkeys/${passkey.id}`,
        t('passkey.removed', 'Passkey removed'), loadPasskeys),
    })),
  ));

  fillTable($('#table-passkeys tbody'), rows, 4,
    t('passkey.empty', 'No passkeys yet. Add one to sign in without a password.'));
}

function wirePasskeys() {
  $('#form-passkey').addEventListener('submit', (e) => {
    e.preventDefault();

    const field = e.target.elements.name;

    mutate(() => registerPasskey(field.value.trim()),
      t('passkey.added.done', 'Passkey added'),
      async () => { field.value = ''; await loadPasskeys(); });
  });

  $('#login-passkey').addEventListener('click', async () => {
    try {
      await signInWithPasskey();
    } catch (err) {
      // A cancelled prompt and a rejected credential are indistinguishable
      // from here, so the message covers both without guessing.
      showLogin(err.message || t('passkey.failed', 'The passkey was not accepted.'));

      return;
    }

    hideLogin();

    try {
      await refreshAll();
      switchView(firstVisibleView());
    } catch (err) {
      toast(`${t('msg.loadFailed', 'Could not load everything')}: ${err.message}`, 'error');
    }
  });
}

// ---------------------------------------------------------------- timezone

/**
 * A short list to fall back on where the browser cannot enumerate zones.
 *
 * Deliberately not a curated "important zones" list, which would be a
 * permanent maintenance burden and would still be wrong for someone: it exists
 * only so the picker is never empty, and the server accepts any valid name.
 */
const FALLBACK_TIMEZONES = [
  'UTC', 'Europe/London', 'Europe/Berlin', 'Europe/Zurich', 'Europe/Vienna',
  'Europe/Moscow', 'America/New_York', 'America/Chicago', 'America/Denver',
  'America/Los_Angeles', 'America/Sao_Paulo', 'Asia/Dubai', 'Asia/Kolkata',
  'Asia/Shanghai', 'Asia/Tokyo', 'Australia/Sydney', 'Pacific/Auckland',
];

/** Every zone this browser knows, so the picker matches the tz database. */
function availableTimezones() {
  try {
    const supported = Intl.supportedValuesOf?.('timeZone');
    if (supported?.length) return supported;
  } catch {
    // Older browsers have no supportedValuesOf; the fallback covers them.
  }

  return FALLBACK_TIMEZONES;
}

/** The current time in a zone, so a choice can be checked at a glance. */
function timeIn(timeZone) {
  try {
    return new Intl.DateTimeFormat(activeLanguage(), {
      timeZone, dateStyle: 'medium', timeStyle: 'short',
    }).format(new Date());
  } catch {
    return '';
  }
}

/**
 * Fills a zone picker.
 *
 * inheritLabel, when given, adds an empty option meaning "follow the instance
 * setting". That option is the point of the personal picker: without it every
 * account would be pinned to a zone the moment someone opened the screen, and
 * changing the instance zone would then move nobody.
 */
function fillTimezoneSelect(select, selected, inheritLabel) {
  const zones = availableTimezones();

  select.replaceChildren();

  if (inheritLabel !== undefined) {
    select.append(el('option', { value: '', text: inheritLabel }));
  }

  for (const zone of zones) {
    select.append(el('option', { value: zone, text: zone }));
  }

  // A stored zone this browser does not list would otherwise be silently
  // dropped, and saving the form would change it without anyone asking.
  if (selected && !zones.includes(selected)) {
    select.append(el('option', { value: selected, text: selected }));
  }

  select.value = selected ?? '';
}

/** Shows what time it currently is in the picked zone. */
function showTimeIn(select, output, fallbackZone) {
  const zone = select.value || fallbackZone;
  const now = timeIn(zone);

  output.textContent = now ? `${zone} — ${now}` : '';
}

/** Loads the personal zone into "My account". */
function fillMyTimezone() {
  const select = $('#my-timezone');
  const inherit = `${t('tz.inherit', 'Follow the instance setting')}`;
  const effective = me.user?.effectiveTimezone ?? 'UTC';

  fillTimezoneSelect(select, me.user?.timezone ?? '', `${inherit} (${effective})`);
  showTimeIn(select, $('#my-timezone-now'), effective);
}

function wireTimezones() {
  const mine = $('#my-timezone');
  const myNow = $('#my-timezone-now');

  mine.addEventListener('change', () => showTimeIn(mine, myNow,
    me.user?.effectiveTimezone ?? 'UTC'));

  $('#form-my-timezone').addEventListener('submit', (e) => {
    e.preventDefault();
    mutate(
      () => api('/me/timezone', {
        method: 'PUT', body: JSON.stringify({ timezone: mine.value }),
      }),
      t('tz.saved', 'Timezone saved'),
      refreshAll);
  });

  const instance = $('#instance-timezone');
  const instanceNow = $('#instance-timezone-now');

  instance.addEventListener('change', () => showTimeIn(instance, instanceNow, 'UTC'));

  $('#form-timezone').addEventListener('submit', (e) => {
    e.preventDefault();
    mutate(
      () => api('/settings/timezone', {
        method: 'PUT', body: JSON.stringify({ timezone: instance.value }),
      }),
      t('tz.saved', 'Timezone saved'),
      refreshAll);
  });
}

// ------------------------------------------------------------------- theme

/**
 * How often "automatic" re-checks the clock.
 *
 * A dashboard left open across the evening should follow the day rather than
 * stay bright until someone reloads it. A minute is far more often than it can
 * matter and costs nothing.
 */
const THEME_RECHECK_MS = 60_000;

/** Stores the choice and applies it, keeping theme.js as the single authority. */
function setThemePreference(preference) {
  const theme = window.gtrTheme;

  document.documentElement.dataset.themePreference = preference;
  document.documentElement.dataset.theme = theme.resolve(preference);

  try {
    if (preference === 'auto') localStorage.removeItem(theme.STORAGE_KEY);
    else localStorage.setItem(theme.STORAGE_KEY, preference);
  } catch {
    // Storage can be refused; the choice still holds for this page.
  }
}

/** Wires the appearance picker and keeps "automatic" honest as the day passes. */
function wireTheme() {
  const picker = $('#theme-picker');
  const theme = window.gtrTheme;

  picker.value = theme.stored();
  picker.addEventListener('change', (e) => setThemePreference(e.target.value));

  setInterval(() => {
    if (document.documentElement.dataset.themePreference !== 'auto') return;

    const now = theme.resolve('auto');
    if (now !== document.documentElement.dataset.theme) {
      document.documentElement.dataset.theme = now;
    }
  }, THEME_RECHECK_MS);
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

  // Only now is the applicable zone known, so the booking date is set here
  // rather than at start-up, where it would still be the browser's guess.
  resetBookingDate();

  // Before every other loader, because the server blocks the rest of the API
  // while the initial password stands - and that is exactly when the wizard is
  // most needed. /setup deliberately stays reachable through that block.
  await loadSetup();

  await loadLanguages();

  // With the initial password still in place the server refuses everything
  // below, so asking would only produce a screenful of errors. What is left -
  // the banner, My account, the wizard - is exactly what is needed to get past
  // it, and My account is where that happens.
  if (me.user?.mustChangePassword) {
    fillSettingsForm();
    switchView('settings');

    return;
  }

  // Roles first: the user table renders a role picker from them.
  await loadRoles();
  await Promise.all([loadUsers(), loadProjects()]);
  await loadTimesheets();
  // After users and projects, so the calendar can resolve names.
  await loadCalendar();
  await loadAdmin();
  await loadTokens();
  await loadPasskeys();
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
      t('msg.userCreated', 'Staff member created'),
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

    mutate(() => request, id ? t('msg.roleSaved', 'Role saved') : t('msg.roleCreated', 'Role created'),
      async () => { resetRoleForm(); await refreshAll(); });
  });

  $('#role-reset').addEventListener('click', resetRoleForm);

  $('#form-project').addEventListener('submit', (e) => {
    e.preventDefault();
    const body = formData(e.target);
    mutate(() => api('/projects', { method: 'POST', body: JSON.stringify(body) }),
      t('msg.projectCreated', 'Project created'),
      async () => { e.target.reset(); await refreshAll(); });
  });

  // A personal category is the same endpoint with "private", which is what
  // makes the caller its owner.
  $('#form-category').addEventListener('submit', (e) => {
    e.preventDefault();
    const body = { ...formData(e.target), private: true };
    mutate(() => api('/projects', { method: 'POST', body: JSON.stringify(body) }),
      t('msg.categoryCreated', 'Project created'),
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
      t('msg.booked', 'Time booked'),
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
      fillTable($('#table-report tbody'), rows, 2, t('ot.empty', 'No bookings in this period.'));
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
      fillTable($('#table-overtime tbody'), rows, 4, t('ot.empty', 'No bookings in this period.'));

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
      t('msg.workingTimesSaved', 'Working hours saved'),
      refreshAll);
  });

  $('#form-password').addEventListener('submit', (e) => {
    e.preventDefault();
    const body = formData(e.target);
    mutate(() => api('/me/password', { method: 'PUT', body: JSON.stringify(body) }),
      t('msg.passwordChanged', 'Password changed. Please sign in again.'),
      async () => {
        e.target.reset();

        // The server ends every session of this user on a password change, so
        // reloading would only produce 401s and leave a dead screen. The
        // message already says to sign in again; this is what takes them
        // there.
        await doLogout();
      });
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
  fillTable($('#table-overtime-team tbody'), rows, 4, t('ot.empty', 'No bookings in this period.'));
  $('#overtime-team-card').hidden = false;
}

async function init() {
  // The sign-in form is wired first and on its own: if anything below fails,
  // the user must still be able to sign in rather than face a form whose
  // submit handler was never attached, which would silently reload the page.
  $('#form-login').addEventListener('submit', submitLogin);
  enhancePasswordFields();

  // Appearance is a device setting and needs no session, so the picker works on
  // the sign-in screen too.
  wireTheme();

  // Branding is public, so the sign-in screen already carries the instance's
  // own title and logo. A failure here must not block signing in.
  try {
    await loadBranding();
  } catch {
    // Falls back to the built-in title.
  }

  // Applied before the first render so the sign-in screen already speaks the
  // browser language; a signed-in user overrides it in loadMe.
  applyLanguage(activeLanguage());

  // Before the sign-in screen is shown, so its passkey button appears with it
  // rather than popping in afterwards.
  await loadPasskeySupport();

  try {
    wireForms();
    wireTOTP();
    wireCalendar();
    wireAdmin();
    wireTokens();
    wireDirectorySync();
    wireTimezones();
    wireOperational();
    wireSetup();
    wireTour();
    wirePasskeys();
    $('#logout').addEventListener('click', doLogout);
    $('#language-picker').addEventListener('change', (e) => mutate(
      () => api('/me/language', { method: 'PUT', body: JSON.stringify({ language: e.target.value }) }),
      null,
      refreshAll));

    resetBookingDate();
  } catch (err) {
    toast(`${t('msg.initFailed', 'Initialisation failed')}: ${err.message}`, 'error');
  }

  try {
    await refreshAll();
    hideLogin();
    switchView(firstVisibleView());

    // After the first view is up, so the tour highlights something that is
    // actually on screen.
    await maybeStartTour();
  } catch {
    // No usable session: the sign-in screen is the whole interface until
    // there is one.
    showLogin();
  }
}

document.addEventListener('DOMContentLoaded', init);
