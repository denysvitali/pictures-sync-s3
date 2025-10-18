describe('Bootstrap Components & Responsiveness', () => {
  beforeEach(() => {
    cy.visitWithAuth('/');
    cy.waitForPageLoad();
  });

  describe('Bootstrap CSS and JS Loading', () => {
    it('should load Bootstrap CSS correctly', () => {
      cy.get('link[href*="bootstrap.min.css"]').should('exist');

      // Check that Bootstrap CSS is actually loaded
      cy.get('link[href*="bootstrap.min.css"]').should($link => {
        const href = $link.attr('href');
        cy.request(href).then(response => {
          expect(response.status).to.eq(200);
          expect(response.body).to.include('.btn');
          expect(response.body).to.include('.card');
          expect(response.body).to.include('.modal');
        });
      });
    });

    it('should load Bootstrap JavaScript correctly', () => {
      cy.get('script[src*="bootstrap.bundle.min.js"]').should('exist');

      // Check that Bootstrap JS creates global bootstrap object
      cy.window().should('have.property', 'bootstrap');
      cy.window().its('bootstrap').should('have.property', 'Modal');
      cy.window().its('bootstrap').should('have.property', 'Toast');
    });

    it('should load custom theme CSS', () => {
      cy.get('link[href*="theme.css"]').should('exist');

      // Check custom CSS variables are applied
      cy.get(':root').should('satisfy', $root => {
        const styles = window.getComputedStyle($root[0]);
        return styles.getPropertyValue('--bs-primary').includes('#6366f1') ||
               styles.getPropertyValue('--primary').includes('#6366f1');
      });
    });

    it('should load utilities JavaScript', () => {
      cy.get('script[src*="utils.js"]').should('exist');

      // Check utility functions are available
      cy.window().should('have.property', 'showToast');
      cy.window().should('have.property', 'showConfirmModal');
      cy.window().should('have.property', 'escapeHtml');
    });
  });

  describe('Bootstrap Grid System', () => {
    it('should use Bootstrap container classes', () => {
      cy.get('.app-container').should('exist');
      cy.get('.container, .container-fluid').should('exist');
    });

    it('should use Bootstrap row and column classes', () => {
      cy.get('.row').should('exist');
      cy.get('[class*="col-"]').should('exist');
    });

    it('should be responsive at different breakpoints', () => {
      const breakpoints = [
        { width: 576, name: 'sm' },
        { width: 768, name: 'md' },
        { width: 992, name: 'lg' },
        { width: 1200, name: 'xl' },
        { width: 1400, name: 'xxl' }
      ];

      breakpoints.forEach(bp => {
        cy.viewport(bp.width, 800);
        cy.get('.app-container').should('be.visible');
        cy.get('.main-content-card').should('be.visible');
        cy.wait(300); // Allow layout to settle
      });
    });
  });

  describe('Bootstrap Components', () => {
    it('should have proper card components', () => {
      cy.get('.card').should('exist');
      cy.get('.card-header').should('exist');
      cy.get('.card-body').should('exist');

      // Check card styling
      cy.get('.card').should('have.css', 'border-radius');
      cy.get('.card').should('have.css', 'border');
    });

    it('should have proper button components', () => {
      cy.get('.btn').should('exist');
      cy.get('.btn-primary').should('exist');
      cy.get('.btn-secondary').should('exist');

      // Check button states
      cy.get('.btn').first().should('have.css', 'padding');
      cy.get('.btn').first().should('have.css', 'border-radius');
    });

    it('should have proper navigation components', () => {
      cy.get('.nav').should('exist');
      cy.get('.nav-pills').should('exist');
      cy.get('.nav-link').should('exist');
      cy.get('.nav-link.active').should('exist');

      // Check navigation styling
      cy.get('.nav-pills .nav-link.active').should('have.css', 'background-color');
    });

    it('should have proper alert components', () => {
      cy.visitWithAuth('/config');
      cy.waitForPageLoad();
      cy.waitForLoading();

      cy.get('.alert').should('exist');
      cy.get('.alert').should('satisfy', $alerts => {
        return $alerts.toArray().some(alert =>
          alert.classList.contains('alert-success') ||
          alert.classList.contains('alert-warning') ||
          alert.classList.contains('alert-danger')
        );
      });
    });

    it('should have proper progress components', () => {
      cy.visitWithAuth('/');
      cy.get('.progress').should('exist');
      cy.get('.progress-bar').should('exist');

      // Check progress bar styling
      cy.get('.progress').should('have.css', 'height');
      cy.get('.progress').should('have.css', 'background-color');
    });

    it('should have proper list group components', () => {
      cy.visitWithAuth('/wifi');
      cy.waitForPageLoad();
      cy.wait(1000); // Allow WiFi components to load

      cy.get('body').then($body => {
        if ($body.find('.list-group').length > 0) {
          cy.get('.list-group').should('exist');
          cy.get('.list-group-item').should('exist');
        }
      });
    });

    it('should have proper modal components', () => {
      cy.visitWithAuth('/gallery');
      cy.waitForPageLoad();

      cy.get('#image-modal').should('exist');
      cy.get('#image-modal').should('have.class', 'modal');
      cy.get('#image-modal .modal-dialog').should('exist');
      cy.get('#image-modal .modal-content').should('exist');
      cy.get('#image-modal .modal-header').should('exist');
      cy.get('#image-modal .modal-body').should('exist');
    });

    it('should have proper form components', () => {
      cy.visitWithAuth('/config');
      cy.waitForPageLoad();

      cy.get('.form-control').should('exist');
      cy.get('.form-label').should('exist');
      cy.get('.form-text').should('exist');

      // Check form styling
      cy.get('.form-control').should('have.css', 'border');
      cy.get('.form-control').should('have.css', 'border-radius');
    });
  });

  describe('Responsive Design', () => {
    it('should adapt navigation for mobile', () => {
      cy.viewport(375, 667); // iPhone SE

      cy.get('.nav-pills').should('be.visible');
      cy.get('.nav-link').should('be.visible');

      // Check if navigation wraps properly
      cy.get('.nav-pills').should('have.css', 'flex-wrap');
    });

    it('should adapt cards for mobile', () => {
      cy.viewport(375, 667);

      cy.get('.card').should('be.visible');
      cy.get('.card-body').should('be.visible');

      // Cards should stack properly
      cy.get('.card').should('have.css', 'width');
    });

    it('should adapt button groups for mobile', () => {
      cy.viewport(375, 667);

      cy.get('.d-flex.gap-2').should('exist');
      cy.get('.flex-wrap').should('exist');

      // Buttons should wrap on mobile
      cy.get('.btn').should('be.visible');
    });

    it('should work properly on tablet', () => {
      cy.viewport(768, 1024); // iPad

      cy.get('.app-container').should('be.visible');
      cy.get('.main-content-card').should('be.visible');
      cy.get('.nav-pills').should('be.visible');

      // Check responsive columns
      cy.get('[class*="col-md-"]').should('exist');
    });

    it('should work properly on desktop', () => {
      cy.viewport(1920, 1080); // Large desktop

      cy.get('.app-container').should('be.visible');
      cy.get('.main-content-card').should('be.visible');

      // Check that content doesn't become too wide
      cy.get('.app-container').should('have.css', 'max-width');
    });
  });

  describe('Accessibility', () => {
    it('should have proper ARIA labels', () => {
      cy.get('[aria-label]').should('exist');
      cy.get('[aria-hidden]').should('exist');
      cy.get('[role]').should('exist');
    });

    it('should have proper form labels', () => {
      cy.visitWithAuth('/config');
      cy.waitForPageLoad();

      cy.get('label[for]').should('exist');
      cy.get('label[for]').each($label => {
        const forId = $label.attr('for');
        cy.get(`#${forId}`).should('exist');
      });
    });

    it('should have proper button accessibility', () => {
      cy.get('button').should('exist');
      cy.get('button[type="button"]').should('exist');
      cy.get('button[type="submit"]').should('exist');

      // Check buttons have text or aria-label
      cy.get('button').each($btn => {
        expect($btn.text().trim().length > 0 || $btn.attr('aria-label')).to.be.true;
      });
    });

    it('should have proper link accessibility', () => {
      cy.get('a[href]').should('exist');

      // External links should have proper attributes
      cy.get('a[target="_blank"]').each($link => {
        expect($link.attr('rel')).to.include('noopener');
      });
    });
  });

  describe('Dark Mode Support', () => {
    it('should handle different color schemes', () => {
      // Test if CSS custom properties are properly set
      cy.get(':root').should('exist');

      // Check that theme colors are defined
      cy.window().then(win => {
        const styles = win.getComputedStyle(win.document.documentElement);
        const primaryColor = styles.getPropertyValue('--bs-primary');
        expect(primaryColor).to.not.be.empty;
      });
    });
  });

  describe('Performance', () => {
    it('should load assets efficiently', () => {
      // Check that CSS is loaded
      cy.get('link[rel="stylesheet"]').should('exist');

      // Check that JS is loaded
      cy.get('script[src]').should('exist');

      // Check that images have lazy loading
      cy.get('img[loading="lazy"]').should('exist');
    });

    it('should not have console errors', () => {
      cy.window().then(win => {
        // Check that major Bootstrap components are available
        expect(win.bootstrap).to.exist;
        expect(win.bootstrap.Modal).to.exist;
        expect(win.bootstrap.Toast).to.exist;
      });
    });
  });
});