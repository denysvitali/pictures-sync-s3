# Mobile Navigation Design for Photo Backup UI

## Executive Summary

This document outlines a comprehensive mobile-first navigation system for the Raspberry Pi photo backup appliance web UI. The design prioritizes quick access to sync status and gallery while maintaining efficient navigation patterns optimized for one-handed mobile use.

## Current State Analysis

**Current Implementation:**
- Horizontal tab bar with 8 tabs (Status, Devices, Gallery, History, Files, WiFi, Network Debug, Configuration)
- Desktop-first design with horizontal scrolling on mobile
- No bottom navigation or gesture support
- Limited mobile optimization (only basic responsive breakpoints at 768px)

**Issues:**
- Too many top-level tabs for mobile screens
- Horizontal scroll for tabs is difficult on mobile
- No quick access to primary functions
- Configuration/debug tabs clutter primary navigation
- No search or filtering capabilities
- Gallery lacks advanced navigation features

## Recommended Navigation Architecture

### 1. Bottom Navigation Bar (Primary Navigation)

**Pattern:** Fixed bottom navigation with 4-5 primary items

**Recommended Items:**
1. **Status** (Home icon) - Default view
   - Current sync status
   - Quick start/stop controls
   - Real-time progress

2. **Gallery** (Photo grid icon)
   - Browse SD card photos
   - Most-used feature for users

3. **History** (Clock/list icon)
   - Past sync operations
   - Card management

4. **More** (Menu/hamburger icon)
   - Settings
   - WiFi configuration
   - Network debug
   - Advanced features

**Implementation Details:**

```css
/* Bottom Navigation */
.bottom-nav {
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    background: var(--card);
    border-top: 1px solid var(--border);
    display: flex;
    justify-content: space-around;
    padding: 0.5rem 0;
    z-index: 1000;
    box-shadow: 0 -2px 10px rgba(0, 0, 0, 0.1);
    /* Safe area for iOS notch/home indicator */
    padding-bottom: max(0.5rem, env(safe-area-inset-bottom));
}

.bottom-nav-item {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.25rem;
    padding: 0.5rem 1rem;
    color: var(--text-secondary);
    text-decoration: none;
    transition: all 0.2s ease;
    font-size: 0.75rem;
    min-width: 64px;
    cursor: pointer;
}

.bottom-nav-item.active {
    color: var(--primary);
}

.bottom-nav-icon {
    font-size: 1.5rem;
    transition: transform 0.2s ease;
}

.bottom-nav-item:active .bottom-nav-icon {
    transform: scale(0.9);
}

.bottom-nav-badge {
    position: absolute;
    top: 0.25rem;
    right: 0.25rem;
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
}
```

**HTML Structure:**

```html
<nav class="bottom-nav">
    <a href="#status" class="bottom-nav-item active" data-tab="status">
        <span class="bottom-nav-icon">📊</span>
        <span>Status</span>
    </a>
    <a href="#gallery" class="bottom-nav-item" data-tab="gallery">
        <span class="bottom-nav-icon">🖼️</span>
        <span>Gallery</span>
    </a>
    <a href="#history" class="bottom-nav-item" data-tab="history">
        <span class="bottom-nav-icon">📚</span>
        <span>History</span>
        <span class="bottom-nav-badge">3</span> <!-- New syncs indicator -->
    </a>
    <a href="#more" class="bottom-nav-item" data-tab="more">
        <span class="bottom-nav-icon">⋯</span>
        <span>More</span>
    </a>
</nav>
```

### 2. Gesture-Based Navigation

**Swipe Gestures:**
- **Swipe left/right** between main tabs (Status ↔ Gallery ↔ History)
- **Swipe down** to refresh current view
- **Swipe up** from gallery item for quick actions menu
- **Pinch zoom** in lightbox for photo viewing
- **Double tap** to zoom to 100% in lightbox

**Implementation:**

```javascript
// Touch gesture handler
class TouchGestureHandler {
    constructor(element, callbacks) {
        this.element = element;
        this.callbacks = callbacks;
        this.startX = 0;
        this.startY = 0;
        this.currentX = 0;
        this.currentY = 0;
        this.threshold = 50; // minimum swipe distance

        this.element.addEventListener('touchstart', this.handleTouchStart.bind(this), { passive: true });
        this.element.addEventListener('touchmove', this.handleTouchMove.bind(this), { passive: false });
        this.element.addEventListener('touchend', this.handleTouchEnd.bind(this), { passive: true });
    }

    handleTouchStart(e) {
        this.startX = e.touches[0].clientX;
        this.startY = e.touches[0].clientY;
    }

    handleTouchMove(e) {
        this.currentX = e.touches[0].clientX;
        this.currentY = e.touches[0].clientY;

        // Visual feedback for swipe
        const deltaX = this.currentX - this.startX;
        if (Math.abs(deltaX) > 10 && this.callbacks.onSwipePreview) {
            this.callbacks.onSwipePreview(deltaX);
        }
    }

    handleTouchEnd(e) {
        const deltaX = this.currentX - this.startX;
        const deltaY = this.currentY - this.startY;

        // Horizontal swipe
        if (Math.abs(deltaX) > Math.abs(deltaY) && Math.abs(deltaX) > this.threshold) {
            if (deltaX > 0 && this.callbacks.onSwipeRight) {
                this.callbacks.onSwipeRight();
            } else if (deltaX < 0 && this.callbacks.onSwipeLeft) {
                this.callbacks.onSwipeLeft();
            }
        }

        // Vertical swipe
        else if (Math.abs(deltaY) > Math.abs(deltaX) && Math.abs(deltaY) > this.threshold) {
            if (deltaY > 0 && this.callbacks.onSwipeDown) {
                this.callbacks.onSwipeDown();
            } else if (deltaY < 0 && this.callbacks.onSwipeUp) {
                this.callbacks.onSwipeUp();
            }
        }

        // Reset
        this.currentX = 0;
        this.currentY = 0;
    }
}

// Usage
const mainContent = document.querySelector('.main-content');
const gestures = new TouchGestureHandler(mainContent, {
    onSwipeLeft: () => navigateToNextTab(),
    onSwipeRight: () => navigateToPreviousTab(),
    onSwipeDown: () => refreshCurrentView(),
    onSwipePreview: (delta) => showSwipeIndicator(delta)
});
```

### 3. Progressive Disclosure System

**Concept:** Hide advanced features behind layers to reduce cognitive load

**Layer 1 - Primary Actions (Always Visible):**
- Current sync status
- Start/Stop sync button
- Gallery thumbnail grid
- Most recent sync history

**Layer 2 - Secondary Actions (One Tap Away):**
- Device selection
- Gallery folder navigation
- Filter/sort options
- WiFi networks list

**Layer 3 - Advanced Features (More Menu):**
- Configuration
- Network debug
- Rclone settings
- System logs

**Implementation Example:**

```html
<!-- Status Tab - Progressive Disclosure -->
<div id="status-tab" class="tab-content active">
    <!-- Layer 1: Primary Status -->
    <div class="status-primary">
        <div class="status-card">
            <div class="status-indicator syncing">
                <div class="pulse-ring"></div>
                <span class="status-icon">⟳</span>
            </div>
            <h2>Syncing Photos</h2>
            <p class="status-subtitle">150 of 500 files</p>
        </div>

        <!-- Large Action Button -->
        <button class="action-button primary" id="sync-action">
            <span class="icon">⏸</span>
            <span class="label">Pause Sync</span>
        </button>
    </div>

    <!-- Layer 2: Expandable Details -->
    <details class="expandable-section">
        <summary class="section-header">
            <span>Progress Details</span>
            <span class="chevron">›</span>
        </summary>
        <div class="section-content">
            <div class="progress-stats">
                <div class="stat">
                    <span class="stat-label">Transfer Rate</span>
                    <span class="stat-value">2.4 MB/s</span>
                </div>
                <div class="stat">
                    <span class="stat-label">Time Remaining</span>
                    <span class="stat-value">3m 42s</span>
                </div>
                <div class="stat">
                    <span class="stat-label">Data Transferred</span>
                    <span class="stat-value">1.2 GB / 3.8 GB</span>
                </div>
            </div>
        </div>
    </details>

    <!-- Layer 3: Advanced Options (collapsed by default) -->
    <details class="expandable-section advanced">
        <summary class="section-header">
            <span>Advanced Options</span>
            <span class="chevron">›</span>
        </summary>
        <div class="section-content">
            <button class="btn-text" onclick="showLogs()">View Sync Logs</button>
            <button class="btn-text" onclick="showCardDetails()">Card Information</button>
            <button class="btn-text" onclick="forceResync()">Force Re-sync</button>
        </div>
    </details>
</div>
```

### 4. Gallery-Specific Navigation

**Breadcrumb Navigation:**

```html
<nav class="gallery-breadcrumb" aria-label="Gallery navigation">
    <button class="breadcrumb-item root" onclick="navigateTo('')">
        <span class="icon">🏠</span>
    </button>
    <span class="separator">›</span>
    <button class="breadcrumb-item" onclick="navigateTo('DCIM')">
        DCIM
    </button>
    <span class="separator">›</span>
    <span class="breadcrumb-item current">
        100CANON
    </span>
</nav>
```

**Sticky Toolbar with Quick Actions:**

```html
<div class="gallery-toolbar sticky">
    <div class="toolbar-section">
        <button class="icon-btn" onclick="toggleView()" title="Grid/List view">
            <span>⊞</span>
        </button>
        <button class="icon-btn" onclick="showSort()" title="Sort options">
            <span>⇅</span>
        </button>
        <button class="icon-btn" onclick="showFilter()" title="Filter">
            <span>⚲</span>
        </button>
    </div>

    <div class="toolbar-section">
        <button class="icon-btn" onclick="selectMode()" title="Select multiple">
            <span>☑</span>
        </button>
        <button class="icon-btn" onclick="shareSelected()" title="Share" disabled>
            <span>⤴</span>
        </button>
    </div>
</div>
```

**Folder Navigation with Cards:**

```html
<div class="gallery-folders">
    <button class="folder-card" onclick="navigateToFolder('DCIM/100CANON')">
        <div class="folder-preview">
            <img src="/api/thumbnail/..." alt="">
            <img src="/api/thumbnail/..." alt="">
            <img src="/api/thumbnail/..." alt="">
            <img src="/api/thumbnail/..." alt="">
        </div>
        <div class="folder-info">
            <h3 class="folder-name">100CANON</h3>
            <p class="folder-meta">324 photos • 2.4 GB</p>
        </div>
        <span class="folder-arrow">›</span>
    </button>
</div>
```

**Infinite Scroll for Photos:**

```javascript
// Intersection Observer for lazy loading
const observerOptions = {
    root: null,
    rootMargin: '200px', // Load before user reaches end
    threshold: 0.1
};

const observer = new IntersectionObserver((entries) => {
    entries.forEach(entry => {
        if (entry.isIntersecting) {
            loadMorePhotos();
        }
    });
}, observerOptions);

// Observe sentinel element at bottom
const sentinel = document.querySelector('.gallery-sentinel');
observer.observe(sentinel);
```

### 5. Search and Filter System

**Search Bar with Auto-Complete:**

```html
<div class="search-container">
    <div class="search-input-wrapper">
        <span class="search-icon">🔍</span>
        <input
            type="search"
            class="search-input"
            placeholder="Search photos by date, camera, location..."
            autocomplete="off"
            id="gallery-search"
        />
        <button class="clear-search" onclick="clearSearch()" style="display: none;">
            ✕
        </button>
    </div>

    <!-- Auto-complete suggestions -->
    <div class="search-suggestions" id="search-suggestions">
        <div class="suggestion-group">
            <h4>Recent Searches</h4>
            <button class="suggestion-item">Canon EOS</button>
            <button class="suggestion-item">January 2025</button>
        </div>
        <div class="suggestion-group">
            <h4>Quick Filters</h4>
            <button class="suggestion-item">📷 Camera: Canon</button>
            <button class="suggestion-item">📅 This Month</button>
            <button class="suggestion-item">⭐ Favorites</button>
        </div>
    </div>
</div>
```

**Bottom Sheet Filter Panel:**

```html
<!-- Slide-up filter panel -->
<div class="bottom-sheet" id="filter-panel">
    <div class="bottom-sheet-handle"></div>

    <div class="bottom-sheet-header">
        <h3>Filter Photos</h3>
        <button class="btn-text" onclick="resetFilters()">Reset</button>
    </div>

    <div class="bottom-sheet-content">
        <!-- Date Range -->
        <div class="filter-section">
            <h4>Date Range</h4>
            <div class="date-pills">
                <button class="pill">Today</button>
                <button class="pill">This Week</button>
                <button class="pill">This Month</button>
                <button class="pill active">All Time</button>
            </div>
            <div class="date-range-picker">
                <input type="date" id="date-from">
                <span>to</span>
                <input type="date" id="date-to">
            </div>
        </div>

        <!-- Camera Model -->
        <div class="filter-section">
            <h4>Camera</h4>
            <div class="checkbox-list">
                <label class="checkbox-item">
                    <input type="checkbox" name="camera" value="Canon">
                    <span>Canon EOS (234 photos)</span>
                </label>
                <label class="checkbox-item">
                    <input type="checkbox" name="camera" value="Nikon">
                    <span>Nikon D850 (89 photos)</span>
                </label>
            </div>
        </div>

        <!-- File Type -->
        <div class="filter-section">
            <h4>File Type</h4>
            <div class="segmented-control">
                <button class="segment active">All</button>
                <button class="segment">JPG</button>
                <button class="segment">RAW</button>
                <button class="segment">Video</button>
            </div>
        </div>

        <!-- Resolution -->
        <div class="filter-section">
            <h4>Minimum Resolution</h4>
            <select class="form-select">
                <option value="">Any</option>
                <option value="1920x1080">Full HD (1920×1080)</option>
                <option value="3840x2160">4K (3840×2160)</option>
                <option value="5472x3648">20MP+</option>
            </select>
        </div>
    </div>

    <div class="bottom-sheet-footer">
        <button class="btn btn-secondary" onclick="closeBottomSheet()">Cancel</button>
        <button class="btn btn-primary" onclick="applyFilters()">Apply Filters</button>
    </div>
</div>
```

**Sort Options with Radio Selection:**

```html
<div class="sort-menu" id="sort-menu">
    <div class="menu-header">
        <h4>Sort By</h4>
    </div>
    <div class="radio-list">
        <label class="radio-item">
            <input type="radio" name="sort" value="date-desc" checked>
            <span>Newest First</span>
            <span class="checkmark">✓</span>
        </label>
        <label class="radio-item">
            <input type="radio" name="sort" value="date-asc">
            <span>Oldest First</span>
            <span class="checkmark">✓</span>
        </label>
        <label class="radio-item">
            <input type="radio" name="sort" value="name-asc">
            <span>Name (A-Z)</span>
            <span class="checkmark">✓</span>
        </label>
        <label class="radio-item">
            <input type="radio" name="sort" value="size-desc">
            <span>Largest First</span>
            <span class="checkmark">✓</span>
        </label>
    </div>
</div>
```

### 6. Quick Action Floating Button (FAB)

**Context-Aware FAB:**

```html
<button class="fab" id="context-fab" onclick="handleFabAction()">
    <span class="fab-icon" id="fab-icon">▶️</span>
</button>
```

```javascript
// Context-aware FAB
function updateFAB() {
    const fab = document.getElementById('context-fab');
    const fabIcon = document.getElementById('fab-icon');
    const currentTab = getCurrentTab();

    switch(currentTab) {
        case 'status':
            if (isSyncing()) {
                fabIcon.textContent = '⏸';
                fab.onclick = pauseSync;
            } else {
                fabIcon.textContent = '▶️';
                fab.onclick = startSync;
            }
            break;

        case 'gallery':
            fabIcon.textContent = '🔍';
            fab.onclick = openSearch;
            break;

        case 'history':
            fabIcon.textContent = '🔄';
            fab.onclick = refreshHistory;
            break;
    }
}
```

```css
.fab {
    position: fixed;
    bottom: 5rem; /* Above bottom nav */
    right: 1rem;
    width: 56px;
    height: 56px;
    border-radius: 50%;
    background: var(--primary);
    color: white;
    border: none;
    font-size: 1.5rem;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
    cursor: pointer;
    transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
    z-index: 900;
}

.fab:active {
    transform: scale(0.9);
}

.fab:hover {
    box-shadow: 0 6px 16px rgba(0, 0, 0, 0.4);
    transform: translateY(-2px);
}
```

### 7. Enhanced Lightbox Navigation

**Mobile-Optimized Lightbox:**

```html
<div class="lightbox mobile-optimized" id="lightbox">
    <!-- Top Controls Bar -->
    <div class="lightbox-top-bar">
        <button class="icon-btn" onclick="closeLightbox()">
            <span>✕</span>
        </button>
        <span class="photo-counter">5 / 234</span>
        <button class="icon-btn" onclick="sharePhoto()">
            <span>⤴</span>
        </button>
    </div>

    <!-- Photo Display with Gesture Support -->
    <div class="lightbox-photo-container" id="photo-container">
        <img
            id="lightbox-img"
            class="lightbox-image"
            src=""
            alt=""
            draggable="false"
        >
    </div>

    <!-- Swipeable Navigation Indicator -->
    <div class="swipe-indicator">
        <span class="indicator-dot prev">‹</span>
        <span class="indicator-dot current">•</span>
        <span class="indicator-dot next">›</span>
    </div>

    <!-- Bottom Info Drawer (Swipe up to expand) -->
    <div class="lightbox-info-drawer" id="info-drawer">
        <div class="drawer-handle"></div>

        <div class="drawer-content">
            <h3 id="photo-filename">IMG_1234.JPG</h3>

            <div class="exif-grid">
                <div class="exif-item">
                    <span class="exif-icon">📅</span>
                    <div>
                        <div class="exif-label">Date Taken</div>
                        <div class="exif-value">Jan 15, 2025 14:32</div>
                    </div>
                </div>
                <div class="exif-item">
                    <span class="exif-icon">📷</span>
                    <div>
                        <div class="exif-label">Camera</div>
                        <div class="exif-value">Canon EOS R5</div>
                    </div>
                </div>
                <div class="exif-item">
                    <span class="exif-icon">🔍</span>
                    <div>
                        <div class="exif-label">Settings</div>
                        <div class="exif-value">f/2.8 • 1/500s • ISO 400</div>
                    </div>
                </div>
                <div class="exif-item">
                    <span class="exif-icon">📏</span>
                    <div>
                        <div class="exif-label">Resolution</div>
                        <div class="exif-value">5472 × 3648 (20MP)</div>
                    </div>
                </div>
            </div>

            <!-- Quick Actions -->
            <div class="drawer-actions">
                <button class="action-btn">
                    <span>⤓</span> Download
                </button>
                <button class="action-btn">
                    <span>⤴</span> Share
                </button>
                <button class="action-btn">
                    <span>ℹ</span> Details
                </button>
            </div>
        </div>
    </div>
</div>
```

**Lightbox Gesture Controls:**

```javascript
// Enhanced lightbox with gestures
class PhotoLightbox {
    constructor() {
        this.currentIndex = 0;
        this.photos = [];
        this.scale = 1;
        this.translateX = 0;
        this.translateY = 0;
        this.initializeGestures();
    }

    initializeGestures() {
        const container = document.getElementById('photo-container');
        const image = document.getElementById('lightbox-img');

        let initialDistance = 0;
        let initialScale = 1;

        // Pinch to zoom
        container.addEventListener('touchstart', (e) => {
            if (e.touches.length === 2) {
                initialDistance = this.getDistance(e.touches[0], e.touches[1]);
                initialScale = this.scale;
            }
        });

        container.addEventListener('touchmove', (e) => {
            if (e.touches.length === 2) {
                e.preventDefault();
                const currentDistance = this.getDistance(e.touches[0], e.touches[1]);
                this.scale = initialScale * (currentDistance / initialDistance);
                this.scale = Math.max(1, Math.min(5, this.scale)); // Clamp between 1x and 5x
                this.updateTransform();
            }
        });

        // Double tap to zoom
        let lastTap = 0;
        container.addEventListener('touchend', (e) => {
            const currentTime = new Date().getTime();
            const tapLength = currentTime - lastTap;

            if (tapLength < 300 && tapLength > 0) {
                if (this.scale === 1) {
                    this.scale = 2.5;
                } else {
                    this.scale = 1;
                }
                this.updateTransform();
            }
            lastTap = currentTime;
        });

        // Horizontal swipe navigation
        new TouchGestureHandler(container, {
            onSwipeLeft: () => this.nextPhoto(),
            onSwipeRight: () => this.previousPhoto(),
            onSwipeDown: () => {
                if (this.scale === 1) {
                    this.close();
                }
            }
        });
    }

    getDistance(touch1, touch2) {
        const dx = touch1.clientX - touch2.clientX;
        const dy = touch1.clientY - touch2.clientY;
        return Math.sqrt(dx * dx + dy * dy);
    }

    updateTransform() {
        const image = document.getElementById('lightbox-img');
        image.style.transform = `scale(${this.scale}) translate(${this.translateX}px, ${this.translateY}px)`;
    }

    nextPhoto() {
        if (this.currentIndex < this.photos.length - 1) {
            this.currentIndex++;
            this.loadPhoto(this.photos[this.currentIndex]);
        }
    }

    previousPhoto() {
        if (this.currentIndex > 0) {
            this.currentIndex--;
            this.loadPhoto(this.photos[this.currentIndex]);
        }
    }
}
```

## CSS Framework for Mobile Navigation

```css
/* ===== Mobile Navigation Framework ===== */

/* Bottom Navigation */
.bottom-nav {
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    background: var(--card);
    border-top: 1px solid var(--border);
    display: flex;
    justify-content: space-around;
    padding: 0.5rem 0;
    padding-bottom: max(0.5rem, env(safe-area-inset-bottom));
    z-index: 1000;
    box-shadow: 0 -2px 10px rgba(0, 0, 0, 0.1);
}

.bottom-nav-item {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.25rem;
    padding: 0.5rem 1rem;
    color: var(--text-secondary);
    text-decoration: none;
    transition: all 0.2s ease;
    font-size: 0.75rem;
    min-width: 64px;
    cursor: pointer;
    position: relative;
}

.bottom-nav-item.active {
    color: var(--primary);
}

.bottom-nav-icon {
    font-size: 1.5rem;
    transition: transform 0.2s ease;
}

/* Tab animation */
.bottom-nav-item:active .bottom-nav-icon {
    transform: scale(0.9);
}

/* Badge for notifications */
.bottom-nav-badge {
    position: absolute;
    top: 0.25rem;
    right: 0.75rem;
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

/* Content area with bottom nav offset */
.main-content {
    padding-bottom: 5rem; /* Space for bottom nav */
}

/* Floating Action Button */
.fab {
    position: fixed;
    bottom: 5rem;
    right: 1rem;
    width: 56px;
    height: 56px;
    border-radius: 50%;
    background: var(--primary);
    color: white;
    border: none;
    font-size: 1.5rem;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
    cursor: pointer;
    transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
    z-index: 900;
    display: flex;
    align-items: center;
    justify-content: center;
}

.fab:active {
    transform: scale(0.9);
}

/* Bottom Sheet for Filters */
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

.bottom-sheet-handle {
    width: 40px;
    height: 4px;
    background: var(--border);
    border-radius: 2px;
    margin: 0.75rem auto;
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
    border-top: 1px solid var(--border);
    display: flex;
    gap: 0.75rem;
}

/* Search Bar */
.search-container {
    position: sticky;
    top: 0;
    background: var(--bg);
    padding: 1rem;
    z-index: 100;
}

.search-input-wrapper {
    position: relative;
    display: flex;
    align-items: center;
    background: var(--card);
    border: 2px solid var(--border);
    border-radius: 2rem;
    padding: 0.75rem 1rem;
    transition: all 0.3s ease;
}

.search-input-wrapper:focus-within {
    border-color: var(--primary);
    box-shadow: 0 0 0 4px rgba(37, 99, 235, 0.1);
}

.search-icon {
    font-size: 1.25rem;
    margin-right: 0.75rem;
    color: var(--text-secondary);
}

.search-input {
    flex: 1;
    border: none;
    background: none;
    font-size: 1rem;
    outline: none;
    color: var(--text);
}

.clear-search {
    background: none;
    border: none;
    color: var(--text-secondary);
    font-size: 1.25rem;
    cursor: pointer;
    padding: 0.25rem;
}

/* Breadcrumb Navigation */
.gallery-breadcrumb {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.75rem 1rem;
    overflow-x: auto;
    white-space: nowrap;
    -webkit-overflow-scrolling: touch;
}

.breadcrumb-item {
    background: none;
    border: none;
    color: var(--primary);
    font-size: 0.875rem;
    cursor: pointer;
    padding: 0.25rem 0.5rem;
    border-radius: 0.25rem;
    transition: background 0.2s;
}

.breadcrumb-item:hover {
    background: var(--bg-secondary);
}

.breadcrumb-item.current {
    color: var(--text);
    font-weight: 600;
    cursor: default;
}

.breadcrumb-item.root .icon {
    font-size: 1.25rem;
}

.separator {
    color: var(--text-secondary);
    font-size: 0.75rem;
}

/* Sticky Toolbar */
.gallery-toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.75rem 1rem;
    background: var(--card);
    border-bottom: 1px solid var(--border);
}

.gallery-toolbar.sticky {
    position: sticky;
    top: 0;
    z-index: 99;
}

.toolbar-section {
    display: flex;
    gap: 0.5rem;
}

.icon-btn {
    background: none;
    border: 1px solid var(--border);
    width: 40px;
    height: 40px;
    border-radius: 0.5rem;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    transition: all 0.2s;
    font-size: 1.25rem;
}

.icon-btn:active {
    transform: scale(0.95);
    background: var(--bg-secondary);
}

.icon-btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
}

/* Filter Pills */
.filter-pills {
    display: flex;
    gap: 0.5rem;
    padding: 0.5rem 1rem;
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
}

.pill {
    padding: 0.5rem 1rem;
    border: 1px solid var(--border);
    border-radius: 9999px;
    background: var(--card);
    font-size: 0.875rem;
    white-space: nowrap;
    cursor: pointer;
    transition: all 0.2s;
}

.pill.active {
    background: var(--primary);
    color: white;
    border-color: var(--primary);
}

/* Segmented Control */
.segmented-control {
    display: flex;
    background: var(--bg-secondary);
    border-radius: 0.5rem;
    padding: 0.25rem;
}

.segment {
    flex: 1;
    padding: 0.5rem 1rem;
    border: none;
    background: none;
    border-radius: 0.375rem;
    cursor: pointer;
    transition: all 0.2s;
    font-size: 0.875rem;
}

.segment.active {
    background: var(--card);
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}

/* Lightbox Mobile Optimizations */
.lightbox-top-bar {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 1rem;
    background: linear-gradient(to bottom, rgba(0,0,0,0.6), transparent);
    z-index: 10;
    color: white;
}

.photo-counter {
    font-size: 0.875rem;
    font-weight: 600;
}

.lightbox-info-drawer {
    position: absolute;
    bottom: 0;
    left: 0;
    right: 0;
    background: var(--card);
    border-radius: 1rem 1rem 0 0;
    max-height: 60%;
    transform: translateY(calc(100% - 60px));
    transition: transform 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.lightbox-info-drawer.expanded {
    transform: translateY(0);
}

.drawer-handle {
    width: 40px;
    height: 4px;
    background: var(--border);
    border-radius: 2px;
    margin: 0.75rem auto;
}

.swipe-indicator {
    position: absolute;
    bottom: 1rem;
    left: 50%;
    transform: translateX(-50%);
    display: flex;
    gap: 0.5rem;
    color: white;
    font-size: 0.75rem;
}

/* Touch-friendly sizing */
@media (max-width: 768px) {
    /* Minimum touch target 44x44px */
    .btn, .icon-btn, .tab, .bottom-nav-item {
        min-height: 44px;
        min-width: 44px;
    }

    /* Larger tap targets */
    .gallery-item {
        min-height: 120px;
    }

    /* Full-width buttons on mobile */
    .bottom-sheet-footer .btn {
        flex: 1;
    }
}

/* Safe area support for notched devices */
@supports (padding: max(0px)) {
    .bottom-nav {
        padding-bottom: max(0.5rem, env(safe-area-inset-bottom));
    }

    .bottom-sheet {
        padding-bottom: env(safe-area-inset-bottom);
    }
}
```

## Implementation Priority

### Phase 1: Core Navigation (Week 1)
1. Implement bottom navigation bar
2. Convert horizontal tabs to bottom nav
3. Add swipe gesture navigation between tabs
4. Mobile-responsive breakpoints

### Phase 2: Gallery Enhancements (Week 2)
1. Breadcrumb navigation for folders
2. Sticky toolbar with sort/filter
3. Enhanced lightbox with gestures
4. Infinite scroll implementation

### Phase 3: Search & Filter (Week 3)
1. Search bar with auto-complete
2. Bottom sheet filter panel
3. Quick filter pills
4. Sort menu

### Phase 4: Advanced Features (Week 4)
1. Progressive disclosure system
2. Context-aware FAB
3. Gesture refinements
4. Performance optimizations

## Performance Considerations

### Lazy Loading
- Load thumbnails on demand using Intersection Observer
- Defer non-critical tabs until first access
- Virtualize long lists (>100 items)

### Touch Performance
- Use `passive: true` for scroll listeners
- Implement CSS `will-change` for animations
- Debounce search input (300ms)
- Throttle scroll handlers (16ms / 60fps)

### Network Optimization
- Progressive image loading (blur-up technique)
- WebP format with JPEG fallback
- Request smaller thumbnails on mobile
- Batch API requests where possible

## Accessibility

### Touch Targets
- Minimum 44×44px touch targets
- Adequate spacing between interactive elements
- Visual feedback for all touch interactions

### Screen Readers
- Proper ARIA labels on navigation elements
- Live regions for status updates
- Semantic HTML structure

### Keyboard Navigation
- Tab order follows visual flow
- Enter/Space for button activation
- Escape to close modals/sheets
- Arrow keys for gallery navigation

## Testing Checklist

- [ ] Bottom nav works on iOS Safari, Chrome Android
- [ ] Swipe gestures don't conflict with browser gestures
- [ ] Safe area insets respected on notched devices
- [ ] Lightbox pinch-zoom smooth on low-end devices
- [ ] Search autocomplete responsive (<100ms)
- [ ] Filter panel smooth open/close animation
- [ ] Gallery infinite scroll doesn't cause jank
- [ ] All touch targets minimum 44×44px
- [ ] Works in landscape orientation
- [ ] Offline state clearly indicated

## Conclusion

This mobile navigation design prioritizes:

1. **Thumb-friendly navigation** with bottom bar
2. **Intuitive gestures** for common actions
3. **Progressive disclosure** to reduce clutter
4. **Fast, responsive interactions** optimized for mobile
5. **Gallery-centric design** matching photo management use case

The implementation uses modern web standards (CSS Grid, Intersection Observer, Touch Events) while maintaining broad compatibility with mobile browsers. The design is production-ready and can be implemented incrementally without disrupting existing functionality.
