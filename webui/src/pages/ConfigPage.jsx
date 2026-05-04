import { useEffect, useState } from 'react'
import {
  Alert,
  AlertDescription,
  AlertIndicator,
  Badge,
  Box,
  Button,
  Card,
  CardBody,
  CardHeader,
  Checkbox,
  Heading,
  HStack,
  Input,
  ListItem,
  ListRoot,
  Spinner,
  Stack,
  Text,
  Textarea,
  VStack,
} from '@chakra-ui/react'
import {
  changeGokrazyPassword,
  getB2Regions,
  getBreakglassAuthorizedKeys,
  getConfig,
  getOtaStatus,
  getSettings,
  installOta,
  saveBreakglassAuthorizedKeys,
  saveB2Config,
  saveSettings,
  testConfig,
} from '../api'

function toNumberOrDefault(value, fallback) {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

export function ConfigPage({ deviceUrl }) {
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [config, setConfig] = useState({ configured: false, remotes: [] })
  const [remoteName, setRemoteName] = useState('')
  const [remotePath, setRemotePath] = useState('')
  const [reformatThreshold, setReformatThreshold] = useState(0.3)
  const [transfers, setTransfers] = useState(4)
  const [checkers, setCheckers] = useState(8)
  const [googlePhotosEnabled, setGooglePhotosEnabled] = useState(false)
  const [googlePhotosRemoteName, setGooglePhotosRemoteName] = useState('')
  const [tailscaleAuthKey, setTailscaleAuthKey] = useState('')
  const [tailscaleAuthKeyConfigured, setTailscaleAuthKeyConfigured] = useState(false)
  const [tailscaleAuthKeyPath, setTailscaleAuthKeyPath] = useState('/perm/tailscale/auth_key')
  const [currentGokrazyPassword, setCurrentGokrazyPassword] = useState('')
  const [newGokrazyPassword, setNewGokrazyPassword] = useState('')
  const [confirmGokrazyPassword, setConfirmGokrazyPassword] = useState('')
  const [savingGokrazyPassword, setSavingGokrazyPassword] = useState(false)
  const [breakglassKeys, setBreakglassKeys] = useState('')
  const [breakglassPath, setBreakglassPath] = useState('/perm/breakglass/authorized_keys')
  const [savingBreakglass, setSavingBreakglass] = useState(false)
  const [otaStatus, setOtaStatus] = useState({ state: 'idle' })
  const [installingOta, setInstallingOta] = useState(false)

  // B2 config state
  const [b2Account, setB2Account] = useState('')
  const [b2Key, setB2Key] = useState('')
  const [b2Bucket, setB2Bucket] = useState('')
  const [b2RemoteName, setB2RemoteName] = useState('b2')
  const [b2RemotePath, setB2RemotePath] = useState('/photos')
  const [b2Endpoint, setB2Endpoint] = useState('')
  const [b2SelectedRegion, setB2SelectedRegion] = useState('')
  const [b2Regions, setB2Regions] = useState([])
  const [savingB2, setSavingB2] = useState(false)
  const [b2BucketError, setB2BucketError] = useState('')

  const load = async () => {
    if (!deviceUrl) return
    setLoading(true)
    setError('')
    try {
      const [configResponse, settingsResponse, breakglassResponse, otaResponse, regionsResponse] = await Promise.all([
        getConfig(deviceUrl),
        getSettings(deviceUrl),
        getBreakglassAuthorizedKeys(deviceUrl),
        getOtaStatus(deviceUrl),
        getB2Regions(deviceUrl).catch(() => []),
      ])
      setConfig(configResponse || { configured: false, remotes: [] })
      setRemoteName(settingsResponse?.remote_name || '')
      setRemotePath(settingsResponse?.remote_path || '')
      setReformatThreshold(toNumberOrDefault(settingsResponse?.reformat_threshold, 0.3))
      setTransfers(toNumberOrDefault(settingsResponse?.transfers, 4))
      setCheckers(toNumberOrDefault(settingsResponse?.checkers, 8))
      setGooglePhotosEnabled(Boolean(settingsResponse?.google_photos_enabled))
      setGooglePhotosRemoteName(settingsResponse?.google_photos_remote_name || '')
      setTailscaleAuthKeyConfigured(Boolean(settingsResponse?.tailscale_auth_key_configured))
      setTailscaleAuthKeyPath(settingsResponse?.tailscale_auth_key_path || '/perm/tailscale/auth_key')
      setBreakglassKeys(breakglassResponse?.authorized_keys || '')
      setBreakglassPath(breakglassResponse?.path || '/perm/breakglass/authorized_keys')
      setOtaStatus(otaResponse || { state: 'idle' })
      const regions = Array.isArray(regionsResponse) ? regionsResponse : []
      setB2Regions(regions)
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [deviceUrl])

  const onSave = async (event) => {
    event.preventDefault()
    setSaving(true)
    setMessage('')
    setError('')
    try {
      await saveSettings(deviceUrl, {
        remote_name: remoteName,
        remote_path: remotePath,
        reformat_threshold: Number(reformatThreshold),
        transfers: Number(transfers),
        checkers: Number(checkers),
        google_photos_enabled: Boolean(googlePhotosEnabled),
        google_photos_remote_name: googlePhotosRemoteName,
        tailscale_auth_key: tailscaleAuthKey,
      })
      setTailscaleAuthKey('')
      setMessage('Settings saved. Your destination is ready.')
      await load()
    } catch (err) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const onTest = async () => {
    setTesting(true)
    setMessage('')
    setError('')
    try {
      const response = await testConfig(deviceUrl)
      if (response?.success) {
        setMessage('Connection test passed. Device can reach the destination.')
      } else {
        setError(response?.error || 'Connection test failed.')
      }
    } catch (err) {
      setError(err.message)
    } finally {
      setTesting(false)
    }
  }

  const onSaveBreakglass = async () => {
    setSavingBreakglass(true)
    setMessage('')
    setError('')
    try {
      const response = await saveBreakglassAuthorizedKeys(deviceUrl, breakglassKeys)
      setMessage(`Breakglass keys saved to ${response?.path || breakglassPath}.`)
      await load()
    } catch (err) {
      setError(err.message)
    } finally {
      setSavingBreakglass(false)
    }
  }

  const onChangeGokrazyPassword = async (event) => {
    event.preventDefault()
    setSavingGokrazyPassword(true)
    setMessage('')
    setError('')
    try {
      if (newGokrazyPassword !== confirmGokrazyPassword) {
        throw new Error('New passwords do not match.')
      }
      await changeGokrazyPassword(deviceUrl, currentGokrazyPassword, newGokrazyPassword)
      setCurrentGokrazyPassword('')
      setNewGokrazyPassword('')
      setConfirmGokrazyPassword('')
      setMessage('Gokrazy UI password changed.')
    } catch (err) {
      setError(err.message)
    } finally {
      setSavingGokrazyPassword(false)
    }
  }

  const onInstallOta = async () => {
    setInstallingOta(true)
    setMessage('')
    setError('')
    try {
      const response = await installOta(deviceUrl)
      setOtaStatus(response || { state: 'checking' })
      setMessage('OTA installation started.')
    } catch (err) {
      setError(err.message)
    } finally {
      setInstallingOta(false)
    }
  }

  const otaBusy = ['checking', 'downloading', 'installing'].includes(otaStatus?.state)

  function validateB2Bucket(name) {
    if (!name) return ''
    if (name.length < 3) return 'Must be at least 3 characters'
    if (name.length > 63) return 'Must be at most 63 characters'
    if (!/^[a-z0-9]/.test(name)) return 'Must start with a lowercase letter or digit'
    if (!/[a-z0-9]$/.test(name)) return 'Must end with a lowercase letter or digit'
    if (!/^[a-z0-9\-]+$/.test(name)) return 'Only lowercase letters, digits, and hyphens allowed'
    if (/--/.test(name)) return 'Must not contain consecutive hyphens'
    return ''
  }

  function onB2BucketChange(value) {
    setB2Bucket(value)
    setB2BucketError(validateB2Bucket(value))
  }

  function onB2RegionChange(regionId) {
    setB2SelectedRegion(regionId)
    const region = b2Regions.find((r) => r.id === regionId)
    if (region) {
      setB2Endpoint(region.endpoint)
    }
  }

  return (
    <Card bg="transparent" border="none">
      <CardHeader>
        <Heading size="lg" color="fg.default">Sync settings</Heading>
      </CardHeader>
      <CardBody>
        <Card variant="panel">
          <CardBody>
            <HStack justify="space-between" align="center">
              <Box>
                <Text fontWeight="semibold" color="fg.default">Destination</Text>
                <Text color="fg.muted" fontSize="sm">Choose where backups are uploaded.</Text>
              </Box>
              <Button size="sm" onClick={load} loading={loading}>
                Refresh
              </Button>
            </HStack>

            <Card variant="panel" mt={4}>
              <CardBody>
                <HStack justify="space-between" wrap="wrap">
                  <Text fontWeight="medium" color="fg.default">Configured destination</Text>
                  <Badge bg={config.configured ? 'success.bg' : 'warning.bg'} color={config.configured ? 'success' : 'warning'}>
                    {config.configured ? 'Configured' : 'Not configured'}
                  </Badge>
                </HStack>
                <Text mt={2} fontSize="sm" color="fg.muted">
                  Remotes detected: {config.remotes?.length || 0}
                </Text>
                <ListRoot mt={2} gap={1} color="fg.default">
                  {(config.remotes || []).map((item) => (
                    <ListItem key={item} color="fg.default">• {item}</ListItem>
                  ))}
                  {(config.remotes || []).length === 0 ? (
                    <ListItem color="fg.subtle">No remote entries found.</ListItem>
                  ) : null}
                </ListRoot>
              </CardBody>
            </Card>

            <Card variant="panel" mt={4}>
              <CardBody>
                <Text fontWeight="medium" color="fg.default" mb={1}>Backblaze B2 quick setup</Text>
                <Text color="fg.muted" fontSize="sm" mb={3}>
                  Enter your B2 Application Key credentials to auto-generate the rclone config.
                  You can find these in the Backblaze web console under
                  <Text as="span" fontWeight="semibold"> App Keys</Text>.
                  The <Text as="span" fontWeight="semibold">keyID</Text> is your Account ID and
                  the <Text as="span" fontWeight="semibold">applicationKey</Text> is your Application Key.
                </Text>
                <form onSubmit={async (event) => {
                  event.preventDefault()
                  if (b2BucketError) return
                  setSavingB2(true)
                  setMessage('')
                  setError('')
                  try {
                    const response = await saveB2Config(deviceUrl, {
                      account_id: b2Account,
                      application_key: b2Key,
                      bucket_name: b2Bucket,
                      remote_name: b2RemoteName || 'b2',
                      remote_path: b2RemotePath || '/photos',
                      endpoint: b2Endpoint || undefined,
                    })
                    if (response?.success) {
                      setMessage('B2 remote configured and connection verified.')
                      setB2Key('')
                      await load()
                    } else {
                      setError(response?.error || 'B2 configuration failed.')
                    }
                  } catch (err) {
                    setError(err.message)
                  } finally {
                    setSavingB2(false)
                  }
                }}>
                  <Stack gap={3}>
                    <Box>
                      <Text color="fg.muted" fontSize="sm" mb={1}>Account ID (keyID)</Text>
                      <Input
                        value={b2Account}
                        onChange={(event) => setB2Account(event.target.value)}
                        placeholder="004..."
                        autoComplete="off"
                      />
                    </Box>
                    <Box>
                      <Text color="fg.muted" fontSize="sm" mb={1}>Application Key</Text>
                      <Input
                        type="password"
                        value={b2Key}
                        onChange={(event) => setB2Key(event.target.value)}
                        placeholder="K004..."
                        autoComplete="off"
                      />
                    </Box>
                    <Box>
                      <Text color="fg.muted" fontSize="sm" mb={1}>Bucket name</Text>
                      <Input
                        value={b2Bucket}
                        onChange={(event) => onB2BucketChange(event.target.value)}
                        placeholder="my-photo-backup"
                        autoComplete="off"
                      />
                      {b2BucketError ? (
                        <Text color="danger" fontSize="xs" mt={1}>{b2BucketError}</Text>
                      ) : null}
                    </Box>
                    {b2Regions.length > 0 ? (
                      <Box>
                        <Text color="fg.muted" fontSize="sm" mb={1}>Region</Text>
                        <select
                          value={b2SelectedRegion}
                          onChange={(event) => onB2RegionChange(event.target.value)}
                          style={{
                            width: '100%',
                            padding: '8px 12px',
                            borderRadius: '6px',
                            border: '1px solid var(--chakra-colors-border)',
                            background: 'var(--chakra-colors-bg)',
                            color: 'var(--chakra-colors-fg-default)',
                            fontSize: '14px',
                          }}
                        >
                          <option value="">Select a region...</option>
                          {b2Regions.map((region) => (
                            <option key={region.id} value={region.id}>
                              {region.name}
                            </option>
                          ))}
                        </select>
                      </Box>
                    ) : null}
                    <Box>
                      <Text color="fg.muted" fontSize="sm" mb={1}>Endpoint</Text>
                      <Input
                        value={b2Endpoint}
                        onChange={(event) => {
                          setB2Endpoint(event.target.value)
                          setB2SelectedRegion('')
                        }}
                        placeholder="https://s3.us-west-004.backblazeb2.com"
                        autoComplete="off"
                      />
                      <Text color="fg.subtle" fontSize="xs" mt={1}>
                        Auto-filled when selecting a region. Only change if using a custom endpoint.
                      </Text>
                    </Box>
                    <HStack gap={4}>
                      <Box flex={1}>
                        <Text color="fg.muted" fontSize="sm" mb={1}>Remote name</Text>
                        <Input
                          value={b2RemoteName}
                          onChange={(event) => setB2RemoteName(event.target.value)}
                          placeholder="b2"
                        />
                      </Box>
                      <Box flex={1}>
                        <Text color="fg.muted" fontSize="sm" mb={1}>Remote path</Text>
                        <Input
                          value={b2RemotePath}
                          onChange={(event) => setB2RemotePath(event.target.value)}
                          placeholder="/photos"
                        />
                      </Box>
                    </HStack>
                    <Button
                      type="submit"
                      variant="brand"
                      loading={savingB2}
                      disabled={!b2Account || !b2Key || !b2Bucket || !!b2BucketError}
                    >
                      Configure B2 remote
                    </Button>
                  </Stack>
                </form>
              </CardBody>
            </Card>

            <form onSubmit={onSave}>
              <Stack mt={4} gap={4}>
                <Box>
                  <Text color="fg.muted" fontSize="sm" mb={1}>Remote name</Text>
                  <Input value={remoteName} onChange={(event) => setRemoteName(event.target.value)} />
                </Box>

                <Box>
                  <Text color="fg.muted" fontSize="sm" mb={1}>Remote path</Text>
                  <Input value={remotePath} onChange={(event) => setRemotePath(event.target.value)} />
                </Box>

                <HStack gap={4}>
                  <Box flex={1}>
                    <Text color="fg.muted" fontSize="sm" mb={1}>Reformat threshold</Text>
                    <Input
                      type="number"
                      value={reformatThreshold}
                      onChange={(event) => setReformatThreshold(event.target.valueAsNumber)}
                    />
                  </Box>
                  <Box flex={1}>
                    <Text color="fg.muted" fontSize="sm" mb={1}>Transfers</Text>
                    <Input 
                      type="number" 
                      value={transfers} 
                      onChange={(event) => setTransfers(event.target.valueAsNumber)} 
                    />
                  </Box>
                  <Box flex={1}>
                    <Text color="fg.muted" fontSize="sm" mb={1}>Checkers</Text>
                    <Input 
                      type="number" 
                      value={checkers} 
                      onChange={(event) => setCheckers(event.target.valueAsNumber)} 
                    />
                  </Box>
                </HStack>

                <Box>
                  <HStack gap={2} mb={2}>
                    <Checkbox 
                      checked={googlePhotosEnabled}
                      onChange={(event) => setGooglePhotosEnabled(event.target.checked)}
                      colorScheme="cyan"
                    />
                    <Text color="fg.default">Also upload a copy to Google Photos</Text>
                  </HStack>
                </Box>

                <Box>
                  <Text color="fg.muted" fontSize="sm" mb={1}>Google Photos remote</Text>
                  <Input
                    disabled={!googlePhotosEnabled}
                    value={googlePhotosRemoteName}
                    onChange={(event) => setGooglePhotosRemoteName(event.target.value)}
                    placeholder="gphotos"
                  />
                </Box>

                <HStack gap={2}>
                  <Button type="submit" variant="brand" loading={saving}>
                    Save destination
                  </Button>
                  <Button type="button" onClick={onTest} loading={testing} variant="outline">
                    Test destination
                  </Button>
                </HStack>
              </Stack>
            </form>

            {loading && (
              <Stack mt={4} gap={2}>
                <Spinner />
              </Stack>
            )}

            {error ? (
              <Alert status="error" mt={4}>
                <AlertIndicator />
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            ) : null}
            {message ? (
              <Alert status="success" mt={4}>
                <AlertIndicator />
                <AlertDescription>{message}</AlertDescription>
              </Alert>
            ) : null}
          </CardBody>
        </Card>

        <details style={{ marginTop: '16px', cursor: 'pointer' }}>
          <summary style={{ 
            color: 'var(--chakra-colors-fg-muted)', 
            fontSize: '0.9rem', 
            fontWeight: 600,
            padding: '8px 0'
          }}>
            Advanced options
          </summary>
          <Card variant="panel" mt={3}>
            <CardHeader>
              <Heading size="sm" color="fg.default">Security and maintenance</Heading>
            </CardHeader>
            <CardBody>
              <Stack gap={6}>
                {/* Gokrazy Password */}
                <form onSubmit={onChangeGokrazyPassword}>
                  <VStack align="stretch" gap={3}>
                    <Text fontWeight="medium" color="fg.default">Gokrazy web password</Text>
                    <Input
                      type="password"
                      autoComplete="current-password"
                      placeholder="Current password"
                      value={currentGokrazyPassword}
                      onChange={(event) => setCurrentGokrazyPassword(event.target.value)}
                    />
                    <Input
                      type="password"
                      autoComplete="new-password"
                      placeholder="New password"
                      value={newGokrazyPassword}
                      onChange={(event) => setNewGokrazyPassword(event.target.value)}
                    />
                    <Input
                      type="password"
                      autoComplete="new-password"
                      placeholder="Confirm new password"
                      value={confirmGokrazyPassword}
                      onChange={(event) => setConfirmGokrazyPassword(event.target.value)}
                    />
                    <Button type="submit" size="sm" variant="brand" loading={savingGokrazyPassword}>
                      Update password
                    </Button>
                  </VStack>
                </form>

                {/* Breakglass SSH Keys */}
                <form onSubmit={(event) => {
                  event.preventDefault()
                  onSaveBreakglass()
                }}>
                  <VStack align="stretch" gap={2}>
                    <Text fontWeight="medium" color="fg.default">Breakglass SSH keys</Text>
                    <Text fontSize="sm" color="fg.muted" fontFamily="mono">
                      {breakglassPath}
                    </Text>
                    <Textarea
                      minH="120px"
                      fontFamily="mono"
                      fontSize="sm"
                      value={breakglassKeys}
                      onChange={(event) => setBreakglassKeys(event.target.value)}
                      placeholder="ssh-ed25519 AAAA..."
                    />
                    <Button size="sm" variant="brand" type="submit" loading={savingBreakglass}>
                      Save keys
                    </Button>
                  </VStack>
                </form>

                {/* Tailscale */}
                <Stack gap={2}>
                  <Text fontWeight="medium" color="fg.default">Tailscale auth key</Text>
                  <HStack>
                    <Text fontSize="sm" color="fg.muted">
                      Stored: {tailscaleAuthKeyConfigured ? 'configured' : 'not configured'}
                    </Text>
                    <Badge bg={tailscaleAuthKeyConfigured ? 'success.bg' : 'warning.bg'} color={tailscaleAuthKeyConfigured ? 'success' : 'warning'}>
                      {tailscaleAuthKeyConfigured ? 'ready' : 'missing'}
                    </Badge>
                  </HStack>
                  <Text fontSize="xs" color="fg.subtle">
                    Enter a key only when updating. Saved at {tailscaleAuthKeyPath}.
                  </Text>
                  <Input
                    type="password"
                    autoComplete="off"
                    value={tailscaleAuthKey}
                    onChange={(event) => setTailscaleAuthKey(event.target.value)}
                    placeholder="tskey-auth-..."
                  />
                </Stack>

                {/* OTA Update */}
                <Stack gap={2}>
                  <HStack justify="space-between" align="center">
                    <Text fontWeight="medium" color="fg.default">System update</Text>
                    <Badge 
                      bg={otaStatus?.state === 'failed' ? 'danger.bg' : otaBusy ? 'accent.muted' : 'success.bg'}
                      color={otaStatus?.state === 'failed' ? 'danger' : otaBusy ? 'accent' : 'success'}
                    >
                      {otaStatus?.state || 'idle'}
                    </Badge>
                  </HStack>
                  <Text fontSize="sm" color="fg.muted">
                    {otaStatus?.release ? `${otaStatus.release} · ${otaStatus.asset || 'OTA image'}` : 'Latest GitHub release'}
                  </Text>
                  {otaStatus?.message && <Text fontSize="sm" color="fg.default">{otaStatus.message}</Text>}
                  {otaStatus?.error && <Text fontSize="sm" color="danger">{otaStatus.error}</Text>}
                  <HStack>
                    <Button size="sm" variant="brand" onClick={onInstallOta} loading={installingOta || otaBusy}>
                      Install latest OTA
                    </Button>
                    <Button size="sm" onClick={load} loading={loading} variant="outline">
                      Re-check
                    </Button>
                  </HStack>
                </Stack>
              </Stack>
            </CardBody>
          </Card>
        </details>
      </CardBody>
    </Card>
  )
}
