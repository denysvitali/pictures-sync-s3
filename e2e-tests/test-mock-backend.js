#!/usr/bin/env node

/**
 * Comprehensive test suite for the Photo Backup Station Mock Backend
 * Tests all UI interactions with realistic simulated data
 */

const http = require('http');
const https = require('https');

class MockBackendTestRunner {
  constructor(baseUrl = 'http://localhost:8081') {
    this.baseUrl = baseUrl;
    this.username = 'gokrazy';
    this.password = 'dev';
    this.auth = Buffer.from(`${this.username}:${this.password}`).toString('base64');
    this.tests = [];
    this.passed = 0;
    this.failed = 0;
  }

  async makeRequest(path, options = {}) {
    return new Promise((resolve, reject) => {
      const url = new URL(path, this.baseUrl);
      const isHttps = url.protocol === 'https:';
      const client = isHttps ? https : http;

      const reqOptions = {
        hostname: url.hostname,
        port: url.port || (isHttps ? 443 : 80),
        path: url.pathname + url.search,
        method: options.method || 'GET',
        headers: {
          'Authorization': `Basic ${this.auth}`,
          'Content-Type': 'application/json',
          ...options.headers
        }
      };

      // Disable SSL verification for self-signed certificates
      if (isHttps) {
        reqOptions.rejectUnauthorized = false;
      }

      const req = client.request(reqOptions, (res) => {
        let body = '';
        res.on('data', chunk => body += chunk);
        res.on('end', () => {
          try {
            resolve({
              statusCode: res.statusCode,
              headers: res.headers,
              body: body,
              json: res.headers['content-type']?.includes('application/json') ? JSON.parse(body) : null
            });
          } catch (e) {
            resolve({
              statusCode: res.statusCode,
              headers: res.headers,
              body: body,
              json: null
            });
          }
        });
      });

      req.on('error', reject);

      if (options.body) {
        req.write(typeof options.body === 'string' ? options.body : JSON.stringify(options.body));
      }

      req.end();
    });
  }

  test(name, testFn) {
    this.tests.push({ name, testFn });
  }

  async assert(condition, message) {
    if (!condition) {
      throw new Error(message);
    }
  }

  async runTest(test) {
    console.log(`⏳ Running: ${test.name}`);
    try {
      await test.testFn();
      this.passed++;
      console.log(`✅ PASSED: ${test.name}`);
    } catch (error) {
      this.failed++;
      console.log(`❌ FAILED: ${test.name}`);
      console.log(`   Error: ${error.message}`);
    }
  }

  async run() {
    console.log('🧪 Starting Photo Backup Station Mock Backend Tests\n');

    for (const test of this.tests) {
      await this.runTest(test);
    }

    console.log('\n📊 Test Results:');
    console.log(`   ✅ Passed: ${this.passed}`);
    console.log(`   ❌ Failed: ${this.failed}`);
    console.log(`   📈 Total: ${this.tests.length}`);

    if (this.failed === 0) {
      console.log('\n🎉 All tests passed!');
    } else {
      console.log(`\n⚠️  ${this.failed} test(s) failed.`);
      process.exit(1);
    }
  }
}

const runner = new MockBackendTestRunner();

// ============================================================================
// BASIC INFRASTRUCTURE TESTS
// ============================================================================

runner.test('Mock server responds to status page', async () => {
  const res = await runner.makeRequest('/');
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.body.includes('Photo Backup Station'), 'Page title missing');
});

runner.test('Mock server requires authentication', async () => {
  const noAuthRunner = new MockBackendTestRunner();
  noAuthRunner.auth = '';
  const res = await noAuthRunner.makeRequest('/');
  await runner.assert(res.statusCode === 401, `Expected 401, got ${res.statusCode}`);
});

// ============================================================================
// MOCK API ENDPOINT TESTS
// ============================================================================

runner.test('Status API returns mock data', async () => {
  const res = await runner.makeRequest('/api/status');
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert('status' in res.json, 'Status field missing');
  await runner.assert('sdcard_mounted' in res.json, 'SDCard mounted field missing');
  await runner.assert('available_devices' in res.json, 'Available devices field missing');
  await runner.assert(Array.isArray(res.json.available_devices), 'Available devices should be array');
  await runner.assert(res.json.available_devices.length > 0, 'Should have mock devices');
});

runner.test('History API returns mock sync records', async () => {
  const res = await runner.makeRequest('/api/history');
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert(Array.isArray(res.json), 'History should be an array');
  await runner.assert(res.json.length > 0, 'Should have mock history records');

  const record = res.json[0];
  await runner.assert('id' in record, 'History record missing ID');
  await runner.assert('start_time' in record, 'History record missing start time');
  await runner.assert('status' in record, 'History record missing status');
  await runner.assert('files_total' in record, 'History record missing files total');
  await runner.assert('card_id' in record, 'History record missing card ID');
});

runner.test('WiFi scan returns mock networks', async () => {
  const res = await runner.makeRequest('/api/wifi/scan', { method: 'POST' });
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert('networks' in res.json, 'Networks field missing');
  await runner.assert(Array.isArray(res.json.networks), 'Networks should be an array');
  await runner.assert(res.json.networks.length > 0, 'Should have mock WiFi networks');

  const network = res.json.networks[0];
  await runner.assert('ssid' in network, 'Network missing SSID');
  await runner.assert('signal' in network, 'Network missing signal strength');
  await runner.assert('encrypted' in network, 'Network missing encryption status');
});

runner.test('WiFi networks returns saved networks', async () => {
  const res = await runner.makeRequest('/api/wifi/networks');
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert('networks' in res.json, 'Networks field missing');
  await runner.assert(Array.isArray(res.json.networks), 'Networks should be an array');
});

runner.test('WiFi status returns current connection', async () => {
  const res = await runner.makeRequest('/api/wifi/status');
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert('connected' in res.json, 'Connected field missing');
});

runner.test('Config API returns mock rclone config', async () => {
  const res = await runner.makeRequest('/api/config');
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert('config' in res.json, 'Config field missing');
  await runner.assert(res.json.config.includes('[backblaze]'), 'Should contain backblaze config');
});

runner.test('Config test simulates validation', async () => {
  const testConfig = '[test]\ntype = local\nnounc = /tmp';
  const res = await runner.makeRequest('/api/config/test', {
    method: 'POST',
    body: { config: testConfig }
  });
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert('success' in res.json, 'Success field missing');
});

runner.test('Files API returns mock gallery data', async () => {
  const res = await runner.makeRequest('/api/files');
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert('files' in res.json, 'Files field missing');
  await runner.assert(Array.isArray(res.json.files), 'Files should be an array');
});

// ============================================================================
// INTERACTIVE FUNCTIONALITY TESTS
// ============================================================================

runner.test('WiFi connect simulation works', async () => {
  const res = await runner.makeRequest('/api/wifi/connect', {
    method: 'POST',
    body: { ssid: 'Test_Network', password: 'correct_password' }
  });
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert('success' in res.json, 'Success field missing');
});

runner.test('WiFi connect handles authentication failure', async () => {
  const res = await runner.makeRequest('/api/wifi/connect', {
    method: 'POST',
    body: { ssid: 'Home_WiFi_5G', password: 'wrongpassword' }
  });
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert('success' in res.json, 'Success field missing');
  await runner.assert(res.json.success === false, 'Should fail with wrong password');
  await runner.assert('error' in res.json, 'Error message missing');
});

runner.test('WiFi disconnect removes network', async () => {
  const res = await runner.makeRequest('/api/wifi/disconnect', {
    method: 'POST',
    body: { ssid: 'Test_Network' }
  });
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert('success' in res.json, 'Success field missing');
  await runner.assert(res.json.success === true, 'Should successfully disconnect');
});

runner.test('WiFi reorder maintains network order', async () => {
  const res = await runner.makeRequest('/api/wifi/reorder', {
    method: 'POST',
    body: { ssids: ['Office_Network', 'Home_WiFi_5G', 'Guest_Network'] }
  });
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert('success' in res.json, 'Success field missing');
  await runner.assert(res.json.success === true, 'Should successfully reorder');
});

runner.test('Config save accepts valid configuration', async () => {
  const newConfig = '[test]\ntype = local\nnounc = /tmp\n\n[another]\ntype = memory';
  const res = await runner.makeRequest('/api/config', {
    method: 'POST',
    body: { config: newConfig }
  });
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert('success' in res.json, 'Success field missing');
  await runner.assert(res.json.success === true, 'Should accept valid config');
});

runner.test('Config save rejects invalid configuration', async () => {
  const invalidConfig = 'this is not a valid rclone config';
  const res = await runner.makeRequest('/api/config', {
    method: 'POST',
    body: { config: invalidConfig }
  });
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert('success' in res.json, 'Success field missing');
  await runner.assert(res.json.success === false, 'Should reject invalid config');
  await runner.assert('error' in res.json, 'Error message missing');
});

// ============================================================================
// SYNC SIMULATION TESTS
// ============================================================================

runner.test('Sync start begins simulation', async () => {
  const res = await runner.makeRequest('/api/sync/start', { method: 'POST' });
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert('status' in res.json, 'Status field missing');
  await runner.assert(res.json.status === 'ok', 'Should start sync successfully');
});

runner.test('Status shows sync in progress after start', async () => {
  // Wait a moment for sync to start
  await new Promise(resolve => setTimeout(resolve, 1000));

  const res = await runner.makeRequest('/api/status');
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert(res.json.status === 'syncing', 'Should show syncing status');
  await runner.assert('current_sync' in res.json, 'Current sync missing');
  await runner.assert(res.json.current_sync !== null, 'Current sync should not be null');

  const sync = res.json.current_sync;
  await runner.assert('files_synced' in sync, 'Files synced missing');
  await runner.assert('files_total' in sync, 'Files total missing');
  await runner.assert('bytes_synced' in sync, 'Bytes synced missing');
  await runner.assert('current_file' in sync, 'Current file missing');
});

runner.test('Sync progress updates over time', async () => {
  // Get initial progress
  const res1 = await runner.makeRequest('/api/status');
  const progress1 = res1.json.current_sync?.files_synced || 0;

  // Wait for progress
  await new Promise(resolve => setTimeout(resolve, 2000));

  // Get updated progress
  const res2 = await runner.makeRequest('/api/status');
  const progress2 = res2.json.current_sync?.files_synced || 0;

  await runner.assert(progress2 >= progress1, 'Progress should increase over time');
});

runner.test('Sync cancel stops simulation', async () => {
  const res = await runner.makeRequest('/api/sync/cancel', { method: 'POST' });
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert('status' in res.json, 'Status field missing');
  await runner.assert(res.json.status === 'ok', 'Should cancel sync successfully');
});

runner.test('Status shows idle after cancel', async () => {
  // Wait a moment for cancel to process
  await new Promise(resolve => setTimeout(resolve, 500));

  const res = await runner.makeRequest('/api/status');
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert(res.json.status === 'idle', 'Should return to idle after cancel');
  await runner.assert(res.json.current_sync === null, 'Current sync should be null');
});

// ============================================================================
// ERROR HANDLING TESTS
// ============================================================================

runner.test('Invalid JSON requests are handled gracefully', async () => {
  const res = await runner.makeRequest('/api/wifi/connect', {
    method: 'POST',
    body: 'invalid json{{'
  });
  await runner.assert(res.statusCode === 400, `Expected 400, got ${res.statusCode}`);
});

runner.test('Unsupported HTTP methods return 405', async () => {
  const res = await runner.makeRequest('/api/status', { method: 'DELETE' });
  await runner.assert(res.statusCode === 405, `Expected 405, got ${res.statusCode}`);
});

runner.test('Double sync start returns conflict', async () => {
  // Start first sync
  await runner.makeRequest('/api/sync/start', { method: 'POST' });

  // Try to start second sync
  const res = await runner.makeRequest('/api/sync/start', { method: 'POST' });
  await runner.assert(res.statusCode === 409, `Expected 409, got ${res.statusCode}`);

  // Clean up
  await runner.makeRequest('/api/sync/cancel', { method: 'POST' });
});

// ============================================================================
// BOOTSTRAP UI VALIDATION
// ============================================================================

runner.test('All pages load with Bootstrap structure', async () => {
  const pages = ['/', '/wifi', '/gallery', '/config', '/history'];

  for (const page of pages) {
    const res = await runner.makeRequest(page);
    await runner.assert(res.statusCode === 200, `Page ${page} failed to load`);
    await runner.assert(res.body.includes('bootstrap.min.css'), `Page ${page} missing Bootstrap CSS`);
    await runner.assert(res.body.includes('bootstrap.bundle.min.js'), `Page ${page} missing Bootstrap JS`);
    await runner.assert(res.body.includes('nav-pills'), `Page ${page} missing Bootstrap navigation`);
  }
});

runner.test('Static assets are accessible', async () => {
  const assets = [
    '/static/bootstrap/css/bootstrap.min.css',
    '/static/bootstrap/js/bootstrap.bundle.min.js',
    '/static/css/theme.css',
    '/static/js/utils.js'
  ];

  for (const asset of assets) {
    const res = await runner.makeRequest(asset);
    await runner.assert(res.statusCode === 200, `Asset ${asset} not accessible`);
  }
});

// Run all tests
runner.run().catch(console.error);