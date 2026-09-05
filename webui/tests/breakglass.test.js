import assert from 'node:assert/strict'
import { test } from 'node:test'
import { getBreakglassAuthorizedKeys, saveBreakglassAuthorizedKeys } from '../src/api.js'

const deviceUrl = 'http://device.example:8080'

for (const keys of ['ssh-ed25519 AAAA... user@example', '\n# note\nssh-ed25519 AAAA...\nssh-rsa AAAA...\n', '']) {
  test(`SSH keys are sent as text (${keys ? 'nonempty' : 'clear'})`, async (t) => {
    t.mock.method(globalThis, 'fetch', async (url, options) => {
      assert.equal(url, `${deviceUrl}/api/breakglass/authorized-keys`)
      assert.equal(options.method, 'POST')
      assert.equal(options.credentials, 'include')
      assert.equal(options.headers['Content-Type'], 'application/json')
      assert.deepEqual(JSON.parse(options.body), { authorized_keys: keys })
      return Response.json({ status: 'ok' })
    })
    assert.deepEqual(await saveBreakglassAuthorizedKeys(deviceUrl, keys), { status: 'ok' })
  })
}

test('loading an empty key file returns empty text', async (t) => {
  t.mock.method(globalThis, 'fetch', async () => Response.json({ authorized_keys: '', count: 0 }))
  assert.equal((await getBreakglassAuthorizedKeys(deviceUrl)).authorized_keys, '')
})

test('SSH validation errors retain the server line number', async (t) => {
  t.mock.method(globalThis, 'fetch', async () => Response.json({
    status: 'error', error: 'line 3 is not a valid SSH authorized key',
  }, { status: 400 }))
  await assert.rejects(saveBreakglassAuthorizedKeys(deviceUrl, '\n# note\ninvalid'), {
    message: 'line 3 is not a valid SSH authorized key',
  })
})
