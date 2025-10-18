describe('WiFi Page - Bootstrap UI', () => {
  beforeEach(() => {
    cy.visitWithAuth('/wifi');
    cy.waitForPageLoad();
  });

  it('should load WiFi page with Bootstrap components', () => {
    cy.get('title').should('contain', 'WiFi Management - Photo Backup Station');
    cy.checkBootstrapLoaded();

    // Check active WiFi tab
    cy.get('.nav-link[href="/wifi"]').should('have.class', 'active');
    cy.get('h2').should('contain', 'WiFi Management');
  });

  it('should display WiFi status card', () => {
    cy.get('.card').contains('Current Status').should('be.visible');
    cy.get('#wifi-status').should('be.visible');

    // Should show loading initially, then content
    cy.get('#wifi-status').should('exist');
  });

  it('should display configured networks section', () => {
    cy.get('.card').contains('Configured Networks').should('be.visible');
    cy.get('#configured-networks').should('exist');

    // Check drag-and-drop instructions
    cy.get('.card-body').contains('Drag networks to reorder').should('be.visible');
  });

  it('should have available networks section with controls', () => {
    cy.get('.card').contains('Available Networks').should('be.visible');
    cy.get('#available-networks').should('exist');

    // Check scan controls
    cy.get('#scan-btn').should('be.visible').and('contain.text', 'Scan');
    cy.get('#sort-select').should('be.visible');

    // Check sort options
    cy.get('#sort-select option').should('have.length', 3);
    cy.get('#sort-select option').eq(0).should('contain.text', 'Signal Strength');
    cy.get('#sort-select option').eq(1).should('contain.text', 'Network Name');
    cy.get('#sort-select option').eq(2).should('contain.text', 'Security Status');
  });

  it('should handle network scanning', () => {
    // Test scan button
    cy.get('#scan-btn').click();

    // Should show loading state
    cy.get('#scan-btn').should('contain.text', 'Scanning');
    cy.get('#scan-btn').should('be.disabled');

    // Wait for scan to complete
    cy.wait(2000);

    // Button should return to normal
    cy.get('#scan-btn').should('contain.text', 'Scan');
    cy.get('#scan-btn').should('not.be.disabled');
  });

  it('should test sort functionality', () => {
    // Change sort option
    cy.get('#sort-select').select('Network Name');
    cy.get('#sort-select').should('have.value', 'name');

    cy.get('#sort-select').select('Security Status');
    cy.get('#sort-select').should('have.value', 'security');

    cy.get('#sort-select').select('Signal Strength');
    cy.get('#sort-select').should('have.value', 'signal');
  });

  it('should have proper Bootstrap list group structure', () => {
    // Check for list-group containers
    cy.get('#configured-networks').should('exist');
    cy.get('#available-networks').should('exist');

    // After scanning, should have list-group elements
    cy.get('#scan-btn').click();
    cy.wait(2000);

    // Even if no networks, should handle empty state properly
    cy.get('#available-networks').should('not.contain.text', 'Loading');
  });

  it('should test network connection modal functionality', () => {
    // This would require actual networks, but we can test modal structure
    // when connect buttons exist

    // Check if modals are properly set up in the page
    cy.get('body').then($body => {
      if ($body.find('[data-action="connect"]').length > 0) {
        cy.get('[data-action="connect"]').first().click();
        cy.get('.modal').should('be.visible');
        cy.get('.modal .btn-close').click();
        cy.get('.modal').should('not.be.visible');
      }
    });
  });

  it('should be responsive on mobile devices', () => {
    cy.testResponsive('.main-content-card');

    // Test mobile layout specifically
    cy.viewport(375, 667);

    // Check that controls wrap properly
    cy.get('.card-header').should('be.visible');
    cy.get('#scan-btn').should('be.visible');
    cy.get('#sort-select').should('be.visible');

    // Check cards stack properly
    cy.get('.card').should('be.visible');
  });

  it('should load WiFi APIs correctly', () => {
    // Test WiFi status API
    cy.testApiEndpoint('wifi/status');

    // Test WiFi networks API
    cy.testApiEndpoint('wifi/networks');
  });

  it('should handle drag and drop setup', () => {
    // Check that drag-and-drop elements have proper attributes
    cy.get('#configured-networks').should('exist');

    // After loading networks, should have draggable elements
    cy.wait(1000);
    cy.get('#configured-networks').then($container => {
      if ($container.find('.network-item').length > 0) {
        cy.get('.network-item').should('have.attr', 'draggable', 'true');
        cy.get('.drag-handle').should('be.visible');
      }
    });
  });

  it('should test form controls and Bootstrap styling', () => {
    // Check form controls have proper Bootstrap classes
    cy.get('#sort-select').should('have.class', 'form-select');
    cy.get('#scan-btn').should('have.class', 'btn');

    // Check responsive utilities
    cy.get('.d-flex').should('exist');
    cy.get('.gap-2').should('exist');
    cy.get('.align-items-center').should('exist');
  });

  it('should handle WiFi status display correctly', () => {
    cy.get('#wifi-status').should('be.visible');

    // Should eventually show either connected or not connected status
    cy.get('#wifi-status', { timeout: 5000 }).should('not.contain.text', 'Loading');

    // Should contain either success or warning alert
    cy.get('#wifi-status').within(() => {
      cy.get('.alert').should('exist');
      cy.get('.alert').should('satisfy', $el => {
        return $el.hasClass('alert-success') || $el.hasClass('alert-warning');
      });
    });
  });
});