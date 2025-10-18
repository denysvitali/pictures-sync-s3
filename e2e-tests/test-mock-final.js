#!/usr/bin/env node

/**
 * Final comprehensive test suite for the Photo Backup Station Mock Backend
 * Tests all UI interactions with proper test isolation and realistic simulated data
 */

const http = require('http');
const https = require('https');

class FinalMockTestRunner {
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
    console.log('🧪 Starting Photo Backup Station Final Mock Backend Tests\n');

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

  async sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
  }
}

const runner = new FinalMockTestRunner();

// ============================================================================
// INFRASTRUCTURE TESTS
// ============================================================================

runner.test('🌐 Server accessibility and authentication', async () => {
  // Test without auth
  const noAuthRunner = new FinalMockTestRunner();
  noAuthRunner.auth = '';
  const noAuthRes = await noAuthRunner.makeRequest('/');
  await runner.assert(noAuthRes.statusCode === 401, `Expected 401 for no auth, got ${noAuthRes.statusCode}`);

  // Test with auth
  const res = await runner.makeRequest('/');
  await runner.assert(res.statusCode === 200, `Expected 200 with auth, got ${res.statusCode}`);
  await runner.assert(res.body.includes('Photo Backup Station'), 'Main page missing title');
});

runner.test('🎨 Bootstrap UI structure and assets', async () => {
  const pages = ['/', '/wifi', '/gallery', '/config', '/history'];

  for (const page of pages) {
    const res = await runner.makeRequest(page);
    await runner.assert(res.statusCode === 200, `Page ${page} failed to load`);
    await runner.assert(res.body.includes('bootstrap.min.css'), `Page ${page} missing Bootstrap CSS`);
    await runner.assert(res.body.includes('bootstrap.bundle.min.js'), `Page ${page} missing Bootstrap JS`);
    await runner.assert(res.body.includes('nav-pills'), `Page ${page} missing Bootstrap navigation`);
  }

  // Test static assets
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

// ============================================================================
// API DATA VALIDATION TESTS
// ============================================================================

runner.test('📊 Status API provides comprehensive state data', async () => {
  const res = await runner.makeRequest('/api/status');
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');

  const state = res.json;
  await runner.assert('status' in state, 'Status field missing');
  await runner.assert(['idle', 'detected', 'syncing', 'success', 'error'].includes(state.status), 'Invalid status value');
  await runner.assert('sdcard_mounted' in state, 'SDCard mounted field missing');
  await runner.assert('available_devices' in state, 'Available devices field missing');
  await runner.assert(Array.isArray(state.available_devices), 'Available devices should be array');
  await runner.assert(state.available_devices.length > 0, 'Should have mock devices');

  // Validate device structure
  const device = state.available_devices[0];
  await runner.assert('device_path' in device, 'Device missing path');
  await runner.assert('size' in device, 'Device missing size');
  await runner.assert('is_usb' in device, 'Device missing USB flag');
  await runner.assert('has_dcim' in device, 'Device missing DCIM flag');
});

runner.test('📝 History API provides detailed sync records', async () => {
  const res = await runner.makeRequest('/api/history');
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert(Array.isArray(res.json), 'History should be an array');
  await runner.assert(res.json.length > 0, 'Should have mock history records');

  const record = res.json[0];
  const requiredFields = ['id', 'start_time', 'end_time', 'status', 'files_total', 'files_synced', 'bytes_total', 'bytes_synced', 'card_id'];
  for (const field of requiredFields) {
    await runner.assert(field in record, `History record missing ${field}`);
  }
  await runner.assert(['success', 'error', 'cancelled'].includes(record.status), 'Invalid sync status');
});

runner.test('🔗 WebSocket token generation', async () => {
  const res = await runner.makeRequest('/api/ws-token');
  await runner.assert(res.statusCode === 200, `Expected 200, got ${res.statusCode}`);
  await runner.assert(res.json !== null, 'Response is not valid JSON');
  await runner.assert('ws_token' in res.json, 'WebSocket token missing');
  await runner.assert(typeof res.json.ws_token === 'string', 'Token should be string');
  await runner.assert(res.json.ws_token.length > 0, 'Token should not be empty');
});

// ============================================================================
// WIFI MANAGEMENT TESTS
// ============================================================================

runner.test('📶 WiFi scanning and network management', async () => {
  // Test network scanning
  const scanRes = await runner.makeRequest('/api/wifi/scan', { method: 'POST' });
  await runner.assert(scanRes.statusCode === 200, `Expected 200, got ${scanRes.statusCode}`);
  await runner.assert('networks' in scanRes.json, 'Scan missing networks');
  await runner.assert(Array.isArray(scanRes.json.networks), 'Networks should be array');
  await runner.assert(scanRes.json.networks.length > 0, 'Should have scanned networks');

  const network = scanRes.json.networks[0];
  await runner.assert('ssid' in network, 'Network missing SSID');
  await runner.assert('signal' in network, 'Network missing signal');
  await runner.assert('encrypted' in network, 'Network missing encryption');

  // Test saved networks
  const savedRes = await runner.makeRequest('/api/wifi/networks');
  await runner.assert(savedRes.statusCode === 200, `Expected 200, got ${savedRes.statusCode}`);
  await runner.assert('networks' in savedRes.json, 'Saved networks missing');

  // Test connection status
  const statusRes = await runner.makeRequest('/api/wifi/status');
  await runner.assert(statusRes.statusCode === 200, `Expected 200, got ${statusRes.statusCode}`);
  await runner.assert('connected' in statusRes.json, 'Connection status missing');
});

runner.test('🔐 WiFi authentication and error handling', async () => {
  // Test successful connection
  const connectRes = await runner.makeRequest('/api/wifi/connect', {
    method: 'POST',
    body: { ssid: 'Test_Network_Open', password: '' }
  });
  await runner.assert(connectRes.statusCode === 200, `Expected 200, got ${connectRes.statusCode}`);

  // Test authentication failure
  const failRes = await runner.makeRequest('/api/wifi/connect', {
    method: 'POST',
    body: { ssid: 'Home_WiFi_5G', password: 'wrongpassword' }
  });
  await runner.assert(failRes.statusCode === 200, `Expected 200, got ${failRes.statusCode}`);
  await runner.assert(failRes.json.success === false, 'Should fail with wrong password');
  await runner.assert('error' in failRes.json, 'Error message missing');

  // Test disconnection
  const disconnectRes = await runner.makeRequest('/api/wifi/disconnect', {
    method: 'POST',
    body: { ssid: 'Test_Network_Open' }
  });
  await runner.assert(disconnectRes.statusCode === 200, `Expected 200, got ${disconnectRes.statusCode}`);
  await runner.assert(disconnectRes.json.success === true, 'Should successfully disconnect');
});

// ============================================================================
// CONFIGURATION MANAGEMENT TESTS
// ============================================================================

runner.test('⚙️ Configuration management and validation', async () => {
  // Get initial config
  const getRes = await runner.makeRequest('/api/config');
  await runner.assert(getRes.statusCode === 200, `Expected 200, got ${getRes.statusCode}`);
  await runner.assert('config' in getRes.json, 'Config field missing');

  const originalConfig = getRes.json.config;

  // Test valid config save
  const validConfig = '[test]\ntype = local\nnounc = /tmp\n\n[backblaze]\ntype = b2\naccount = test';
  const saveRes = await runner.makeRequest('/api/config', {
    method: 'POST',
    body: { config: validConfig }
  });
  await runner.assert(saveRes.statusCode === 200, `Expected 200, got ${saveRes.statusCode}`);
  await runner.assert(saveRes.json.success === true, 'Should accept valid config');

  // Test invalid config rejection
  const invalidConfig = 'this is not a valid rclone config';
  const invalidRes = await runner.makeRequest('/api/config', {
    method: 'POST',
    body: { config: invalidConfig }
  });
  await runner.assert(invalidRes.statusCode === 200, `Expected 200, got ${invalidRes.statusCode}`);
  await runner.assert(invalidRes.json.success === false, 'Should reject invalid config');

  // Test config validation
  const testRes = await runner.makeRequest('/api/config/test', { method: 'POST' });
  await runner.assert(testRes.statusCode === 200, `Expected 200, got ${testRes.statusCode}`);
  await runner.assert('success' in testRes.json, 'Test result missing');
});

// ============================================================================
// SYNC SIMULATION TESTS
// ============================================================================

runner.test('🔄 Sync simulation lifecycle', async () => {
  // Ensure we start in idle state - force cancel any existing sync
  let statusRes = await runner.makeRequest('/api/status');
  if (statusRes.json.status === 'syncing' || statusRes.json.current_sync !== null) {
    try {
      await runner.makeRequest('/api/sync/cancel', { method: 'POST' });
      await runner.sleep(3000); // Give more time for cleanup
    } catch (e) {
      // Ignore error if no sync to cancel
    }
  }

  // Start sync
  const startRes = await runner.makeRequest('/api/sync/start', { method: 'POST' });
  await runner.assert(startRes.statusCode === 200, `Expected 200, got ${startRes.statusCode}`);
  await runner.assert(startRes.json.status === 'ok', 'Should start sync successfully');

  // Check status shows syncing
  await runner.sleep(1000);
  statusRes = await runner.makeRequest('/api/status');
  await runner.assert(statusRes.json.status === 'syncing', 'Should show syncing status');
  await runner.assert(statusRes.json.current_sync !== null, 'Current sync should not be null');

  const sync = statusRes.json.current_sync;
  await runner.assert('files_synced' in sync, 'Files synced missing');
  await runner.assert('files_total' in sync, 'Files total missing');
  await runner.assert('bytes_synced' in sync, 'Bytes synced missing');

  // Test progress updates
  const progress1 = sync.files_synced;
  await runner.sleep(2000);

  const statusRes2 = await runner.makeRequest('/api/status');
  if (statusRes2.json.current_sync) {
    const progress2 = statusRes2.json.current_sync.files_synced;
    await runner.assert(progress2 >= progress1, 'Progress should increase over time');
  }

  // Cancel sync
  const cancelRes = await runner.makeRequest('/api/sync/cancel', { method: 'POST' });
  await runner.assert(cancelRes.statusCode === 200, `Expected 200, got ${cancelRes.statusCode}`);
  await runner.assert(cancelRes.json.status === 'ok', 'Should cancel sync successfully');

  // Verify return to idle (give more time for cleanup)
  await runner.sleep(2000);
  const finalStatusRes = await runner.makeRequest('/api/status');
  await runner.assert(finalStatusRes.json.status === 'idle', 'Should return to idle after cancel');
  await runner.assert(finalStatusRes.json.current_sync == null, 'Current sync should be null/undefined after cancel (omitempty)');
});

// ============================================================================
// FILE GALLERY TESTS
// ============================================================================

runner.test('🖼️ Gallery file management', async () => {
  // Test root directory
  const rootRes = await runner.makeRequest('/api/files');
  await runner.assert(rootRes.statusCode === 200, `Expected 200, got ${rootRes.statusCode}`);
  await runner.assert('files' in rootRes.json, 'Files field missing');
  await runner.assert(Array.isArray(rootRes.json.files), 'Files should be array');

  // Test subdirectory
  const subRes = await runner.makeRequest('/api/files?path=card-test');
  await runner.assert(subRes.statusCode === 200, `Expected 200, got ${subRes.statusCode}`);
  await runner.assert('files' in subRes.json, 'Subdirectory files missing');
});

// ============================================================================
// ERROR HANDLING AND EDGE CASES
// ============================================================================

runner.test('🚫 Error handling and validation', async () => {
  // Invalid JSON
  const invalidJsonRes = await runner.makeRequest('/api/wifi/connect', {
    method: 'POST',
    body: 'invalid json{{'
  });
  await runner.assert(invalidJsonRes.statusCode === 400, `Expected 400, got ${invalidJsonRes.statusCode}`);

  // Unsupported methods
  const methodRes = await runner.makeRequest('/api/status', { method: 'DELETE' });
  await runner.assert(methodRes.statusCode === 405, `Expected 405, got ${methodRes.statusCode}`);

  // Double sync start
  await runner.makeRequest('/api/sync/start', { method: 'POST' });
  const doubleRes = await runner.makeRequest('/api/sync/start', { method: 'POST' });
  await runner.assert(doubleRes.statusCode === 409, `Expected 409, got ${doubleRes.statusCode}`);

  // Cleanup - make sure we fully cancel
  await runner.makeRequest('/api/sync/cancel', { method: 'POST' });
  await runner.sleep(3000); // Give more time for cleanup to ensure next test starts clean
});

// ============================================================================
// DEVICE MANAGEMENT TESTS
// ============================================================================

runner.test('💾 Device management', async () => {
  const devicesRes = await runner.makeRequest('/api/devices');
  await runner.assert(devicesRes.statusCode === 200, `Expected 200, got ${devicesRes.statusCode}`);
  await runner.assert(Array.isArray(devicesRes.json), 'Devices should be array');
  await runner.assert(devicesRes.json.length > 0, 'Should have mock devices');

  const device = devicesRes.json[0];
  await runner.assert('device_path' in device, 'Device missing path');
  await runner.assert('device_name' in device, 'Device missing name');
  await runner.assert('size' in device, 'Device missing size');
});

// Run all tests
runner.run().catch(console.error);