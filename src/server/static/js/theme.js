// theme.js - instant-preview enhancement only (AI.md PART 16 "Theme Toggle").
// The real POST still happens on submit, so the cookie is always set
// server-side and the toggle works identically with JavaScript disabled.
// The next mode is recomputed from the LIVE <html> class on every click rather
// than from the form's hidden value, which is rendered once at page load and
// goes stale after the first JS-driven switch.
(function () {
  'use strict';

  var THEME_CYCLE = ['dark', 'light', 'auto'];

  // "auto" is a real class in this project (html.theme-auto carries the
  // prefers-color-scheme rules), so it is applied rather than cleared.
  function currentTheme() {
    var root = document.documentElement;
    if (root.classList.contains('theme-light')) return 'light';
    if (root.classList.contains('theme-auto')) return 'auto';
    return 'dark';
  }

  document.querySelectorAll('.theme-toggle-form').forEach(function (form) {
    form.addEventListener('submit', function () {
      // No preventDefault(): the form still submits normally so the server
      // sets the cookie and re-renders with the correct next target.
      var next = THEME_CYCLE[(THEME_CYCLE.indexOf(currentTheme()) + 1) % THEME_CYCLE.length];
      document.documentElement.classList.remove('theme-dark', 'theme-light', 'theme-auto');
      document.documentElement.classList.add('theme-' + next);
    });
  });
})();
