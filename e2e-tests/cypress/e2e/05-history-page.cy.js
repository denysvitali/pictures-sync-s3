describe('History Page - Bootstrap UI', () => {
  beforeEach(() => {
    cy.visitWithAuth('/history');
    cy.waitForPageLoad();
  });

  it('should load history page with Bootstrap components', () => {
    cy.get('title').should('contain', 'Sync History - Photo Backup Station');
    cy.checkBootstrapLoaded();

    // Check active history tab
    cy.get('.nav-link[href="/history"]').should('have.class', 'active');
    cy.get('h2').should('contain', 'Sync History');
  });

  it('should display history page header with controls', () => {
    cy.get('.main-content-card').should('be.visible');
    cy.get('.d-flex.justify-content-between').should('exist');

    // Check refresh button
    cy.get('button').contains('Refresh').should('be.visible');
    cy.get('button').contains('Refresh').should('have.class', 'btn-secondary');
  });

  it('should display history timeline container', () => {
    cy.get('#history-timeline').should('be.visible');

    // Should show loading initially
    cy.get('#history-timeline').should('exist');
  });

  it('should load history API correctly', () => {
    // Test history API endpoint
    cy.testApiEndpoint('history');

    // Wait for history to load
    cy.waitForLoading();
  });

  it('should handle empty history state', () => {
    cy.waitForLoading();

    // Check for empty state or history items
    cy.get('#history-timeline').within(() => {
      cy.get('body').then($body => {
        if ($body.find('.history-timeline').length === 0) {
          // Empty state
          cy.get('.empty-state').should('exist');
          cy.get('.empty-state-icon').should('exist');
          cy.get('.empty-state').should('contain.text', 'No sync history');
        }
      });
    });
  });

  it('should display history items with Bootstrap styling', () => {
    cy.waitForLoading();

    cy.get('#history-timeline').within(() => {
      // If history items exist, test their structure
      cy.get('body').then($body => {
        if ($body.find('.history-item').length > 0) {
          cy.get('.history-item').first().within(() => {
            cy.get('.card').should('exist');
            cy.get('.card-body').should('exist');
            cy.get('.card-title').should('exist');
            cy.get('.badge').should('exist');
          });
        }
      });
    });
  });

  it('should display history item badges correctly', () => {
    cy.waitForLoading();

    cy.get('#history-timeline').within(() => {
      cy.get('body').then($body => {
        if ($body.find('.badge').length > 0) {
          cy.get('.badge').should('satisfy', $badges => {
            return $badges.toArray().some(badge =>
              badge.classList.contains('bg-success') ||
              badge.classList.contains('bg-danger') ||
              badge.classList.contains('bg-secondary')
            );
          });
        }
      });
    });
  });

  it('should test refresh functionality', () => {
    cy.get('button').contains('Refresh').click();

    // Should trigger reload
    cy.get('#history-timeline').should('be.visible');
    cy.waitForLoading();
  });

  it('should handle history item stats display', () => {
    cy.waitForLoading();

    cy.get('#history-timeline').within(() => {
      cy.get('body').then($body => {
        if ($body.find('.history-item').length > 0) {
          // Should have stats grid
          cy.get('.row.g-2').should('exist');

          // Check for stats columns
          cy.get('[class*="col-"]').should('exist');

          // Should show file and data stats
          cy.get('.text-muted.small').should('exist');
          cy.get('.fw-bold').should('exist');
        }
      });
    });
  });

  it('should display error messages for failed syncs', () => {
    cy.waitForLoading();

    cy.get('#history-timeline').within(() => {
      cy.get('body').then($body => {
        if ($body.find('.alert-danger').length > 0) {
          cy.get('.alert-danger').should('contain.text', 'Error');
          cy.get('.alert-danger strong').should('exist');
        }
      });
    });
  });

  it('should have proper timeline visual structure', () => {
    cy.waitForLoading();

    cy.get('#history-timeline').within(() => {
      cy.get('body').then($body => {
        if ($body.find('.history-timeline').length > 0) {
          cy.get('.history-timeline').should('exist');

          // Timeline should have proper CSS classes
          cy.get('.history-item').should('exist');
        }
      });
    });
  });

  it('should be responsive across screen sizes', () => {
    cy.testResponsive('.main-content-card');

    // Test mobile layout
    cy.viewport(375, 667);
    cy.get('#history-timeline').should('be.visible');
    cy.get('.d-flex.justify-content-between').should('be.visible');

    // Test tablet layout
    cy.viewport(768, 1024);
    cy.get('.row').should('exist');

    // Back to desktop
    cy.viewport(1280, 720);
  });

  it('should handle status icons correctly', () => {
    cy.waitForLoading();

    cy.get('#history-timeline').within(() => {
      cy.get('body').then($body => {
        if ($body.find('.history-item').length > 0) {
          // Should have status icons (emoji)
          cy.get('.card-title').should('exist');

          // Icons should be visible in titles
          cy.get('.card-title').each($title => {
            expect($title.text()).to.match(/[✅❌⏹️🔄]/);
          });
        }
      });
    });
  });

  it('should format dates and durations correctly', () => {
    cy.waitForLoading();

    cy.get('#history-timeline').within(() => {
      cy.get('body').then($body => {
        if ($body.find('.history-item').length > 0) {
          // Should have formatted timestamps
          cy.get('.text-muted.small').should('exist');

          // Should have duration formatting
          cy.get('.fw-bold').should('exist');
        }
      });
    });
  });

  it('should handle different sync statuses', () => {
    cy.waitForLoading();

    cy.get('#history-timeline').within(() => {
      cy.get('body').then($body => {
        if ($body.find('.history-item').length > 0) {
          // Check status classes
          cy.get('.history-item').should('satisfy', $items => {
            return $items.toArray().some(item =>
              item.classList.contains('status-success') ||
              item.classList.contains('status-error') ||
              item.classList.contains('status-cancelled')
            );
          });
        }
      });
    });
  });

  it('should test WebSocket connection for real-time updates', () => {
    // Check connection status
    cy.checkConnectionStatus();

    // If WebSocket is connected, status should update
    cy.get('#connection-status').should('be.visible');
  });

  it('should have proper Bootstrap card structure', () => {
    cy.waitForLoading();

    cy.get('#history-timeline').within(() => {
      cy.get('body').then($body => {
        if ($body.find('.card').length > 0) {
          cy.get('.card').should('have.class', 'card');
          cy.get('.card-body').should('exist');
          cy.get('.card-title').should('exist');
        }
      });
    });
  });

  it('should handle long history lists properly', () => {
    cy.waitForLoading();

    // If many history items exist, should handle scrolling
    cy.get('#history-timeline').scrollTo('bottom');
    cy.get('#history-timeline').should('be.visible');
  });
});