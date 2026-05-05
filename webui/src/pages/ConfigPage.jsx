import { useState, useEffect, useCallback } from 'react'
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
  }, [deviceUrl, draftConfig, toast])

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
          <div className="flex items-center justify-between gap-3">
            <div>
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
            <div className="flex shrink-0 items-center gap-2">
              <Button
                variant="secondary"
                size="sm"
                onClick={() => setEditing((v) => !v)}
                disabled={saving}
              >
                <Icon name="pencil" className="w-4 h-4" />
                {editing ? 'Cancel' : 'Edit Config'}
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

  useEffect(() => {
    let cancelled = false
    getSettings(deviceUrl)
      .then((data) => {
        if (cancelled) return
        if (data) {
          setTransfers(String(data.transfers ?? '4'))
          setBandwidth(data.bandwidth || '')
          setGooglePhotos(!!data.google_photos)
        }
      })
      .catch(() => {})
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [deviceUrl])

  const handleSave = useCallback(async () => {
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
  }, [deviceUrl, transfers, bandwidth, googlePhotos, toast])

  return (
    <AccordionSection title="Sync Settings" icon="settings">
      {loading ? (
        <div className="flex items-center justify-center py-6">
          <LoadingSpinner size="sm" />
        </div>
      ) : (
        <div className="space-y-1">
          <Field label="Parallel Transfers" hint="Number of simultaneous file transfers (1-64)">
            <TextInput
              value={transfers}
              onChange={setTransfers}
              placeholder="4"
              type="number"
              disabled={saving}
            />
          </Field>
          <Field label="Bandwidth Limit" hint="e.g. 10M, 1G, or leave empty for unlimited">
            <TextInput
              value={bandwidth}
              onChange={setBandwidth}
              placeholder="unlimited"
              disabled={saving}
            />
          </Field>
          <div className="py-2">
            <Toggle
              checked={googlePhotos}
              onChange={setGooglePhotos}
              label="Enable Google Photos integration"
              disabled={saving}
            />
          </div>
          <div className="flex justify-end pt-2">
            <Button variant="primary" size="sm" loading={saving} onClick={handleSave}>
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

  const handleInstall = useCallback(async () => {
    setInstalling(true)
    try {
      await installOta(deviceUrl)
      toast.success('Update installation started')
      fetchStatus()
    } catch (err) {
      toast.error(describeError(err) || 'Failed to start update')
    } finally {
      setInstalling(false)
    }
  }, [deviceUrl, toast, fetchStatus])

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
          <div className="grid grid-cols-2 gap-3">
            <div className="bg-surface-900 rounded-lg p-3">
              <p className="text-xs text-surface-400 mb-1">Current Version</p>
              <p className="text-sm font-medium text-surface-100">
                {status?.current_version || 'unknown'}
              </p>
            </div>
            <div className="bg-surface-900 rounded-lg p-3">
              <p className="text-xs text-surface-400 mb-1">Latest Version</p>
              <p className="text-sm font-medium text-surface-100">
                {status?.latest_version || 'unknown'}
              </p>
            </div>
          </div>
          <div className="flex justify-between items-center pt-1">
            <Button
              variant="ghost"
              size="sm"
              onClick={fetchStatus}
              disabled={installing}
            >
              <Icon name="arrow-path" className="w-4 h-4" />
              Refresh
            </Button>
            <Button
              variant="primary"
              size="sm"
              loading={installing}
              disabled={!status?.update_available}
              onClick={handleInstall}
            >
              <Icon name="arrow-up-tray" className="w-4 h-4" />
              Install Update
            </Button>
          </div>
        </div>
      )}
    </AccordionSection>
  )
}

// ---------------------------------------------------------------------------
// Section 5 - Danger Zone (Password + Breakglass)
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

  useEffect(() => {
    let cancelled = false
    getBreakglassAuthorizedKeys(deviceUrl)
      .then((data) => {
        if (!cancelled && data?.authorized_keys) {
          setKeys(Array.isArray(data.authorized_keys)
            ? data.authorized_keys.join('\n')
            : data.authorized_keys
          )
        }
      })
      .catch(() => {})
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [deviceUrl])

  const handleSave = useCallback(async () => {
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
  }, [deviceUrl, keys, toast])

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
          <Field
            label="Authorized Keys"
            hint="One SSH public key per line. These keys allow breakglass SSH access."
          >
            <textarea
              value={keys}
              onChange={(e) => setKeys(e.target.value)}
              placeholder="ssh-ed25519 AAAA..."
              disabled={saving}
              rows={4}
              className={`${inputClass} resize-y font-mono text-xs`}
            />
          </Field>
          <div className="flex justify-end pt-2">
            <Button variant="danger" size="sm" loading={saving} onClick={handleSave}>
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
    <div className="max-w-2xl mx-auto px-4 py-6 space-y-0">
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
      <DangerZoneSection />
    </div>
  )
}
