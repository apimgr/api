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
  if (!form)returnfalse;
  
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
});
