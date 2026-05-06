import { useState, useEffect, useCallback, useMemo } from 'react'
import { useDevice } from '../DeviceContext.jsx'
import { useToast } from '../components/Toast.jsx'
import { Card, CardHeader, CardTitle } from '../components/Card.jsx'
import { StatusBadge } from '../components/StatusBadge.jsx'
import { Button } from '../components/Button.jsx'
import { Icon } from '../components/Icons.jsx'
import { LoadingSpinner } from '../components/LoadingSpinner.jsx'
import {
  getConfig,
  saveConfig,
  testConfig,
  getB2Regions,
  saveB2Config,
  getSettings,
  saveSettings,
  changeGokrazyPassword,
  getBreakglassAuthorizedKeys,
  saveBreakglassAuthorizedKeys,
  getOtaStatus,
  installOta,
  getSystemTime,
  syncSystemTime,
  generateTLSCertificate,
} from '../api.js'

function describeError(err) {
  if (!err) return 'Unknown error'
  const msg = err.message || String(err)
  if (msg.includes('Failed to fetch') || msg.includes('NetworkError') || msg.includes('ERR_NETWORK')) {
    return 'Device unreachable — is it powered on and connected to the network?'
  }
  if (msg.includes('ERR_CONNECTION_REFUSED')) {
    return 'Connection refused — the web server may not be running'
  }
  if (msg.includes('timeout')) {
    return 'Request timed out — the device may be unreachable'
  }
  return msg
}

function formatReleaseDate(value) {
  const timestamp = Date.parse(value)
  if (Number.isNaN(timestamp)) return 'Unknown date'
  return new Date(timestamp).toLocaleString()
}

function formatReleaseSize(size) {
  if (!size || size < 0) return '--'
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  if (size < 1024 * 1024 * 1024) return `${(size / (1024 * 1024)).toFixed(1)} MB`
  return `${(size / (1024 * 1024 * 1024)).toFixed(1)} GB`
}

function formatBytes(size) {
  if (!size || size < 0) return '--'
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  if (size < 1024 * 1024 * 1024) return `${(size / (1024 * 1024)).toFixed(1)} MB`
  return `${(size / (1024 * 1024 * 1024)).toFixed(1)} GB`
}

function formatDate(value) {
  const timestamp = Date.parse(value)
  if (Number.isNaN(timestamp)) return 'Unknown time'
  return new Date(timestamp).toLocaleString()
}

function formatPartition(partition) {
  if (!partition) return '--'
  return `root${partition}`
}

function getDeviceHost(deviceUrl) {
  try {
    return new URL(deviceUrl).hostname
  } catch {
    return ''
  }
}

// ---------------------------------------------------------------------------
// Accordion wrapper
// ---------------------------------------------------------------------------

function AccordionSection({ title, icon, defaultOpen = false, badge, children }) {
  const [open, setOpen] = useState(defaultOpen)

  return (
    <Card className="mb-4 overflow-hidden">
      <button
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center justify-between p-4 text-left select-none -m-4 mb-0 hover:bg-surface-700/30 transition-colors"
      >
        <div className="flex items-center gap-3">
          {icon && <Icon name={icon} className="w-5 h-5 text-brand-400 shrink-0" />}
          <span className="text-sm font-semibold text-surface-100">{title}</span>
          {badge}
        </div>
        <Icon
          name="chevron-right"
          className={`w-4 h-4 text-surface-400 transition-transform duration-200 ${open ? 'rotate-90' : ''}`}
        />
      </button>
      <div
        className={`grid transition-all duration-200 ease-in-out ${
          open ? 'grid-rows-[1fr] opacity-100 mt-4' : 'grid-rows-[0fr] opacity-0 mt-0'
        }`}
      >
        <div className="overflow-hidden">{children}</div>
      </div>
    </Card>
  )
}

// ---------------------------------------------------------------------------
// Reusable form input
// ---------------------------------------------------------------------------

function Field({ label, children, hint }) {
  return (
    <div className="mb-3">
      <label className="block text-xs font-medium text-surface-300 mb-1.5">{label}</label>
      {children}
      {hint && <p className="mt-1 text-xs text-surface-500">{hint}</p>}
    </div>
  )
}

const inputClass =
  'w-full bg-surface-900 border border-surface-600 rounded-lg px-3 py-2 text-sm text-surface-50 placeholder-surface-500 focus:outline-none focus:ring-2 focus:ring-brand-500 transition-colors'

function TextInput({ value, onChange, placeholder, type = 'text', disabled }) {
  return (
    <input
      type={type}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      disabled={disabled}
      className={inputClass}
    />
  )
}

function PasswordInput({ value, onChange, placeholder, disabled }) {
  const [visible, setVisible] = useState(false)
  return (
    <div className="relative">
      <input
        type={visible ? 'text' : 'password'}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        disabled={disabled}
        className={`${inputClass} pr-10`}
      />
      <button
        type="button"
        onClick={() => setVisible((v) => !v)}
        className="absolute right-2 top-1/2 -translate-y-1/2 text-surface-400 hover:text-surface-200 p-1"
        tabIndex={-1}
      >
        <Icon name={visible ? 'x' : 'lock'} className="w-4 h-4" />
      </button>
    </div>
  )
}

function SelectInput({ value, onChange, options, placeholder, disabled }) {
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      disabled={disabled}
      className={inputClass}
    >
      {placeholder && (
        <option value="" disabled>
          {placeholder}
        </option>
      )}
      {options.map((opt) => (
        <option key={typeof opt === 'string' ? opt : opt.value} value={typeof opt === 'string' ? opt : opt.value}>
          {typeof opt === 'string' ? opt : opt.label}
        </option>
      ))}
    </select>
  )
}

function Toggle({ checked, onChange, label, disabled }) {
  return (
    <label className={`flex items-center justify-between gap-3 cursor-pointer ${disabled ? 'opacity-50 cursor-not-allowed' : ''}`}>
      <span className="text-sm text-surface-300">{label}</span>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        disabled={disabled}
        onClick={() => !disabled && onChange(!checked)}
        className={`relative inline-flex h-6 w-11 shrink-0 rounded-full border-2 border-transparent transition-colors duration-200 ${
          checked ? 'bg-brand-600' : 'bg-surface-600'
        }`}
      >
        <span
          className={`pointer-events-none inline-block h-5 w-5 rounded-full bg-white shadow-sm transform transition-transform duration-200 ${
            checked ? 'translate-x-5' : 'translate-x-0'
          }`}
        />
      </button>
    </label>
  )
}

// ---------------------------------------------------------------------------
// Section 1 - Remote Storage Config
// ---------------------------------------------------------------------------

function RemoteStorageSection() {
  const { deviceUrl } = useDevice()
  const toast = useToast()
  const [config, setConfig] = useState(null)
  const [editing, setEditing] = useState(false)
  const [draftConfig, setDraftConfig] = useState('')
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [loading, setLoading] = useState(true)

  const loadConfig = useCallback((cancelledRef = { current: false }) => {
    setLoading(true)
    getConfig(deviceUrl)
      .then((data) => { if (!cancelledRef.current) setConfig(data) })
      .catch(() => {})
      .finally(() => { if (!cancelledRef.current) setLoading(false) })
  }, [deviceUrl])

  useEffect(() => {
    const cancelledRef = { current: false }
    loadConfig(cancelledRef)
    const onConfigChanged = () => loadConfig(cancelledRef)
    window.addEventListener('rclone-config-changed', onConfigChanged)
    return () => {
      cancelledRef.current = true
      window.removeEventListener('rclone-config-changed', onConfigChanged)
    }
  }, [loadConfig])

  const handleTest = useCallback(async () => {
    setTesting(true)
    try {
      const res = await testConfig(deviceUrl)
      if (res?.success) {
        toast.success(res.message || 'Connection test passed')
      } else {
        toast.error(res?.error || res?.message || 'Connection test failed')
      }
    } catch (err) {
      toast.error(describeError(err) || 'Connection test failed')
    } finally {
      setTesting(false)
    }
  }, [deviceUrl, toast])

  const handleSaveConfig = useCallback(async () => {
    if (!draftConfig.trim()) {
      toast.error('rclone.conf content is required')
      return
    }
    if (config?.configured && !window.confirm('Replace the existing remote storage configuration?')) {
      return
    }

    setSaving(true)
    try {
      await saveConfig(deviceUrl, draftConfig)
      toast.success('Remote config saved')
      setEditing(false)
      setDraftConfig('')
      window.dispatchEvent(new Event('rclone-config-changed'))
    } catch (err) {
      toast.error(describeError(err) || 'Failed to save remote config')
    } finally {
      setSaving(false)
    }
  }, [deviceUrl, draftConfig, config?.configured, toast])

  return (
    <AccordionSection title="Remote Storage" icon="cloud" defaultOpen badge={
      loading ? null : (
        <StatusBadge variant={config?.configured ? 'success' : 'warning'}>
          {config?.configured ? 'Configured' : 'Not configured'}
        </StatusBadge>
      )
    }>
      {loading ? (
        <div className="flex items-center justify-center py-6">
          <LoadingSpinner size="sm" />
        </div>
      ) : (
        <div className="space-y-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="min-w-0">
              <p className="text-sm text-surface-200">
                {config?.configured ? 'Remote backend is configured' : 'No remote backend configured'}
              </p>
              {config?.configured && (config?.remote_name || config?.remotes?.length > 0) && (
                <p className="text-xs text-surface-400 mt-1">
                  Remote: {config.remote_name || config.remotes[0]}
                  {config?.provider ? ` (${config.provider})` : ''}
                </p>
              )}
              {config?.configured && config?.remote_path && (
                <p className="text-xs text-surface-400 mt-1">Path: {config.remote_path}</p>
              )}
            </div>
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
              <Button
                variant="secondary"
                size="sm"
                onClick={() => setEditing((v) => !v)}
                disabled={saving}
              >
                <Icon name="pencil" className="w-4 h-4" />
                {editing ? 'Cancel' : 'Replace Config'}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                loading={testing}
                onClick={handleTest}
                disabled={!config?.configured || saving}
              >
                <Icon name="arrow-path" className="w-4 h-4" />
                Test Connection
              </Button>
            </div>
          </div>
          {config?.config_redacted && (
            <pre className="max-h-56 overflow-auto rounded-lg border border-surface-700 bg-surface-900 p-3 text-xs text-surface-200 whitespace-pre-wrap break-words">
              {config.config_redacted}
            </pre>
          )}
          {editing && (
            <div className="rounded-lg border border-surface-700 bg-surface-900/60 p-3">
              <Field label="rclone.conf" hint="Paste the complete config. Redacted secrets cannot be saved.">
                <textarea
                  value={draftConfig}
                  onChange={(e) => setDraftConfig(e.target.value)}
                  placeholder={'[b2]\ntype = b2\naccount = <application_key_id>\nkey = <application_key>'}
                  disabled={saving}
                  rows={8}
                  className={`${inputClass} resize-y font-mono text-xs`}
                />
              </Field>
              <div className="flex justify-end pt-1">
                <Button variant="primary" size="sm" loading={saving} onClick={handleSaveConfig}>
                  Save Config
                </Button>
              </div>
            </div>
          )}
        </div>
      )}
    </AccordionSection>
  )
}

// ---------------------------------------------------------------------------
// Section 2 - Backblaze B2 Quick Setup
// ---------------------------------------------------------------------------

function B2SetupSection() {
  const { deviceUrl } = useDevice()
  const toast = useToast()

  const [bucket, setBucket] = useState('')
  const [keyId, setKeyId] = useState('')
  const [appKey, setAppKey] = useState('')
  const [region, setRegion] = useState('')
  const [regions, setRegions] = useState([])
  const [saving, setSaving] = useState(false)
  const [loadingRegions, setLoadingRegions] = useState(true)

  useEffect(() => {
    let cancelled = false
    getB2Regions(deviceUrl)
      .then((data) => { if (!cancelled) setRegions(Array.isArray(data) ? data : []) })
      .catch(() => {})
      .finally(() => { if (!cancelled) setLoadingRegions(false) })
    return () => { cancelled = true }
  }, [deviceUrl])

  const handleSave = useCallback(async () => {
    if (!bucket.trim()) { toast.error('Bucket name is required'); return }
    if (!keyId.trim()) { toast.error('Key ID is required'); return }
    if (!appKey.trim()) { toast.error('Application key is required'); return }
    if (!region) { toast.error('Region is required'); return }

    const bucketName = bucket.trim()
    setSaving(true)
    try {
      const res = await saveB2Config(deviceUrl, {
        bucket_name: bucketName,
        account_id: keyId.trim(),
        application_key: appKey.trim(),
        remote_name: 'b2',
        remote_path: `${bucketName}/photos`,
        region,
      })
      if (res?.success === false) {
        throw new Error(res.error || 'Failed to save B2 configuration')
      }
      if (res?.warning) {
        toast.warning(res.warning)
      } else {
        toast.success('Backblaze B2 configuration saved')
      }
      setAppKey('')
      window.dispatchEvent(new Event('rclone-config-changed'))
    } catch (err) {
      toast.error(describeError(err) || 'Failed to save B2 configuration')
    } finally {
      setSaving(false)
    }
  }, [deviceUrl, bucket, keyId, appKey, region, regions, toast])

  return (
    <AccordionSection title="Backblaze B2 Quick Setup" icon="cloud">
      <div className="space-y-1">
        <Field label="Bucket Name" hint="Name of your Backblaze B2 bucket">
          <TextInput
            value={bucket}
            onChange={setBucket}
            placeholder="my-photo-backup"
            disabled={saving}
          />
        </Field>
        <Field label="Key ID" hint="Application Key ID from Backblaze console">
          <TextInput
            value={keyId}
            onChange={setKeyId}
            placeholder="001a1b2c3d4e5f6a7b8c9d0e1f"
            disabled={saving}
          />
        </Field>
        <Field label="Application Key" hint="Keep this secret -- will not be shown after saving">
          <PasswordInput
            value={appKey}
            onChange={setAppKey}
            placeholder="K001..."
            disabled={saving}
          />
        </Field>
        <Field label="Region">
          {loadingRegions ? (
            <div className="flex items-center gap-2 py-2">
              <LoadingSpinner size="sm" />
              <span className="text-xs text-surface-400">Loading regions...</span>
            </div>
          ) : (
            <SelectInput
              value={region}
              onChange={setRegion}
              placeholder="Select a region"
              options={regions.map((r) =>
                typeof r === 'string' ? r : { value: r.id || r.name || r.value, label: r.name || r.id || r.value }
              )}
              disabled={saving}
            />
          )}
        </Field>
        <div className="flex justify-end pt-2">
          <Button variant="primary" size="sm" loading={saving} onClick={handleSave}>
            Save B2 Config
          </Button>
        </div>
      </div>
    </AccordionSection>
  )
}

// ---------------------------------------------------------------------------
// Section 3 - Sync Settings
// ---------------------------------------------------------------------------

function SyncSettingsSection() {
  const { deviceUrl } = useDevice()
  const toast = useToast()

  const [transfers, setTransfers] = useState('4')
  const [bandwidth, setBandwidth] = useState('')
  const [googlePhotos, setGooglePhotos] = useState(false)
  const [saving, setSaving] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setLoadError(null)
    getSettings(deviceUrl)
      .then((data) => {
        if (cancelled) return
        if (data) {
          setTransfers(String(data.transfers ?? '4'))
          setBandwidth(data.bandwidth || '')
          setGooglePhotos(!!data.google_photos)
        }
      })
      .catch((err) => { if (!cancelled) setLoadError(describeError(err)) })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [deviceUrl])

  const handleSave = useCallback(async () => {
    if (loadError) {
      toast.error('Reload sync settings before saving')
      return
    }
    const t = parseInt(transfers, 10)
    if (isNaN(t) || t < 1 || t > 64) {
      toast.error('Transfers must be between 1 and 64')
      return
    }

    setSaving(true)
    try {
      await saveSettings(deviceUrl, {
        transfers: t,
        bandwidth: bandwidth.trim() || undefined,
        google_photos: googlePhotos,
      })
      toast.success('Sync settings saved')
    } catch (err) {
      toast.error(describeError(err) || 'Failed to save sync settings')
    } finally {
      setSaving(false)
    }
  }, [deviceUrl, transfers, bandwidth, googlePhotos, loadError, toast])

  return (
    <AccordionSection title="Sync Settings" icon="settings">
      {loading ? (
        <div className="flex items-center justify-center py-6">
          <LoadingSpinner size="sm" />
        </div>
      ) : (
        <div className="space-y-1">
          {loadError ? (
            <div className="mb-3 rounded-lg border border-danger/20 bg-danger/10 p-3 text-xs text-danger">
              {loadError}
            </div>
          ) : null}
          <Field label="Parallel Transfers" hint="Number of simultaneous file transfers (1-64)">
            <TextInput
              value={transfers}
              onChange={setTransfers}
              placeholder="4"
              type="number"
              disabled={saving || !!loadError}
            />
          </Field>
          <Field label="Bandwidth Limit" hint="e.g. 10M, 1G, or leave empty for unlimited">
            <TextInput
              value={bandwidth}
              onChange={setBandwidth}
              placeholder="unlimited"
              disabled={saving || !!loadError}
            />
          </Field>
          <div className="py-2">
            <Toggle
              checked={googlePhotos}
              onChange={setGooglePhotos}
              label="Enable Google Photos integration"
              disabled={saving || !!loadError}
            />
          </div>
          <div className="flex justify-end pt-2">
            <Button variant="primary" size="sm" loading={saving} disabled={!!loadError} onClick={handleSave}>
              Save Settings
            </Button>
          </div>
        </div>
      )}
    </AccordionSection>
  )
}

// ---------------------------------------------------------------------------
// Section 4 - OTA Updates
// ---------------------------------------------------------------------------

function OtaSection() {
  const { deviceUrl } = useDevice()
  const toast = useToast()

  const [status, setStatus] = useState(null)
  const [loading, setLoading] = useState(true)
  const [installing, setInstalling] = useState(false)
  const [selectedRelease, setSelectedRelease] = useState('')

  const installationRunning = !!status?.state && ['checking', 'downloading', 'installing'].includes(status.state)
  const installedVersions = useMemo(
    () => (Array.isArray(status?.installed_versions) ? status.installed_versions : []),
    [status?.installed_versions]
  )
  const installHistory = useMemo(
    () => (Array.isArray(status?.install_history) ? status.install_history : []),
    [status?.install_history]
  )
  const activeInfo = status?.ab_partitions?.active_info
  const inactiveInfo = status?.ab_partitions?.inactive_info
  const updateInfo = status?.ab_partitions?.update_info
  const installPartitionLabel = formatPartition(status?.ab_partitions?.update_slot)
  const activePartitionLabel = formatPartition(status?.ab_partitions?.active)
  const inactivePartitionLabel = formatPartition(status?.ab_partitions?.inactive)

  const releases = useMemo(() => {
    if (!Array.isArray(status?.releases)) return []
    return [...status.releases].sort((a, b) => {
      const aTs = Date.parse(a?.published_at)
      const bTs = Date.parse(b?.published_at)
      if (Number.isNaN(aTs) || Number.isNaN(bTs)) return 0
      return bTs - aTs
    })
  }, [status?.releases])

  const selectedReleaseInfo = useMemo(
    () => releases.find((release) => release.tag_name === selectedRelease),
    [releases, selectedRelease]
  )
  const canInstall = !!selectedReleaseInfo && !installationRunning && !installing

  const releaseOptions = useMemo(() => {
    return releases.map((release) => {
      const label = `${release.tag_name} — ${formatReleaseDate(release.published_at)} (${formatReleaseSize(
        release.asset_size
      )})`
      return {
        value: release.tag_name,
        label: release.tag_name === status?.current_version ? `${label} (installed)` : label,
      }
    })
  }, [releases, status?.current_version])

  const fetchStatus = useCallback(async () => {
    setLoading(true)
    try {
      const data = await getOtaStatus(deviceUrl)
      setStatus(data)
    } catch (err) {
      toast.error(describeError(err) || 'Failed to check OTA status')
    } finally {
      setLoading(false)
    }
  }, [deviceUrl, toast])

  useEffect(() => {
    fetchStatus()
  }, [fetchStatus])

  useEffect(() => {
    if (!installationRunning) return
    const timer = window.setTimeout(fetchStatus, 5000)
    return () => window.clearTimeout(timer)
  }, [installationRunning, fetchStatus, status?.state])

  useEffect(() => {
    if (!selectedRelease && releases.length > 0) {
      setSelectedRelease(releases[0].tag_name)
      return
    }
    if (selectedRelease && !releases.some((release) => release.tag_name === selectedRelease)) {
      setSelectedRelease(releases[0]?.tag_name || '')
    }
  }, [selectedRelease, releases])

  const handleInstall = useCallback(async () => {
    if (!selectedRelease) {
      toast.error('Select a release to install')
      return
    }
    if (installationRunning) {
      toast.error('An OTA update is already in progress')
      return
    }

    if (!selectedReleaseInfo) {
      toast.error('Selected release is not available')
      return
    }

    const selectedSize = formatBytes(selectedReleaseInfo?.asset_size || 0)
    const installTarget = status?.ab_partitions?.update_slot
    const targetLabel = formatPartition(installTarget)
    const activeLabel = activePartitionLabel
    const confirmLabel = [
      `Install ${selectedReleaseInfo.name || selectedRelease}?`,
      `Version: ${selectedReleaseInfo.tag_name}`,
      `Active partition now: ${activeLabel}`,
      `Target partition: ${targetLabel}`,
      `Estimated size: ${selectedSize}`,
      'Device will reboot after install.',
    ].join('\n')

    if (!window.confirm(confirmLabel)) {
      return
    }

    setInstalling(true)
    try {
      await installOta(deviceUrl, selectedRelease)
      toast.success('Update installation started')
      fetchStatus()
    } catch (err) {
      toast.error(describeError(err) || 'Failed to start update')
    } finally {
      setInstalling(false)
    }
  }, [
    deviceUrl,
    selectedRelease,
    selectedReleaseInfo,
    installationRunning,
    activePartitionLabel,
    status?.ab_partitions?.update_slot,
    toast,
    fetchStatus,
  ])

  return (
    <AccordionSection title="OTA Updates" icon="arrow-up-tray" badge={
      !loading && status?.update_available ? (
        <StatusBadge variant="info" pulse>Update available</StatusBadge>
      ) : null
    }>
      {loading ? (
        <div className="flex items-center justify-center py-6">
          <LoadingSpinner size="sm" />
        </div>
      ) : (
        <div className="space-y-3">
          {installedVersions.length ? (
            <div className="bg-surface-900 rounded-lg p-3">
              <p className="text-xs text-surface-400 mb-1">Known installed versions</p>
              <div className="flex flex-wrap gap-2">
                {installedVersions.map((versionEntry) => (
                  <StatusBadge
                    key={versionEntry}
                    variant={versionEntry === status?.current_version ? 'success' : 'neutral'}
                  >
                    {versionEntry}
                    {versionEntry === status?.current_version ? ' (active)' : ''}
                  </StatusBadge>
                ))}
              </div>
            </div>
          ) : null}
          {status?.ab_partitions ? (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="bg-surface-900 rounded-lg p-3">
                <p className="text-xs text-surface-400 mb-1">A/B Partitions</p>
                <p className="text-xs text-surface-500">
                  Active {activePartitionLabel}
                  {activeInfo?.path ? ` (${activeInfo.path})` : ''}
                </p>
                <p className="text-xs text-surface-500 mt-1">
                  Inactive {inactivePartitionLabel}
                  {inactiveInfo?.path ? ` (${inactiveInfo.path})` : ''}
                </p>
                <p className="text-xs text-surface-500 mt-1">
                  Next install target {installPartitionLabel}
                  {updateInfo?.path ? ` (${updateInfo.path})` : ''}
                </p>
                {activeInfo?.size_human || inactiveInfo?.size_human ? (
                  <p className="text-xs text-surface-500 mt-1">
                    Size estimate: {activeInfo?.size_human || inactiveInfo?.size_human || 'unknown'}
                  </p>
                ) : null}
                <p className="text-xs text-surface-500 mt-1">
                  Source: {status?.ab_partitions?.source || 'unknown'}
                </p>
              </div>
              <div className="bg-surface-900 rounded-lg p-3">
                <p className="text-xs text-surface-400 mb-1">Update target</p>
                <p className="text-xs text-surface-500">
                  {installPartitionLabel}
                </p>
                {updateInfo?.size_human || updateInfo?.size_bytes ? (
                  <p className="text-xs text-surface-500 mt-1">
                    Estimated size: {updateInfo?.size_human || formatBytes(updateInfo?.size_bytes || 0)}
                  </p>
                ) : null}
                <p className="text-xs text-surface-500 mt-1">
                  Update destination is always the inactive partition.
                </p>
              </div>
            </div>
          ) : null}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="bg-surface-900 rounded-lg p-3">
              <p className="text-xs text-surface-400 mb-1">Current Version</p>
              <p className="text-sm font-medium text-surface-100">
                {status?.current_version || 'unknown'}
              </p>
              {status?.current_commit ? (
                <p className="text-xs text-surface-500 mt-1 break-all">
                  {status.current_commit}
                </p>
              ) : null}
              <p className="text-xs text-surface-500 mt-1">
                {status?.current_build_date || 'Build date unknown'}
              </p>
            </div>
            <div className="bg-surface-900 rounded-lg p-3">
              <p className="text-xs text-surface-400 mb-1">Latest Release</p>
              <p className="text-sm font-medium text-surface-100">
                {status?.latest_version || 'unknown'}
              </p>
              <p className="text-xs text-surface-500 mt-1">
                {status?.releases?.[0] ? formatReleaseDate(status.releases[0].published_at) : 'No release date'}
              </p>
            </div>
          </div>
          <Field label="Select release to install">
            {releaseOptions.length > 0 ? (
              <SelectInput
                value={selectedRelease}
                onChange={setSelectedRelease}
                options={releaseOptions}
                disabled={installing}
              />
            ) : (
              <p className="text-xs text-surface-500">No installable releases found</p>
            )}
          </Field>
          {selectedReleaseInfo ? (
            <p className="text-xs text-surface-500 mt-1">
              {selectedReleaseInfo.name || selectedReleaseInfo.tag_name}
            </p>
          ) : null}
          {selectedReleaseInfo?.published_at ? (
            <p className="text-xs text-surface-500 mt-1">
              Published {formatReleaseDate(selectedReleaseInfo.published_at)}
            </p>
          ) : null}
          <div className="flex justify-between items-center pt-1">
            <Button
              variant="ghost"
              size="sm"
              onClick={fetchStatus}
              disabled={installing}
            >
              <Icon name="arrow-path" className="w-4 h-4" />
              Check for updates
            </Button>
            <Button
              variant="primary"
              size="sm"
              loading={installing}
              disabled={!canInstall || releaseOptions.length === 0}
              title={installationRunning ? 'An OTA update is already running' : ''}
              onClick={handleInstall}
            >
              <Icon name="arrow-up-tray" className="w-4 h-4" />
              Install Selected Version
            </Button>
          </div>
          {installHistory.length ? (
            <div className="bg-surface-900 rounded-lg p-3">
              <p className="text-xs text-surface-400 mb-2">Recent install history</p>
              <div className="space-y-2">
                {installHistory.slice(0, 5).map((entry, idx) => (
                  <div
                    key={`${entry.release || 'unknown'}-${entry.started_at || entry.finished_at || idx}`}
                    className="rounded-md border border-surface-700 bg-surface-950 p-2"
                  >
                    <p className="text-xs text-surface-200">{entry.release || 'unknown release'}</p>
                    <p className="text-xs text-surface-500 mt-1">
                      {entry.state || 'unknown state'}
                      {entry.finished_at || entry.started_at
                        ? ` · ${entry.state === 'installed' ? 'finished' : 'started'} ${formatDate(entry.finished_at || entry.started_at)}`
                        : ''}
                    </p>
                    {entry.error ? <p className="text-xs text-danger mt-1">{entry.error}</p> : null}
                  </div>
                ))}
              </div>
            </div>
          ) : null}
          {status?.releases?.length ? (
            <p className="text-xs text-surface-500">
              {status.releases.length} release{status.releases.length === 1 ? '' : 's'} available
            </p>
          ) : null}
        </div>
      )}
    </AccordionSection>
  )
}

// ---------------------------------------------------------------------------
// Section 5 - System Clock + TLS
// ---------------------------------------------------------------------------

function SystemMaintenanceSection() {
  const { deviceUrl } = useDevice()
  const toast = useToast()

  const [status, setStatus] = useState(null)
  const [loading, setLoading] = useState(true)
  const [syncingTime, setSyncingTime] = useState(false)
  const [generatingCert, setGeneratingCert] = useState(false)

  const cert = status?.tls_certificate || {}
  const certReady = !!cert.exists && !!cert.valid_now && !cert.needs_regeneration
  const timeReady = !!status?.time_reasonable

  const fetchStatus = useCallback(async () => {
    setLoading(true)
    try {
      const data = await getSystemTime(deviceUrl)
      setStatus(data)
    } catch (err) {
      toast.error(describeError(err) || 'Failed to load system time')
    } finally {
      setLoading(false)
    }
  }, [deviceUrl, toast])

  useEffect(() => {
    fetchStatus()
  }, [fetchStatus])

  const handleSyncTime = useCallback(async () => {
    setSyncingTime(true)
    try {
      const data = await syncSystemTime(deviceUrl, new Date().toISOString())
      setStatus(data)
      toast.success('Device time synced')
    } catch (err) {
      toast.error(describeError(err) || 'Failed to sync device time')
    } finally {
      setSyncingTime(false)
    }
  }, [deviceUrl, toast])

  const handleGenerateCert = useCallback(async () => {
    setGeneratingCert(true)
    try {
      const host = getDeviceHost(deviceUrl)
      const data = await generateTLSCertificate(deviceUrl, host ? [host] : [])
      setStatus(data)
      toast.success('TLS certificate generated')
    } catch (err) {
      toast.error(describeError(err) || 'Failed to generate TLS certificate')
    } finally {
      setGeneratingCert(false)
    }
  }, [deviceUrl, toast])

  return (
    <AccordionSection title="System Clock & TLS" icon="clock" badge={
      loading ? null : (
        <StatusBadge variant={timeReady && certReady ? 'success' : 'warning'}>
          {timeReady && certReady ? 'Ready' : 'Needs attention'}
        </StatusBadge>
      )
    }>
      {loading ? (
        <div className="flex items-center justify-center py-6">
          <LoadingSpinner size="sm" />
        </div>
      ) : (
        <div className="space-y-3">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div className="bg-surface-900 rounded-lg p-3">
              <p className="text-xs text-surface-400 mb-1">Device Time</p>
              <p className="text-sm font-medium text-surface-100">
                {formatDate(status?.current_time)}
              </p>
              <div className="mt-2">
                <StatusBadge variant={timeReady ? 'success' : 'warning'}>
                  {timeReady ? 'Valid' : 'Invalid'}
                </StatusBadge>
              </div>
            </div>
            <div className="bg-surface-900 rounded-lg p-3">
              <p className="text-xs text-surface-400 mb-1">Persistent TLS</p>
              <p className="text-sm font-medium text-surface-100">
                {cert.exists ? (cert.valid_now ? 'Certificate valid' : 'Certificate invalid') : 'No certificate'}
              </p>
              <p className="text-xs text-surface-500 mt-1 break-all">
                {cert.cert_file || '/perm/ssl/gokrazy-web.pem'}
              </p>
              {cert.not_after ? (
                <p className="text-xs text-surface-500 mt-1">
                  Expires {formatDate(cert.not_after)}
                </p>
              ) : null}
            </div>
          </div>
          {cert.error ? (
            <p className="text-xs text-danger bg-danger/10 border border-danger/20 rounded-lg p-2">
              {cert.error}
            </p>
          ) : null}
          <div className="flex flex-col sm:flex-row sm:justify-between sm:items-center gap-3 pt-1">
            <Button
              variant="ghost"
              size="sm"
              onClick={fetchStatus}
              disabled={syncingTime || generatingCert}
            >
              <Icon name="arrow-path" className="w-4 h-4" />
              Refresh
            </Button>
            <div className="flex flex-col sm:flex-row sm:items-center gap-2">
              <Button
                variant="secondary"
                size="sm"
                loading={syncingTime}
                disabled={generatingCert}
                onClick={handleSyncTime}
              >
                <Icon name="clock" className="w-4 h-4" />
                Sync Time
              </Button>
              <Button
                variant="primary"
                size="sm"
                loading={generatingCert}
                disabled={syncingTime || !timeReady}
                onClick={handleGenerateCert}
              >
                <Icon name="lock" className="w-4 h-4" />
                Generate Cert
              </Button>
            </div>
          </div>
        </div>
      )}
    </AccordionSection>
  )
}

// ---------------------------------------------------------------------------
// Section 6 - Danger Zone (Password + Breakglass)
// ---------------------------------------------------------------------------

function DangerZonePassword() {
  const { deviceUrl } = useDevice()
  const toast = useToast()

  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [saving, setSaving] = useState(false)

  const handleSave = useCallback(async () => {
    if (!currentPassword) { toast.error('Current password is required'); return }
    if (!newPassword) { toast.error('New password is required'); return }
    if (newPassword.length < 8) { toast.error('New password must be at least 8 characters'); return }
    if (newPassword !== confirmPassword) { toast.error('Passwords do not match'); return }

    setSaving(true)
    try {
      await changeGokrazyPassword(deviceUrl, currentPassword, newPassword)
      toast.success('Password changed successfully')
      setCurrentPassword('')
      setNewPassword('')
      setConfirmPassword('')
    } catch (err) {
      toast.error(describeError(err) || 'Failed to change password')
    } finally {
      setSaving(false)
    }
  }, [deviceUrl, currentPassword, newPassword, confirmPassword, toast])

  return (
    <div className="space-y-1">
      <h4 className="text-xs font-semibold text-surface-300 uppercase tracking-wider mb-3">
        Change Gokrazy Password
      </h4>
      <Field label="Current Password">
        <PasswordInput
          value={currentPassword}
          onChange={setCurrentPassword}
          placeholder="Current password"
          disabled={saving}
        />
      </Field>
      <Field label="New Password" hint="Minimum 8 characters">
        <PasswordInput
          value={newPassword}
          onChange={setNewPassword}
          placeholder="New password"
          disabled={saving}
        />
      </Field>
      <Field label="Confirm New Password">
        <PasswordInput
          value={confirmPassword}
          onChange={setConfirmPassword}
          placeholder="Confirm new password"
          disabled={saving}
        />
      </Field>
      <div className="flex justify-end pt-2">
        <Button variant="danger" size="sm" loading={saving} onClick={handleSave}>
          <Icon name="lock" className="w-4 h-4" />
          Change Password
        </Button>
      </div>
    </div>
  )
}

function DangerZoneBreakglass() {
  const { deviceUrl } = useDevice()
  const toast = useToast()

  const [keys, setKeys] = useState('')
  const [saving, setSaving] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setLoadError(null)
    getBreakglassAuthorizedKeys(deviceUrl)
      .then((data) => {
        if (!cancelled && data?.authorized_keys) {
          setKeys(Array.isArray(data.authorized_keys)
            ? data.authorized_keys.join('\n')
            : data.authorized_keys
          )
        }
      })
      .catch((err) => { if (!cancelled) setLoadError(describeError(err)) })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [deviceUrl])

  const handleSave = useCallback(async () => {
    if (loadError) {
      toast.error('Reload breakglass keys before saving')
      return
    }
    const parsed = keys
      .split('\n')
      .map((k) => k.trim())
      .filter(Boolean)

    setSaving(true)
    try {
      await saveBreakglassAuthorizedKeys(deviceUrl, parsed)
      toast.success('Breakglass SSH keys saved')
    } catch (err) {
      toast.error(describeError(err) || 'Failed to save breakglass keys')
    } finally {
      setSaving(false)
    }
  }, [deviceUrl, keys, loadError, toast])

  return (
    <div className="space-y-1">
      <h4 className="text-xs font-semibold text-surface-300 uppercase tracking-wider mb-3">
        Breakglass SSH Keys
      </h4>
      {loading ? (
        <div className="flex items-center justify-center py-4">
          <LoadingSpinner size="sm" />
        </div>
      ) : (
        <>
          {loadError ? (
            <div className="mb-3 rounded-lg border border-danger/20 bg-danger/10 p-3 text-xs text-danger">
              {loadError}
            </div>
          ) : null}
          <Field
            label="Authorized Keys"
            hint="One SSH public key per line. These keys allow breakglass SSH access."
          >
            <textarea
              value={keys}
              onChange={(e) => setKeys(e.target.value)}
              placeholder="ssh-ed25519 AAAA..."
              disabled={saving || !!loadError}
              rows={4}
              className={`${inputClass} resize-y font-mono text-xs`}
            />
          </Field>
          <div className="flex justify-end pt-2">
            <Button variant="danger" size="sm" loading={saving} disabled={!!loadError} onClick={handleSave}>
              <Icon name="lock" className="w-4 h-4" />
              Save SSH Keys
            </Button>
          </div>
        </>
      )}
    </div>
  )
}

function DangerZoneSection() {
  return (
    <AccordionSection title="Danger Zone" icon="exclamation-triangle">
      <div className="space-y-6">
        <DangerZonePassword />
        <div className="border-t border-surface-700/50" />
        <DangerZoneBreakglass />
      </div>
    </AccordionSection>
  )
}

// ---------------------------------------------------------------------------
// Main ConfigPage
// ---------------------------------------------------------------------------

export default function ConfigPage() {
  return (
    <div className="mx-auto max-w-4xl space-y-0">
      <div className="mb-6">
        <h1 className="text-lg font-bold text-surface-50">Configuration</h1>
        <p className="text-sm text-surface-400 mt-1">
          Manage storage, sync, security, and update settings
        </p>
      </div>

      <RemoteStorageSection />
      <B2SetupSection />
      <SyncSettingsSection />
      <OtaSection />
      <SystemMaintenanceSection />
      <DangerZoneSection />
    </div>
  )
}
