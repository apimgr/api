// app.js - Minimal JavaScript per PART 17 (ONE file, no frameworks)

const appBaseURL = document.documentElement.dataset.baseUrl || window.location.origin;
const appBasePath = new URL(appBaseURL, window.location.href).pathname.replace(/\/+$/, '');

function appPath(path) {
  if (/^[a-z][a-z\d+.-]*:/i.test(path) || path.startsWith('//')) return path;
  return `${appBasePath}${path.startsWith('/') ? path : `/${path}`}`;
}

// Dynamic UI text comes from the same embedded locale files as server templates.
const appLocale = {};
const appLocaleReady = fetch(appPath(`/locales/${encodeURIComponent(document.documentElement.lang || 'en')}.json`), {
  headers: { Accept: 'application/json' },
})
  .then(response => {
    if (!response.ok) throw new Error('Locale request failed');
    return response.json();
  })
  .catch(() => fetch(appPath('/locales/en.json'), { headers: { Accept: 'application/json' } })
    .then(response => response.ok ? response.json() : {}))
  .then(messages => Object.assign(appLocale, messages))
  .catch(() => {});

function appText(key, values = {}) {
  const message = key.split('.').reduce((value, part) => value && value[part], appLocale);
  if (typeof message !== 'string') return key;
  return message.replace(/\{([a-zA-Z0-9_]+)\}/g, (placeholder, name) =>
    Object.prototype.hasOwnProperty.call(values, name) ? String(values[name]) : placeholder);
}

// ============================================================================
// CSRF double-submit token per PART 16
// ============================================================================
// The csrf_token cookie is deliberately not HttpOnly so this file can read it
// and echo the value back in the X-CSRF-Token header on frontend mutations.
function csrfToken() {
  const match = document.cookie.match(/(?:^|;\s*)csrf_token=([^;]*)/);
  return match ? decodeURIComponent(match[1]) : '';
}

// csrfHeaders merges the CSRF header into an existing header object.
function csrfHeaders(headers) {
  const merged = Object.assign({}, headers || {});
  const token = csrfToken();
  if (token) {
    merged['X-CSRF-Token'] = token;
  }
  return merged;
}

// ============================================================================
// Clipboard with feedback per PART 17
// ============================================================================
async function copyToClipboard(text, btn) {
  try {
    await navigator.clipboard.writeText(text);
    await appLocaleReady;
    const original = btn.textContent;
    btn.textContent = appText('javascript.copied');
    btn.classList.add('copied');
    setTimeout(() => {
      btn.textContent = original;
      btn.classList.remove('copied');
    }, 2000);
  } catch (err) {
    await appLocaleReady;
    showToast(appText('javascript.copy_failed'), 'danger');
  }
}

// Helper for copying from code blocks
function copyCode(btn) {
  if (btn.dataset.copy) {
    copyToClipboard(btn.dataset.copy, btn);
    return;
  }
  const codeBlock = btn.closest('.code-block');
  const code = codeBlock.querySelector('pre').textContent;
  copyToClipboard(code, btn);
}

// ============================================================================
// Toast notifications per PART 17
// ============================================================================
function showToast(message, type = 'info', duration = 3000) {
  const toast = document.createElement('div');
  toast.className = `toast toast-${type}`;
  toast.textContent = message;
  document.body.appendChild(toast);
  
  setTimeout(() => {
    toast.remove();
  }, duration);
}

// ============================================================================
// Modal helpers per PART 17 (for native <dialog>)
// ============================================================================
function openModal(id) {
  const modal = document.getElementById(id);
  if (modal) modal.showModal();
}

function closeModal(id) {
  const modal = document.getElementById(id);
  if (modal) modal.close();
}

// ============================================================================
// Form helpers per PART 17
// ============================================================================
// Delete confirmation uses the native <dialog> pattern - never confirm().
// Trigger buttons declare data-confirm-dialog="{dialog-id}"; the dialog's
// Cancel button closes via <form method="dialog"> (zero JS), and its
// Confirm button submits the real form via the HTML5 form="{form-id}"
// attribute - no message/state passed through JS at all.
document.querySelectorAll('[data-confirm-dialog]').forEach(function(btn) {
  btn.addEventListener('click', function() {
    document.getElementById(btn.dataset.confirmDialog).showModal();
  });
});

// Form validation helper
function validateForm(formId) {
  const form = document.getElementById(formId);
  if (!form) return false;
  
  const required = form.querySelectorAll('[required]');
  let valid = true;
  
  required.forEach(field => {
    if (!field.value.trim()){
      field.classList.add('is-invalid');
      valid = false;
    } else {
      field.classList.remove('is-invalid');
    }
  });
  
  return valid;
}

// ============================================================================
// Tool page helpers (CasTools specific)
// ============================================================================
function toolFormData(form) {
  const formData = new FormData(form);
  formData.delete('csrf_token');
  formData.delete('_csrf');
  return formData;
}

async function executeTool(toolId, endpoint) {
  await appLocaleReady;
  const form = document.getElementById(toolId);
  const resultDiv = document.getElementById(`${toolId}-result`);
  const submitBtn = form.querySelector('button[type="submit"]');
  const originalLabel = submitBtn.textContent;
  
  // Get form data
  const formData = toolFormData(form);
  const params = new URLSearchParams(formData);
  
  // Show loading state
  submitBtn.disabled = true;
  submitBtn.textContent = appText('javascript.processing');
  resultDiv.hidden = false;
  resultDiv.innerHTML = '<div class="spinner"></div>';

  // Make API request
  fetch(`${appPath(endpoint)}?${params}`)
    .then(response => response.text())
    .then(result => {
      resultDiv.textContent = result;
      submitBtn.disabled = false;
      submitBtn.textContent = originalLabel;
    })
    .catch(error => {
      resultDiv.textContent = appText('javascript.error', { message: error.message });
      submitBtn.disabled = false;
      submitBtn.textContent = originalLabel;
      showToast(appText('javascript.request_failed'), 'danger');
    });
}

// executeToolTemplate builds a GET URL by substituting {field} placeholders
// in a path template with the matching form field's value, for tools backed
// by path-param API routes (e.g. /api/v1/text/hash/{algorithm}/{input}).
async function executeToolTemplate(toolId, urlTemplate) {
  await appLocaleReady;
  const form = document.getElementById(toolId);
  const resultDiv = document.getElementById(form.dataset.resultId || `${toolId}-result`);
  const submitBtn = form.querySelector('button[type="submit"]');
  const originalLabel = submitBtn.textContent;
  const formData = toolFormData(form);
  let selectedTemplate = urlTemplate;
  if (form.dataset.emptyTemplate && !formData.get('ip')) {
    selectedTemplate = form.dataset.emptyTemplate;
  }
  if (form.dataset.batchTemplate && Number.parseInt(formData.get('count') || '1', 10) > 1) {
    selectedTemplate = form.dataset.batchTemplate;
  }

  const url = selectedTemplate.replace(/\{(\w+)\}/g, function(match, field) {
    const value = formData.get(field);
    return value !== null ? encodeURIComponent(value) : match;
  });

  submitBtn.disabled = true;
  submitBtn.textContent = appText('javascript.processing');
  resultDiv.hidden = false;
  resultDiv.innerHTML = '<div class="spinner"></div>';

  fetch(appPath(url))
    .then(response => response.text())
    .then(result => {
      resultDiv.textContent = result;
      submitBtn.disabled = false;
      submitBtn.textContent = originalLabel;
    })
    .catch(error => {
      resultDiv.textContent = appText('javascript.error', { message: error.message });
      submitBtn.disabled = false;
      submitBtn.textContent = originalLabel;
      showToast(appText('javascript.request_failed'), 'danger');
    });
}

// executeToolBody POSTs the value of a form's single textarea as the raw
// request body, for tools backed by raw-body POST API routes
// (e.g. /api/v1/parse/json).
async function executeToolBody(toolId, endpoint) {
  await appLocaleReady;
  const form = document.getElementById(toolId);
  const resultDiv = document.getElementById(`${toolId}-result`);
  const submitBtn = form.querySelector('button[type="submit"]');
  const originalLabel = submitBtn.textContent;
  const bodyField = form.querySelector('textarea');
  const body = bodyField.value;

  // Any other named fields (selects, checkboxes, text inputs) besides the
  // raw-body textarea are appended to the endpoint as query-string params,
  // for tools like /api/v1/dev/base64 that take ?action=&urlsafe= alongside
  // a raw request body.
  const query = new URLSearchParams();
  toolFormData(form).forEach(function(value, key) {
    if (key !== bodyField.name && value !== '') {
      query.append(key, value);
    }
  });
  const queryString = query.toString();
  const url = queryString ? `${endpoint}?${queryString}` : endpoint;

  submitBtn.disabled = true;
  submitBtn.textContent = appText('javascript.processing');
  resultDiv.hidden = false;
  resultDiv.innerHTML = '<div class="spinner"></div>';

  fetch(appPath(url), { method: 'POST', body: body })
    .then(response => response.text())
    .then(result => {
      resultDiv.textContent = result;
      submitBtn.disabled = false;
      submitBtn.textContent = originalLabel;
    })
    .catch(error => {
      resultDiv.textContent = appText('javascript.error', { message: error.message });
      submitBtn.disabled = false;
      submitBtn.textContent = originalLabel;
      showToast(appText('javascript.request_failed'), 'danger');
    });
}

// executeToolImage builds a GET URL by substituting {field} placeholders in
// a path template (like executeToolTemplate), appends any remaining form
// fields as query-string params, and renders the response as an <img> rather
// than text, for tools backed by binary-image API routes
// (e.g. /api/v1/image/placeholder/{width}/{height}?format=&bg=).
async function executeToolImage(toolId, urlTemplate) {
  await appLocaleReady;
  const form = document.getElementById(toolId);
  const resultDiv = document.getElementById(`${toolId}-result`);
  const submitBtn = form.querySelector('button[type="submit"]');
  const originalLabel = submitBtn.textContent;
  const formData = toolFormData(form);
  const consumed = new Set();

  const path = urlTemplate.replace(/\{(\w+)\}/g, function(match, field) {
    const value = formData.get(field);
    consumed.add(field);
    return value !== null ? encodeURIComponent(value) : match;
  });

  const query = new URLSearchParams();
  formData.forEach(function(value, key) {
    if (!consumed.has(key) && value !== '') {
      query.append(key, value);
    }
  });
  const queryString = query.toString();
  const url = queryString ? `${path}?${queryString}` : path;

  submitBtn.disabled = true;
  submitBtn.textContent = appText('javascript.processing');
  resultDiv.hidden = false;
  const image = document.createElement('img');
  image.src = url;
  image.alt = appText('javascript.generated_image');
  image.className = 'tool-result-image';
  resultDiv.replaceChildren(image);
  submitBtn.disabled = false;
  submitBtn.textContent = originalLabel;
}

// executeToolQueryPost POSTs a form's fields as a query string (rather than a
// JSON body), for tools backed by POST-only API routes that read their
// parameters via query string (e.g. /api/v1/validate/email?email=...).
async function executeToolQueryPost(toolId, endpoint) {
  await appLocaleReady;
  const form = document.getElementById(toolId);
  const resultDiv = document.getElementById(`${toolId}-result`);
  const submitBtn = form.querySelector('button[type="submit"]');
  const originalLabel = submitBtn.textContent;
  const formData = toolFormData(form);
  const params = new URLSearchParams(formData);

  submitBtn.disabled = true;
  submitBtn.textContent = appText('javascript.processing');
  resultDiv.hidden = false;
  resultDiv.innerHTML = '<div class="spinner"></div>';

  fetch(`${appPath(endpoint)}?${params}`, { method: 'POST' })
    .then(response => response.text())
    .then(result => {
      resultDiv.textContent = result;
      submitBtn.disabled = false;
      submitBtn.textContent = originalLabel;
    })
    .catch(error => {
      resultDiv.textContent = appText('javascript.error', { message: error.message });
      submitBtn.disabled = false;
      submitBtn.textContent = originalLabel;
      showToast(appText('javascript.request_failed'), 'danger');
    });
}

// ============================================================================
// Legacy per-page tool handlers (migrated from inline onsubmit/<script>
// blocks so pages carry zero inline JS; the API call shape is unchanged)
// ============================================================================
async function executeDateTimeTool() {
  await appLocaleReady;
  const form = document.getElementById('datetime-form');
  const timezone = form.timezone.value;
  const resultDiv = document.getElementById('datetime-result');
  const submitBtn = form.querySelector('button[type="submit"]');
  const originalLabel = submitBtn.textContent;

  submitBtn.disabled = true;
  submitBtn.textContent = appText('javascript.loading');
  resultDiv.hidden = false;
  resultDiv.innerHTML = '<div class="spinner"></div>';

  // The handler reads the query key `timezone`, matching its path parameter;
  // the older `tz` key was never read, so the field silently did nothing.
  const url = appPath(timezone ? `/api/v1/datetime/now?timezone=${encodeURIComponent(timezone)}` : '/api/v1/datetime/now');

  fetch(url)
    .then(response => response.json())
    .then(data => {
      const output = document.createElement('pre');
      output.textContent = JSON.stringify(data, null, 2);
      resultDiv.replaceChildren(output);
      submitBtn.disabled = false;
      submitBtn.textContent = originalLabel;
      addToHistory('datetime-now');
    })
    .catch(error => {
      resultDiv.textContent = appText('javascript.error', { message: error.message });
      submitBtn.disabled = false;
      submitBtn.textContent = originalLabel;
      showToast(appText('javascript.request_failed'), 'danger');
    });
}

async function executeIPTool() {
  await appLocaleReady;
  const form = document.getElementById('network-ip-form');
  const ip = form.ip.value || '';
  const resultDiv = document.getElementById('network-ip-result');
  const submitBtn = form.querySelector('button[type="submit"]');
  const originalLabel = submitBtn.textContent;

  submitBtn.disabled = true;
  submitBtn.textContent = appText('javascript.looking_up');
  resultDiv.hidden = false;
  resultDiv.innerHTML = '<div class="spinner"></div>';

  const url = appPath(ip ? `/api/v1/network/ip/${encodeURIComponent(ip)}` : '/api/v1/network/ip');

  fetch(url)
    .then(response => response.json())
    .then(data => {
      const output = document.createElement('pre');
      output.textContent = JSON.stringify(data, null, 2);
      resultDiv.replaceChildren(output);
      submitBtn.disabled = false;
      submitBtn.textContent = originalLabel;
      addToHistory('network-ip');
    })
    .catch(error => {
      resultDiv.textContent = appText('javascript.error', { message: error.message });
      submitBtn.disabled = false;
      submitBtn.textContent = originalLabel;
      showToast(appText('javascript.request_failed'), 'danger');
    });
}

async function executePasswordTool() {
  await appLocaleReady;
  const form = document.getElementById('password-form');
  const length = form.length.value;
  const resultDiv = document.getElementById('password-result');
  const submitBtn = form.querySelector('button[type="submit"]');
  const originalLabel = submitBtn.textContent;

  submitBtn.disabled = true;
  submitBtn.textContent = appText('javascript.generating');
  resultDiv.hidden = false;
  resultDiv.innerHTML = '<div class="spinner"></div>';

  fetch(appPath(`/api/v1/crypto/password/${length}.txt`))
    .then(response => response.text())
    .then(result => {
      resultDiv.textContent = result;
      submitBtn.disabled = false;
      submitBtn.textContent = originalLabel;
      addToHistory('crypto-password');
    })
    .catch(error => {
      resultDiv.textContent = appText('javascript.error', { message: error.message });
      submitBtn.disabled = false;
      submitBtn.textContent = originalLabel;
      showToast(appText('javascript.request_failed'), 'danger');
    });
}

async function executeUUIDTool() {
  await appLocaleReady;
  const form = document.getElementById('uuid-form');
  const version = form.version.value;
  const count = form.count.value;
  const resultDiv = document.getElementById('uuid-result');
  const submitBtn = form.querySelector('button[type="submit"]');
  const originalLabel = submitBtn.textContent;

  submitBtn.disabled = true;
  submitBtn.textContent = appText('javascript.generating');
  resultDiv.hidden = false;
  resultDiv.innerHTML = '<div class="spinner"></div>';

  const endpoint = count > 1
    ? `/api/v1/text/uuid/${version}/${count}`
    : `/api/v1/text/uuid/${version}`;

  fetch(appPath(endpoint))
    .then(response => response.text())
    .then(result => {
      resultDiv.textContent = result;
      submitBtn.disabled = false;
      submitBtn.textContent = originalLabel;
      addToHistory('text-uuid');
    })
    .catch(error => {
      resultDiv.textContent = appText('javascript.error', { message: error.message });
      submitBtn.disabled = false;
      submitBtn.textContent = originalLabel;
      showToast(appText('javascript.request_failed'), 'danger');
    });
}

// ============================================================================
// Search functionality
// ============================================================================
function filterTools(searchTerm) {
  const tools = document.querySelectorAll('.tool-card, .category-card');
  const term = searchTerm.toLowerCase();
  
  tools.forEach(tool => {
    const title = tool.querySelector('.tool-title, .category-title')?.textContent.toLowerCase() || '';
    const description = tool.querySelector('.tool-description, .category-description')?.textContent.toLowerCase() || '';
    
    if (title.includes(term) || description.includes(term)) {
      tool.style.display = '';
    } else {
      tool.style.display = 'none';
    }
  });
}

// ============================================================================
// localStorage helpers (for favorites/history)
// ============================================================================
async function addToFavorites(toolId) {
  await appLocaleReady;
  const favorites = JSON.parse(localStorage.getItem('castools-favorites') || '[]');
  if (!favorites.includes(toolId)){
    favorites.push(toolId);
    localStorage.setItem('castools-favorites', JSON.stringify(favorites));
    showToast(appText('javascript.favorite_added'), 'success');
  }
}

async function removeFromFavorites(toolId) {
  await appLocaleReady;
  let favorites = JSON.parse(localStorage.getItem('castools-favorites') || '[]');
  favorites = favorites.filter(id => id !== toolId);
  localStorage.setItem('castools-favorites', JSON.stringify(favorites));
  showToast(appText('javascript.favorite_removed'), 'info');
}

function addToHistory(toolId) {
  const history = JSON.parse(localStorage.getItem('castools-history') || '[]');
  // Keep last 20 items
  const updated = [toolId, ...history.filter(id => id !== toolId)].slice(0, 20);
  localStorage.setItem('castools-history', JSON.stringify(updated));
}

// ============================================================================
// Initialize on page load
// ============================================================================
document.addEventListener('DOMContentLoaded', function() {
  // Add keyboard shortcut: Ctrl+K or Cmd+K for search
  document.addEventListener('keydown', function(e) {
    if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
      e.preventDefault();
      const searchInput = document.querySelector('input[type="search"]');
      if (searchInput) searchInput.focus();
    }
  });
  
  // Close modals on Escape key
  document.addEventListener('keydown', function(e) {
    if (e.key === 'Escape') {
      document.querySelectorAll('dialog[open]').forEach(dialog => dialog.close());
    }
  });

  // Wire tool forms declared with data-endpoint (no inline onsubmit)
  document.querySelectorAll('form.tool-form[data-endpoint]').forEach(function(form) {
    form.addEventListener('submit', function(e) {
      e.preventDefault();
      executeTool(form.id, form.dataset.endpoint);
    });
  });

  // Wire tool forms declared with data-template (path-param GET endpoints)
  document.querySelectorAll('form.tool-form[data-template]').forEach(function(form) {
    form.addEventListener('submit', function(e) {
      e.preventDefault();
      executeToolTemplate(form.id, form.dataset.template);
    });
  });

  // Wire tool forms declared with data-body-endpoint (raw-body POST endpoints)
  document.querySelectorAll('form.tool-form[data-body-endpoint]').forEach(function(form) {
    form.addEventListener('submit', function(e) {
      e.preventDefault();
      executeToolBody(form.id, form.dataset.bodyEndpoint);
    });
  });

  // Wire tool forms declared with data-query-post-endpoint (POST-only,
  // query-string-param endpoints)
  document.querySelectorAll('form.tool-form[data-query-post-endpoint]').forEach(function(form) {
    form.addEventListener('submit', function(e) {
      e.preventDefault();
      executeToolQueryPost(form.id, form.dataset.queryPostEndpoint);
    });
  });

  // Wire tool forms declared with data-image-template (binary-image GET
  // endpoints rendered as an <img> rather than text)
  document.querySelectorAll('form.tool-form[data-image-template]').forEach(function(form) {
    form.addEventListener('submit', function(e) {
      e.preventDefault();
      executeToolImage(form.id, form.dataset.imageTemplate);
    });
  });

  // Wire favorite buttons declared with data-favorite (no inline onclick)
  document.querySelectorAll('[data-favorite]').forEach(function(btn) {
    btn.addEventListener('click', function() {
      addToFavorites(btn.dataset.favorite);
    });
  });

  // Wire copy buttons declared with data-copy (no inline onclick)
  document.querySelectorAll('[data-copy]').forEach(function(btn) {
    btn.addEventListener('click', function() {
      copyCode(btn);
    });
  });

  // Wire back buttons declared with data-back (no inline onclick)
  document.querySelectorAll('[data-back]').forEach(function(btn) {
    btn.addEventListener('click', function() {
      history.back();
    });
  });

  // Wire legacy per-page tool forms migrated from inline onsubmit handlers
  // (unique ids, no data-endpoint/data-template since their request shape
  // is bespoke)
  const legacyToolForms = {
    'datetime-form': executeDateTimeTool,
    'network-ip-form': executeIPTool,
    'password-form': executePasswordTool,
    'uuid-form': executeUUIDTool,
  };
  Object.keys(legacyToolForms).forEach(function(formId) {
    const form = document.getElementById(formId);
    if (form && !form.matches('[data-endpoint], [data-template], [data-body-endpoint], [data-query-post-endpoint], [data-image-template], [data-api-endpoint]')) {
      form.addEventListener('submit', function(e) {
        e.preventDefault();
        legacyToolForms[formId]();
      });
    }
  });

  // Offline indicator - reflects live connectivity state (data-* markup,
  // no inline handlers)
  const offlineIndicator = document.getElementById('offline-indicator');
  if (offlineIndicator) {
    if (!navigator.onLine) {
      offlineIndicator.hidden = false;
    }
    window.addEventListener('online', function() {
      offlineIndicator.hidden = true;
    });
    window.addEventListener('offline', function() {
      offlineIndicator.hidden = false;
    });
  }

  // Install prompt button per PART 16 "App Install Prompt" - hidden until
  // beforeinstallprompt fires (Android/desktop) or shown with manual
  // instructions on iOS, which never fires that event
  const installBtn = document.getElementById('pwa-install-btn');
  if (installBtn) {
    installBtn.addEventListener('click', installApp);
    if (isIOSSafari() && !isInstalledPWA()) {
      installBtn.hidden = false;
      installBtn.addEventListener('click', function() {
        appLocaleReady.then(() => showToast(appText('javascript.ios_install'), 'info', 5000));
      });
    }
  }
});

// ============================================================================
// PWA support per AI.md PART 16 "PWA Support"
// ============================================================================

// Service worker registration - the site must be fully usable if this
// never installs; the SW only enhances an already-working no-JS site.
if ('serviceWorker' in navigator) {
  window.addEventListener('load', async function() {
    try {
      const registration = await navigator.serviceWorker.register(appPath('/sw.js'), { scope: appPath('/') });

      registration.addEventListener('updatefound', function() {
        const newWorker = registration.installing;
        if (!newWorker) return;
        newWorker.addEventListener('statechange', function() {
          if (newWorker.state === 'installed' && navigator.serviceWorker.controller) {
            showUpdateNotification();
          }
        });
      });

      // Check for a new service worker once an hour while the app is open
      setInterval(function() {
        registration.update();
      }, 60 * 60 * 1000);
    } catch (error) {
      // Service worker is an enhancement only - failure is not fatal
    }
  });
}

// Show a dismissible banner prompting the user to activate a waiting SW
async function showUpdateNotification() {
  await appLocaleReady;
  if (document.querySelector('.update-banner')) return;
  const banner = document.createElement('div');
  banner.className = 'update-banner';
  const label = document.createElement('span');
  label.textContent = appText('javascript.update_available');
  const updateBtn = document.createElement('button');
  updateBtn.className = 'btn btn-sm btn-primary';
  updateBtn.textContent = appText('javascript.update_now');
  updateBtn.addEventListener('click', updateApp);
  const laterBtn = document.createElement('button');
  laterBtn.className = 'btn btn-sm';
  laterBtn.textContent = appText('javascript.later');
  laterBtn.addEventListener('click', function() { banner.remove(); });
  banner.append(label, updateBtn, laterBtn);
  document.body.appendChild(banner);
}

// The controller listener is registered once; an update reloads only after
// the visitor accepts it and never more than once.
let updateAccepted = false;
let updateReloading = false;

function reloadAfterUpdate() {
  if (updateReloading) return;
  updateReloading = true;
  window.location.reload();
}

if ('serviceWorker' in navigator) {
  navigator.serviceWorker.addEventListener('controllerchange', function() {
    if (updateAccepted) reloadAfterUpdate();
  });
}

// Tell the waiting service worker to activate, then guarantee a reload.
function updateApp() {
  updateAccepted = true;
  setTimeout(reloadAfterUpdate, 5000);
  navigator.serviceWorker.ready.then(function(reg) {
    if (reg.waiting) {
      reg.waiting.postMessage({ type: 'SKIP_WAITING' });
    } else {
      reloadAfterUpdate();
    }
  }).catch(reloadAfterUpdate);
}

// App install prompt (Android/desktop) - captured and replayed from the
// header's Install App button instead of the browser's default mini-infobar
let deferredInstallPrompt;

window.addEventListener('beforeinstallprompt', function(event) {
  event.preventDefault();
  deferredInstallPrompt = event;
  const installBtn = document.getElementById('pwa-install-btn');
  if (installBtn) installBtn.hidden = false;
});

function installApp() {
  if (!deferredInstallPrompt) return;
  deferredInstallPrompt.prompt();
  deferredInstallPrompt.userChoice.then(function() {
    deferredInstallPrompt = null;
    const installBtn = document.getElementById('pwa-install-btn');
    if (installBtn) installBtn.hidden = true;
  });
}

window.addEventListener('appinstalled', function() {
  deferredInstallPrompt = null;
  const installBtn = document.getElementById('pwa-install-btn');
  if (installBtn) installBtn.hidden = true;
});

// Detect standalone/installed mode (Android/desktop display-mode, iOS
// navigator.standalone)
function isInstalledPWA() {
  return window.matchMedia('(display-mode: standalone)').matches
    || window.navigator.standalone === true;
}

// iOS Safari never fires beforeinstallprompt - detect it so the Install
// App button can fall back to manual "Add to Home Screen" instructions
function isIOSSafari() {
  const ua = window.navigator.userAgent;
  const isIOS = /iPad|iPhone|iPod/.test(ua) && !window.MSStream;
  const isSafari = /Safari/.test(ua) && !/CriOS|FxiOS|EdgiOS/.test(ua);
  return isIOS && isSafari;
}
