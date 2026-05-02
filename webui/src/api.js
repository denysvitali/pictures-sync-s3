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
      if (value === undefined || value === null || value === '') {
        return
      }
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
      try {
        const error = JSON.parse(raw)
        throw new Error(
          error?.error ||
            error?.message ||
            `Request failed (${response.status})`
        )
      } catch (parseError) {
        if (parseError instanceof Error && parseError.message.includes('Request failed')) {
          throw parseError
        }
        throw new Error(raw || `Request failed (${response.status})`)
      }
    }
    throw new Error(raw || `Request failed (${response.status})`)
  }

  if (!contentType.includes('application/json')) {
    return raw
  }

  try {
    return JSON.parse(raw)
  } catch (err) {
    return raw
  }
}

function bodyPayload(payload) {
  if (payload === null || payload === undefined) {
    return undefined
  }
  if (typeof payload === 'string') {
    return payload
  }
  return JSON.stringify(payload)
}

export async function apiRequest(path, options = {}) {
  const {
    deviceUrl,
    query,
    method = 'GET',
    headers = {},
    body,
    ...fetchOptions
  } = options

  const resolvedBody = bodyPayload(body)
  const requestHeaders = {
    ...(resolvedBody !== undefined && !(resolvedBody instanceof FormData) ? { 'Content-Type': 'application/json' } : {}),
    ...headers
  }

  const response = await fetch(buildUrl(deviceUrl, path, query), {
    method,
    credentials: 'include',
    ...fetchOptions,
    headers: requestHeaders,
    body: resolvedBody
  })

  return parseResponse(response)
}

export const getStatus = (deviceUrl) => apiRequest('/api/status', { deviceUrl })
export const getHistory = (deviceUrl) => apiRequest('/api/history', { deviceUrl })
export const getWifiStatus = (deviceUrl) => apiRequest('/api/wifi/status', { deviceUrl })
export const getWifiNetworks = (deviceUrl) => apiRequest('/api/wifi/networks', { deviceUrl })
export const startSync = (deviceUrl) => apiRequest('/api/sync/start', { deviceUrl, method: 'POST' })
export const cancelSync = (deviceUrl) => apiRequest('/api/sync/cancel', { deviceUrl, method: 'POST' })
export const scanWifi = (deviceUrl, sortBy) =>
  apiRequest('/api/wifi/scan', {
    deviceUrl,
    method: 'POST',
    body: { sort_by: sortBy }
  })
export const connectWifi = (deviceUrl, ssid, password = '') =>
  apiRequest('/api/wifi/connect', {
    deviceUrl,
    method: 'POST',
    body: { ssid, password }
  })
export const disconnectWifi = (deviceUrl, ssid) =>
  apiRequest('/api/wifi/disconnect', {
    deviceUrl,
    method: 'POST',
    body: { ssid }
  })
export const reorderWifi = (deviceUrl, ssids) =>
  apiRequest('/api/wifi/reorder', {
    deviceUrl,
    method: 'POST',
    body: { ssids }
  })
export const getFiles = (deviceUrl, path = '') =>
  apiRequest('/api/files', { deviceUrl, query: { path } })
export const getFilesPaginated = (deviceUrl, { path = '', page = 1, pageSize = 50 }) =>
  apiRequest('/api/files/paginated', {
    deviceUrl,
    query: {
      path,
      page,
      page_size: pageSize
    }
  })
export const getConfig = (deviceUrl) => apiRequest('/api/config', { deviceUrl })
export const getSettings = (deviceUrl) => apiRequest('/api/settings', { deviceUrl })
export const saveSettings = (deviceUrl, payload) =>
  apiRequest('/api/settings', {
    deviceUrl,
    method: 'POST',
    body: payload
  })
export const testConfig = (deviceUrl) =>
  apiRequest('/api/config/test', {
    deviceUrl,
    method: 'POST'
  })

export const getFileViewUrl = (deviceUrl, filePath) =>
  `${normalizeBaseUrl(deviceUrl)}/api/files/view?path=${encodeURIComponent(filePath || '')}`
