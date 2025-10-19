/**
 * Photo Backup Station - SPA Router
 * Hash-based routing with deep linking support
 */

import { escapeHtml } from './utils.js';

// Route configuration
const routes = {
        '/status': {
            endpoint: '/api/pages/status',
            title: 'Status - Photo Backup Station',
            navIndex: 0
        },
        '/history': {
            endpoint: '/api/pages/history',
            title: 'Sync History - Photo Backup Station',
            navIndex: 1
        },
        '/wifi': {
            endpoint: '/api/pages/wifi',
            title: 'WiFi Management - Photo Backup Station',
            navIndex: 2
        },
        '/gallery': {
            endpoint: '/api/pages/gallery',
            title: 'Photo Gallery - Photo Backup Station',
            navIndex: 3
        },
        '/config': {
            endpoint: '/api/pages/config',
            title: 'Configuration - Photo Backup Station',
            navIndex: 4
        }
    };

const defaultRoute = '/status';

export class SPARouter {
        constructor() {
            this.currentRoute = null;
            this.contentContainer = null;
            this.navLinks = null;
        }

    /**
     * Initialize the router
     */
    init() {
            this.contentContainer = document.getElementById('main-content');
            this.navLinks = document.querySelectorAll('.app-nav .nav-link');

            // Handle hash changes (back/forward/direct navigation)
            window.addEventListener('hashchange', () => this.handleRouteChange());

            // Handle nav link clicks
            this.navLinks.forEach((link, index) => {
                link.addEventListener('click', (e) => {
                    e.preventDefault();
                    const hash = link.getAttribute('href');
                    if (hash && hash.startsWith('#/')) {
                        window.location.hash = hash;
                    }
                });
            });

        // Load initial route
        this.handleRouteChange();
    }

    /**
     * Get current route from hash
     */
    getCurrentRoute() {
            const hash = window.location.hash;
            if (!hash || hash === '#' || hash === '#/') {
                return defaultRoute;
            }
            // Support sub-routes like #/gallery?path=/DCIM
            const route = hash.split('?')[0].substring(1); // Remove #
        return routes[route] ? route : defaultRoute;
    }

    /**
     * Get query parameters from hash
     */
    getQueryParams() {
            const hash = window.location.hash;
            const queryString = hash.split('?')[1];
            if (!queryString) return {};

            const params = {};
            queryString.split('&').forEach(param => {
                const [key, value] = param.split('=');
                params[decodeURIComponent(key)] = decodeURIComponent(value || '');
            });
        return params;
    }

    /**
     * Navigate to a route with optional parameters
     */
    navigate(route, params = {}) {
            let hash = '#' + route;

            // Add query parameters
            const queryParams = Object.entries(params)
                .filter(([_, value]) => value !== null && value !== undefined)
                .map(([key, value]) => `${encodeURIComponent(key)}=${encodeURIComponent(value)}`)
                .join('&');

            if (queryParams) {
                hash += '?' + queryParams;
            }

        window.location.hash = hash;
    }

    /**
     * Handle route changes
     */
    async handleRouteChange() {
            const route = this.getCurrentRoute();
            const params = this.getQueryParams();
            const routeConfig = routes[route];

            if (!routeConfig) {
                console.error('Unknown route:', route);
                return;
            }

            // Don't reload if already on this route (unless params changed)
            if (this.currentRoute === route && Object.keys(params).length === 0) {
                return;
            }

            this.currentRoute = route;

            // Update document title
            document.title = routeConfig.title;

            // Update active nav link
            this.updateActiveNav(routeConfig.navIndex);

            // Build URL with query parameters
            let url = routeConfig.endpoint;
            const queryString = Object.entries(params)
                .map(([key, value]) => `${encodeURIComponent(key)}=${encodeURIComponent(value)}`)
                .join('&');
            if (queryString) {
                url += '?' + queryString;
            }

            // Load content via htmx
            try {
                const response = await fetch(url, {
                    headers: {
                        'HX-Request': 'true'
                    }
                });

                if (!response.ok) {
                    throw new Error(`HTTP ${response.status}: ${response.statusText}`);
                }

                const html = await response.text();

                // Update content
                this.contentContainer.innerHTML = html;

                // Add transition effect
                this.contentContainer.classList.remove('page-transition');
                void this.contentContainer.offsetWidth; // Trigger reflow
                this.contentContainer.classList.add('page-transition');

                // Execute page scripts
                this.executePageScripts();

                // Scroll to top
                window.scrollTo({ top: 0, behavior: 'smooth' });

            } catch (error) {
                console.error('Failed to load page:', error);
                this.contentContainer.innerHTML = `
                    <div class="alert alert-danger" role="alert">
                        <h4 class="alert-heading">Failed to load page</h4>
                        <p>${escapeHtml(error.message)}</p>
                        <hr>
                        <button class="btn btn-primary" onclick="SPARouter.handleRouteChange()">
                            🔄 Retry
                        </button>
                    </div>
                `;
        }
    }

    /**
     * Update active navigation link
     */
    updateActiveNav(activeIndex) {
            // Update based on index if provided
            if (typeof activeIndex === 'number') {
                this.navLinks.forEach((link, index) => {
                    if (index === activeIndex) {
                        link.classList.add('active');
                        link.setAttribute('aria-current', 'page');
                    } else {
                        link.classList.remove('active');
                        link.removeAttribute('aria-current');
                    }
                });
                return;
            }

            // Fallback: match based on current route
            const currentRoute = this.getCurrentRoute();
            this.navLinks.forEach((link) => {
                const href = link.getAttribute('href');
                if (href && href.substring(1) === currentRoute) {
                    link.classList.add('active');
                    link.setAttribute('aria-current', 'page');
                } else {
                    link.classList.remove('active');
                    link.removeAttribute('aria-current');
                }
            });
    }

    /**
     * Execute scripts in loaded content
     */
    executePageScripts() {
            const scripts = this.contentContainer.querySelectorAll('script');
            scripts.forEach(script => {
                if (script.textContent) {
                    try {
                        // Create a new script element to ensure execution
                        const newScript = document.createElement('script');

                        // Preserve the script type (especially type="module")
                        if (script.type) {
                            newScript.type = script.type;
                        }

                        // Copy other important attributes
                        if (script.src) {
                            newScript.src = script.src;
                        }
                        if (script.async) {
                            newScript.async = script.async;
                        }
                        if (script.defer) {
                            newScript.defer = script.defer;
                        }

                        newScript.textContent = script.textContent;
                        script.parentNode.replaceChild(newScript, script);
                    } catch (e) {
                        console.error('Script execution error:', e);
                        // Don't trigger global error handler for known script execution issues
                        e.preventDefault?.();
                    }
            }
        });
    }
}

// Create global router instance
export const router = new SPARouter();
window.SPARouter = router;

// Helper function for programmatic navigation
export function navigateTo(route, params) {
    router.navigate(route, params);
}
window.navigateTo = navigateTo;

// Helper to get current query params
export function getRouteParams() {
    return router.getQueryParams();
}
window.getRouteParams = getRouteParams;
