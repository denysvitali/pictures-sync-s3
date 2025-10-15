# Mobile Navigation Wireframes

## Screen Layouts (ASCII Wireframes)

### 1. Status Tab (Home View)

```
┌─────────────────────────────────────┐
│  📸 Photo Backup Station        🌙  │ ← Header with theme toggle
├─────────────────────────────────────┤
│                                     │
│    ┌───────────────────────┐        │
│    │      ┌─────────┐      │        │
│    │      │  ⟳ 📷  │      │        │ ← Status indicator
│    │      └─────────┘      │        │   with pulse animation
│    │                       │        │
│    │   Syncing Photos      │        │
│    │   150 of 500 files    │        │
│    └───────────────────────┘        │
│                                     │
│    [==============·······]          │ ← Progress bar
│    68% • 2.4 MB/s                   │
│                                     │
│  ┌─────────────────────────────┐   │
│  │       ⏸  Pause Sync         │   │ ← Large action button
│  └─────────────────────────────┘   │
│                                     │
│  ⌃ Progress Details                 │ ← Expandable sections
│  ⌃ Advanced Options                 │   (collapsed by default)
│                                     │
│                                     │
│                                     │
│                                     │
│                                     │
│                                     │
├─────────────────────────────────────┤
│  📊     🖼️     📚     ⋯            │ ← Bottom navigation
│Status  Gallery History  More        │   (always visible)
└─────────────────────────────────────┘
                                  ┌───┐
                              ▶️  │ │ ← FAB (context-aware)
                                  └───┘
```

### 2. Gallery Tab with Folder Navigation

```
┌─────────────────────────────────────┐
│  🏠 › DCIM › 100CANON          🔍   │ ← Breadcrumb + Search
├─────────────────────────────────────┤
│  ⊞  ⇅  ⚲         ☑  ⤴             │ ← Sticky toolbar
├─────────────────────────────────────┤
│                                     │
│  ┌────┐ ┌────┐ ┌────┐ ┌────┐      │
│  │    │ │    │ │    │ │    │      │
│  │ 📷 │ │ 📷 │ │ 📷 │ │ 📷 │      │ ← Photo grid
│  │    │ │    │ │    │ │    │      │   (responsive)
│  └────┘ └────┘ └────┘ └────┘      │
│                                     │
│  ┌────┐ ┌────┐ ┌────┐ ┌────┐      │
│  │    │ │    │ │    │ │    │      │
│  │ 📷 │ │ 📷 │ │ 📷 │ │ 📷 │      │
│  │    │ │    │ │    │ │    │      │
│  └────┘ └────┘ └────┘ └────┘      │
│                                     │
│  ┌────┐ ┌────┐ ┌────┐ ┌────┐      │
│  │    │ │    │ │    │ │    │      │
│  │ 📷 │ │ 📷 │ │ 📷 │ │ 📷 │      │
│  │    │ │    │ │    │ │    │      │
│  └────┘ └────┘ └────┘ └────┘      │
│                                     │
│  Loading more...                    │ ← Infinite scroll
│                                     │
├─────────────────────────────────────┤
│  📊     🖼️     📚     ⋯            │
│Status  Gallery History  More        │
└─────────────────────────────────────┘
```

### 3. Gallery with Folder View

```
┌─────────────────────────────────────┐
│  🏠 › DCIM                      🔍   │
├─────────────────────────────────────┤
│  ⊞  ⇅  ⚲                           │
├─────────────────────────────────────┤
│                                     │
│  ┌─────────────────────────────┐   │
│  │ ┌──┐ ┌──┐ ┌──┐ ┌──┐        │   │
│  │ │  │ │  │ │  │ │  │   ›    │   │ ← Folder card with
│  │ └──┘ └──┘ └──┘ └──┘        │   │   4-photo preview
│  │ 100CANON                    │   │
│  │ 324 photos • 2.4 GB         │   │
│  └─────────────────────────────┘   │
│                                     │
│  ┌─────────────────────────────┐   │
│  │ ┌──┐ ┌──┐ ┌──┐ ┌──┐        │   │
│  │ │  │ │  │ │  │ │  │   ›    │   │
│  │ └──┘ └──┘ └──┘ └──┘        │   │
│  │ 101CANON                    │   │
│  │ 156 photos • 1.8 GB         │   │
│  └─────────────────────────────┘   │
│                                     │
│  ┌─────────────────────────────┐   │
│  │ ┌──┐ ┌──┐ ┌──┐ ┌──┐        │   │
│  │ │  │ │  │ │  │ │  │   ›    │   │
│  │ └──┘ └──┘ └──┘ └──┘        │   │
│  │ 102CANON                    │   │
│  │ 89 photos • 1.2 GB          │   │
│  └─────────────────────────────┘   │
│                                     │
├─────────────────────────────────────┤
│  📊     🖼️     📚     ⋯            │
│Status  Gallery History  More        │
└─────────────────────────────────────┘
```

### 4. Lightbox with Photo Viewer

```
┌─────────────────────────────────────┐
│  ✕            5/234            ⤴   │ ← Top controls
│                                     │
│                                     │
│                                     │
│          ┌─────────────┐            │
│          │             │            │
│     ‹    │    📷       │    ›       │ ← Swipe left/right
│          │   Photo     │            │   or tap arrows
│          │             │            │
│          └─────────────┘            │
│                                     │
│            ‹  •  ›                  │ ← Swipe indicator
│                                     │
│ ══════════════════════════════════  │ ← Drawer handle
│  IMG_1234.JPG                       │   (swipe up to expand)
│                                     │
│  📅 Jan 15, 2025   📷 Canon EOS R5  │
│  🔍 f/2.8 • 1/500s   📏 5472×3648   │
│                                     │
│  [ ⤓ Download ] [ ⤴ Share ] [ ℹ ]  │
└─────────────────────────────────────┘

Gestures:
• Swipe left/right: Next/previous photo
• Swipe down: Close lightbox
• Pinch: Zoom in/out
• Double tap: Toggle zoom
• Swipe up on bottom: Expand info drawer
```

### 5. Search with Autocomplete

```
┌─────────────────────────────────────┐
│  🔍  Search photos...           ✕   │ ← Search input
├─────────────────────────────────────┤
│                                     │
│  Recent Searches                    │
│  ┌─────────────────────────────┐   │
│  │ Canon EOS                   │   │ ← Suggestion items
│  └─────────────────────────────┘   │
│  ┌─────────────────────────────┐   │
│  │ January 2025                │   │
│  └─────────────────────────────┘   │
│                                     │
│  Quick Filters                      │
│  ┌─────────────────────────────┐   │
│  │ 📷 Camera: Canon            │   │
│  └─────────────────────────────┘   │
│  ┌─────────────────────────────┐   │
│  │ 📅 This Month               │   │
│  └─────────────────────────────┘   │
│  ┌─────────────────────────────┐   │
│  │ ⭐ Favorites                 │   │
│  └─────────────────────────────┘   │
│                                     │
│                                     │
│                                     │
├─────────────────────────────────────┤
│  📊     🖼️     📚     ⋯            │
│Status  Gallery History  More        │
└─────────────────────────────────────┘
```

### 6. Filter Bottom Sheet

```
┌─────────────────────────────────────┐
│                                     │
│         ══════                      │ ← Drag handle
│  Filter Photos              Reset   │
├─────────────────────────────────────┤
│                                     │
│  Date Range                         │
│  ┌──────┐ ┌──────┐ ┌──────┐ ┌──┐  │
│  │Today │ │ Week │ │Month │ │All│  │ ← Pills
│  └──────┘ └──────┘ └──────┘ └──┘  │
│                                     │
│  From: [2025-01-01] To: [2025-01-15]│
│                                     │
│  Camera                             │
│  ☑ Canon EOS (234 photos)           │ ← Checkboxes
│  ☐ Nikon D850 (89 photos)           │
│                                     │
│  File Type                          │
│  ┌────┬────┬─────┬──────┐          │
│  │All │JPG │ RAW │Video │          │ ← Segmented control
│  └────┴────┴─────┴──────┘          │
│                                     │
│  Minimum Resolution                 │
│  [Any ▼]                            │ ← Select dropdown
│                                     │
├─────────────────────────────────────┤
│  [ Cancel ]    [ Apply Filters ]    │ ← Action buttons
└─────────────────────────────────────┘
```

### 7. Sort Menu (Overlay)

```
┌─────────────────────────────────────┐
│  Gallery                            │
│                                     │
│                                     │
│    ┌───────────────────────────┐   │
│    │ Sort By                   │   │
│    ├───────────────────────────┤   │
│    │ ◉ Newest First         ✓ │   │ ← Radio selection
│    │ ○ Oldest First           │   │
│    │ ○ Name (A-Z)             │   │
│    │ ○ Largest First          │   │
│    └───────────────────────────┘   │
│                                     │
│                                     │
│                                     │
│                                     │
├─────────────────────────────────────┤
│  📊     🖼️     📚     ⋯            │
│Status  Gallery History  More        │
└─────────────────────────────────────┘
```

### 8. History Tab

```
┌─────────────────────────────────────┐
│  📸 Photo Backup Station        🌙  │
├─────────────────────────────────────┤
│                                     │
│  Sync History                       │
│                                     │
│  ┌─────────────────────────────┐   │
│  │ ✅ Sync Completed           │   │
│  │ Card: card-a1b2c3d4         │   │
│  │ 234 files • 1.8 GB          │   │ ← History card
│  │ Jan 15, 2025 14:32          │   │   (expandable)
│  │                         ⌄   │   │
│  └─────────────────────────────┘   │
│                                     │
│  ┌─────────────────────────────┐   │
│  │ ✅ Sync Completed           │   │
│  │ Card: card-x9y8z7w6         │   │
│  │ 156 files • 1.2 GB          │   │
│  │ Jan 14, 2025 09:15          │   │
│  │                         ⌄   │   │
│  └─────────────────────────────┘   │
│                                     │
│  ┌─────────────────────────────┐   │
│  │ ⚠️  Sync Failed              │   │
│  │ Card: card-m5n4o3p2         │   │
│  │ Error: Network timeout      │   │
│  │ Jan 13, 2025 16:42          │   │
│  │                         ⌄   │   │
│  └─────────────────────────────┘   │
│                                     │
├─────────────────────────────────────┤
│  📊     🖼️     📚     ⋯            │
│Status  Gallery History  More        │
└─────────────────────────────────────┘
```

### 9. More Tab (Settings Menu)

```
┌─────────────────────────────────────┐
│  📸 Photo Backup Station        🌙  │
├─────────────────────────────────────┤
│                                     │
│  Settings & Configuration           │
│                                     │
│  ┌─────────────────────────────┐   │
│  │ ⚙️  Rclone Configuration    │   │ ← Menu items
│  └─────────────────────────────┘   │
│                                     │
│  ┌─────────────────────────────┐   │
│  │ 📡 WiFi Networks            │   │
│  └─────────────────────────────┘   │
│                                     │
│  ┌─────────────────────────────┐   │
│  │ 💾 Storage Devices          │   │
│  └─────────────────────────────┘   │
│                                     │
│  ┌─────────────────────────────┐   │
│  │ 📁 Remote Files             │   │
│  └─────────────────────────────┘   │
│                                     │
│  ┌─────────────────────────────┐   │
│  │ 🔧 Network Debug            │   │
│  └─────────────────────────────┘   │
│                                     │
│  ┌─────────────────────────────┐   │
│  │ ℹ️  About                    │   │
│  └─────────────────────────────┘   │
│                                     │
├─────────────────────────────────────┤
│  📊     🖼️     📚     ⋯            │
│Status  Gallery History  More        │
└─────────────────────────────────────┘
```

### 10. Expanded Status with Details

```
┌─────────────────────────────────────┐
│  📸 Photo Backup Station        🌙  │
├─────────────────────────────────────┤
│                                     │
│    ┌───────────────────────┐        │
│    │      ┌─────────┐      │        │
│    │      │  ⟳ 📷  │      │        │
│    │      └─────────┘      │        │
│    │   Syncing Photos      │        │
│    │   150 of 500 files    │        │
│    └───────────────────────┘        │
│                                     │
│  [==============·······] 68%        │
│                                     │
│  ┌─────────────────────────────┐   │
│  │       ⏸  Pause Sync         │   │
│  └─────────────────────────────┘   │
│                                     │
│  ⌄ Progress Details                 │ ← EXPANDED
│  ┌─────────────────────────────┐   │
│  │ Transfer Rate    2.4 MB/s   │   │
│  │ Time Remaining   3m 42s     │   │
│  │ Data Transferred 1.2/3.8 GB │   │
│  │ Current File     IMG_4567   │   │
│  └─────────────────────────────┘   │
│                                     │
│  ⌃ Advanced Options                 │ ← Collapsed
│                                     │
├─────────────────────────────────────┤
│  📊     🖼️     📚     ⋯            │
│Status  Gallery History  More        │
└─────────────────────────────────────┘
```

## Navigation Flow Diagrams

### Tab Navigation Flow

```
           ┌──────────┐
           │  Status  │ ◄─── Default view
           └────┬─────┘
                │
    ┌───────────┼───────────┬────────────┐
    │           │           │            │
    ▼           ▼           ▼            ▼
┌────────┐  ┌────────┐  ┌────────┐  ┌──────┐
│Gallery │  │History │  │  More  │  │      │
└────────┘  └────────┘  └───┬────┘  │      │
    │                       │        │      │
    ▼                       ▼        │      │
┌────────┐              ┌────────┐  │      │
│Lightbox│              │ Config │  │      │
└────────┘              ├────────┤  │      │
                        │  WiFi  │  │      │
    Swipe gestures:     ├────────┤  │      │
    ◄──────────────►    │Devices │  │      │
    Between tabs        ├────────┤  │      │
                        │ Files  │  │      │
                        ├────────┤  │      │
                        │Network │  │      │
                        └────────┘  │      │
                                    └──────┘
```

### Gallery Navigation Flow

```
Root (🏠)
  │
  ├─ DCIM/ ───────────────┐
  │   │                   │
  │   ├─ 100CANON/        │ ← Breadcrumb navigation
  │   │   ├─ IMG_0001.JPG │   (tap any segment to jump back)
  │   │   ├─ IMG_0002.JPG │
  │   │   └─ ...          │
  │   │                   │
  │   ├─ 101CANON/        │
  │   └─ 102CANON/        │
  │                       │
  └─ PRIVATE/             │
      ├─ 200CANON/        │
      └─ ...              │
                          │
                          ▼
                    ┌──────────┐
                    │ Lightbox │ ← Tap photo to open
                    └──────────┘
                          │
                    Swipe ◄─► to navigate
                    Swipe ▼ to close
                    Pinch to zoom
```

### Filter/Search Flow

```
Gallery View
     │
     ├──► Tap 🔍 ──► Search Bar
     │                   │
     │                   ├─ Type query ──► Auto-complete
     │                   │                      │
     │                   │                      ▼
     │                   │              Apply search filter
     │                   │
     │                   └─ Quick filter ──► Immediate apply
     │
     └──► Tap ⚲ ──► Filter Bottom Sheet
                         │
                         ├─ Date range
                         ├─ Camera
                         ├─ File type
                         └─ Resolution
                              │
                              ▼
                      [ Apply Filters ]
                              │
                              ▼
                      Filtered results
```

### Gesture Map

```
┌─────────────────────────────────────┐
│                                     │
│      Swipe ▼ to refresh             │ ← Pull-to-refresh
│                                     │
│           Gallery                   │
│                                     │
│  Swipe ◄─────────────────► Swipe   │ ← Navigate tabs
│                                     │
│  Tap photo ──► Open lightbox        │
│                                     │
│  Long press ──► Quick actions       │
│                                     │
│  Swipe ▲ on filter ──► Open sheet  │
│                                     │
│                                     │
└─────────────────────────────────────┘

Lightbox gestures:
┌─────────────────────────────────────┐
│  Pinch ◄►  Zoom in/out              │
│  Double tap  Toggle zoom            │
│  Swipe ◄─►  Next/prev photo         │
│  Swipe ▼  Close lightbox            │
│  Swipe ▲ on info  Expand details    │
└─────────────────────────────────────┘
```

## Component Interaction Diagrams

### Bottom Navigation + FAB Interaction

```
State: Syncing
┌─────────────────────────────────────┐
│         Status Content              │
│                                     │
│  [Sync in progress]                 │
│  [Progress bar]                     │
│                                     │
│                                     │
│                                     │
│                                ┌───┐│
│                            ⏸  │FAB││ ← Pause action
│                                └───┘│
├─────────────────────────────────────┤
│  📊     🖼️     📚     ⋯            │
│Status  Gallery History  More        │ ← Active: Status
└─────────────────────────────────────┘

State: Gallery browsing
┌─────────────────────────────────────┐
│         Gallery Content             │
│                                     │
│  [Photo grid]                       │
│  [Photo grid]                       │
│  [Photo grid]                       │
│                                     │
│                                ┌───┐│
│                            🔍  │FAB││ ← Search action
│                                └───┘│
├─────────────────────────────────────┤
│  📊     🖼️     📚     ⋯            │
│Status  Gallery History  More        │ ← Active: Gallery
└─────────────────────────────────────┘
```

### Progressive Disclosure Example

```
Level 1 (Always visible):
┌─────────────────────────┐
│ ✅ Sync Completed       │
│ 234 files • 1.8 GB      │
│                     ⌄   │ ← Tap to expand
└─────────────────────────┘

Level 2 (One tap):
┌─────────────────────────┐
│ ✅ Sync Completed       │
│ 234 files • 1.8 GB      │
│                     ⌃   │
│ ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄  │
│ Card ID: card-a1b2c3d4  │
│ Duration: 8m 32s        │
│ Transfer: 2.1 MB/s avg  │
│ Error count: 0          │
│ [View Details]          │ ← Tap for level 3
└─────────────────────────┘

Level 3 (Two taps):
┌─────────────────────────┐
│ Detailed Sync Log       │
├─────────────────────────┤
│ 14:32:01 Started sync   │
│ 14:32:15 Found 234 files│
│ 14:35:43 Transferred... │
│ 14:40:33 Completed      │
│                         │
│ [Download Full Log]     │
│ [Share Report]          │
└─────────────────────────┘
```

## Responsive Breakpoints

### Portrait Phone (< 768px)
```
Bottom Navigation: 4 items
Gallery Grid: 3 columns
Touch Targets: 44×44px minimum
Font Scale: 1.0x
```

### Landscape Phone (768px - 1024px)
```
Bottom Navigation: 5 items (can add one more)
Gallery Grid: 5 columns
Touch Targets: 44×44px minimum
Font Scale: 0.95x
```

### Tablet Portrait (1024px+)
```
Side Navigation: Optional left sidebar
Gallery Grid: 6 columns
Touch Targets: 48×48px
Font Scale: 1.05x
```

## Animation Timing

```
Navigation Transitions:
├─ Tab switch: 200ms ease-out
├─ Bottom sheet: 300ms cubic-bezier(0.4, 0, 0.2, 1)
├─ FAB action: 150ms ease-in-out
└─ Lightbox open: 250ms ease-out

User Feedback:
├─ Button press: 100ms (visual feedback)
├─ Ripple effect: 400ms
└─ Loading spinner: 1000ms rotation

Scroll Performance:
├─ Infinite load trigger: 200px from bottom
├─ Debounce search: 300ms
└─ Throttle scroll: 16ms (60fps)
```

## Touch Target Map

```
Minimum Touch Targets (44×44px):
┌─────────────────────────────────────┐
│ [Theme 44×44]                       │
├─────────────────────────────────────┤
│                                     │
│  [Button 44×∞]                      │
│                                     │
│  [Gallery item 120×120 min]         │
│                                     │
│  [Icon button 44×44]                │
│                                     │
├─────────────────────────────────────┤
│ [Nav 44×] [Nav 44×] [Nav 44×]       │
└─────────────────────────────────────┘

Spacing between targets: 8px minimum
```

## Color-Coded State Indicators

```
Status States:
┌──────┬──────────┬─────────────┐
│State │ Color    │ Indicator   │
├──────┼──────────┼─────────────┤
│Idle  │ Blue     │ ○ Still     │
│Detect│ Orange   │ ◐ Pulse     │
│Sync  │ Yellow   │ ⟳ Spin      │
│Done  │ Green    │ ✓ Still     │
│Error │ Red      │ ✗ Still     │
└──────┴──────────┴─────────────┘

Visual Legend:
Idle:    [████████] Blue gradient
Syncing: [⟳⟳⟳⟳⟳⟳] Yellow + animation
Success: [✓✓✓✓✓✓] Green gradient
Error:   [✗✗✗✗✗✗] Red gradient
```

## Implementation Notes

1. **Bottom Navigation**
   - Use `position: fixed; bottom: 0`
   - Add `padding-bottom` to main content
   - Support safe-area-inset for notched devices

2. **Gestures**
   - Use Touch Events API (not mouse events)
   - Add `passive: true` for scroll performance
   - Implement visual feedback during gestures

3. **Bottom Sheets**
   - Use transform instead of top/bottom for animation
   - Add backdrop overlay with fade
   - Support drag-to-dismiss

4. **Infinite Scroll**
   - Use Intersection Observer (not scroll events)
   - Load 200px before reaching bottom
   - Show loading indicator

5. **Search**
   - Debounce input (300ms)
   - Show spinner for server requests
   - Clear button when text present

6. **Lightbox**
   - Prevent body scroll when open
   - Support keyboard (ESC, arrows)
   - Preload adjacent images
