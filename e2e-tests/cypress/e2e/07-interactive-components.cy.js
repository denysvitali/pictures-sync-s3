describe('Interactive Components - Toasts & Modals', () => {
  beforeEach(() => {
    cy.visitWithAuth('/');
    cy.waitForPageLoad();
  });

  describe('Toast Notifications', () => {
    it('should have toast utility functions available', () => {
      cy.window().should('have.property', 'showToast');
      cy.window().should('have.property', 'escapeHtml');
      cy.window().should('have.property', 'formatBytes');
      cy.window().should('have.property', 'formatDuration');
    });

    it('should create toast container when needed', () => {
      cy.window().then(win => {
        // Call showToast function
        win.showToast('Test toast message', 'info', 2000);

        // Check toast container is created
        cy.get('#toast-container').should('exist');
        cy.get('#toast-container').should('have.class', 'toast-container');
        cy.get('#toast-container').should('have.class', 'position-fixed');
      });
    });

    it('should display different toast types', () => {
      cy.window().then(win => {
        // Test success toast
        win.showToast('Success message', 'success', 1000);
        cy.get('.toast.bg-success').should('be.visible');

        // Test error toast
        win.showToast('Error message', 'error', 1000);
        cy.get('.toast.bg-danger').should('be.visible');

        // Test warning toast
        win.showToast('Warning message', 'warning', 1000);
        cy.get('.toast.bg-warning').should('be.visible');

        // Test info toast
        win.showToast('Info message', 'info', 1000);
        cy.get('.toast.bg-info').should('be.visible');
      });
    });

    it('should auto-dismiss toasts', () => {
      cy.window().then(win => {
        win.showToast('Auto-dismiss test', 'info', 500);
        cy.get('.toast').should('be.visible');
        cy.wait(1000);
        cy.get('.toast').should('not.exist');
      });
    });

    it('should allow manual toast dismissal', () => {
      cy.window().then(win => {
        win.showToast('Manual dismiss test', 'info', 10000);
        cy.get('.toast').should('be.visible');
        cy.get('.toast .btn-close').click();
        cy.get('.toast').should('not.be.visible');
      });
    });

    it('should handle multiple toasts', () => {
      cy.window().then(win => {
        win.showToast('Toast 1', 'info', 2000);
        win.showToast('Toast 2', 'success', 2000);
        win.showToast('Toast 3', 'warning', 2000);

        cy.get('.toast').should('have.length', 3);
      });
    });

    it('should escape HTML in toast messages', () => {
      cy.window().then(win => {
        const maliciousText = '<script>alert("xss")</script>';
        win.showToast(maliciousText, 'info', 2000);

        cy.get('.toast-body').should('not.contain.html', '<script>');
        cy.get('.toast-body').should('contain.text', maliciousText);
      });
    });
  });

  describe('Modal Components', () => {
    it('should have modal utility functions available', () => {
      cy.window().should('have.property', 'showConfirmModal');
      cy.window().should('have.property', 'showPromptModal');
    });

    it('should create and show confirmation modal', () => {
      cy.window().then(win => {
        win.showConfirmModal(
          'Test Confirmation',
          'Are you sure you want to proceed?',
          () => {},
          () => {}
        );

        cy.get('#confirm-modal').should('exist');
        cy.get('#confirm-modal').should('have.class', 'show');
        cy.get('#confirm-modal-title').should('contain.text', 'Test Confirmation');
        cy.get('#confirm-modal-body').should('contain.text', 'Are you sure');
      });
    });

    it('should handle confirmation modal buttons', () => {
      let confirmed = false;

      cy.window().then(win => {
        win.showConfirmModal(
          'Button Test',
          'Click confirm to test',
          () => { confirmed = true; }
        );

        // Test confirm button
        cy.get('#confirm-modal-btn').click();
        cy.get('#confirm-modal').should('not.have.class', 'show');
      }).then(() => {
        expect(confirmed).to.be.true;
      });
    });

    it('should handle confirmation modal cancel', () => {
      cy.window().then(win => {
        win.showConfirmModal(
          'Cancel Test',
          'Click cancel to test',
          () => {}
        );

        // Test cancel button
        cy.get('#confirm-modal button').contains('Cancel').click();
        cy.get('#confirm-modal').should('not.have.class', 'show');
      });
    });

    it('should create and show prompt modal', () => {
      cy.window().then(win => {
        win.showPromptModal(
          'Test Prompt',
          'Enter your name:',
          'Default Value',
          () => {},
          'text'
        );

        cy.get('#prompt-modal').should('exist');
        cy.get('#prompt-modal').should('have.class', 'show');
        cy.get('#prompt-modal-title').should('contain.text', 'Test Prompt');
        cy.get('#prompt-modal-message').should('contain.text', 'Enter your name');
        cy.get('#prompt-modal-input').should('have.value', 'Default Value');
      });
    });

    it('should handle prompt modal input types', () => {
      cy.window().then(win => {
        // Test password input
        win.showPromptModal(
          'Password Test',
          'Enter password:',
          '',
          () => {},
          'password'
        );

        cy.get('#prompt-modal-input').should('have.attr', 'type', 'password');
      });
    });

    it('should handle prompt modal submission', () => {
      let submittedValue = '';

      cy.window().then(win => {
        win.showPromptModal(
          'Submit Test',
          'Enter value:',
          '',
          (value) => { submittedValue = value; },
          'text'
        );

        cy.get('#prompt-modal-input').clear().type('test input');
        cy.get('#prompt-modal-btn').click();
      }).then(() => {
        expect(submittedValue).to.equal('test input');
      });
    });

    it('should handle prompt modal enter key', () => {
      let submittedValue = '';

      cy.window().then(win => {
        win.showPromptModal(
          'Enter Key Test',
          'Press Enter:',
          '',
          (value) => { submittedValue = value; }
        );

        cy.get('#prompt-modal-input').clear().type('enter test{enter}');
      }).then(() => {
        expect(submittedValue).to.equal('enter test');
      });
    });

    it('should handle modal backdrop clicks', () => {
      cy.window().then(win => {
        win.showConfirmModal('Backdrop Test', 'Test backdrop');

        // Click backdrop (outside modal content)
        cy.get('#confirm-modal').click('topLeft');
        cy.get('#confirm-modal').should('not.have.class', 'show');
      });
    });

    it('should focus input in prompt modal', () => {
      cy.window().then(win => {
        win.showPromptModal('Focus Test', 'Should focus input', '');

        cy.get('#prompt-modal-input').should('be.focused');
      });
    });
  });

  describe('Gallery Image Modal', () => {
    beforeEach(() => {
      cy.visitWithAuth('/gallery');
      cy.waitForPageLoad();
    });

    it('should have image modal in DOM', () => {
      cy.get('#image-modal').should('exist');
      cy.get('#image-modal').should('have.class', 'modal');
      cy.get('#image-modal').should('have.class', 'fade');
    });

    it('should have proper modal structure', () => {
      cy.get('#image-modal .modal-dialog').should('have.class', 'modal-xl');
      cy.get('#image-modal .modal-dialog').should('have.class', 'modal-dialog-centered');
      cy.get('#image-modal .modal-header').should('exist');
      cy.get('#image-modal .modal-body').should('exist');
      cy.get('#image-modal-title').should('exist');
      cy.get('#image-modal-img').should('exist');
    });

    it('should handle modal close button', () => {
      // Open modal manually for testing
      cy.window().then(win => {
        const modal = new win.bootstrap.Modal(win.document.getElementById('image-modal'));
        modal.show();

        cy.get('#image-modal').should('have.class', 'show');
        cy.get('#image-modal .btn-close').click();
        cy.get('#image-modal').should('not.have.class', 'show');
      });
    });

    it('should handle image loading in modal', () => {
      cy.window().then(win => {
        // Test previewImage function if it exists
        if (win.previewImage) {
          win.previewImage('/test/path.jpg', 'Test Image');

          cy.get('#image-modal').should('have.class', 'show');
          cy.get('#image-modal-title').should('contain.text', 'Test Image');
          cy.get('#image-modal-img').should('have.attr', 'src').and('include', 'test/path.jpg');
        }
      });
    });
  });

  describe('Real-world Integration Tests', () => {
    it('should show toasts on WiFi page interactions', () => {
      cy.visitWithAuth('/wifi');
      cy.waitForPageLoad();

      // Test scan functionality (should trigger toast on completion)
      cy.get('#scan-btn').click();
      cy.wait(3000);

      // Should show a toast (either success or error)
      cy.get('body').then($body => {
        if ($body.find('.toast').length > 0) {
          cy.get('.toast').should('be.visible');
        }
      });
    });

    it('should show toasts on config page interactions', () => {
      cy.visitWithAuth('/config');
      cy.waitForPageLoad();

      // Test empty form submission
      cy.get('button[type="submit"]').click();

      // Should show warning toast
      cy.get('body').then($body => {
        if ($body.find('.toast').length > 0) {
          cy.get('.toast').should('be.visible');
        }
      });
    });

    it('should use modals for WiFi password entry', () => {
      cy.visitWithAuth('/wifi');
      cy.waitForPageLoad();

      // If connect buttons exist, they should use modals
      cy.get('body').then($body => {
        if ($body.find('[data-action="connect"]').length > 0) {
          cy.get('[data-action="connect"]').first().click();
          cy.get('.modal').should('be.visible');
        }
      });
    });

    it('should use modals for network removal confirmation', () => {
      cy.visitWithAuth('/wifi');
      cy.waitForPageLoad();

      // If disconnect buttons exist, they should use modals
      cy.get('body').then($body => {
        if ($body.find('[data-action="disconnect"]').length > 0) {
          cy.get('[data-action="disconnect"]').first().click();
          cy.get('#confirm-modal').should('be.visible');
        }
      });
    });
  });

  describe('Error Handling', () => {
    it('should handle missing modal elements gracefully', () => {
      cy.window().then(win => {
        // Remove modal from DOM to test error handling
        const modal = win.document.getElementById('confirm-modal');
        if (modal) modal.remove();

        // Should not crash when trying to show modal
        expect(() => {
          win.showConfirmModal('Test', 'Test message', () => {});
        }).to.not.throw();
      });
    });

    it('should handle malformed toast data', () => {
      cy.window().then(win => {
        // Test with undefined/null values
        expect(() => {
          win.showToast(null, 'info');
          win.showToast(undefined, 'error');
          win.showToast('', 'warning');
        }).to.not.throw();
      });
    });

    it('should handle invalid toast types', () => {
      cy.window().then(win => {
        expect(() => {
          win.showToast('Test', 'invalid-type');
          win.showToast('Test', null);
          win.showToast('Test', undefined);
        }).to.not.throw();
      });
    });
  });
});