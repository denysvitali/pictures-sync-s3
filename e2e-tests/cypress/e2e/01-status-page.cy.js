describe('Status Page - Bootstrap UI', () => {
  beforeEach(() => {
    cy.visitWithAuth('/');
    cy.waitForPageLoad();
  });

  it('should load the status page with Bootstrap components', () => {
    // Check basic page structure
    cy.get('title').should('contain', 'Status - Photo Backup Station');
    cy.checkBootstrapLoaded();

    // Check header
    cy.get('.app-header').should('be.visible');
    cy.get('.app-header h1').should('contain', 'Photo Backup Station');

    // Check connection status indicator
    cy.checkConnectionStatus();
  });

  it('should have proper navigation with Bootstrap nav-pills', () => {
    cy.testNavigation();

    // Check active status page tab
    cy.get('.nav-link[href="/"]').should('have.class', 'active');
    cy.get('.nav-link[href="/"]').should('contain.text', 'Status');
  });

  it('should display system status card with Bootstrap styling', () => {
    cy.get('.main-content-card').should('be.visible');
    cy.get('h2').should('contain', 'System Status');

    // Check status card structure
    cy.get('.card').should('exist');
    cy.get('.card-body').should('be.visible');

    // Check status icon and text
    cy.get('#status-icon').should('be.visible');
    cy.get('#status-text').should('be.visible');
    cy.get('#status-subtitle').should('be.visible');
  });

  it('should have progress section with Bootstrap components', () => {
    // Progress section should exist (even if hidden initially)
    cy.get('#progress-section').should('exist');

    // Check progress bar structure when visible
    cy.get('#progress-section').then($el => {
      if (!$el.hasClass('d-none')) {
        cy.get('.progress').should('exist');
        cy.get('.progress-bar').should('exist');
        cy.get('#progress-text').should('exist');
      }
    });

    // Check stats grid
    cy.get('#progress-section .row').should('exist');
    cy.get('#files-progress').should('exist');
    cy.get('#bytes-progress').should('exist');
    cy.get('#speed').should('exist');
    cy.get('#eta').should('exist');
  });

  it('should have quick actions with Bootstrap buttons', () => {
    cy.get('.card').contains('Quick Actions').should('be.visible');

    // Check action buttons
    cy.get('button').contains('Configure Cloud Storage').should('have.class', 'btn-primary');
    cy.get('button').contains('Manage WiFi').should('have.class', 'btn-secondary');
    cy.get('button').contains('View Gallery').should('have.class', 'btn-secondary');
    cy.get('button').contains('Refresh Status').should('have.class', 'btn-secondary');

    // Test button functionality
    cy.get('button').contains('Manage WiFi').click();
    cy.url().should('include', '/wifi');
    cy.go('back');
  });

  it('should be responsive across different screen sizes', () => {
    cy.testResponsive('.main-content-card');

    // Test mobile layout
    cy.viewport(375, 667);
    cy.get('.app-header').should('be.visible');
    cy.get('.nav-pills').should('be.visible');
    cy.get('.main-content-card').should('be.visible');

    // Test tablet layout
    cy.viewport(768, 1024);
    cy.get('.row').should('be.visible');
    cy.get('[class*="col-"]').should('be.visible');

    // Back to desktop
    cy.viewport(1280, 720);
  });

  it('should load and call status API correctly', () => {
    // Test status API endpoint
    cy.testApiEndpoint('status');

    // Check that JavaScript successfully loads status
    cy.get('#status-text').should('not.be.empty');
    cy.get('#status-icon').should('not.be.empty');
  });

  it('should handle WebSocket connection', () => {
    // Check WebSocket token endpoint
    cy.testApiEndpoint('ws-token');

    // Check connection status updates
    cy.get('#connection-status').should('be.visible');
    cy.get('#connection-status .status-text').should('contain.text', 'Connect');
  });

  it('should have proper Bootstrap styling classes', () => {
    // Check key Bootstrap classes are applied
    cy.get('.app-container').should('exist');
    cy.get('.card').should('have.class', 'mb-4');
    cy.get('.btn').should('exist');
    cy.get('.alert').should('exist');
    cy.get('.progress').should('exist');

    // Check custom theme classes
    cy.get('.app-header').should('exist');
    cy.get('.app-nav').should('exist');
    cy.get('.main-content-card').should('exist');
  });

  it('should refresh status when button clicked', () => {
    cy.get('button').contains('Refresh Status').click();

    // Should trigger loadStatus() function
    cy.get('#status-text').should('be.visible');
    cy.get('#status-icon').should('be.visible');
  });
});