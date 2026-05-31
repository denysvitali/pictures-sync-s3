function normalizeBaseUrl(raw) {
  return String(raw || '').trim().replace(/\/$/, '')
}

function normalizeApiPath(path) {
  const normalized = String(path || '')
  if (!normalized.startsWith('/')) {
    return `/${normalized}`
  }
  return normalized
}

function buildUrl(baseUrl, path, query = null) {
  const base = normalizeBaseUrl(baseUrl)
  if (!base) {
    throw new Error('Device URL is not configured. Set a device URL first.')
  }

  const apiPath = normalizeApiPath(path)
  const url = new URL(`${base}${apiPath}`)

  if (query && typeof query === 'object') {
    Object.entries(query).forEach(([key, value]) => {
      if (value === undefined || value === null || value === '') return
      if (Array.isArray(value)) {
        value.forEach((entry) => {
          if (entry !== undefined && entry !== null) {
            url.searchParams.append(key, String(entry))
          }
        })
        return
      }
      url.searchParams.set(key, String(value))
    })
  }

  return url.toString()
}

async function parseResponse(response) {
  const contentType = response.headers.get('content-type') || ''
  const raw = await response.text()

  if (!response.ok) {
    if (contentType.includes('application/json')) {
      let error = null
      try {
        error = JSON.parse(raw)
      } catch {
        // Fall back to the raw response body below.
      }
      throw new Error(error?.error || error?.message || raw || `Request failed (${response.status})`)
    }
    throw new Error(raw || `Request failed (${response.status})`)
  }

  if (!contentType.includes('application/json')) return raw

  try {
    return JSON.parse(raw)
  } catch {
    return raw
  }
}

async function parseTextResponse(response) {
  const raw = await response.text()
  const contentType = response.headers.get('content-type') || ''
  if (!response.ok) {
    if (contentType.includes('application/json')) {
      let error = null
      try {
        error = JSON.parse(raw)
      } catch {
        // Fall back to the raw response body below.
      }
      throw new Error(error?.error || error?.message || raw || `Request failed (${response.status})`)
    }
    throw new Error(raw || `Request failed (${response.status})`)
  }

  return {
    content: raw,
    contentType,
  }
}

function bodyPayload(payload) {
  if (payload === null || payload === undefined) return undefined
  if (typeof payload === 'string') return payload
  return JSON.stringify(payload)
}

export async function apiRequest(path, options = {}) {
  const { deviceUrl, query, method = 'GET', headers = {}, body, timeoutMs = 0, ...fetchOptions } = options

  const resolvedBody = bodyPayload(body)
  const requestHeaders = {
    ...(resolvedBody !== undefined && !(resolvedBody instanceof FormData)
      ? { 'Content-Type': 'application/json' }
      : {}),
    ...headers
  }

  let timeoutId = null
  let controller = null
  let signal = fetchOptions.signal

  if (timeoutMs > 0) {
    controller = new AbortController()
    signal = controller.signal
    timeoutId = window.setTimeout(() => controller.abort(), timeoutMs)
  }

  try {
    const response = await fetch(buildUrl(deviceUrl, path, query), {
      method,
      credentials: 'include',
      ...fetchOptions,
      signal,
      headers: requestHeaders,
      body: resolvedBody
    })

    return parseResponse(response)
  } catch (err) {
    if (err?.name === 'AbortError' && timeoutMs > 0) {
      throw new Error('Request timed out')
    }
    throw err
  } finally {
    if (timeoutId !== null) {
      window.clearTimeout(timeoutId)
    }
  }
}

export const getStatus = (d) => apiRequest('/api/status', { deviceUrl: d })
export const getWSToken = (d) => apiRequest('/api/ws-token', { deviceUrl: d })
export const getVersion = (d) => apiRequest('/api/version', { deviceUrl: d })
export const getHistory = (d) => apiRequest('/api/history', { deviceUrl: d })
export const getWifiStatus = (d) => apiRequest('/api/wifi/status', { deviceUrl: d })
export const getWifiNetworks = (d) => apiRequest('/api/wifi/networks', { deviceUrl: d })
export const startSync = (d) => apiRequest('/api/sync/start', { deviceUrl: d, method: 'POST' })
export const cancelSync = (d) => apiRequest('/api/sync/cancel', { deviceUrl: d, method: 'POST' })
export const scanWifi = (d, sortBy) =>
  apiRequest('/api/wifi/scan', { deviceUrl: d, method: 'POST', body: { sort_by: sortBy } })
export const connectWifi = (d, ssid, password = '') =>
  apiRequest('/api/wifi/connect', { deviceUrl: d, method: 'POST', body: { ssid, password } })
export const disconnectWifi = (d, ssid) =>
  apiRequest('/api/wifi/disconnect', { deviceUrl: d, method: 'POST', body: { ssid } })
export const reorderWifi = (d, ssids) =>
  apiRequest('/api/wifi/reorder', { deviceUrl: d, method: 'POST', body: { ssids } })
export const getFiles = (d, path = '') =>
  apiRequest('/api/files', { deviceUrl: d, query: { path } })
export const getFilesPaginated = (d, { path = '', page = 1, pageSize = 50 }) =>
  apiRequest('/api/files/paginated', { deviceUrl: d, query: { path, page, page_size: pageSize } })
export const getSDCardFiles = (d, path = '') =>
  apiRequest('/api/sdcard/files', { deviceUrl: d, query: { path } })
export const getSDCardPreviewUrl = (d, filePath) =>
  `${normalizeBaseUrl(d)}/api/sdcard/preview?path=${encodeURIComponent(filePath || '')}`
export const getSDCardFileUrl = (d, filePath, { download = false } = {}) =>
  `${normalizeBaseUrl(d)}/api/sdcard/file?path=${encodeURIComponent(filePath || '')}${download ? '&download=1' : ''}`
export async function getSDCardFileContent(d, filePath) {
  const response = await fetch(buildUrl(d, '/api/sdcard/file', { path: filePath }), {
    credentials: 'include',
  })
  return parseTextResponse(response)
}
export const getConfig = (d) => apiRequest('/api/config', { deviceUrl: d })
export const saveConfig = (d, configText) =>
  apiRequest('/api/config', {
    deviceUrl: d,
    method: 'POST',
    headers: { 'Content-Type': 'text/plain' },
    body: configText
  })
export const getBreakglassAuthorizedKeys = (d) =>
  apiRequest('/api/breakglass/authorized-keys', { deviceUrl: d })
export const saveBreakglassAuthorizedKeys = (d, authorizedKeys) =>
  apiRequest('/api/breakglass/authorized-keys', {
    deviceUrl: d, method: 'POST', body: { authorized_keys: authorizedKeys }
  })
export const getSettings = (d) => apiRequest('/api/settings', { deviceUrl: d })
export const saveSettings = (d, payload) =>
  apiRequest('/api/settings', { deviceUrl: d, method: 'POST', body: payload })
export const changeGokrazyPassword = (d, currentPassword, newPassword) =>
  apiRequest('/api/auth/password', {
    deviceUrl: d, method: 'POST',
    body: { current_password: currentPassword, new_password: newPassword }
  })
export const testConfig = (d) =>
  apiRequest('/api/config/test', { deviceUrl: d, method: 'POST' })
export const getB2Regions = (d) => apiRequest('/api/config/b2/regions', { deviceUrl: d })
export const saveB2Config = (d, payload) =>
  apiRequest('/api/config/b2', { deviceUrl: d, method: 'POST', body: payload })
export const getOtaStatus = (d) => apiRequest('/api/ota/status', { deviceUrl: d })
export const installOta = (d, releaseTag = null) =>
  apiRequest('/api/ota/install', { deviceUrl: d, method: 'POST', body: releaseTag ? { release_tag: releaseTag } : undefined })
export const getSystemTime = (d) => apiRequest('/api/system/time', { deviceUrl: d })
export const syncSystemTime = (d, clientTime) =>
  apiRequest('/api/system/time', { deviceUrl: d, method: 'POST', body: { client_time: clientTime } })
export const generateTLSCertificate = (d, hosts = []) =>
  apiRequest('/api/system/tls-certificate', { deviceUrl: d, method: 'POST', body: { hosts } })
export const restartAppServices = (d, services = ['pictures-sync', 'webui']) =>
  apiRequest('/api/system/services/restart', { deviceUrl: d, method: 'POST', body: { services } })
export const getSystemPanic = (d) => apiRequest('/api/system/panic', { deviceUrl: d })
export const clearSystemPanic = (d) =>
  apiRequest('/api/system/panic', { deviceUrl: d, method: 'DELETE' })
export const getSystemStats = (d, opts = {}) => {
  if (typeof opts === 'number') {
    return apiRequest('/api/system/stats', { deviceUrl: d, query: { hours: opts } })
  }
  const { since, until, resolution, hours } = opts
  const query = {}
  if (since !== undefined) query.since = since
  if (until !== undefined) query.until = until
  if (resolution !== undefined && resolution !== null) query.resolution = resolution
  if (hours !== undefined && since === undefined) query.hours = hours
  return apiRequest('/api/system/stats', { deviceUrl: d, query })
}
export const getDevices = (d) => apiRequest('/api/devices', { deviceUrl: d })
export const selectDevice = (d, devicePath) =>
  apiRequest('/api/devices/select', { deviceUrl: d, method: 'POST', body: { device_path: devicePath } })
export const formatSDCard = (d, devicePath, confirmation, label = '') =>
  apiRequest('/api/devices/format', {
    deviceUrl: d,
    method: 'POST',
    body: { device_path: devicePath, confirmation, label }
  })
export const redetectSDCard = (d) =>
  apiRequest('/api/devices/redetect', { deviceUrl: d, method: 'POST' })
export const getFilePublicLink = (d, filePath) =>
  apiRequest('/api/files/link', { deviceUrl: d, query: { path: filePath } })
export const getFileViewUrl = (d, filePath) =>
  `${normalizeBaseUrl(d)}/api/files/view?path=${encodeURIComponent(filePath || '')}`
export async function getFileViewContent(d, filePath) {
  const response = await fetch(buildUrl(d, '/api/files/view', { path: filePath }), {
    credentials: 'include',
  })
  return parseTextResponse(response)
}
export const getThumbnailUrl = (d, filePath) =>
  `${normalizeBaseUrl(d)}/api/thumbnail?path=${encodeURIComponent(filePath || '')}`

export const getGooglePhotosStatus = (d) => apiRequest('/api/googlephotos/status', { deviceUrl: d, timeoutMs: 10000 })
export const startGooglePhotosAuth = (d, redirectUri) =>
  apiRequest('/api/googlephotos/auth/start', { deviceUrl: d, method: 'POST', body: { redirect_uri: redirectUri } })
export const disconnectGooglePhotos = (d) =>
  apiRequest('/api/googlephotos/auth/disconnect', { deviceUrl: d, method: 'POST' })
export const startGooglePhotosSync = (d, force = false) =>
  apiRequest(`/api/googlephotos/sync${force ? '?force=true' : ''}`, {
    deviceUrl: d,
    method: 'POST',
  })
export const cancelGooglePhotosSync = (d) =>
  apiRequest('/api/googlephotos/sync/cancel', { deviceUrl: d, method: 'POST' })
export const getGooglePhotosSyncProgress = (d) =>
  apiRequest('/api/googlephotos/sync/progress', { deviceUrl: d })
export const getGooglePhotosAlbums = (d) =>
  apiRequest('/api/googlephotos/albums', { deviceUrl: d })
export const clearGooglePhotosAlbum = (d, albumId) =>
  apiRequest(`/api/googlephotos/albums/${albumId}`, { deviceUrl: d, method: 'DELETE' })
export const getGooglePhotosAlbumClearProgress = (d, albumId) =>
  apiRequest(`/api/googlephotos/albums/${albumId}/clear/progress`, { deviceUrl: d })
export const sortGooglePhotosAlbum = (d, albumId) =>
  apiRequest(`/api/googlephotos/albums/${albumId}/sort`, { deviceUrl: d, method: 'POST' })
export const getGooglePhotosAlbumSortProgress = (d, albumId) =>
  apiRequest(`/api/googlephotos/albums/${albumId}/sort/progress`, { deviceUrl: d })

export function getWebSocketUrl(deviceUrl) {
  const base = normalizeBaseUrl(deviceUrl)
  if (!base) {
    throw new Error('Device URL is not configured. Set a device URL first.')
  }
  const url = new URL(base)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  url.pathname = '/ws'
  url.search = ''
  url.hash = ''
  return url.toString()
}
