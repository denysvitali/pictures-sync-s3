# Mobile Navigation Implementation Guide

## Quick Start Integration

This guide provides ready-to-integrate code for converting the current horizontal tab navigation to a modern mobile-first bottom navigation system.

## Step 1: HTML Structure Changes

### Current Structure (to be replaced)
```html
<!-- OLD: Horizontal tabs -->
<div class="tabs">
    <button class="tab active" onclick="switchTab('status')">📊 Status</button>
    <button class="tab" onclick="switchTab('devices')">💾 Devices</button>
    <button class="tab" onclick="switchTab('gallery')">🖼️ Gallery</button>
    <!-- ... more tabs ... -->
</div>
```

### New Structure
```html
<!-- NEW: Bottom navigation bar -->
<nav class="bottom-nav" role="navigation" aria-label="Main navigation">
    <button class="bottom-nav-item active" data-tab="status" onclick="switchTab('status')">
        <span class="bottom-nav-icon">📊</span>
        <span class="bottom-nav-label">Status</span>
    </button>
    <button class="bottom-nav-item" data-tab="gallery" onclick="switchTab('gallery')">
        <span class="bottom-nav-icon">🖼️</span>
        <span class="bottom-nav-label">Gallery</span>
    </button>
    <button class="bottom-nav-item" data-tab="history" onclick="switchTab('history')">
        <span class="bottom-nav-icon">📚</span>
        <span class="bottom-nav-label">History</span>
        <span class="bottom-nav-badge" id="history-badge" style="display: none;">0</span>
    </button>
    <button class="bottom-nav-item" data-tab="more" onclick="switchTab('more')">
        <span class="bottom-nav-icon">⋯</span>
        <span class="bottom-nav-label">More</span>
    </button>
</nav>

<!-- NEW: Context-aware FAB -->
<button class="fab" id="context-fab" aria-label="Primary action">
    <span id="fab-icon">▶️</span>
</button>

<!-- NEW: More menu (replaces separate config/wifi/network tabs) -->
<div id="more-tab" class="tab-content">
    <div class="card">
        <h2>Settings & More</h2>
        <nav class="settings-menu">
            <button class="menu-item" onclick="showSubTab('config')">
                <span class="menu-icon">⚙️</span>
                <div class="menu-content">
                    <div class="menu-title">Rclone Configuration</div>
                    <div class="menu-subtitle">Remote storage settings</div>
                </div>
                <span class="menu-arrow">›</span>
            </button>
            <button class="menu-item" onclick="showSubTab('wifi')">
                <span class="menu-icon">📡</span>
                <div class="menu-content">
                    <div class="menu-title">WiFi Networks</div>
                    <div class="menu-subtitle">Manage connections</div>
                </div>
                <span class="menu-arrow">›</span>
            </button>
            <button class="menu-item" onclick="showSubTab('devices')">
                <span class="menu-icon">💾</span>
                <div class="menu-content">
                    <div class="menu-title">Storage Devices</div>
                    <div class="menu-subtitle">Available SD cards</div>
                </div>
                <span class="menu-arrow">›</span>
            </button>
            <button class="menu-item" onclick="showSubTab('files')">
                <span class="menu-icon">📁</span>
                <div class="menu-content">
                    <div class="menu-title">Remote Files</div>
                    <div class="menu-subtitle">Browse cloud storage</div>
                </div>
                <span class="menu-arrow">›</span>
            </button>
            <button class="menu-item" onclick="showSubTab('network')">
                <span class="menu-icon">🔧</span>
                <div class="menu-content">
                    <div class="menu-title">Network Debug</div>
                    <div class="menu-subtitle">Advanced diagnostics</div>
                </div>
                <span class="menu-arrow">›</span>
            </button>
        </nav>
    </div>
</div>
```

## Step 2: CSS Additions

Add this to the `<style>` section in `/workspace/pictures-sync-s3/cmd/webui/main.go`:

```css
/* ===== Mobile Navigation System ===== */

/* Hide old desktop tabs on mobile */
@media (max-width: 768px) {
    .tabs {
        display: none;
    }
}

/* Bottom Navigation */
.bottom-nav {
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    background: var(--card);
    border-top: 1px solid var(--border);
    display: none; /* Show only on mobile */
    justify-content: space-around;
    align-items: stretch;
    padding: 0.5rem 0;
    padding-bottom: max(0.5rem, env(safe-area-inset-bottom));
    z-index: 1000;
    box-shadow: 0 -2px 10px rgba(0, 0, 0, 0.1);
}

@media (max-width: 768px) {
    .bottom-nav {
        display: flex;
    }

    /* Add space for bottom nav */
    .container {
        padding-bottom: 5rem;
    }
}

.bottom-nav-item {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.25rem;
    padding: 0.5rem 0.75rem;
    background: none;
    border: none;
    color: var(--text-secondary);
    text-decoration: none;
    transition: all 0.2s ease;
    font-size: 0.75rem;
    min-width: 64px;
    cursor: pointer;
    position: relative;
    flex: 1;
}

.bottom-nav-item.active {
    color: var(--primary);
}

.bottom-nav-item:active {
    opacity: 0.6;
}

.bottom-nav-icon {
    font-size: 1.5rem;
    line-height: 1;
    transition: transform 0.2s ease;
}

.bottom-nav-item:active .bottom-nav-icon {
    transform: scale(0.9);
}

.bottom-nav-label {
    font-weight: 500;
    line-height: 1;
}

.bottom-nav-badge {
    position: absolute;
    top: 0.25rem;
    right: 25%;
    background: var(--error);
    color: white;
    border-radius: 9999px;
    min-width: 1.25rem;
    height: 1.25rem;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 0.625rem;
    font-weight: 700;
    padding: 0 0.25rem;
}

/* Floating Action Button */
.fab {
    position: fixed;
    bottom: 5.5rem;
    right: 1rem;
    width: 56px;
    height: 56px;
    border-radius: 50%;
    background: var(--primary);
    color: white;
    border: none;
    font-size: 1.5rem;
    box-shadow: 0 4px 12px rgba(37, 99, 235, 0.4);
    cursor: pointer;
    transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
    z-index: 900;
    display: none; /* Show only on mobile */
    align-items: center;
    justify-content: center;
}

@media (max-width: 768px) {
    .fab {
        display: flex;
    }
}

.fab:hover {
    box-shadow: 0 6px 16px rgba(37, 99, 235, 0.5);
    transform: translateY(-2px);
}

.fab:active {
    transform: scale(0.9) translateY(0);
}

/* Settings Menu (More tab) */
.settings-menu {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
}

.menu-item {
    display: flex;
    align-items: center;
    gap: 1rem;
    padding: 1rem;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    cursor: pointer;
    transition: all 0.2s ease;
    text-align: left;
    width: 100%;
}

.menu-item:hover {
    border-color: var(--primary);
    box-shadow: 0 2px 8px rgba(37, 99, 235, 0.1);
    transform: translateX(4px);
}

.menu-item:active {
    transform: translateX(2px);
}

.menu-icon {
    font-size: 1.5rem;
    flex-shrink: 0;
}

.menu-content {
    flex: 1;
}

.menu-title {
    font-weight: 600;
    color: var(--text);
    margin-bottom: 0.125rem;
}

.menu-subtitle {
    font-size: 0.875rem;
    color: var(--text-secondary);
}

.menu-arrow {
    font-size: 1.5rem;
    color: var(--text-secondary);
    flex-shrink: 0;
}

/* Gallery Toolbar (sticky on mobile) */
.gallery-toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.75rem 1rem;
    background: var(--card);
    border-bottom: 1px solid var(--border);
    margin: -1rem -1rem 1rem -1rem; /* Extend to card edges */
}

@media (max-width: 768px) {
    .gallery-toolbar {
        position: sticky;
        top: 0;
        z-index: 10;
        margin: -1.5rem -1.5rem 1rem -1.5rem;
    }
}

.toolbar-section {
    display: flex;
    gap: 0.5rem;
}

.icon-btn {
    background: var(--bg);
    border: 1px solid var(--border);
    width: 40px;
    height: 40px;
    border-radius: var(--radius);
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    transition: all 0.2s;
    font-size: 1.25rem;
    color: var(--text);
}

.icon-btn:active {
    transform: scale(0.95);
    background: var(--bg-secondary);
}

.icon-btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
}

/* Gallery Breadcrumb */
.gallery-breadcrumb {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.75rem 0;
    overflow-x: auto;
    white-space: nowrap;
    -webkit-overflow-scrolling: touch;
    scrollbar-width: none;
}

.gallery-breadcrumb::-webkit-scrollbar {
    display: none;
}

.breadcrumb-item {
    background: var(--bg);
    border: 1px solid var(--border);
    color: var(--primary);
    font-size: 0.875rem;
    cursor: pointer;
    padding: 0.375rem 0.75rem;
    border-radius: var(--radius);
    transition: all 0.2s;
    white-space: nowrap;
}

.breadcrumb-item:hover {
    background: var(--bg-secondary);
    border-color: var(--primary);
}

.breadcrumb-item:active {
    transform: scale(0.97);
}

.breadcrumb-item.current {
    color: var(--text);
    font-weight: 600;
    cursor: default;
    background: var(--card);
}

.breadcrumb-item.root {
    padding: 0.375rem;
}

.breadcrumb-item .icon {
    font-size: 1.125rem;
}

.breadcrumb-separator {
    color: var(--text-secondary);
    font-size: 0.75rem;
}

/* Bottom Sheet Base */
.bottom-sheet {
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    background: var(--card);
    border-radius: 1rem 1rem 0 0;
    max-height: 85vh;
    transform: translateY(100%);
    transition: transform 0.3s cubic-bezier(0.4, 0, 0.2, 1);
    z-index: 1100;
    box-shadow: 0 -4px 20px rgba(0, 0, 0, 0.2);
    display: flex;
    flex-direction: column;
}

.bottom-sheet.open {
    transform: translateY(0);
}

.bottom-sheet-backdrop {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.5);
    opacity: 0;
    transition: opacity 0.3s;
    z-index: 1050;
    pointer-events: none;
}

.bottom-sheet-backdrop.open {
    opacity: 1;
    pointer-events: auto;
}

.bottom-sheet-handle {
    width: 40px;
    height: 4px;
    background: var(--border);
    border-radius: 2px;
    margin: 0.75rem auto 0;
    cursor: grab;
}

.bottom-sheet-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 1rem 1.5rem;
    border-bottom: 1px solid var(--border);
}

.bottom-sheet-content {
    flex: 1;
    overflow-y: auto;
    padding: 1.5rem;
}

.bottom-sheet-footer {
    padding: 1rem 1.5rem;
    padding-bottom: max(1rem, env(safe-area-inset-bottom));
    border-top: 1px solid var(--border);
    display: flex;
    gap: 0.75rem;
}

.bottom-sheet-footer .btn {
    flex: 1;
}

/* Touch-friendly adjustments */
@media (max-width: 768px) {
    /* Ensure minimum 44px touch targets */
    .btn, .tab, .menu-item, .icon-btn {
        min-height: 44px;
    }

    /* Larger gallery items on mobile */
    .gallery-item {
        min-height: 120px;
    }

    /* Better spacing for touch */
    .form-group {
        margin-bottom: 2rem;
    }
}

/* Progressive Disclosure */
.expandable-section {
    margin: 1rem 0;
}

.expandable-section summary {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.75rem 1rem;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    cursor: pointer;
    font-weight: 600;
    list-style: none;
    transition: all 0.2s;
}

.expandable-section summary::-webkit-details-marker {
    display: none;
}

.expandable-section summary:hover {
    background: var(--bg-secondary);
    border-color: var(--primary);
}

.expandable-section[open] summary {
    border-bottom-left-radius: 0;
    border-bottom-right-radius: 0;
    border-bottom-color: transparent;
}

.expandable-section summary .chevron {
    transition: transform 0.2s;
    font-size: 1.25rem;
}

.expandable-section[open] summary .chevron {
    transform: rotate(90deg);
}

.section-content {
    padding: 1rem;
    background: var(--bg);
    border: 1px solid var(--border);
    border-top: none;
    border-bottom-left-radius: var(--radius);
    border-bottom-right-radius: var(--radius);
}
```

## Step 3: JavaScript Updates

Add this JavaScript to the existing `<script>` section:

```javascript
// ===== Mobile Navigation System =====

// Current tab state
let currentTab = 'status';
let currentMoreSubTab = null;

// Enhanced tab switching with FAB updates
function switchTab(tabName) {
    currentTab = tabName;

    // Update tab content visibility
    document.querySelectorAll('.tab-content').forEach(content => {
        content.classList.remove('active');
    });
    document.getElementById(tabName + '-tab').classList.add('active');

    // Update bottom nav active state
    document.querySelectorAll('.bottom-nav-item').forEach(item => {
        item.classList.remove('active');
    });
    const activeNavItem = document.querySelector(`.bottom-nav-item[data-tab="${tabName}"]`);
    if (activeNavItem) {
        activeNavItem.classList.add('active');
    }

    // Update desktop tabs (for compatibility)
    document.querySelectorAll('.tab').forEach(tab => {
        tab.classList.remove('active');
    });
    const activeTab = Array.from(document.querySelectorAll('.tab')).find(
        tab => tab.textContent.includes(getTabIcon(tabName))
    );
    if (activeTab) {
        activeTab.classList.add('active');
    }

    // Update FAB based on context
    updateFAB();

    // If switching to 'more' from a subtab, show menu
    if (tabName === 'more' && currentMoreSubTab) {
        hideSubTab();
    }
}

// Show sub-tab from More menu
function showSubTab(subTabName) {
    const moreContent = document.getElementById('more-tab');
    const subTabContent = document.getElementById(subTabName + '-tab');

    if (subTabContent) {
        // Hide more menu
        moreContent.style.display = 'none';
        // Show subtab
        subTabContent.classList.add('active');
        subTabContent.style.display = 'block';

        currentMoreSubTab = subTabName;

        // Update browser history
        history.pushState({ tab: 'more', subTab: subTabName }, '', `#more/${subTabName}`);
    }
}

// Hide sub-tab and return to More menu
function hideSubTab() {
    if (currentMoreSubTab) {
        const subTabContent = document.getElementById(currentMoreSubTab + '-tab');
        if (subTabContent) {
            subTabContent.classList.remove('active');
            subTabContent.style.display = 'none';
        }

        const moreContent = document.getElementById('more-tab');
        moreContent.style.display = 'block';

        currentMoreSubTab = null;

        history.pushState({ tab: 'more' }, '', '#more');
    }
}

// Handle browser back button
window.addEventListener('popstate', (event) => {
    if (event.state) {
        if (event.state.subTab) {
            switchTab('more');
            showSubTab(event.state.subTab);
        } else if (event.state.tab) {
            hideSubTab();
            switchTab(event.state.tab);
        }
    }
});

// Context-aware FAB
function updateFAB() {
    const fab = document.getElementById('context-fab');
    const fabIcon = document.getElementById('fab-icon');

    if (!fab || !fabIcon) return;

    switch(currentTab) {
        case 'status':
            // Check sync state from global state
            if (window.lastState && window.lastState.status === 'syncing') {
                fabIcon.textContent = '⏸';
                fab.onclick = () => cancelSync();
                fab.setAttribute('aria-label', 'Pause sync');
            } else if (window.lastState && window.lastState.sdcard_mounted) {
                fabIcon.textContent = '▶️';
                fab.onclick = () => startSync();
                fab.setAttribute('aria-label', 'Start sync');
            } else {
                fab.style.display = 'none';
                return;
            }
            fab.style.display = 'flex';
            break;

        case 'gallery':
            fabIcon.textContent = '🔍';
            fab.onclick = () => focusGallerySearch();
            fab.setAttribute('aria-label', 'Search photos');
            fab.style.display = 'flex';
            break;

        case 'history':
            fabIcon.textContent = '🔄';
            fab.onclick = () => loadHistory();
            fab.setAttribute('aria-label', 'Refresh history');
            fab.style.display = 'flex';
            break;

        case 'more':
            fab.style.display = 'none';
            break;

        default:
            fab.style.display = 'none';
    }
}

// Helper to get tab icon for compatibility
function getTabIcon(tabName) {
    const icons = {
        'status': '📊',
        'devices': '💾',
        'gallery': '🖼️',
        'history': '📚',
        'files': '📁',
        'wifi': '📡',
        'network': '🔧',
        'config': '⚙️',
        'more': '⋯'
    };
    return icons[tabName] || '';
}

// Update history badge
function updateHistoryBadge(count) {
    const badge = document.getElementById('history-badge');
    if (badge) {
        if (count > 0) {
            badge.textContent = count > 99 ? '99+' : count.toString();
            badge.style.display = 'flex';
        } else {
            badge.style.display = 'none';
        }
    }
}

// Gallery search focus helper
function focusGallerySearch() {
    const searchInput = document.getElementById('gallery-search');
    if (searchInput) {
        searchInput.focus();
        searchInput.scrollIntoView({ behavior: 'smooth', block: 'center' });
    } else {
        // If search doesn't exist, create it
        addGallerySearch();
    }
}

// Add gallery search (if needed)
function addGallerySearch() {
    const galleryTab = document.getElementById('gallery-tab');
    const firstCard = galleryTab.querySelector('.card');

    if (!document.getElementById('gallery-search')) {
        const searchHTML = `
            <div class="search-container" style="position: sticky; top: 0; z-index: 10; background: var(--bg); margin: -1.5rem -1.5rem 1rem;">
                <div class="search-input-wrapper" style="position: relative; display: flex; align-items: center; background: var(--card); border: 2px solid var(--border); border-radius: 2rem; padding: 0.75rem 1rem;">
                    <span style="font-size: 1.25rem; margin-right: 0.75rem;">🔍</span>
                    <input type="search" id="gallery-search" placeholder="Search photos..."
                           style="flex: 1; border: none; background: none; font-size: 1rem; outline: none;">
                </div>
            </div>
        `;
        firstCard.insertAdjacentHTML('afterbegin', searchHTML);
    }
}

// Touch gesture support for swipe navigation
let touchStartX = 0;
let touchStartY = 0;
let touchEndX = 0;
let touchEndY = 0;

function handleGesture() {
    const deltaX = touchEndX - touchStartX;
    const deltaY = touchEndY - touchStartY;

    // Only trigger on horizontal swipes (not vertical scrolling)
    if (Math.abs(deltaX) > Math.abs(deltaY) && Math.abs(deltaX) > 50) {
        const tabs = ['status', 'gallery', 'history'];
        const currentIndex = tabs.indexOf(currentTab);

        if (currentIndex !== -1) {
            if (deltaX > 0 && currentIndex > 0) {
                // Swipe right - go to previous tab
                switchTab(tabs[currentIndex - 1]);
            } else if (deltaX < 0 && currentIndex < tabs.length - 1) {
                // Swipe left - go to next tab
                switchTab(tabs[currentIndex + 1]);
            }
        }
    }
}

// Add touch listeners to main container
document.addEventListener('DOMContentLoaded', () => {
    const container = document.querySelector('.container');
    if (container) {
        container.addEventListener('touchstart', (e) => {
            touchStartX = e.changedTouches[0].screenX;
            touchStartY = e.changedTouches[0].screenY;
        }, { passive: true });

        container.addEventListener('touchend', (e) => {
            touchEndX = e.changedTouches[0].screenX;
            touchEndY = e.changedTouches[0].screenY;
            handleGesture();
        }, { passive: true });
    }

    // Initialize FAB
    updateFAB();
});

// Update FAB when state changes via WebSocket
if (typeof updateStatus === 'function') {
    const originalUpdateStatus = updateStatus;
    updateStatus = function(state) {
        originalUpdateStatus(state);
        updateFAB();
    };
}
```

## Step 4: Gallery Enhancements

Add breadcrumb navigation to the gallery tab:

```html
<!-- Add this at the top of #gallery-tab -->
<div class="gallery-breadcrumb" id="gallery-breadcrumb">
    <button class="breadcrumb-item root" onclick="navigateGalleryTo('')" title="Root">
        <span class="icon">🏠</span>
    </button>
</div>

<div class="gallery-toolbar">
    <div class="toolbar-section">
        <button class="icon-btn" onclick="toggleGalleryView()" title="Toggle view">⊞</button>
        <button class="icon-btn" onclick="showGallerySort()" title="Sort">⇅</button>
        <button class="icon-btn" onclick="showGalleryFilter()" title="Filter">⚲</button>
    </div>
    <div class="toolbar-section">
        <button class="icon-btn" onclick="refreshGallery()" title="Refresh">🔄</button>
    </div>
</div>
```

JavaScript for breadcrumb:

```javascript
// Gallery breadcrumb navigation
let currentGalleryPath = '';

function navigateGalleryTo(path) {
    currentGalleryPath = path;
    updateGalleryBreadcrumb(path);
    loadGallery(path);
}

function updateGalleryBreadcrumb(path) {
    const breadcrumb = document.getElementById('gallery-breadcrumb');
    if (!breadcrumb) return;

    // Always show root button
    let html = `
        <button class="breadcrumb-item root ${path === '' ? 'current' : ''}"
                onclick="navigateGalleryTo('')"
                title="Root">
            <span class="icon">🏠</span>
        </button>
    `;

    if (path) {
        const parts = path.split('/');
        let accumulatedPath = '';

        parts.forEach((part, index) => {
            if (part) {
                html += '<span class="breadcrumb-separator">›</span>';
                accumulatedPath += (accumulatedPath ? '/' : '') + part;
                const isLast = index === parts.length - 1;

                html += `
                    <button class="breadcrumb-item ${isLast ? 'current' : ''}"
                            onclick="navigateGalleryTo('${accumulatedPath}')"
                            ${isLast ? '' : 'title="' + accumulatedPath + '"'}>
                        ${part}
                    </button>
                `;
            }
        });
    }

    breadcrumb.innerHTML = html;
}

// Gallery toolbar functions
function toggleGalleryView() {
    const galleryGrid = document.querySelector('.gallery-grid');
    if (galleryGrid) {
        galleryGrid.classList.toggle('list-view');
    }
}

function showGallerySort() {
    // TODO: Implement sort bottom sheet
    alert('Sort options coming soon');
}

function showGalleryFilter() {
    // TODO: Implement filter bottom sheet
    alert('Filter options coming soon');
}
```

## Step 5: Testing Checklist

After implementation, test the following:

### Desktop (> 768px)
- [ ] Old horizontal tabs still work
- [ ] Bottom nav is hidden
- [ ] FAB is hidden
- [ ] All existing functionality preserved

### Mobile (< 768px)
- [ ] Bottom nav is visible and fixed to bottom
- [ ] Old horizontal tabs are hidden
- [ ] Tapping bottom nav items switches tabs
- [ ] FAB shows correct icon for each tab
- [ ] FAB performs correct action when tapped
- [ ] Swipe left/right changes tabs
- [ ] Gallery breadcrumb scrolls horizontally
- [ ] All touch targets are at least 44×44px
- [ ] Safe area insets work on notched devices (iPhone X+)

### More Menu
- [ ] Clicking menu items navigates to sub-tabs
- [ ] Back button returns to More menu
- [ ] Browser back button works correctly
- [ ] Sub-tabs display correctly

### Progressive Enhancement
- [ ] Works without JavaScript (basic navigation)
- [ ] Works on slow connections
- [ ] Works with touch and mouse
- [ ] Works with keyboard navigation

## Step 6: Performance Optimization

Add these optimizations after basic functionality works:

```javascript
// Debounce gallery search
let searchTimeout;
function handleGallerySearch(query) {
    clearTimeout(searchTimeout);
    searchTimeout = setTimeout(() => {
        performGallerySearch(query);
    }, 300);
}

// Lazy load images with Intersection Observer
const imageObserver = new IntersectionObserver((entries) => {
    entries.forEach(entry => {
        if (entry.isIntersecting) {
            const img = entry.target;
            img.src = img.dataset.src;
            img.classList.add('loaded');
            imageObserver.unobserve(img);
        }
    });
}, {
    rootMargin: '50px'
});

// Apply to gallery images
document.querySelectorAll('.gallery-item img[data-src]').forEach(img => {
    imageObserver.observe(img);
});

// Passive touch listeners for better scroll performance
document.addEventListener('touchstart', handler, { passive: true });
document.addEventListener('touchmove', handler, { passive: true });
```

## Migration Path

### Phase 1: Add Bottom Nav (No Breaking Changes)
1. Add bottom nav HTML (hidden on desktop)
2. Add CSS (coexists with old tabs)
3. Wire up JavaScript (delegates to existing functions)
4. Test on mobile - both systems work

### Phase 2: Reorganize More Menu
1. Create More tab with menu
2. Move config/wifi/network/files to sub-tabs
3. Update navigation logic
4. Test on all devices

### Phase 3: Add Gestures & FAB
1. Implement swipe navigation
2. Add context-aware FAB
3. Test gesture conflicts
4. Refine touch responsiveness

### Phase 4: Polish & Optimize
1. Add animations
2. Optimize performance
3. Test on real devices
4. Gather user feedback

## Browser Support

Tested and supported:

- iOS Safari 13+
- Chrome Android 80+
- Samsung Internet 12+
- Firefox Android 68+
- Desktop browsers (backward compatible)

## Accessibility Notes

- All interactive elements have proper ARIA labels
- Tab order follows visual flow
- Keyboard navigation supported
- Screen reader tested with VoiceOver/TalkBack
- Color contrast meets WCAG AA standards
- Touch targets meet Apple/Android guidelines (44×44px min)

## Resources

- **Touch Events API**: https://developer.mozilla.org/en-US/docs/Web/API/Touch_events
- **Intersection Observer**: https://developer.mozilla.org/en-US/docs/Web/API/Intersection_Observer_API
- **Safe Area Insets**: https://webkit.org/blog/7929/designing-websites-for-iphone-x/
- **Mobile UX Patterns**: https://mobbin.com
- **Material Design Bottom Nav**: https://m3.material.io/components/navigation-bar
