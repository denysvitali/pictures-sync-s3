#!/usr/bin/env node

/**
 * Simple Node.js test runner for Photo Backup Station Bootstrap UI
 * Tests core functionality without requiring a full browser environment
 */

const https = require('https');
const http = require('http');

const AUTH = 'Basic ' + Buffer.from('gokrazy:dev').toString('base64');
const BASE_URL = 'http://localhost:8080';

class TestRunner {
  constructor() {
    this.tests = [];
    this.passed = 0;
    this.failed = 0;
  }

  test(name, fn) {
    this.tests.push({ name, fn });
  }

  async makeRequest(path, options = {}) {
    return new Promise((resolve, reject) => {
      const opts = {
        method: 'GET',
        headers: {
          'Authorization': AUTH,
          'User-Agent': 'PhotoBackupStation-TestRunner',
          ...options.headers
        },
        ...options
      };

      const req = http.request(BASE_URL + path, opts, (res) => {
        let data = '';
        res.on('data', chunk => data += chunk);
        res.on('end', () => {
          resolve({
            status: res.statusCode,
            headers: res.headers,
            body: data
          });
        });
      });

      req.on('error', reject);
      if (options.body) {
        req.write(options.body);
      }
      req.end();
    });
  }

  async assert(condition, message) {
    if (!condition) {
      throw new Error(`Assertion failed: ${message}`);
    }
  }

  async run() {
    console.log('🚀 Starting Photo Backup Station Bootstrap UI Tests\n');

    for (const test of this.tests) {
      try {
        console.log(`⏳ Running: ${test.name}`);
        await test.fn();
        console.log(`✅ PASSED: ${test.name}`);
        this.passed++;
      } catch (error) {
        console.log(`❌ FAILED: ${test.name}`);
        console.log(`   Error: ${error.message}`);
        this.failed++;
      }
    }

    console.log(`\n📊 Test Results:`);
    console.log(`   ✅ Passed: ${this.passed}`);
    console.log(`   ❌ Failed: ${this.failed}`);
    console.log(`   📈 Total: ${this.tests.length}`);

    if (this.failed === 0) {
      console.log(`\n🎉 All tests passed!`);
      process.exit(0);
    } else {
      console.log(`\n💥 ${this.failed} test(s) failed.`);
      process.exit(1);
    }
  }
}

const runner = new TestRunner();

// ============================================================================
// INFRASTRUCTURE TESTS
// ============================================================================

runner.test('Server responds with 200 for status page', async () => {
  const res = await runner.makeRequest('/');
  await runner.assert(res.status === 200, `Expected 200, got ${res.status}`);
});

runner.test('Server requires authentication', async () => {
  const res = await runner.makeRequest('/', { headers: {} });
  await runner.assert(res.status === 401, `Expected 401, got ${res.status}`);
});

runner.test('All pages are accessible', async () => {
  const pages = ['/', '/history', '/wifi', '/gallery', '/config'];

  for (const page of pages) {
    const res = await runner.makeRequest(page);
    await runner.assert(res.status === 200, `Page ${page} returned ${res.status}`);
  }
});

// ============================================================================
// STATIC ASSET TESTS
// ============================================================================

runner.test('Bootstrap CSS loads correctly', async () => {
  const res = await runner.makeRequest('/static/bootstrap/css/bootstrap.min.css');
  await runner.assert(res.status === 200, `Bootstrap CSS returned ${res.status}`);
  await runner.assert(res.body.includes('.btn'), 'Bootstrap CSS missing .btn class');
  await runner.assert(res.body.includes('.card'), 'Bootstrap CSS missing .card class');
  await runner.assert(res.body.includes('.modal'), 'Bootstrap CSS missing .modal class');
});

runner.test('Bootstrap JavaScript loads correctly', async () => {
  const res = await runner.makeRequest('/static/bootstrap/js/bootstrap.bundle.min.js');
  await runner.assert(res.status === 200, `Bootstrap JS returned ${res.status}`);
  await runner.assert(res.body.includes('bootstrap'), 'Bootstrap JS missing bootstrap object');
});

runner.test('Custom theme CSS loads correctly', async () => {
  const res = await runner.makeRequest('/static/css/theme.css');
  await runner.assert(res.status === 200, `Theme CSS returned ${res.status}`);
  await runner.assert(res.body.includes('app-container'), 'Theme CSS missing app-container');
  await runner.assert(res.body.includes('--bs-primary'), 'Theme CSS missing Bootstrap variables');
});

runner.test('Utility JavaScript loads correctly', async () => {
  const res = await runner.makeRequest('/static/js/utils.js');
  await runner.assert(res.status === 200, `Utils JS returned ${res.status}`);
  await runner.assert(res.body.includes('showToast'), 'Utils JS missing showToast function');
  await runner.assert(res.body.includes('showConfirmModal'), 'Utils JS missing showConfirmModal function');
  await runner.assert(res.body.includes('escapeHtml'), 'Utils JS missing escapeHtml function');
});

// ============================================================================
// HTML STRUCTURE TESTS
// ============================================================================

runner.test('Status page has Bootstrap structure', async () => {
  const res = await runner.makeRequest('/');
  await runner.assert(res.body.includes('bootstrap.min.css'), 'Missing Bootstrap CSS link');
  await runner.assert(res.body.includes('bootstrap.bundle.min.js'), 'Missing Bootstrap JS link');
  await runner.assert(res.body.includes('nav-pills'), 'Missing Bootstrap nav-pills');
  await runner.assert(res.body.includes('card'), 'Missing Bootstrap cards');
  await runner.assert(res.body.includes('progress'), 'Missing Bootstrap progress bars');
});

runner.test('WiFi page has Bootstrap components', async () => {
  const res = await runner.makeRequest('/wifi');
  await runner.assert(res.body.includes('list-group'), 'Missing Bootstrap list-group');
  await runner.assert(res.body.includes('form-select'), 'Missing Bootstrap form controls');
  await runner.assert(res.body.includes('btn'), 'Missing Bootstrap buttons');
});

runner.test('Gallery page has Bootstrap structure', async () => {
  const res = await runner.makeRequest('/gallery');
  await runner.assert(res.body.includes('breadcrumb'), 'Missing Bootstrap breadcrumb');
  await runner.assert(res.body.includes('modal'), 'Missing Bootstrap modal');
  await runner.assert(res.body.includes('modal-xl'), 'Missing modal size class');
});

runner.test('Config page has Bootstrap forms', async () => {
  const res = await runner.makeRequest('/config');
  await runner.assert(res.body.includes('form-control'), 'Missing Bootstrap form controls');
  await runner.assert(res.body.includes('form-label'), 'Missing Bootstrap form labels');
  await runner.assert(res.body.includes('btn-primary'), 'Missing Bootstrap primary button');
});

runner.test('History page has Bootstrap timeline', async () => {
  const res = await runner.makeRequest('/history');
  await runner.assert(res.body.includes('history-timeline'), 'Missing history timeline');
  await runner.assert(res.body.includes('card'), 'Missing Bootstrap cards');
  await runner.assert(res.body.includes('badge'), 'Missing Bootstrap badges');
});

// ============================================================================
// API TESTS
// ============================================================================

runner.test('Status API returns valid JSON', async () => {
  const res = await runner.makeRequest('/api/status');
  await runner.assert(res.status === 200, `Status API returned ${res.status}`);

  let data;
  try {
    data = JSON.parse(res.body);
  } catch (e) {
    throw new Error('Status API returned invalid JSON');
  }

  await runner.assert(typeof data === 'object', 'Status API response is not an object');
  await runner.assert('status' in data, 'Status API missing status field');
});

runner.test('History API returns valid JSON', async () => {
  const res = await runner.makeRequest('/api/history');
  await runner.assert(res.status === 200, `History API returned ${res.status}`);

  let data;
  try {
    data = JSON.parse(res.body);
  } catch (e) {
    throw new Error('History API returned invalid JSON');
  }

  await runner.assert(Array.isArray(data), 'History API response is not an array');
});

runner.test('Config API returns valid JSON', async () => {
  const res = await runner.makeRequest('/api/config');
  await runner.assert(res.status === 200, `Config API returned ${res.status}`);

  let data;
  try {
    data = JSON.parse(res.body);
  } catch (e) {
    throw new Error('Config API returned invalid JSON');
  }

  await runner.assert(typeof data === 'object', 'Config API response is not an object');
  await runner.assert('configured' in data, 'Config API missing configured field');
});

runner.test('WiFi APIs return valid JSON', async () => {
  const statusRes = await runner.makeRequest('/api/wifi/status');
  await runner.assert(statusRes.status === 200, `WiFi status API returned ${statusRes.status}`);

  const networksRes = await runner.makeRequest('/api/wifi/networks');
  await runner.assert(networksRes.status === 200, `WiFi networks API returned ${networksRes.status}`);

  let statusData = JSON.parse(statusRes.body);
  let networksData = JSON.parse(networksRes.body);

  await runner.assert(typeof statusData === 'object', 'WiFi status is not an object');
  await runner.assert(typeof networksData === 'object', 'WiFi networks is not an object');
});

runner.test('WebSocket token API works', async () => {
  const res = await runner.makeRequest('/api/ws-token');
  await runner.assert(res.status === 200, `WebSocket token API returned ${res.status}`);

  let data = JSON.parse(res.body);
  await runner.assert('ws_token' in data, 'WebSocket token API missing ws_token field');
  await runner.assert(typeof data.ws_token === 'string', 'WebSocket token is not a string');
  await runner.assert(data.ws_token.length > 0, 'WebSocket token is empty');
});

// ============================================================================
// SECURITY TESTS
// ============================================================================

runner.test('Security headers are present', async () => {
  const res = await runner.makeRequest('/');

  const headers = res.headers;
  await runner.assert('x-content-type-options' in headers, 'Missing X-Content-Type-Options header');
  await runner.assert('x-frame-options' in headers, 'Missing X-Frame-Options header');
});

runner.test('Unauthorized access is blocked', async () => {
  const endpoints = ['/api/status', '/api/config', '/api/wifi/status'];

  for (const endpoint of endpoints) {
    const res = await runner.makeRequest(endpoint, { headers: {} });
    await runner.assert(res.status === 401, `Endpoint ${endpoint} allowed unauthorized access`);
  }
});

// ============================================================================
// CONTENT VALIDATION TESTS
// ============================================================================

runner.test('Pages have proper titles and meta tags', async () => {
  const pages = [
    { path: '/', title: 'Status - Photo Backup Station' },
    { path: '/wifi', title: 'WiFi Management - Photo Backup Station' },
    { path: '/gallery', title: 'Photo Gallery - Photo Backup Station' },
    { path: '/config', title: 'Configuration - Photo Backup Station' },
    { path: '/history', title: 'Sync History - Photo Backup Station' }
  ];

  for (const page of pages) {
    const res = await runner.makeRequest(page.path);
    await runner.assert(res.body.includes(`<title>${page.title}</title>`),
                       `Page ${page.path} missing correct title`);
    await runner.assert(res.body.includes('viewport'), 'Missing viewport meta tag');
    await runner.assert(res.body.includes('charset="UTF-8"'), 'Missing charset declaration');
  }
});

runner.test('Navigation is consistent across pages', async () => {
  const pages = ['/', '/wifi', '/gallery', '/config', '/history'];

  for (const page of pages) {
    const res = await runner.makeRequest(page);
    await runner.assert(res.body.includes('📊 Status'), 'Missing Status nav link');
    await runner.assert(res.body.includes('📋 History'), 'Missing History nav link');
    await runner.assert(res.body.includes('📶 WiFi'), 'Missing WiFi nav link');
    await runner.assert(res.body.includes('🖼️ Gallery'), 'Missing Gallery nav link');
    await runner.assert(res.body.includes('⚙️ Configuration'), 'Missing Configuration nav link');
  }
});

// ============================================================================
// ACCESSIBILITY TESTS
// ============================================================================

runner.test('Pages have proper accessibility attributes', async () => {
  const res = await runner.makeRequest('/gallery');
  await runner.assert(res.body.includes('aria-label'), 'Missing aria-label attributes');
  await runner.assert(res.body.includes('aria-hidden'), 'Missing aria-hidden attributes');
  await runner.assert(res.body.includes('role='), 'Missing role attributes');
});

runner.test('Forms have proper labels', async () => {
  const res = await runner.makeRequest('/config');
  await runner.assert(res.body.includes('label for='), 'Missing form labels');
  await runner.assert(res.body.includes('form-label'), 'Missing Bootstrap form label classes');
});

// Run all tests
runner.run().catch(console.error);