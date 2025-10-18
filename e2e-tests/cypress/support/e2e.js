// ***********************************************************
// This file is loaded automatically before test files
// ***********************************************************

import './commands'

// Hide fetch/XHR logs to reduce noise
const app = window.top;
if (!app.document.head.querySelector('[data-hide-command-log-request]')) {
  const style = app.document.createElement('style');
  style.innerHTML = '.command-name-request, .command-name-xhr { display: none }';
  style.setAttribute('data-hide-command-log-request', '');
  app.document.head.appendChild(style);
}

// Global error handling
Cypress.on('uncaught:exception', (err, runnable) => {
  // Don't fail tests on uncaught exceptions (helps with some WebSocket/async errors)
  if (err.message.includes('WebSocket') || err.message.includes('NetworkError')) {
    return false;
  }
  return true;
});

// Set default viewport for consistent testing
beforeEach(() => {
  cy.viewport(1280, 720);
});