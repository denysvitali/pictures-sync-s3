describe('Gallery Page - Bootstrap UI', () => {
  beforeEach(() => {
    cy.visitWithAuth('/gallery');
    cy.waitForPageLoad();
  });

  it('should load gallery page with Bootstrap components', () => {
    cy.get('title').should('contain', 'Photo Gallery - Photo Backup Station');
    cy.checkBootstrapLoaded();

    // Check active gallery tab
    cy.get('.nav-link[href="/gallery"]').should('have.class', 'active');
    cy.get('h2').should('contain', 'Photo Gallery');
  });

  it('should display gallery header with controls', () => {
    cy.get('.main-content-card').should('be.visible');
    cy.get('h2').should('contain', 'Photo Gallery');

    // Check refresh button
    cy.get('button').contains('Refresh').should('be.visible');
    cy.get('button').contains('Refresh').should('have.class', 'btn-secondary');
  });

  it('should have breadcrumb navigation', () => {
    cy.get('nav[aria-label="breadcrumb"]').should('be.visible');
    cy.get('.breadcrumb').should('exist');
    cy.get('#breadcrumb').should('be.visible');

    // Should show at least home breadcrumb
    cy.get('.breadcrumb-item').should('exist');
  });

  it('should display gallery container', () => {
    cy.get('#gallery-container').should('be.visible');

    // Should show loading initially
    cy.get('#gallery-container').should('exist');
  });

  it('should have image preview modal', () => {
    // Check modal exists in DOM
    cy.get('#image-modal').should('exist');
    cy.get('#image-modal .modal-dialog').should('have.class', 'modal-xl');
    cy.get('#image-modal .modal-dialog').should('have.class', 'modal-dialog-centered');

    // Check modal structure
    cy.get('#image-modal .modal-header').should('exist');
    cy.get('#image-modal .modal-body').should('exist');
    cy.get('#image-modal-title').should('exist');
    cy.get('#image-modal-img').should('exist');
  });

  it('should handle file loading and API calls', () => {
    // Test files API endpoint
    cy.testApiEndpoint('files');

    // Wait for files to load
    cy.waitForLoading();

    // Should not show loading spinner after load
    cy.get('.spinner-border').should('not.exist');
  });

  it('should display file grid with Bootstrap layout', () => {
    cy.waitForLoading();

    // Check for file grid structure
    cy.get('#gallery-container').within(() => {
      // Should either show files or empty state
      cy.get('body').then($body => {
        if ($body.find('.file-grid').length > 0) {
          cy.get('.file-grid').should('exist');
          cy.get('.file-item').should('exist');
        } else {
          // Empty state
          cy.get('.empty-state').should('exist');
          cy.get('.empty-state-icon').should('exist');
        }
      });
    });
  });

  it('should handle breadcrumb navigation clicks', () => {
    cy.waitForLoading();

    // Test breadcrumb home click
    cy.get('.breadcrumb-item a[data-action="navigate"]').first().click();

    // Should trigger navigation
    cy.get('#breadcrumb').should('be.visible');
  });

  it('should test refresh functionality', () => {
    cy.get('button').contains('Refresh').click();

    // Should reload the files
    cy.get('#gallery-container').should('be.visible');
  });

  it('should handle folder navigation if folders exist', () => {
    cy.waitForLoading();

    cy.get('#gallery-container').within(() => {
      // If folder items exist, test navigation
      cy.get('body').then($body => {
        if ($body.find('.file-item [data-action="navigate"]').length > 0) {
          cy.get('.file-item [data-action="navigate"]').first().click();
          cy.get('.breadcrumb-item').should('have.length.gte', 2);
        }
      });
    });
  });

  it('should test image preview modal functionality', () => {
    cy.waitForLoading();

    cy.get('#gallery-container').within(() => {
      // If image items exist, test modal
      cy.get('body').then($body => {
        if ($body.find('.image-file').length > 0) {
          cy.get('.image-file').first().click();
          cy.get('#image-modal').should('have.class', 'show');
          cy.get('#image-modal .btn-close').click();
          cy.get('#image-modal').should('not.have.class', 'show');
        }
      });
    });
  });

  it('should be responsive across screen sizes', () => {
    cy.testResponsive('.main-content-card');

    // Test file grid responsiveness
    cy.viewport(375, 667); // Mobile
    cy.get('.file-grid').should('exist');

    cy.viewport(768, 1024); // Tablet
    cy.get('.file-grid').should('exist');

    cy.viewport(1280, 720); // Desktop
    cy.get('.file-grid').should('exist');
  });

  it('should handle empty state properly', () => {
    cy.waitForLoading();

    // If no files, should show empty state
    cy.get('#gallery-container').within(() => {
      cy.get('body').then($body => {
        if ($body.find('.empty-state').length > 0) {
          cy.get('.empty-state').should('be.visible');
          cy.get('.empty-state-icon').should('be.visible');
          cy.get('.empty-state').should('contain.text', 'No files found');
        }
      });
    });
  });

  it('should have proper Bootstrap modal structure', () => {
    // Check modal has proper Bootstrap classes
    cy.get('#image-modal').should('have.class', 'modal');
    cy.get('#image-modal').should('have.class', 'fade');
    cy.get('#image-modal .modal-content').should('exist');
    cy.get('#image-modal .btn-close').should('exist');
  });

  it('should handle image loading errors gracefully', () => {
    cy.waitForLoading();

    // Check that images have error handling
    cy.get('#gallery-container').within(() => {
      cy.get('body').then($body => {
        if ($body.find('img').length > 0) {
          cy.get('img').should('have.attr', 'onerror');
          cy.get('img').should('have.attr', 'loading', 'lazy');
        }
      });
    });
  });

  it('should test breadcrumb updates correctly', () => {
    cy.waitForLoading();

    // Initial breadcrumb should show Home
    cy.get('#breadcrumb').should('contain.text', 'Home');

    // If navigation occurs, breadcrumb should update
    cy.get('#gallery-container').within(() => {
      cy.get('body').then($body => {
        if ($body.find('[data-action="navigate"][data-path]').length > 0) {
          const firstFolder = $body.find('[data-action="navigate"][data-path]').first();
          const path = firstFolder.attr('data-path');
          if (path) {
            cy.wrap(firstFolder).click();
            cy.get('#breadcrumb').should('contain.text', path.split('/').pop());
          }
        }
      });
    });
  });
});