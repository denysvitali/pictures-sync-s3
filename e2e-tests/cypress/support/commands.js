// ***********************************************
// Custom Cypress commands for Photo Backup Station
// ***********************************************

// Login command for basic auth
Cypress.Commands.add('login', (username = Cypress.env('username'), password = Cypress.env('password')) => {
  cy.request({
    method: 'GET',
    url: '/',
    auth: {
      username,
      password
    }
  }).then((response) => {
    expect(response.status).to.eq(200);
  });

  // Set authorization header for subsequent requests
  cy.window().then((win) => {
    const authString = btoa(`${username}:${password}`);
    win.localStorage.setItem('authHeader', `Basic ${authString}`);
  });
});

// Visit page with auth
Cypress.Commands.add('visitWithAuth', (url = '/', username = Cypress.env('username'), password = Cypress.env('password')) => {
  cy.visit(url, {
    auth: {
      username,
      password
    }
  });
});

// Wait for page to fully load
Cypress.Commands.add('waitForPageLoad', () => {
  cy.get('body').should('be.visible');
  cy.get('.app-container').should('exist');
  cy.get('.main-content-card').should('be.visible');
});

// Check Bootstrap components are loaded
Cypress.Commands.add('checkBootstrapLoaded', () => {
  cy.get('link[href*="bootstrap.min.css"]').should('exist');
  cy.get('script[src*="bootstrap.bundle.min.js"]').should('exist');
  cy.get('script[src*="utils.js"]').should('exist');
});

// Test navigation
Cypress.Commands.add('testNavigation', () => {
  cy.get('.nav-pills').should('be.visible');
  cy.get('.nav-link').should('have.length', 5);

  // Test each navigation link
  const pages = [
    { selector: 'a[href="/"]', text: 'Status' },
    { selector: 'a[href="/history"]', text: 'History' },
    { selector: 'a[href="/wifi"]', text: 'WiFi' },
    { selector: 'a[href="/gallery"]', text: 'Gallery' },
    { selector: 'a[href="/config"]', text: 'Configuration' }
  ];

  pages.forEach(page => {
    cy.get(page.selector).should('contain.text', page.text).and('be.visible');
  });
});

// Check connection status indicator
Cypress.Commands.add('checkConnectionStatus', () => {
  cy.get('#connection-status').should('be.visible');
  cy.get('#connection-status .status-dot').should('exist');
  cy.get('#connection-status .status-text').should('exist');
});

// Test responsive design at different viewports
Cypress.Commands.add('testResponsive', (selector) => {
  const viewports = [
    { width: 375, height: 667, device: 'mobile' },   // iPhone SE
    { width: 768, height: 1024, device: 'tablet' },  // iPad
    { width: 1280, height: 720, device: 'desktop' }  // Desktop
  ];

  viewports.forEach(viewport => {
    cy.viewport(viewport.width, viewport.height);
    cy.get(selector).should('be.visible');
    cy.wait(500); // Allow animations to complete
  });
});

// Test Bootstrap modals
Cypress.Commands.add('testModal', (triggerSelector, modalSelector) => {
  cy.get(triggerSelector).click();
  cy.get(modalSelector).should('be.visible').and('have.class', 'show');
  cy.get(`${modalSelector} .btn-close`).click();
  cy.get(modalSelector).should('not.have.class', 'show');
});

// Test Bootstrap toasts (if they appear)
Cypress.Commands.add('checkToast', (message, type = 'info') => {
  cy.get('.toast-container').should('exist');
  cy.get('.toast').should('be.visible').and('contain.text', message);

  if (type) {
    cy.get('.toast').should('have.class', `bg-${type === 'error' ? 'danger' : type}`);
  }
});

// Test API endpoints
Cypress.Commands.add('testApiEndpoint', (endpoint, expectedStatus = 200) => {
  cy.request({
    method: 'GET',
    url: `/api/${endpoint}`,
    auth: {
      username: Cypress.env('username'),
      password: Cypress.env('password')
    },
    failOnStatusCode: false
  }).then((response) => {
    expect(response.status).to.eq(expectedStatus);
    if (expectedStatus === 200) {
      expect(response.body).to.exist;
    }
  });
});

// Test form submission
Cypress.Commands.add('submitForm', (formSelector, data) => {
  cy.get(formSelector).within(() => {
    Object.keys(data).forEach(field => {
      cy.get(`[name="${field}"], #${field}`).clear().type(data[field]);
    });
    cy.get('[type="submit"]').click();
  });
});

// Wait for loading to complete
Cypress.Commands.add('waitForLoading', () => {
  cy.get('.spinner-border').should('not.exist');
  cy.get('.loading').should('not.exist');
  cy.get('[data-loading="true"]').should('not.exist');
});

// Check Bootstrap grid system
Cypress.Commands.add('checkBootstrapGrid', () => {
  cy.get('.container, .container-fluid').should('exist');
  cy.get('.row').should('exist');
  cy.get('[class*="col-"]').should('exist');
});

// Test drag and drop (for WiFi network reordering)
Cypress.Commands.add('dragAndDrop', (sourceSelector, targetSelector) => {
  cy.get(sourceSelector).trigger('dragstart');
  cy.get(targetSelector).trigger('drop');
});