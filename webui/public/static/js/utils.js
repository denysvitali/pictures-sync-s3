/**
 * Photo Backup Station - Shared JavaScript Utilities
 * Common functions for Bootstrap components and API interactions
 */

// ========================================
// Toast Notifications
// ========================================

/**
 * Show a Bootstrap toast notification
 * @param {string} message - The message to display
 * @param {string} type - Type of toast: 'success', 'error', 'warning', 'info'
 * @param {number} duration - Duration in milliseconds (default: 5000)
 */
function showToast(message, type = 'info', duration = 5000) {
    // Create toast container if it doesn't exist
    let container = document.getElementById('toast-container');
    if (!container) {
        container = document.createElement('div');
        container.id = 'toast-container';
        container.className = 'toast-container position-fixed top-0 end-0 p-3';
        container.style.zIndex = '1090';
        document.body.appendChild(container);
    }

    // Map types to Bootstrap classes
    const typeClasses = {
        success: 'bg-success text-white',
        error: 'bg-danger text-white',
        warning: 'bg-warning text-dark',
        info: 'bg-info text-white'
    };

    const typeIcons = {
        success: '✓',
        error: '✕',
        warning: '⚠',
        info: 'ℹ'
    };

    // Create toast element
    const toastId = 'toast-' + Date.now();
    const toastHtml = `
        <div id="${toastId}" class="toast ${typeClasses[type] || typeClasses.info}" role="alert" aria-live="assertive" aria-atomic="true">
            <div class="toast-header ${typeClasses[type] || typeClasses.info}">
                <strong class="me-auto">
                    <span style="font-size: 1.2rem; margin-right: 0.5rem;">${typeIcons[type] || typeIcons.info}</span>
                    ${type.charAt(0).toUpperCase() + type.slice(1)}
                </strong>
                <button type="button" class="btn-close btn-close-white" data-bs-dismiss="toast" aria-label="Close"></button>
            </div>
            <div class="toast-body">
                ${escapeHtml(message)}
            </div>
        </div>
    `;

    container.insertAdjacentHTML('beforeend', toastHtml);

    // Initialize and show toast
    const toastElement = document.getElementById(toastId);
    const toast = new bootstrap.Toast(toastElement, {
        autohide: true,
        delay: duration
    });

    toast.show();

    // Remove from DOM after hidden
    toastElement.addEventListener('hidden.bs.toast', () => {
        toastElement.remove();
    });
}

// ========================================
// Modal Utilities
// ========================================

/**
 * Show a confirmation modal
 * @param {string} title - Modal title
 * @param {string} message - Modal message
 * @param {Function} onConfirm - Callback when confirmed
 * @param {Function} onCancel - Callback when cancelled (optional)
 */
function showConfirmModal(title, message, onConfirm, onCancel = null) {
    // Create modal if it doesn't exist
    let modal = document.getElementById('confirm-modal');
    if (!modal) {
        const modalHtml = `
            <div class="modal fade" id="confirm-modal" tabindex="-1" aria-hidden="true">
                <div class="modal-dialog">
                    <div class="modal-content">
                        <div class="modal-header">
                            <h5 class="modal-title" id="confirm-modal-title"></h5>
                            <button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Close"></button>
                        </div>
                        <div class="modal-body" id="confirm-modal-body"></div>
                        <div class="modal-footer">
                            <button type="button" class="btn btn-secondary" data-bs-dismiss="modal">Cancel</button>
                            <button type="button" class="btn btn-primary" id="confirm-modal-btn">Confirm</button>
                        </div>
                    </div>
                </div>
            </div>
        `;
        document.body.insertAdjacentHTML('beforeend', modalHtml);
        modal = document.getElementById('confirm-modal');
    }

    // Update content
    document.getElementById('confirm-modal-title').textContent = title;
    document.getElementById('confirm-modal-body').innerHTML = escapeHtml(message);

    // Setup event handlers
    const confirmBtn = document.getElementById('confirm-modal-btn');
    const newConfirmBtn = confirmBtn.cloneNode(true);
    confirmBtn.parentNode.replaceChild(newConfirmBtn, confirmBtn);

    newConfirmBtn.addEventListener('click', () => {
        const bsModal = bootstrap.Modal.getInstance(modal);
        bsModal.hide();
        if (onConfirm) onConfirm();
    });

    if (onCancel) {
        modal.addEventListener('hidden.bs.modal', onCancel, { once: true });
    }

    // Show modal
    const bsModal = new bootstrap.Modal(modal);
    bsModal.show();
}

/**
 * Show a prompt modal (replacement for window.prompt)
 * @param {string} title - Modal title
 * @param {string} message - Modal message
 * @param {string} defaultValue - Default input value
 * @param {Function} onSubmit - Callback with input value when submitted
 * @param {string} inputType - Input type (text, password, etc.)
 */
function showPromptModal(title, message, defaultValue = '', onSubmit, inputType = 'text') {
    // Create modal if it doesn't exist
    let modal = document.getElementById('prompt-modal');
    if (!modal) {
        const modalHtml = `
            <div class="modal fade" id="prompt-modal" tabindex="-1" aria-hidden="true">
                <div class="modal-dialog">
                    <div class="modal-content">
                        <div class="modal-header">
                            <h5 class="modal-title" id="prompt-modal-title"></h5>
                            <button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Close"></button>
                        </div>
                        <div class="modal-body">
                            <p id="prompt-modal-message"></p>
                            <input type="text" class="form-control" id="prompt-modal-input">
                        </div>
                        <div class="modal-footer">
                            <button type="button" class="btn btn-secondary" data-bs-dismiss="modal">Cancel</button>
                            <button type="button" class="btn btn-primary" id="prompt-modal-btn">Submit</button>
                        </div>
                    </div>
                </div>
            </div>
        `;
        document.body.insertAdjacentHTML('beforeend', modalHtml);
        modal = document.getElementById('prompt-modal');
    }

    // Update content
    document.getElementById('prompt-modal-title').textContent = title;
    document.getElementById('prompt-modal-message').textContent = message;
    const input = document.getElementById('prompt-modal-input');
    input.type = inputType;
    input.value = defaultValue;

    // Setup event handlers
    const submitBtn = document.getElementById('prompt-modal-btn');
    const newSubmitBtn = submitBtn.cloneNode(true);
    submitBtn.parentNode.replaceChild(newSubmitBtn, submitBtn);

    const handleSubmit = () => {
        const value = input.value;
        const bsModal = bootstrap.Modal.getInstance(modal);
        bsModal.hide();
        if (onSubmit && value) onSubmit(value);
    };

    newSubmitBtn.addEventListener('click', handleSubmit);
    input.addEventListener('keypress', (e) => {
        if (e.key === 'Enter') handleSubmit();
    });

    // Show modal and focus input
    const bsModal = new bootstrap.Modal(modal);
    bsModal.show();
    modal.addEventListener('shown.bs.modal', () => {
        input.focus();
        input.select();
    }, { once: true });
}

// ========================================
// Utility Functions
// ========================================

/**
 * Escape HTML to prevent XSS
 * @param {string} unsafe - Unsafe string
 * @returns {string} - Escaped HTML string
 */
function escapeHtml(unsafe) {
    if (!unsafe) return '';
    const div = document.createElement('div');
    div.textContent = unsafe.toString();
    return div.innerHTML;
}

/**
 * Format bytes to human-readable string
 * @param {number} bytes - Number of bytes
 * @param {number} decimals - Number of decimal places
 * @returns {string} - Formatted string (e.g., "1.5 MB")
 */
function formatBytes(bytes, decimals = 2) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const dm = decimals < 0 ? 0 : decimals;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
}

/**
 * Format duration in seconds to human-readable string
 * @param {number} seconds - Duration in seconds
 * @returns {string} - Formatted string (e.g., "2m 30s")
 */
function formatDuration(seconds) {
    if (seconds < 60) return `${seconds}s`;
    const minutes = Math.floor(seconds / 60);
    const secs = seconds % 60;
    if (minutes < 60) return `${minutes}m ${secs}s`;
    const hours = Math.floor(minutes / 60);
    const mins = minutes % 60;
    return `${hours}h ${mins}m ${secs}s`;
}

/**
 * Format date to locale string
 * @param {string|Date} date - Date to format
 * @returns {string} - Formatted date string
 */
function formatDate(date) {
    if (!date) return '-';
    const d = typeof date === 'string' ? new Date(date) : date;
    return d.toLocaleString();
}

/**
 * Debounce function calls
 * @param {Function} func - Function to debounce
 * @param {number} wait - Wait time in milliseconds
 * @returns {Function} - Debounced function
 */
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

/**
 * Show loading state on element
 * @param {HTMLElement} element - Element to show loading on
 */
function showLoading(element) {
    element.innerHTML = `
        <div class="text-center py-5">
            <div class="spinner-border text-primary" role="status">
                <span class="visually-hidden">Loading...</span>
            </div>
            <p class="mt-3 text-muted">Loading...</p>
        </div>
    `;
}

/**
 * Show error state on element
 * @param {HTMLElement} element - Element to show error on
 * @param {string} message - Error message
 */
function showError(element, message) {
    element.innerHTML = `
        <div class="alert alert-danger" role="alert">
            <strong>Error:</strong> ${escapeHtml(message)}
        </div>
    `;
}

/**
 * Show empty state on element
 * @param {HTMLElement} element - Element to show empty state on
 * @param {string} message - Empty state message
 * @param {string} icon - Optional icon/emoji
 */
function showEmptyState(element, message, icon = '📭') {
    element.innerHTML = `
        <div class="empty-state">
            <div class="empty-state-icon">${icon}</div>
            <p>${escapeHtml(message)}</p>
        </div>
    `;
}

// ========================================
// API Helper Functions
// ========================================

/**
 * Make API request with error handling
 * @param {string} url - API endpoint URL
 * @param {object} options - Fetch options
 * @returns {Promise} - Fetch promise
 */
async function apiRequest(url, options = {}) {
    try {
        const response = await fetch(url, {
            headers: {
                'Content-Type': 'application/json',
                ...options.headers
            },
            ...options
        });

        if (!response.ok) {
            const errorText = await response.text();
            throw new Error(errorText || `HTTP ${response.status}: ${response.statusText}`);
        }

        // Handle empty responses
        const contentType = response.headers.get('content-type');
        if (contentType && contentType.includes('application/json')) {
            return await response.json();
        }

        return await response.text();
    } catch (error) {
        console.error('API Request failed:', error);
        throw error;
    }
}

// ========================================
// WebSocket Helper
// ========================================

/**
 * Create WebSocket connection with auto-reconnect
 * @param {Function} onMessage - Message handler
 * @param {Function} onConnect - Connect handler (optional)
 * @param {Function} onDisconnect - Disconnect handler (optional)
 * @returns {object} - WebSocket manager object
 */
function createWebSocket(onMessage, onConnect = null, onDisconnect = null) {
    let ws = null;
    let reconnectInterval = null;
    let connectionIndicator = document.getElementById('connection-status');

    const updateStatus = (connected, error = false) => {
        if (!connectionIndicator) return;

        const dot = connectionIndicator.querySelector('.status-dot');
        const text = connectionIndicator.querySelector('.status-text');

        if (connected) {
            connectionIndicator.classList.add('connected');
            if (text) text.textContent = 'Connected';
        } else {
            connectionIndicator.classList.remove('connected');
            if (text) text.textContent = error ? 'Connection Error' : 'Disconnected';
        }
    };

    const connect = () => {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws`;

        ws = new WebSocket(wsUrl);

        ws.onopen = () => {
            console.log('WebSocket connected');
            updateStatus(true);
            if (reconnectInterval) {
                clearInterval(reconnectInterval);
                reconnectInterval = null;
            }
            if (onConnect) onConnect();
        };

        ws.onmessage = (event) => {
            try {
                const data = JSON.parse(event.data);
                if (onMessage) onMessage(data);
            } catch (error) {
                console.error('WebSocket message parse error:', error);
            }
        };

        ws.onclose = () => {
            console.log('WebSocket disconnected');
            updateStatus(false);
            if (onDisconnect) onDisconnect();

            // Auto-reconnect after 3 seconds
            if (!reconnectInterval) {
                reconnectInterval = setInterval(() => {
                    console.log('Attempting to reconnect WebSocket...');
                    connect();
                }, 3000);
            }
        };

        ws.onerror = (error) => {
            console.error('WebSocket error:', error);
            updateStatus(false, true);
        };
    };

    // Initial connection
    connect();

    return {
        send: (data) => {
            if (ws && ws.readyState === WebSocket.OPEN) {
                ws.send(JSON.stringify(data));
            }
        },
        close: () => {
            if (reconnectInterval) {
                clearInterval(reconnectInterval);
            }
            if (ws) {
                ws.close();
            }
        }
    };
}

// ========================================
// Initialize Bootstrap Components
// ========================================

document.addEventListener('DOMContentLoaded', () => {
    // Initialize all tooltips
    const tooltipTriggerList = [].slice.call(document.querySelectorAll('[data-bs-toggle="tooltip"]'));
    tooltipTriggerList.map((tooltipTriggerEl) => {
        return new bootstrap.Tooltip(tooltipTriggerEl);
    });

    // Initialize all popovers
    const popoverTriggerList = [].slice.call(document.querySelectorAll('[data-bs-toggle="popover"]'));
    popoverTriggerList.map((popoverTriggerEl) => {
        return new bootstrap.Popover(popoverTriggerEl);
    });
});

// ========================================
// Enhanced Error Handling & Retry Logic
// ========================================

/**
 * Retry a failed request with exponential backoff
 * @param {Function} fn - Async function to retry
 * @param {number} maxRetries - Maximum number of retries
 * @param {number} baseDelay - Base delay in ms (doubles each retry)
 * @returns {Promise} - Result of the function
 */
async function retryWithBackoff(fn, maxRetries = 3, baseDelay = 1000) {
    let lastError;

    for (let attempt = 0; attempt <= maxRetries; attempt++) {
        try {
            return await fn();
        } catch (error) {
            lastError = error;

            if (attempt < maxRetries) {
                const delay = baseDelay * Math.pow(2, attempt);
                console.log(`Retry attempt ${attempt + 1}/${maxRetries} after ${delay}ms`);
                await sleep(delay);
            }
        }
    }

    throw lastError;
}

/**
 * Enhanced API request with retry logic
 * @param {string} url - API endpoint URL
 * @param {object} options - Fetch options
 * @param {number} maxRetries - Maximum retries (default: 3)
 * @returns {Promise} - Fetch promise
 */
async function apiRequestWithRetry(url, options = {}, maxRetries = 3) {
    return retryWithBackoff(
        () => apiRequest(url, options),
        maxRetries,
        1000
    );
}

/**
 * Sleep/delay utility
 * @param {number} ms - Milliseconds to sleep
 * @returns {Promise} - Promise that resolves after delay
 */
function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

/**
 * Check if user is online
 * @returns {boolean} - Online status
 */
function isOnline() {
    return navigator.onLine;
}

/**
 * Wait for online status
 * @param {number} timeout - Maximum wait time in ms (default: 30000)
 * @returns {Promise<boolean>} - True if online, false if timeout
 */
function waitForOnline(timeout = 30000) {
    return new Promise((resolve) => {
        if (navigator.onLine) {
            resolve(true);
            return;
        }

        const timeoutId = setTimeout(() => {
            window.removeEventListener('online', onlineHandler);
            resolve(false);
        }, timeout);

        const onlineHandler = () => {
            clearTimeout(timeoutId);
            window.removeEventListener('online', onlineHandler);
            resolve(true);
        };

        window.addEventListener('online', onlineHandler);
    });
}

// Offline/Online detection
window.addEventListener('offline', () => {
    showToast('You are offline. Some features may be unavailable.', 'warning', 0);
});

window.addEventListener('online', () => {
    showToast('Back online!', 'success', 3000);
    // Dismiss any offline warnings
    const offlineToasts = document.querySelectorAll('.toast.bg-warning');
    offlineToasts.forEach(toast => {
        const bsToast = bootstrap.Toast.getInstance(toast);
        if (bsToast) bsToast.hide();
    });
});

// Global error handler
window.addEventListener('error', (event) => {
    console.error('Global error:', event.error);

    // Don't show toast for script errors in development
    if (event.filename && event.filename.includes('chrome-extension')) {
        return;
    }

    // Show user-friendly error message
    if (!event.defaultPrevented) {
        showToast('An unexpected error occurred. Please refresh if issues persist.', 'error');
    }
});

// Unhandled promise rejection handler
window.addEventListener('unhandledrejection', (event) => {
    console.error('Unhandled promise rejection:', event.reason);

    if (!event.defaultPrevented) {
        showToast('A background operation failed. Please try again.', 'error');
    }
});
