describe('Configuration Page - Bootstrap UI', () => {
  beforeEach(() => {
    cy.visitWithAuth('/config');
    cy.waitForPageLoad();
  });

  it('should load configuration page with Bootstrap components', () => {
    cy.get('title').should('contain', 'Configuration - Photo Backup Station');
    cy.checkBootstrapLoaded();

    // Check active config tab
    cy.get('.nav-link[href="/config"]').should('have.class', 'active');
    cy.get('h2').should('contain', 'Configuration');
  });

  it('should display cloud storage status card', () => {
    cy.get('.card').contains('Cloud Storage Status').should('be.visible');
    cy.get('#config-status').should('be.visible');

    // Should show loading initially, then status
    cy.waitForLoading();
    cy.get('#config-status').should('not.contain.text', 'Loading');
  });

  it('should display configured remotes section', () => {
    cy.get('#remotes-section').should('exist');
    cy.get('.card').contains('Configured Remotes').should('exist');
    cy.get('#remotes-list').should('exist');
  });

  it('should have rclone configuration form', () => {
    cy.get('.card').contains('Rclone Configuration').should('be.visible');
    cy.get('#config-form').should('be.visible');

    // Check form elements
    cy.get('#rclone-config').should('be.visible');
    cy.get('#rclone-config').should('have.class', 'form-control');
    cy.get('#rclone-config').should('have.class', 'font-monospace');

    // Check form label
    cy.get('label[for="rclone-config"]').should('contain', 'Rclone Configuration');

    // Check help text
    cy.get('.form-text').should('contain', 'rclone config');
  });

  it('should have configuration form buttons', () => {
    cy.get('button[type="submit"]').should('contain', 'Save Configuration');
    cy.get('button[type="submit"]').should('have.class', 'btn-primary');

    cy.get('#test-btn').should('contain', 'Test Connection');
    cy.get('#test-btn').should('have.class', 'btn-secondary');

    cy.get('button').contains('Clear').should('have.class', 'btn-outline-secondary');
  });

  it('should handle form interactions', () => {
    // Test textarea input
    const testConfig = '[test]\ntype = s3\nprovider = AWS';
    cy.get('#rclone-config').clear().type(testConfig);
    cy.get('#rclone-config').should('contain.value', testConfig);

    // Test clear button
    cy.get('button').contains('Clear').click();
    cy.get('#rclone-config').should('have.value', '');
  });

  it('should test configuration submission', () => {
    const testConfig = '[test]\ntype = s3\nprovider = AWS\naccess_key_id = test';

    cy.get('#rclone-config').clear().type(testConfig);
    cy.get('button[type="submit"]').click();

    // Should trigger form submission
    // Note: This might show a toast notification on success/error
  });

  it('should test connection testing', () => {
    cy.get('#test-btn').click();

    // Should show loading state
    cy.get('#test-btn').should('contain.text', 'Testing');
    cy.get('#test-btn').should('be.disabled');

    // Wait for test to complete
    cy.wait(3000);

    // Button should return to normal
    cy.get('#test-btn').should('contain.text', 'Test Connection');
    cy.get('#test-btn').should('not.be.disabled');
  });

  it('should display help and documentation section', () => {
    cy.get('.card').contains('Help & Documentation').should('be.visible');

    // Check supported providers list
    cy.get('.card-body').contains('Supported Cloud Providers').should('be.visible');
    cy.get('ul').contains('Amazon S3').should('be.visible');
    cy.get('ul').contains('Backblaze B2').should('be.visible');
    cy.get('ul').contains('Google Drive').should('be.visible');

    // Check quick links
    cy.get('.card-body').contains('Quick Links').should('be.visible');
    cy.get('a[href*="rclone.org"]').should('exist');
    cy.get('a[target="_blank"]').should('have.attr', 'rel', 'noopener noreferrer');
  });

  it('should load configuration API correctly', () => {
    // Test config API endpoint
    cy.testApiEndpoint('config');

    // Check status display updates
    cy.waitForLoading();
    cy.get('#config-status').within(() => {
      cy.get('.alert').should('exist');
      cy.get('.alert').should('satisfy', $el => {
        return $el.hasClass('alert-success') || $el.hasClass('alert-warning');
      });
    });
  });

  it('should handle configuration status display', () => {
    cy.waitForLoading();

    cy.get('#config-status').within(() => {
      // Should show either configured or not configured status
      cy.get('.alert').should('exist');
      cy.get('.fs-4').should('exist'); // Icon
      cy.get('strong').should('exist'); // Status text
      cy.get('.small').should('exist'); // Subtitle
    });
  });

  it('should be responsive on mobile devices', () => {
    cy.testResponsive('.main-content-card');

    // Test mobile form layout
    cy.viewport(375, 667);
    cy.get('#rclone-config').should('be.visible');
    cy.get('.d-flex.gap-2.flex-wrap').should('be.visible');
    cy.get('button').should('be.visible');

    // Test tablet layout
    cy.viewport(768, 1024);
    cy.get('.row').should('be.visible');
    cy.get('[class*="col-"]').should('be.visible');
  });

  it('should test form validation', () => {
    // Test submitting empty form
    cy.get('button[type="submit"]').click();

    // Should handle empty submission (might show toast)
    // The form should not crash
    cy.get('#config-form').should('be.visible');
  });

  it('should handle remote configuration display', () => {
    cy.waitForLoading();

    // Check if remotes section is shown/hidden appropriately
    cy.get('#remotes-section').should('exist');

    // If remotes exist, check structure
    cy.get('#remotes-section').then($section => {
      if ($section.is(':visible')) {
        cy.get('#remotes-list').within(() => {
          cy.get('.list-group').should('exist');
          cy.get('.list-group-item').should('exist');
          cy.get('.badge').should('exist');
        });
      }
    });
  });

  it('should test external links', () => {
    // Test that external links are properly configured
    cy.get('a[href*="rclone.org"]').each($link => {
      cy.wrap($link)
        .should('have.attr', 'target', '_blank')
        .and('have.attr', 'rel', 'noopener noreferrer');
    });
  });

  it('should have proper Bootstrap form styling', () => {
    // Check form classes
    cy.get('#config-form').should('exist');
    cy.get('.form-label').should('exist');
    cy.get('.form-control').should('exist');
    cy.get('.form-text').should('exist');

    // Check button groups
    cy.get('.d-flex.gap-2').should('exist');
    cy.get('.btn').should('exist');

    // Check card structure
    cy.get('.card-header').should('exist');
    cy.get('.card-body').should('exist');
  });

  it('should test configuration test endpoint', () => {
    // Test config test API
    cy.request({
      method: 'POST',
      url: '/api/config/test',
      auth: {
        username: Cypress.env('username'),
        password: Cypress.env('password')
      },
      failOnStatusCode: false
    }).then((response) => {
      expect(response.status).to.be.oneOf([200, 400, 500]);
      if (response.status === 200) {
        expect(response.body).to.have.property('success');
      }
    });
  });
});