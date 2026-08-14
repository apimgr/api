// app.js - Minimal JavaScript per PART 17 (ONE file, no frameworks)

// ============================================================================
// Clipboard with feedback per PART 17
// ============================================================================
function copyToClipboard(text, btn) {
  navigator.clipboard.writeText(text).then(() => {
    const original = btn.textContent;
    btn.textContent = 'Copied!';
    btn.classList.add('copied');
    setTimeout(() => {
      btn.textContent = original;
      btn.classList.remove('copied');
    }, 2000);
  }).catch(err => {
    showToast('Failed to copy', 'danger');
  });
}

// Helper for copying from code blocks
function copyCode(btn) {
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
function confirmDelete(form, message = 'Are you sure you want to delete this?') {
  if (confirm(message)) {
    form.submit();
  }
}

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
function executeTool(toolId, endpoint) {
  const form = document.getElementById(toolId);
  const resultDiv = document.getElementById(`${toolId}-result`);
  const submitBtn = form.querySelector('button[type="submit"]');
  
  // Get form data
  const formData = new FormData(form);
  const params = new URLSearchParams(formData);
  
  // Show loading state
  submitBtn.disabled = true;
  submitBtn.textContent = 'Processing...';
  resultDiv.hidden = false;
  resultDiv.innerHTML = '<div class="spinner"></div>';

  // Make API request
  fetch(`${endpoint}?${params}`)
    .then(response => response.text())
    .then(result => {
      resultDiv.textContent = result;
      submitBtn.disabled = false;
      submitBtn.textContent = 'Execute';
    })
    .catch(error => {
      resultDiv.textContent = `Error: ${error.message}`;
      submitBtn.disabled = false;
      submitBtn.textContent = 'Execute';
      showToast('Request failed', 'danger');
    });
}

// executeToolTemplate builds a GET URL by substituting {field} placeholders
// in a path template with the matching form field's value, for tools backed
// by path-param API routes (e.g. /api/v1/text/hash/{algorithm}/{input}).
function executeToolTemplate(toolId, urlTemplate) {
  const form = document.getElementById(toolId);
  const resultDiv = document.getElementById(`${toolId}-result`);
  const submitBtn = form.querySelector('button[type="submit"]');
  const formData = new FormData(form);

  const url = urlTemplate.replace(/\{(\w+)\}/g, function(match, field) {
    const value = formData.get(field);
    return value !== null ? encodeURIComponent(value) : match;
  });

  submitBtn.disabled = true;
  submitBtn.textContent = 'Processing...';
  resultDiv.hidden = false;
  resultDiv.innerHTML = '<div class="spinner"></div>';

  fetch(url)
    .then(response => response.text())
    .then(result => {
      resultDiv.textContent = result;
      submitBtn.disabled = false;
      submitBtn.textContent = 'Execute';
    })
    .catch(error => {
      resultDiv.textContent = `Error: ${error.message}`;
      submitBtn.disabled = false;
      submitBtn.textContent = 'Execute';
      showToast('Request failed', 'danger');
    });
}

// executeToolBody POSTs the value of a form's single textarea as the raw
// request body, for tools backed by raw-body POST API routes
// (e.g. /api/v1/parse/json).
function executeToolBody(toolId, endpoint) {
  const form = document.getElementById(toolId);
  const resultDiv = document.getElementById(`${toolId}-result`);
  const submitBtn = form.querySelector('button[type="submit"]');
  const bodyField = form.querySelector('textarea');
  const body = bodyField.value;

  // Any other named fields (selects, checkboxes, text inputs) besides the
  // raw-body textarea are appended to the endpoint as query-string params,
  // for tools like /api/v1/dev/base64 that take ?action=&urlsafe= alongside
  // a raw request body.
  const query = new URLSearchParams();
  new FormData(form).forEach(function(value, key) {
    if (key !== bodyField.name && value !== '') {
      query.append(key, value);
    }
  });
  const queryString = query.toString();
  const url = queryString ? `${endpoint}?${queryString}` : endpoint;

  submitBtn.disabled = true;
  submitBtn.textContent = 'Processing...';
  resultDiv.hidden = false;
  resultDiv.innerHTML = '<div class="spinner"></div>';

  fetch(url, { method: 'POST', body: body })
    .then(response => response.text())
    .then(result => {
      resultDiv.textContent = result;
      submitBtn.disabled = false;
      submitBtn.textContent = 'Execute';
    })
    .catch(error => {
      resultDiv.textContent = `Error: ${error.message}`;
      submitBtn.disabled = false;
      submitBtn.textContent = 'Execute';
      showToast('Request failed', 'danger');
    });
}

// executeToolImage builds a GET URL by substituting {field} placeholders in
// a path template (like executeToolTemplate), appends any remaining form
// fields as query-string params, and renders the response as an <img> rather
// than text, for tools backed by binary-image API routes
// (e.g. /api/v1/image/placeholder/{width}/{height}?format=&bg=).
function executeToolImage(toolId, urlTemplate) {
  const form = document.getElementById(toolId);
  const resultDiv = document.getElementById(`${toolId}-result`);
  const submitBtn = form.querySelector('button[type="submit"]');
  const formData = new FormData(form);
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
  submitBtn.textContent = 'Processing...';
  resultDiv.hidden = false;
  resultDiv.innerHTML = `<img src="${url}" alt="Generated image" class="tool-result-image">`;
  submitBtn.disabled = false;
  submitBtn.textContent = 'Execute';
}

// executeToolQueryPost POSTs a form's fields as a query string (rather than a
// JSON body), for tools backed by POST-only API routes that read their
// parameters via query string (e.g. /api/v1/validate/email?email=...).
function executeToolQueryPost(toolId, endpoint) {
  const form = document.getElementById(toolId);
  const resultDiv = document.getElementById(`${toolId}-result`);
  const submitBtn = form.querySelector('button[type="submit"]');
  const formData = new FormData(form);
  const params = new URLSearchParams(formData);

  submitBtn.disabled = true;
  submitBtn.textContent = 'Processing...';
  resultDiv.hidden = false;
  resultDiv.innerHTML = '<div class="spinner"></div>';

  fetch(`${endpoint}?${params}`, { method: 'POST' })
    .then(response => response.text())
    .then(result => {
      resultDiv.textContent = result;
      submitBtn.disabled = false;
      submitBtn.textContent = 'Execute';
    })
    .catch(error => {
      resultDiv.textContent = `Error: ${error.message}`;
      submitBtn.disabled = false;
      submitBtn.textContent = 'Execute';
      showToast('Request failed', 'danger');
    });
}

// ============================================================================
// Legacy per-page tool handlers (migrated from inline onsubmit/<script>
// blocks so pages carry zero inline JS; the API call shape is unchanged)
// ============================================================================
function executeDateTimeTool() {
  const form = document.getElementById('datetime-form');
  const timezone = form.timezone.value;
  const resultDiv = document.getElementById('datetime-result');
  const submitBtn = form.querySelector('button[type="submit"]');

  submitBtn.disabled = true;
  submitBtn.textContent = 'Loading...';
  resultDiv.hidden = false;
  resultDiv.innerHTML = '<div class="spinner"></div>';

  const url = timezone ? `/api/v1/datetime/now?tz=${encodeURIComponent(timezone)}` : '/api/v1/datetime/now';

  fetch(url)
    .then(response => response.json())
    .then(data => {
      resultDiv.innerHTML = `<pre>${JSON.stringify(data, null, 2)}</pre>`;
      submitBtn.disabled = false;
      submitBtn.textContent = 'Get Current Time';
      addToHistory('datetime-now');
    })
    .catch(error => {
      resultDiv.textContent = `Error: ${error.message}`;
      submitBtn.disabled = false;
      submitBtn.textContent = 'Get Current Time';
      showToast('Request failed', 'danger');
    });
}

function executeIPTool() {
  const form = document.getElementById('network-ip-form');
  const ip = form.ip.value || '';
  const resultDiv = document.getElementById('network-ip-result');
  const submitBtn = form.querySelector('button[type="submit"]');

  submitBtn.disabled = true;
  submitBtn.textContent = 'Looking up...';
  resultDiv.hidden = false;
  resultDiv.innerHTML = '<div class="spinner"></div>';

  const url = ip ? `/api/v1/network/ip/${ip}` : '/api/v1/network/ip';

  fetch(url)
    .then(response => response.json())
    .then(data => {
      resultDiv.innerHTML = `<pre>${JSON.stringify(data, null, 2)}</pre>`;
      submitBtn.disabled = false;
      submitBtn.textContent = 'Lookup IP';
      addToHistory('network-ip');
    })
    .catch(error => {
      resultDiv.textContent = `Error: ${error.message}`;
      submitBtn.disabled = false;
      submitBtn.textContent = 'Lookup IP';
      showToast('Request failed', 'danger');
    });
}

function executePasswordTool() {
  const form = document.getElementById('password-form');
  const length = form.length.value;
  const resultDiv = document.getElementById('password-result');
  const submitBtn = form.querySelector('button[type="submit"]');

  submitBtn.disabled = true;
  submitBtn.textContent = 'Generating...';
  resultDiv.hidden = false;
  resultDiv.innerHTML = '<div class="spinner"></div>';

  fetch(`/api/v1/crypto/password/${length}.txt`)
    .then(response => response.text())
    .then(result => {
      resultDiv.textContent = result;
      submitBtn.disabled = false;
      submitBtn.textContent = 'Generate Password';
      addToHistory('crypto-password');
    })
    .catch(error => {
      resultDiv.textContent = `Error: ${error.message}`;
      submitBtn.disabled = false;
      submitBtn.textContent = 'Generate Password';
      showToast('Request failed', 'danger');
    });
}

function executeUUIDTool() {
  const form = document.getElementById('uuid-form');
  const version = form.version.value;
  const count = form.count.value;
  const resultDiv = document.getElementById('uuid-result');
  const submitBtn = form.querySelector('button[type="submit"]');

  submitBtn.disabled = true;
  submitBtn.textContent = 'Generating...';
  resultDiv.hidden = false;
  resultDiv.innerHTML = '<div class="spinner"></div>';

  const endpoint = count > 1
    ? `/api/v1/text/uuid/${version}/${count}`
    : `/api/v1/text/uuid/${version}`;

  fetch(endpoint)
    .then(response => response.text())
    .then(result => {
      resultDiv.textContent = result;
      submitBtn.disabled = false;
      submitBtn.textContent = 'Generate UUID';
      addToHistory('text-uuid');
    })
    .catch(error => {
      resultDiv.textContent = `Error: ${error.message}`;
      submitBtn.disabled = false;
      submitBtn.textContent = 'Generate UUID';
      showToast('Request failed', 'danger');
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
function addToFavorites(toolId) {
  const favorites = JSON.parse(localStorage.getItem('castools-favorites') || '[]');
  if (!favorites.includes(toolId)){
    favorites.push(toolId);
    localStorage.setItem('castools-favorites', JSON.stringify(favorites));
    showToast('Added to favorites', 'success');
  }
}

function removeFromFavorites(toolId) {
  let favorites = JSON.parse(localStorage.getItem('castools-favorites') || '[]');
  favorites = favorites.filter(id => id !== toolId);
  localStorage.setItem('castools-favorites', JSON.stringify(favorites));
  showToast('Removed from favorites', 'info');
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
    if (form) {
      form.addEventListener('submit', function(e) {
        e.preventDefault();
        legacyToolForms[formId]();
      });
    }
  });
});
