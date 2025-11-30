// Main JavaScript for CasTools

// Theme handling
(function() {
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    const stored = localStorage.getItem('theme');
    const theme = stored || (prefersDark ? 'dark' : 'light');
    document.documentElement.setAttribute('data-theme', theme);
})();

function toggleTheme() {
    const html = document.documentElement;
    const current = html.getAttribute('data-theme');
    const next = current === 'dark' ? 'light' : 'dark';
    html.setAttribute('data-theme', next);
    localStorage.setItem('theme', next);
}

// Copy to clipboard utility
async function copyToClipboard(text) {
    try {
        await navigator.clipboard.writeText(text);
        return true;
    } catch (err) {
        // Fallback for older browsers
        const textarea = document.createElement('textarea');
        textarea.value = text;
        textarea.style.position = 'fixed';
        textarea.style.opacity = '0';
        document.body.appendChild(textarea);
        textarea.select();
        try {
            document.execCommand('copy');
            document.body.removeChild(textarea);
            return true;
        } catch (e) {
            document.body.removeChild(textarea);
            return false;
        }
    }
}

// Format numbers with commas
function formatNumber(num) {
    return num.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ',');
}

// Debounce utility
function debounce(func, wait) {
    let timeout;
    return function executedFunction(...args) {
        const later = () => {
            clearTimeout(timeout);
            func(...args);
        };
        clearTimeout(timeout);
        timeout = setTimeout(later, wait);
    };
}

// API fetch wrapper with error handling
async function apiFetch(url, options = {}) {
    try {
        const response = await fetch(url, {
            ...options,
            headers: {
                'Accept': 'application/json',
                ...options.headers
            }
        });

        const data = await response.json();

        if (!response.ok) {
            throw new Error(data.error || `HTTP error ${response.status}`);
        }

        return { success: true, data };
    } catch (error) {
        return { success: false, error: error.message };
    }
}

// Show notification
function showNotification(message, type = 'info') {
    const notification = document.createElement('div');
    notification.className = `notification notification-${type}`;
    notification.textContent = message;

    // Add styles
    Object.assign(notification.style, {
        position: 'fixed',
        bottom: '20px',
        right: '20px',
        padding: '12px 24px',
        borderRadius: '8px',
        color: 'white',
        fontWeight: '500',
        zIndex: '9999',
        animation: 'slideIn 0.3s ease',
        backgroundColor: type === 'success' ? '#22c55e' :
                        type === 'error' ? '#ef4444' :
                        type === 'warning' ? '#f59e0b' : '#6366f1'
    });

    document.body.appendChild(notification);

    setTimeout(() => {
        notification.style.animation = 'slideOut 0.3s ease';
        setTimeout(() => notification.remove(), 300);
    }, 3000);
}

// Add animation keyframes
const style = document.createElement('style');
style.textContent = `
    @keyframes slideIn {
        from { transform: translateX(100%); opacity: 0; }
        to { transform: translateX(0); opacity: 1; }
    }
    @keyframes slideOut {
        from { transform: translateX(0); opacity: 1; }
        to { transform: translateX(100%); opacity: 0; }
    }
`;
document.head.appendChild(style);

// Form input validation
function validateInput(input, rules = {}) {
    const value = input.value.trim();
    const errors = [];

    if (rules.required && !value) {
        errors.push('This field is required');
    }

    if (rules.minLength && value.length < rules.minLength) {
        errors.push(`Minimum length is ${rules.minLength} characters`);
    }

    if (rules.maxLength && value.length > rules.maxLength) {
        errors.push(`Maximum length is ${rules.maxLength} characters`);
    }

    if (rules.pattern && !rules.pattern.test(value)) {
        errors.push(rules.patternMessage || 'Invalid format');
    }

    return { valid: errors.length === 0, errors };
}

// Initialize on DOM ready
document.addEventListener('DOMContentLoaded', function() {
    // Add click handlers for copy buttons
    document.querySelectorAll('[data-copy]').forEach(button => {
        button.addEventListener('click', async function() {
            const target = this.getAttribute('data-copy');
            const element = document.querySelector(target);
            if (element) {
                const success = await copyToClipboard(element.textContent);
                if (success) {
                    showNotification('Copied to clipboard!', 'success');
                }
            }
        });
    });

    // Handle form submissions
    document.querySelectorAll('form[data-api]').forEach(form => {
        form.addEventListener('submit', async function(e) {
            e.preventDefault();
            const url = this.getAttribute('data-api');
            const method = this.getAttribute('method') || 'GET';
            const formData = new FormData(this);

            let fetchUrl = url;
            if (method === 'GET') {
                const params = new URLSearchParams(formData);
                fetchUrl = `${url}?${params}`;
            }

            const result = await apiFetch(fetchUrl, {
                method,
                body: method !== 'GET' ? formData : undefined
            });

            const resultElement = this.querySelector('[data-result]');
            if (resultElement) {
                if (result.success) {
                    resultElement.innerHTML = `<pre>${JSON.stringify(result.data, null, 2)}</pre>`;
                } else {
                    resultElement.innerHTML = `<span class="error">${result.error}</span>`;
                }
                resultElement.style.display = 'block';
            }
        });
    });
});

// Keyboard shortcuts
document.addEventListener('keydown', function(e) {
    // Ctrl/Cmd + K for search focus
    if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
        e.preventDefault();
        const searchInput = document.querySelector('input[type="search"], input[name="search"], #search');
        if (searchInput) {
            searchInput.focus();
        }
    }
});
