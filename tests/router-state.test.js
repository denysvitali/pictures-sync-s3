/**
 * Tests for router state management and active page indicators
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { JSDOM } from 'jsdom';

describe('Router State Management', () => {
    let dom;
    let window;
    let document;
    let SPARouter;

    beforeEach(async () => {
        // Create a fresh DOM for each test
        dom = new JSDOM(`
            <!DOCTYPE html>
            <html>
            <head></head>
            <body>
                <nav class="app-nav">
                    <a href="#/status" class="nav-link">Status</a>
                    <a href="#/history" class="nav-link">History</a>
                    <a href="#/wifi" class="nav-link">WiFi</a>
                    <a href="#/gallery" class="nav-link">Gallery</a>
                    <a href="#/config" class="nav-link">Config</a>
                </nav>
                <div id="main-content"></div>
            </body>
            </html>
        `, {
            url: 'http://localhost:8080',
            runScripts: 'dangerously',
            resources: 'usable'
        });

        window = dom.window;
        document = window.document;
        global.window = window;
        global.document = document;
        global.bootstrap = {
            Modal: class {
                show() {}
                hide() {}
            }
        };

        // Mock fetch
        global.fetch = vi.fn(() =>
            Promise.resolve({
                ok: true,
                text: () => Promise.resolve('<div>Test Content</div>'),
                headers: new Map([['content-type', 'text/html']])
            })
        );

        // Load router module
        const routerModule = await import('../pkg/webui/static/js/router.js');
        SPARouter = routerModule.SPARouter;
    });

    afterEach(() => {
        dom.window.close();
        vi.clearAllMocks();
    });

    describe('Active Nav Indicator', () => {
        it('should set active class on correct nav link by index', () => {
            const router = new SPARouter();
            router.init();

            // Manually call updateActiveNav with index
            router.updateActiveNav(3); // Gallery index

            const navLinks = document.querySelectorAll('.nav-link');
            expect(navLinks[0].classList.contains('active')).toBe(false); // Status
            expect(navLinks[1].classList.contains('active')).toBe(false); // History
            expect(navLinks[2].classList.contains('active')).toBe(false); // WiFi
            expect(navLinks[3].classList.contains('active')).toBe(true);  // Gallery
            expect(navLinks[4].classList.contains('active')).toBe(false); // Config
        });

        it('should set active class based on current route when index not provided', () => {
            const router = new SPARouter();
            router.init();

            // Set hash to gallery
            window.location.hash = '#/gallery';

            // Call updateActiveNav without index (fallback mode)
            router.updateActiveNav();

            const galleryLink = document.querySelector('a[href="#/gallery"]');
            expect(galleryLink.classList.contains('active')).toBe(true);
            expect(galleryLink.getAttribute('aria-current')).toBe('page');
        });

        it('should remove active class from all links except active one', () => {
            const router = new SPARouter();
            router.init();

            // First set history active
            router.updateActiveNav(1);
            let historyLink = document.querySelectorAll('.nav-link')[1];
            expect(historyLink.classList.contains('active')).toBe(true);

            // Then set gallery active
            router.updateActiveNav(3);
            historyLink = document.querySelectorAll('.nav-link')[1];
            const galleryLink = document.querySelectorAll('.nav-link')[3];

            expect(historyLink.classList.contains('active')).toBe(false);
            expect(galleryLink.classList.contains('active')).toBe(true);
        });

        it('should set aria-current attribute correctly', () => {
            const router = new SPARouter();
            router.init();

            router.updateActiveNav(2); // WiFi

            const wifiLink = document.querySelectorAll('.nav-link')[2];
            expect(wifiLink.getAttribute('aria-current')).toBe('page');

            // Other links should not have aria-current
            const statusLink = document.querySelectorAll('.nav-link')[0];
            expect(statusLink.getAttribute('aria-current')).toBe(null);
        });
    });

    describe('Route Navigation', () => {
        it('should correctly identify current route from hash', () => {
            const router = new SPARouter();
            router.init();

            window.location.hash = '#/gallery';
            expect(router.getCurrentRoute()).toBe('/gallery');

            window.location.hash = '#/status';
            expect(router.getCurrentRoute()).toBe('/status');
        });

        it('should handle route with query parameters', () => {
            const router = new SPARouter();
            router.init();

            window.location.hash = '#/gallery?path=/DCIM';
            expect(router.getCurrentRoute()).toBe('/gallery');

            const params = router.getQueryParams();
            expect(params.path).toBe('/DCIM');
        });

        it('should default to /status for empty hash', () => {
            const router = new SPARouter();
            router.init();

            window.location.hash = '';
            expect(router.getCurrentRoute()).toBe('/status');

            window.location.hash = '#/';
            expect(router.getCurrentRoute()).toBe('/status');
        });

        it('should handle invalid routes by defaulting to /status', () => {
            const router = new SPARouter();
            router.init();

            window.location.hash = '#/invalid-page';
            expect(router.getCurrentRoute()).toBe('/status');
        });
    });

    describe('Query Parameters', () => {
        it('should parse single query parameter', () => {
            const router = new SPARouter();
            router.init();

            window.location.hash = '#/gallery?path=/photos';
            const params = router.getQueryParams();

            expect(params.path).toBe('/photos');
        });

        it('should parse multiple query parameters', () => {
            const router = new SPARouter();
            router.init();

            window.location.hash = '#/gallery?path=/photos&page=2&size=50';
            const params = router.getQueryParams();

            expect(params.path).toBe('/photos');
            expect(params.page).toBe('2');
            expect(params.size).toBe('50');
        });

        it('should handle URL-encoded parameters', () => {
            const router = new SPARouter();
            router.init();

            window.location.hash = '#/gallery?path=%2FDCIM%2F100CANON';
            const params = router.getQueryParams();

            expect(params.path).toBe('/DCIM/100CANON');
        });

        it('should return empty object for routes without parameters', () => {
            const router = new SPARouter();
            router.init();

            window.location.hash = '#/status';
            const params = router.getQueryParams();

            expect(params).toEqual({});
        });
    });

    describe('Navigation Integration', () => {
        it('should update active indicator when navigating', async () => {
            const router = new SPARouter();
            router.init();

            // Navigate to gallery
            window.location.hash = '#/gallery';
            await router.handleRouteChange();

            const galleryLink = document.querySelector('a[href="#/gallery"]');
            expect(galleryLink.classList.contains('active')).toBe(true);
        });

        it('should maintain active state during route changes', async () => {
            const router = new SPARouter();
            router.init();

            // Start at status
            window.location.hash = '#/status';
            await router.handleRouteChange();

            let statusLink = document.querySelector('a[href="#/status"]');
            expect(statusLink.classList.contains('active')).toBe(true);

            // Navigate to gallery
            window.location.hash = '#/gallery';
            await router.handleRouteChange();

            statusLink = document.querySelector('a[href="#/status"]');
            const galleryLink = document.querySelector('a[href="#/gallery"]');

            expect(statusLink.classList.contains('active')).toBe(false);
            expect(galleryLink.classList.contains('active')).toBe(true);
        });

        it('should handle rapid navigation changes', async () => {
            const router = new SPARouter();
            router.init();

            // Rapidly change routes
            window.location.hash = '#/status';
            window.location.hash = '#/history';
            window.location.hash = '#/gallery';

            await router.handleRouteChange();

            const galleryLink = document.querySelector('a[href="#/gallery"]');
            expect(galleryLink.classList.contains('active')).toBe(true);
        });
    });

    describe('Edge Cases', () => {
        it('should handle missing nav links gracefully', () => {
            // Remove all nav links
            const nav = document.querySelector('.app-nav');
            nav.innerHTML = '';

            const router = new SPARouter();
            router.init();

            // Should not throw error
            expect(() => router.updateActiveNav(0)).not.toThrow();
        });

        it('should handle nav index out of bounds', () => {
            const router = new SPARouter();
            router.init();

            // Try to set active index beyond available links
            expect(() => router.updateActiveNav(99)).not.toThrow();

            // No link should be active
            const activeLinks = document.querySelectorAll('.nav-link.active');
            expect(activeLinks.length).toBe(0);
        });

        it('should handle malformed hash correctly', () => {
            const router = new SPARouter();
            router.init();

            window.location.hash = '###/gallery';
            const route = router.getCurrentRoute();

            // Should gracefully handle malformed hash
            expect(route).toBeDefined();
        });
    });

    describe('Accessibility', () => {
        it('should only set aria-current on active link', () => {
            const router = new SPARouter();
            router.init();

            router.updateActiveNav(3); // Gallery

            const navLinks = document.querySelectorAll('.nav-link');
            navLinks.forEach((link, index) => {
                if (index === 3) {
                    expect(link.getAttribute('aria-current')).toBe('page');
                } else {
                    expect(link.hasAttribute('aria-current')).toBe(false);
                }
            });
        });

        it('should maintain proper ARIA attributes during navigation', async () => {
            const router = new SPARouter();
            router.init();

            window.location.hash = '#/status';
            await router.handleRouteChange();

            window.location.hash = '#/gallery';
            await router.handleRouteChange();

            const galleryLink = document.querySelector('a[href="#/gallery"]');
            const statusLink = document.querySelector('a[href="#/status"]');

            expect(galleryLink.getAttribute('aria-current')).toBe('page');
            expect(statusLink.hasAttribute('aria-current')).toBe(false);
        });
    });
});
