import { useState, useEffect, useCallback, useMemo, useRef, useId } from 'react'
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
  saveB2Config,
  getSettings,
  saveSettings,
  changeGokrazyPassword,
  getBreakglassAuthorizedKeys,
  saveBreakglassAuthorizedKeys,
  getWSToken,
  getWebSocketUrl,
  getOtaStatus,
  installOta,
  getSystemTime,
  syncSystemTime,
  generateTLSCertificate,
  restartAppServices,
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
  return formatReleaseDate(value)
}

function formatPartition(partition) {
  if (!partition) return '--'
  return `root${partition}`
}

function formatSpeed(bytesPerSecond) {
  if (!bytesPerSecond || bytesPerSecond < 0) return ''
  return `${formatBytes(bytesPerSecond)}/s`
}

function clampPercent(value) {
  if (!Number.isFinite(value)) return 0
  return Math.max(0, Math.min(100, value))
}

function getOtaProgress(status) {
  if (!status) return { percent: 0, label: 'Idle' }
  if (Number.isFinite(status.progress_percent)) {
    return { percent: clampPercent(status.progress_percent), label: formatOtaPhase(status) }
  }

  const fallback = {
    checking: 2,
    downloading: 20,
    installing: 70,
    installed: 100,
    failed: 100,
  }
  return { percent: fallback[status.state] || 0, label: formatOtaPhase(status) }
}

function formatOtaPhase(status) {
  const phase = status?.phase || status?.state
  const labels = {
    checking: 'Checking releases',
    downloading: 'Downloading',
    flashing: 'Flashing inactive partition',
    switching: 'Switching partitions',
    rebooting: 'Requesting reboot',
    installed: 'Installed',
    failed: 'Failed',
    idle: 'Idle',
  }
  return labels[phase] || status?.message || 'Update in progress'
}

function OtaProgress({ status }) {
  if (!status || status.state === 'idle') return null

  const progress = getOtaProgress(status)
  const width = `${progress.percent.toFixed(0)}%`
  const downloadText = status.downloaded_bytes
    ? `${formatBytes(status.downloaded_bytes)}${status.total_bytes ? ` of ${formatBytes(status.total_bytes)}` : ''}`
    : ''
  const speedText = formatSpeed(status.download_speed_bps)
  const detail = [downloadText, speedText].filter(Boolean).join(' · ')
  const barColor = status.state === 'failed' ? 'bg-danger' : 'bg-brand-500'

  return (
    <div className="rounded-lg border border-brand-500/20 bg-surface-900/60 p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-[11px] font-medium uppercase tracking-wide text-surface-500 mb-1">Update progress</p>
          <p className="text-sm font-medium text-surface-100">{progress.label}</p>
          {status.message ? <p className="text-xs text-surface-500 mt-1">{status.message}</p> : null}
        </div>
        <span className="text-sm font-semibold text-surface-100 tabular-nums shrink-0">{width}</span>
      </div>
      <div
        className="mt-3 h-2 rounded-full bg-surface-700 overflow-hidden"
        role="progressbar"
        aria-valuenow={Math.round(progress.percent)}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label={progress.label}
      >
        <div className={`h-full rounded-full transition-all duration-500 ${barColor}`} style={{ width }} />
      </div>
      {detail ? <p className="text-xs text-surface-500 mt-2">{detail}</p> : null}
      {status.error ? <p className="text-xs text-danger mt-2">{status.error}</p> : null}
    </div>
  )
}

function mergePushedOtaStatus(current, pushed) {
  const metadata = current
    ? {
        current_version: current.current_version,
        current_commit: current.current_commit,
        current_build_date: current.current_build_date,
        current_go_version: current.current_go_version,
        current_module: current.current_module,
        current_dirty: current.current_dirty,
        ab_partitions: current.ab_partitions,
        latest_version: current.latest_version,
        installed_versions: current.installed_versions,
        releases: current.releases,
        install_history: current.install_history,
        update_available: current.update_available,
      }
    : {}
  return { ...metadata, ...pushed }
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

function AccordionSection({ title, description, icon, tone = 'brand', defaultOpen = false, badge, children }) {
  const [open, setOpen] = useState(defaultOpen)
  const panelId = useId()
  const headerId = useId()

  const iconTone =
    tone === 'danger'
      ? 'bg-danger/10 ring-danger/25 text-danger'
      : 'bg-brand-500/10 ring-brand-500/20 text-brand-400'

  return (
    <Card className="mb-4 overflow-hidden">
      <h2 id={headerId} className="contents">
        <button
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          aria-controls={panelId}
          className="w-full flex items-center justify-between gap-3 p-4 text-left select-none -m-4 mb-0 hover:bg-surface-700/30 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-brand-400/60"
        >
          <div className="flex items-center gap-3 min-w-0">
            {icon && (
              <span className={`flex items-center justify-center w-9 h-9 rounded-lg ring-1 shrink-0 ${iconTone}`}>
                <Icon name={icon} className="w-5 h-5" aria-hidden="true" />
              </span>
            )}
            <div className="min-w-0">
              <div className="flex items-center gap-2 flex-wrap">
                <span className="text-sm font-semibold text-surface-100">{title}</span>
                {badge}
              </div>
              {description && <p className="text-xs text-surface-400 mt-0.5 truncate">{description}</p>}
            </div>
          </div>
          <Icon
            name="chevron-right"
            className={`w-4 h-4 text-surface-400 transition-transform duration-200 shrink-0 ${open ? 'rotate-90' : ''}`}
            aria-hidden="true"
          />
        </button>
      </h2>
      <div
        id={panelId}
        role="region"
        aria-labelledby={headerId}
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

// Field wires a label to its control and surfaces a hint via aria-describedby.
// The child control receives `id` and `describedBy` props automatically.
function Field({ label, children, hint }) {
  const fieldId = useId()
  const hintId = useId()
  const control =
    typeof children === 'function'
      ? children({ id: fieldId, describedBy: hint ? hintId : undefined })
      : children

  return (
    <div className="mb-3.5">
      <label htmlFor={fieldId} className="block text-xs font-medium text-surface-300 mb-1.5">
        {label}
      </label>
      {control}
      {hint && (
        <p id={hintId} className="mt-1.5 text-xs text-surface-500 leading-relaxed">
          {hint}
        </p>
      )}
    </div>
  )
}

// A labelled cluster of related controls within a section.
function FormGroup({ title, children }) {
  return (
    <fieldset className="min-w-0">
      {title && (
        <legend className="text-[11px] font-semibold uppercase tracking-wider text-surface-500 mb-2">
          {title}
        </legend>
      )}
      {children}
    </fieldset>
  )
}

// A compact read-only label/value panel used to display device state.
function InfoTile({ label, children, className = '' }) {
  return (
    <div className={`rounded-lg border border-surface-700/40 bg-surface-900/50 p-3 ${className}`}>
      <p className="text-[11px] font-medium uppercase tracking-wide text-surface-500 mb-1">{label}</p>
      {children}
    </div>
  )
}

// Consistent inline error / warning banner.
function FormAlert({ message }) {
  return (
    <div
      role="alert"
      className="flex items-start gap-2 rounded-lg border border-danger/25 bg-danger/10 p-3 text-xs text-danger"
    >
      <Icon name="alert-triangle" className="w-4 h-4 shrink-0 mt-px" aria-hidden="true" />
      <span className="leading-relaxed">{message}</span>
    </div>
  )
}

const inputClass =
  'w-full min-h-11 bg-surface-900 border border-surface-600/80 rounded-lg px-3 py-2.5 text-sm text-surface-50 placeholder-surface-500 transition-colors hover:border-surface-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500/60 focus:border-brand-500/60 disabled:opacity-60 disabled:cursor-not-allowed'

function TextInput({ value, onChange, placeholder, type = 'text', disabled, id, describedBy, inputMode }) {
  return (
    <input
      id={id}
      type={type}
      inputMode={inputMode}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      disabled={disabled}
      aria-describedby={describedBy}
      autoComplete="off"
      className={inputClass}
    />
  )
}

function PasswordInput({ value, onChange, placeholder, disabled, id, describedBy }) {
  const [visible, setVisible] = useState(false)
  return (
    <div className="relative">
      <input
        id={id}
        type={visible ? 'text' : 'password'}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        disabled={disabled}
        aria-describedby={describedBy}
        autoComplete="off"
        className={`${inputClass} pr-11`}
      />
      <button
        type="button"
        onClick={() => setVisible((v) => !v)}
        disabled={disabled}
        className="absolute right-1.5 top-1/2 -translate-y-1/2 flex items-center justify-center w-8 h-8 text-surface-400 hover:text-surface-100 transition-colors rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70 disabled:opacity-50"
        aria-label={visible ? 'Hide value' : 'Show value'}
        aria-pressed={visible}
      >
        <Icon name={visible ? 'x' : 'lock'} className="w-4 h-4" aria-hidden="true" />
      </button>
    </div>
  )
}

function SelectInput({ value, onChange, options, placeholder, disabled, id, describedBy }) {
  return (
    <div className="relative">
      <select
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
        aria-describedby={describedBy}
        className={`${inputClass} appearance-none pr-10 cursor-pointer`}
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
      <Icon
        name="chevron-right"
        className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 w-4 h-4 text-surface-400 rotate-90"
        aria-hidden="true"
      />
    </div>
  )
}

function Toggle({ checked, onChange, label, hint, disabled }) {
  const labelId = useId()
  const hintId = useId()
  return (
    <div
      className={`flex items-start justify-between gap-3 rounded-lg border border-surface-700/40 bg-surface-900/30 px-3 py-2.5 transition-colors ${
        disabled ? 'opacity-50' : 'hover:border-surface-600/60'
      }`}
    >
      <div className="min-w-0">
        <span id={labelId} className="block text-sm text-surface-200 leading-snug">
          {label}
        </span>
        {hint && (
          <span id={hintId} className="block text-xs text-surface-500 mt-0.5 leading-relaxed">
            {hint}
          </span>
        )}
      </div>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-labelledby={labelId}
        aria-describedby={hint ? hintId : undefined}
        disabled={disabled}
        onClick={() => !disabled && onChange(!checked)}
        className={`relative inline-flex h-6 w-11 shrink-0 mt-0.5 rounded-full border-2 border-transparent transition-colors duration-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70 focus-visible:ring-offset-2 focus-visible:ring-offset-surface-800 disabled:cursor-not-allowed ${
          checked ? 'bg-brand-600' : 'bg-surface-600'
        }`}
      >
        <span
          className={`pointer-events-none inline-block h-5 w-5 rounded-full bg-white shadow-sm transform transition-transform duration-200 ${
            checked ? 'translate-x-5' : 'translate-x-0'
          }`}
        />
      </button>
    </div>
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
    <AccordionSection
      title="Remote Storage"
      description="rclone backend for cloud uploads"
      icon="cloud"
      defaultOpen
      badge={
        loading ? null : (
          <StatusBadge variant={config?.configured ? 'success' : 'warning'}>
            {config?.configured ? 'Configured' : 'Not configured'}
          </StatusBadge>
        )
      }
    >
      {loading ? (
        <div className="flex items-center justify-center py-6">
          <LoadingSpinner size="sm" />
        </div>
      ) : (
        <div className="space-y-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="min-w-0 space-y-1.5">
              <p className="text-sm text-surface-200">
                {config?.configured ? 'Remote backend is configured' : 'No remote backend configured'}
              </p>
              {config?.configured && (config?.remote_name || config?.remotes?.length > 0) && (
                <div className="flex items-center gap-1.5 text-xs text-surface-400">
                  <span className="text-surface-500">Remote</span>
                  <span className="font-mono text-surface-300">{config.remote_name || config.remotes[0]}</span>
                  {config?.provider ? <StatusBadge variant="neutral" size="sm">{config.provider}</StatusBadge> : null}
                </div>
              )}
              {config?.configured && config?.remote_path && (
                <div className="flex items-center gap-1.5 text-xs text-surface-400">
                  <span className="text-surface-500">Path</span>
                  <span className="font-mono text-surface-300 break-all">{config.remote_path}</span>
                </div>
              )}
            </div>
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:shrink-0">
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
            <div className="rounded-lg border border-surface-700/60 bg-surface-950/60 overflow-hidden">
              <div className="flex items-center gap-2 px-3 py-2 border-b border-surface-700/50 bg-surface-900/40">
                <Icon name="terminal" className="w-3.5 h-3.5 text-surface-500" aria-hidden="true" />
                <span className="text-xs font-medium text-surface-400">Current configuration (redacted)</span>
              </div>
              <pre className="max-h-56 overflow-auto p-3 text-xs text-surface-200 whitespace-pre-wrap break-words leading-relaxed">
                {config.config_redacted}
              </pre>
            </div>
          )}
          {editing && (
            <div className="rounded-lg border border-brand-500/30 bg-surface-900/60 p-3">
              <Field label="rclone.conf" hint="Paste the complete config. Redacted secrets cannot be saved.">
                {({ id, describedBy }) => (
                  <textarea
                    id={id}
                    aria-describedby={describedBy}
                    value={draftConfig}
                    onChange={(e) => setDraftConfig(e.target.value)}
                    placeholder={'[b2]\ntype = b2\naccount = <application_key_id>\nkey = <application_key>'}
                    disabled={saving}
                    rows={8}
                    className={`${inputClass} min-h-0 resize-y font-mono text-xs leading-relaxed`}
                  />
                )}
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
  const [saving, setSaving] = useState(false)

  const handleSave = useCallback(async () => {
    if (!bucket.trim()) { toast.error('Bucket name is required'); return }
    if (!keyId.trim()) { toast.error('Key ID is required'); return }
    if (!appKey.trim()) { toast.error('Application key is required'); return }

    const bucketName = bucket.trim()
    setSaving(true)
    try {
      const res = await saveB2Config(deviceUrl, {
        bucket_name: bucketName,
        account_id: keyId.trim(),
        application_key: appKey.trim(),
        remote_name: 'b2',
        remote_path: `${bucketName}/photos`,
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
  }, [deviceUrl, bucket, keyId, appKey, toast])

  return (
    <AccordionSection title="Backblaze B2 Quick Setup" description="Guided setup for a B2 bucket" icon="cloud">
      <div>
        <Field label="Bucket Name" hint="Name of your Backblaze B2 bucket">
          {({ id, describedBy }) => (
            <TextInput
              id={id}
              describedBy={describedBy}
              value={bucket}
              onChange={setBucket}
              placeholder="my-photo-backup"
              disabled={saving}
            />
          )}
        </Field>
        <Field label="Key ID" hint="Application Key ID from Backblaze console">
          {({ id, describedBy }) => (
            <TextInput
              id={id}
              describedBy={describedBy}
              value={keyId}
              onChange={setKeyId}
              placeholder="001a1b2c3d4e5f6a7b8c9d0e1f"
              disabled={saving}
            />
          )}
        </Field>
        <Field label="Application Key" hint="Keep this secret — it will not be shown after saving">
          {({ id, describedBy }) => (
            <PasswordInput
              id={id}
              describedBy={describedBy}
              value={appKey}
              onChange={setAppKey}
              placeholder="K001..."
              disabled={saving}
            />
          )}
        </Field>
        <div className="flex justify-end pt-2">
          <Button variant="primary" size="sm" loading={saving} onClick={handleSave}>
            <Icon name="check" className="w-4 h-4" aria-hidden="true" />
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
  const [googlePhotosOAuth, setGooglePhotosOAuth] = useState(false)
  const [googlePhotosClientID, setGooglePhotosClientID] = useState('')
  const [googlePhotosClientSecret, setGooglePhotosClientSecret] = useState('')
  const [prefer5GHzWiFi, setPrefer5GHzWiFi] = useState(true)
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
          setGooglePhotos(!!(data.google_photos_enabled ?? data.google_photos))
          setGooglePhotosOAuth(!!data.google_photos_oauth_enabled)
          setGooglePhotosClientID(data.google_photos_client_id || '')
          setGooglePhotosClientSecret(data.google_photos_client_secret || '')
          setPrefer5GHzWiFi(data.prefer_5ghz_wifi ?? true)
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
        google_photos_enabled: googlePhotos,
        google_photos_oauth_enabled: googlePhotosOAuth,
        google_photos_client_id: googlePhotosClientID.trim() || undefined,
        google_photos_client_secret: googlePhotosClientSecret.trim() || undefined,
        prefer_5ghz_wifi: prefer5GHzWiFi,
      })
      toast.success('Sync settings saved')
    } catch (err) {
      toast.error(describeError(err) || 'Failed to save sync settings')
    } finally {
      setSaving(false)
    }
  }, [deviceUrl, transfers, bandwidth, googlePhotos, googlePhotosOAuth, googlePhotosClientID, googlePhotosClientSecret, prefer5GHzWiFi, loadError, toast])

  return (
    <AccordionSection title="Sync Settings" description="Transfer performance and integrations" icon="settings">
      {loading ? (
        <div className="flex items-center justify-center py-6">
          <LoadingSpinner size="sm" />
        </div>
      ) : (
        <div className="space-y-5">
          {loadError ? <FormAlert message={loadError} /> : null}

          <FormGroup title="Transfers">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-4">
              <Field label="Parallel Transfers" hint="Simultaneous file transfers (1–64)">
                {({ id, describedBy }) => (
                  <TextInput
                    id={id}
                    describedBy={describedBy}
                    value={transfers}
                    onChange={setTransfers}
                    placeholder="4"
                    type="number"
                    inputMode="numeric"
                    disabled={saving || !!loadError}
                  />
                )}
              </Field>
              <Field label="Bandwidth Limit" hint="e.g. 10M, 1G — empty for unlimited">
                {({ id, describedBy }) => (
                  <TextInput
                    id={id}
                    describedBy={describedBy}
                    value={bandwidth}
                    onChange={setBandwidth}
                    placeholder="unlimited"
                    disabled={saving || !!loadError}
                  />
                )}
              </Field>
            </div>
          </FormGroup>

          <FormGroup title="Google Photos">
            <div className="space-y-2">
              <Toggle
                checked={googlePhotos}
                onChange={setGooglePhotos}
                label="Google Photos integration"
                hint="Sync via rclone's Google Photos backend"
                disabled={saving || !!loadError}
              />
              <Toggle
                checked={googlePhotosOAuth}
                onChange={setGooglePhotosOAuth}
                label="Native OAuth"
                hint="Use a self-managed Google Cloud OAuth client"
                disabled={saving || !!loadError}
              />
            </div>
            {googlePhotosOAuth && (
              <div className="mt-3 rounded-lg border border-surface-700/40 bg-surface-900/30 p-3">
                <Field label="Client ID" hint="Google Cloud OAuth 2.0 client ID">
                  {({ id, describedBy }) => (
                    <TextInput
                      id={id}
                      describedBy={describedBy}
                      value={googlePhotosClientID}
                      onChange={setGooglePhotosClientID}
                      placeholder="your-client-id.apps.googleusercontent.com"
                      disabled={saving || !!loadError}
                    />
                  )}
                </Field>
                <Field label="Client Secret" hint="Google Cloud OAuth 2.0 client secret">
                  {({ id, describedBy }) => (
                    <PasswordInput
                      id={id}
                      describedBy={describedBy}
                      value={googlePhotosClientSecret}
                      onChange={setGooglePhotosClientSecret}
                      placeholder="your-client-secret"
                      disabled={saving || !!loadError}
                    />
                  )}
                </Field>
              </div>
            )}
          </FormGroup>

          <FormGroup title="Network">
            <Toggle
              checked={prefer5GHzWiFi}
              onChange={setPrefer5GHzWiFi}
              label="Prefer 5 GHz Wi-Fi"
              hint="Choose the 5 GHz band for networks with matching names"
              disabled={saving || !!loadError}
            />
          </FormGroup>

          <div className="flex justify-end pt-1">
            <Button variant="primary" size="sm" loading={saving} disabled={!!loadError} onClick={handleSave}>
              <Icon name="check" className="w-4 h-4" aria-hidden="true" />
              Save Settings
            </Button>
          </div>
        </div>
      )}
    </AccordionSection>
  )
}

// ---------------------------------------------------------------------------
// Section 4 - Tailscale
// ---------------------------------------------------------------------------

function TailscaleSection() {
  const { deviceUrl } = useDevice()
  const toast = useToast()

  const [configured, setConfigured] = useState(false)
  const [authKeyPath, setAuthKeyPath] = useState('/perm/tailscale/authkey')
  const [authKey, setAuthKey] = useState('')
  const [saving, setSaving] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(null)

  const fetchSettings = useCallback(async () => {
    setLoading(true)
    setLoadError(null)
    try {
      const data = await getSettings(deviceUrl)
      setConfigured(!!data?.tailscale_auth_key_configured)
      setAuthKeyPath(data?.tailscale_auth_key_path || '/perm/tailscale/authkey')
    } catch (err) {
      setLoadError(describeError(err))
    } finally {
      setLoading(false)
    }
  }, [deviceUrl])

  useEffect(() => {
    fetchSettings()
  }, [fetchSettings])

  const handleSave = useCallback(async () => {
    if (loadError) {
      toast.error('Reload Tailscale settings before saving')
      return
    }

    const key = authKey.trim()
    if (!key) {
      toast.error('Tailscale auth key is required')
      return
    }
    if (!key.startsWith('tskey-auth-')) {
      toast.error('Tailscale auth key must start with tskey-auth-')
      return
    }

    setSaving(true)
    try {
      const res = await saveSettings(deviceUrl, { tailscale_auth_key: key })
      setConfigured(true)
      setAuthKey('')
      if (res?.tailscale_auth_key_path) {
        setAuthKeyPath(res.tailscale_auth_key_path)
      }
      if (res?.warning) {
        toast.warning(res.warning)
      } else {
        toast.success('Tailscale auth key saved')
      }
    } catch (err) {
      toast.error(describeError(err) || 'Failed to save Tailscale auth key')
    } finally {
      setSaving(false)
    }
  }, [deviceUrl, authKey, loadError, toast])

  return (
    <AccordionSection title="Tailscale" description="Private mesh VPN access" icon="wifi" badge={
      loading ? null : (
        <StatusBadge variant={configured ? 'success' : 'warning'}>
          {configured ? 'Configured' : 'Not configured'}
        </StatusBadge>
      )
    }>
      {loading ? (
        <div className="flex items-center justify-center py-6">
          <LoadingSpinner size="sm" />
        </div>
      ) : (
        <div className="space-y-3">
          {loadError ? <FormAlert message={loadError} /> : null}
          <InfoTile label="Auth Key">
            <p className="text-sm font-medium text-surface-100">
              {configured ? 'Stored' : 'No key stored'}
            </p>
            <p className="text-xs text-surface-500 mt-1 break-all font-mono">{authKeyPath}</p>
          </InfoTile>
          <Field label="New Auth Key" hint="Paste a Tailscale auth key. It is stored locally and not shown after saving.">
            {({ id, describedBy }) => (
              <PasswordInput
                id={id}
                describedBy={describedBy}
                value={authKey}
                onChange={setAuthKey}
                placeholder="tskey-auth-..."
                disabled={saving || !!loadError}
              />
            )}
          </Field>
          <div className="flex justify-end pt-1">
            <Button variant="primary" size="sm" loading={saving} disabled={!!loadError} onClick={handleSave}>
              <Icon name="lock" className="w-4 h-4" aria-hidden="true" />
              Save & Connect
            </Button>
          </div>
        </div>
      )}
    </AccordionSection>
  )
}

// ---------------------------------------------------------------------------
// Section 5 - OTA Updates
// ---------------------------------------------------------------------------

function OtaSection() {
  const { deviceUrl } = useDevice()
  const toast = useToast()

  const [status, setStatus] = useState(null)
  const [loading, setLoading] = useState(true)
  const [installing, setInstalling] = useState(false)
  const [selectedRelease, setSelectedRelease] = useState('')
  const wsReconnectRef = useRef(null)
  const terminalRefreshRef = useRef('')

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
        label: release.installed ? `${label} (installed)` : label,
      }
    })
  }, [releases])

  const fetchStatus = useCallback(async ({ background = false } = {}) => {
    if (!background) setLoading(true)
    try {
      const data = await getOtaStatus(deviceUrl)
      setStatus(data)
    } catch (err) {
      toast.error(describeError(err) || 'Failed to check OTA status')
    } finally {
      if (!background) setLoading(false)
    }
  }, [deviceUrl, toast])

  useEffect(() => {
    fetchStatus()
  }, [fetchStatus])

  useEffect(() => {
    if (!deviceUrl) return undefined

    let cancelled = false
    let reconnectAttempts = 0
    let socket = null
    terminalRefreshRef.current = ''

    const scheduleReconnect = () => {
      if (cancelled) return
      const delay = Math.min(1000 * Math.pow(2, reconnectAttempts), 15000)
      reconnectAttempts += 1
      wsReconnectRef.current = window.setTimeout(connect, delay)
    }

    const connect = async () => {
      try {
        const tokenData = await getWSToken(deviceUrl)
        if (cancelled) return

        socket = new WebSocket(getWebSocketUrl(deviceUrl))
        socket.onopen = () => {
          reconnectAttempts = 0
          socket.send(JSON.stringify({ type: 'auth', token: tokenData.ws_token }))
        }
        socket.onmessage = (event) => {
          let message = null
          try {
            message = JSON.parse(event.data)
          } catch {
            return
          }

          if (message.type !== 'ota_status' || !message.data) return

          setStatus((current) => mergePushedOtaStatus(current, message.data))
          setLoading(false)

          const state = message.data.state
          const startedAt = message.data.started_at || ''
          const terminalKey = `${state}:${startedAt}`
          if ((state === 'installed' || state === 'failed') && terminalRefreshRef.current !== terminalKey) {
            terminalRefreshRef.current = terminalKey
            fetchStatus({ background: true })
          }
        }
        socket.onclose = scheduleReconnect
        socket.onerror = () => socket?.close()
      } catch {
        scheduleReconnect()
      }
    }

    connect()

    return () => {
      cancelled = true
      if (wsReconnectRef.current) window.clearTimeout(wsReconnectRef.current)
      if (socket) socket.close()
    }
  }, [deviceUrl, fetchStatus])

  useEffect(() => {
    if (!installationRunning) return
    const timer = window.setTimeout(() => fetchStatus({ background: true }), 5000)
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
      fetchStatus({ background: true })
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
    <AccordionSection title="OTA Updates" description="Firmware version and A/B updates" icon="arrow-up-tray" badge={
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
            <InfoTile label="Known installed versions">
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
            </InfoTile>
          ) : null}
          <OtaProgress status={status} />
          {status?.ab_partitions ? (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <InfoTile label="A/B Partitions">
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
              </InfoTile>
              <InfoTile label="Update target">
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
              </InfoTile>
            </div>
          ) : null}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <InfoTile label="Current Version">
              <p className="text-sm font-medium text-surface-100">
                {status?.current_version || 'unknown'}
              </p>
              {status?.current_commit ? (
                <p className="text-xs text-surface-500 mt-1 break-all font-mono">
                  {status.current_commit}
                </p>
              ) : null}
              <p className="text-xs text-surface-500 mt-1">
                {status?.current_build_date ? formatReleaseDate(status.current_build_date) : 'Build date unknown'}
              </p>
            </InfoTile>
            <InfoTile label="Latest Release">
              <p className="text-sm font-medium text-surface-100">
                {status?.latest_version || 'unknown'}
              </p>
              <p className="text-xs text-surface-500 mt-1">
                {releases[0] ? formatReleaseDate(releases[0].published_at) : 'No release date'}
              </p>
            </InfoTile>
          </div>
          <Field
            label="Select release to install"
            hint={
              selectedReleaseInfo
                ? `${selectedReleaseInfo.name || selectedReleaseInfo.tag_name}${
                    selectedReleaseInfo.published_at ? ` · published ${formatReleaseDate(selectedReleaseInfo.published_at)}` : ''
                  }`
                : undefined
            }
          >
            {({ id, describedBy }) =>
              releaseOptions.length > 0 ? (
                <SelectInput
                  id={id}
                  describedBy={describedBy}
                  value={selectedRelease}
                  onChange={setSelectedRelease}
                  options={releaseOptions}
                  disabled={installing}
                />
              ) : (
                <p id={describedBy} className="text-xs text-surface-500 py-2">No installable releases found</p>
              )
            }
          </Field>
          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-between sm:items-center pt-1">
            <Button
              variant="ghost"
              size="sm"
              onClick={fetchStatus}
              disabled={installing}
            >
              <Icon name="arrow-path" className="w-4 h-4" aria-hidden="true" />
              Check for updates
            </Button>
            <Button
              variant="primary"
              size="sm"
              loading={installing}
              disabled={!canInstall || releaseOptions.length === 0}
              title={installationRunning ? 'An OTA update is already running' : ''}
              onClick={handleInstall}
              className="sm:w-auto"
            >
              <Icon name="arrow-up-tray" className="w-4 h-4" aria-hidden="true" />
              Install Selected Version
            </Button>
          </div>
          {installHistory.length ? (
            <InfoTile label="Recent install history">
              <div className="space-y-2 mt-1">
                {installHistory.slice(0, 5).map((entry, idx) => (
                  <div
                    key={`${entry.release || 'unknown'}-${entry.started_at || entry.finished_at || idx}`}
                    className="rounded-md border border-surface-700/60 bg-surface-950 p-2"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <p className="text-xs font-medium text-surface-200 truncate">{entry.release || 'unknown release'}</p>
                      <StatusBadge
                        size="sm"
                        variant={entry.state === 'installed' ? 'success' : entry.state === 'failed' ? 'danger' : 'neutral'}
                      >
                        {entry.state || 'unknown'}
                      </StatusBadge>
                    </div>
                    {entry.finished_at || entry.started_at ? (
                      <p className="text-xs text-surface-500 mt-1">
                        {entry.state === 'installed' ? 'Finished' : 'Started'} {formatDate(entry.finished_at || entry.started_at)}
                      </p>
                    ) : null}
                    {entry.error ? <p className="text-xs text-danger mt-1">{entry.error}</p> : null}
                  </div>
                ))}
              </div>
            </InfoTile>
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
// Section 6 - System Clock + TLS
// ---------------------------------------------------------------------------

function SystemMaintenanceSection() {
  const { deviceUrl } = useDevice()
  const toast = useToast()

  const [status, setStatus] = useState(null)
  const [loading, setLoading] = useState(true)
  const [syncingTime, setSyncingTime] = useState(false)
  const [generatingCert, setGeneratingCert] = useState(false)
  const [restartTarget, setRestartTarget] = useState('app')
  const [restartingServices, setRestartingServices] = useState(false)

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

  const handleRestartServices = useCallback(async () => {
    const labels = {
      app: 'app services',
      'pictures-sync': 'sync daemon',
      webui: 'web UI',
    }
    const services = restartTarget === 'app' ? ['pictures-sync', 'webui'] : [restartTarget]
    if (!window.confirm(`Restart ${labels[restartTarget] || 'selected services'}?`)) {
      return
    }

    setRestartingServices(true)
    try {
      await restartAppServices(deviceUrl, services)
      toast.success('Service restart requested')
    } catch (err) {
      toast.error(describeError(err) || 'Failed to restart services')
    } finally {
      setRestartingServices(false)
    }
  }, [deviceUrl, restartTarget, toast])

  return (
    <AccordionSection title="System Clock & TLS" description="Time, certificates, and service control" icon="clock" badge={
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
        <div className="space-y-4">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <InfoTile label="Device Time">
              <div className="flex items-start justify-between gap-2">
                <p className="text-sm font-medium text-surface-100">
                  {formatDate(status?.current_time)}
                </p>
                <StatusBadge variant={timeReady ? 'success' : 'warning'} size="sm">
                  {timeReady ? 'Valid' : 'Invalid'}
                </StatusBadge>
              </div>
            </InfoTile>
            <InfoTile label="Persistent TLS">
              <div className="flex items-start justify-between gap-2">
                <p className="text-sm font-medium text-surface-100">
                  {cert.exists ? (cert.valid_now ? 'Certificate valid' : 'Certificate invalid') : 'No certificate'}
                </p>
                <StatusBadge variant={certReady ? 'success' : 'warning'} size="sm">
                  {certReady ? 'Ready' : 'Action needed'}
                </StatusBadge>
              </div>
              <p className="text-xs text-surface-500 mt-1 break-all font-mono">
                {cert.cert_file || '/perm/ssl/gokrazy-web.pem'}
              </p>
              {cert.not_after ? (
                <p className="text-xs text-surface-500 mt-1">
                  Expires {formatDate(cert.not_after)}
                </p>
              ) : null}
            </InfoTile>
          </div>
          {cert.error ? <FormAlert message={cert.error} /> : null}

          <FormGroup title="Services">
            <Field label="Restart services" hint="App services restart both the sync daemon and the web UI.">
              {({ id, describedBy }) => (
                <div className="flex flex-col gap-2 sm:flex-row">
                  <SelectInput
                    id={id}
                    describedBy={describedBy}
                    value={restartTarget}
                    onChange={setRestartTarget}
                    disabled={restartingServices}
                    options={[
                      { value: 'app', label: 'App services' },
                      { value: 'pictures-sync', label: 'Sync daemon' },
                      { value: 'webui', label: 'Web UI' },
                    ]}
                  />
                  <Button
                    variant="secondary"
                    size="sm"
                    loading={restartingServices}
                    disabled={syncingTime || generatingCert || restartingServices}
                    onClick={handleRestartServices}
                    className="sm:shrink-0"
                  >
                    <Icon name="arrow-path" className="w-4 h-4" aria-hidden="true" />
                    Restart
                  </Button>
                </div>
              )}
            </Field>
          </FormGroup>

          <div className="flex flex-col sm:flex-row sm:justify-between sm:items-center gap-3 pt-1 border-t border-surface-700/40">
            <Button
              variant="ghost"
              size="sm"
              onClick={fetchStatus}
              disabled={syncingTime || generatingCert}
              className="mt-3 sm:mt-0"
            >
              <Icon name="arrow-path" className="w-4 h-4" aria-hidden="true" />
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
                <Icon name="clock" className="w-4 h-4" aria-hidden="true" />
                Sync Time
              </Button>
              <Button
                variant="primary"
                size="sm"
                loading={generatingCert}
                disabled={syncingTime || !timeReady}
                title={!timeReady ? 'Sync the device time before generating a certificate' : ''}
                onClick={handleGenerateCert}
              >
                <Icon name="lock" className="w-4 h-4" aria-hidden="true" />
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
// Section 7 - Danger Zone (Password + Breakglass)
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

  const mismatch = confirmPassword.length > 0 && newPassword !== confirmPassword
  const tooShort = newPassword.length > 0 && newPassword.length < 8

  return (
    <div>
      <h4 className="text-xs font-semibold text-surface-300 uppercase tracking-wider mb-3">
        Change Gokrazy Password
      </h4>
      <Field label="Current Password">
        {({ id, describedBy }) => (
          <PasswordInput
            id={id}
            describedBy={describedBy}
            value={currentPassword}
            onChange={setCurrentPassword}
            placeholder="Current password"
            disabled={saving}
          />
        )}
      </Field>
      <Field label="New Password" hint={tooShort ? undefined : 'Minimum 8 characters'}>
        {({ id, describedBy }) => (
          <>
            <PasswordInput
              id={id}
              describedBy={describedBy}
              value={newPassword}
              onChange={setNewPassword}
              placeholder="New password"
              disabled={saving}
            />
            {tooShort && (
              <p className="mt-1.5 text-xs text-danger">Must be at least 8 characters.</p>
            )}
          </>
        )}
      </Field>
      <Field label="Confirm New Password">
        {({ id }) => (
          <>
            <PasswordInput
              id={id}
              value={confirmPassword}
              onChange={setConfirmPassword}
              placeholder="Confirm new password"
              disabled={saving}
            />
            {mismatch && (
              <p className="mt-1.5 text-xs text-danger">Passwords do not match.</p>
            )}
          </>
        )}
      </Field>
      <div className="flex justify-end pt-2">
        <Button variant="danger" size="sm" loading={saving} onClick={handleSave}>
          <Icon name="lock" className="w-4 h-4" aria-hidden="true" />
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
    <div>
      <h4 className="text-xs font-semibold text-surface-300 uppercase tracking-wider mb-3">
        Breakglass SSH Keys
      </h4>
      {loading ? (
        <div className="flex items-center justify-center py-4">
          <LoadingSpinner size="sm" />
        </div>
      ) : (
        <>
          {loadError ? <FormAlert message={loadError} /> : null}
          <Field
            label="Authorized Keys"
            hint="One SSH public key per line. These keys allow breakglass SSH access."
          >
            {({ id, describedBy }) => (
              <textarea
                id={id}
                aria-describedby={describedBy}
                value={keys}
                onChange={(e) => setKeys(e.target.value)}
                placeholder="ssh-ed25519 AAAA..."
                disabled={saving || !!loadError}
                rows={4}
                className={`${inputClass} min-h-0 resize-y font-mono text-xs leading-relaxed`}
              />
            )}
          </Field>
          <div className="flex justify-end pt-2">
            <Button variant="danger" size="sm" loading={saving} disabled={!!loadError} onClick={handleSave}>
              <Icon name="lock" className="w-4 h-4" aria-hidden="true" />
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
    <AccordionSection
      title="Danger Zone"
      description="Credentials and emergency SSH access"
      icon="exclamation-triangle"
      tone="danger"
    >
      <div className="rounded-lg border border-danger/20 bg-danger/[0.04] p-4 space-y-6">
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
      <div className="mb-6 flex items-center gap-3">
        <span className="flex items-center justify-center w-10 h-10 rounded-xl bg-brand-500/10 ring-1 ring-brand-500/20 text-brand-400 shrink-0">
          <Icon name="settings" className="w-5 h-5" aria-hidden="true" />
        </span>
        <div>
          <h1 className="text-lg font-bold text-surface-50">Configuration</h1>
          <p className="text-sm text-surface-400 mt-0.5">
            Manage storage, sync, security, and update settings
          </p>
        </div>
      </div>

      <RemoteStorageSection />
      <B2SetupSection />
      <SyncSettingsSection />
      <TailscaleSection />
      <OtaSection />
      <SystemMaintenanceSection />
      <DangerZoneSection />
    </div>
  )
}
