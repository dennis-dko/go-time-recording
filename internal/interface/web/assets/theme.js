'use strict';

/**
 * Decides light or dark before the page is painted.
 *
 * This is a separate file loaded synchronously in <head> rather than part of
 * app.js, for two reasons. It has to run before the first paint or the page
 * flashes in the wrong colours, and the Content-Security-Policy forbids inline
 * script - so the usual one-liner in a <script> tag is not available here.
 *
 * Keep it small: everything in this file blocks rendering.
 */
(function applyThemeEarly() {
  /**
   * Where the choice is kept. Appearance belongs to the device rather than the
   * account: someone who wants dark on a laptop at night wants it whoever is
   * signed in.
   */
  var STORAGE_KEY = 'gtr.theme';

  /**
   * Dark from dusk to dawn, in local time - that is what "evening" means to the
   * person looking at the screen.
   */
  var DARK_FROM_HOUR = 19;
  var DARK_UNTIL_HOUR = 7;

  /**
   * What "auto" means at a given hour.
   * @param {number} hour
   * @returns {'light'|'dark'}
   */
  function themeForHour(hour) {
    return hour >= DARK_FROM_HOUR || hour < DARK_UNTIL_HOUR ? 'dark' : 'light';
  }

  /** @returns {'auto'|'light'|'dark'} */
  function storedPreference() {
    try {
      var stored = localStorage.getItem(STORAGE_KEY);

      return stored === 'light' || stored === 'dark' ? stored : 'auto';
    } catch (e) {
      // Private browsing can refuse storage entirely; the default still works.
      return 'auto';
    }
  }

  /** @returns {'light'|'dark'} */
  function resolve(preference) {
    return preference === 'auto' ? themeForHour(new Date().getHours()) : preference;
  }

  var preference = storedPreference();

  document.documentElement.dataset.theme = resolve(preference);
  document.documentElement.dataset.themePreference = preference;

  applyStoredBranding();

  /**
   * Puts the instance's own name and mark on the page before it is painted.
   *
   * Both are configured, so both live in the database, so the page cannot know
   * either until it has asked - and asking is a request that finishes after the
   * document has been parsed and shown. Until then the tab reads "Time Recording"
   * and shows the shipped mark, whatever this installation calls itself. On a
   * reload that is a visible flicker back to a name nobody chose.
   *
   * The same trick as the theme above, for the same reason: what was true last
   * time is remembered on the device and applied before the first paint, and the
   * answer from the server corrects it a moment later if it has changed. The
   * first visit on a new device still starts from the default, which is the one
   * case where there is genuinely nothing to know yet.
   */
  function applyStoredBranding() {
    var stored;

    try {
      stored = JSON.parse(localStorage.getItem('gtr.branding') || 'null');
    } catch (e) {
      return;
    }

    if (!stored || typeof stored !== 'object') return;

    if (typeof stored.title === 'string' && stored.title) {
      document.title = stored.title;
    }

    // Only a data: URI, exactly as the interface does later: a logo that is a
    // link to somewhere else would make the tab icon a request to that
    // somewhere.
    if (typeof stored.logo !== 'string' || stored.logo.indexOf('data:image/') !== 0) {
      return;
    }

    var icons = document.querySelectorAll('link[rel~="icon"]');
    var link = document.createElement('link');

    link.rel = 'icon';
    link.href = stored.logo;
    document.head.appendChild(link);

    for (var i = 0; i < icons.length; i += 1) {
      icons[i].parentNode.removeChild(icons[i]);
    }
  }

  // Exposed so app.js reuses these rules rather than restating them.
  window.gtrTheme = {
    STORAGE_KEY: STORAGE_KEY,
    DARK_FROM_HOUR: DARK_FROM_HOUR,
    DARK_UNTIL_HOUR: DARK_UNTIL_HOUR,
    forHour: themeForHour,
    stored: storedPreference,
    resolve: resolve,
  };
})();
