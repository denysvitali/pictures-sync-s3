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
  getBreakglassAuthorizedKeys,
  getConfig,
  getOtaStatus,
  getSettings,
  installOta,
  saveBreakglassAuthorizedKeys,
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

  const load = async () => {
    if (!deviceUrl) return
    setLoading(true)
    setError('')
    try {
      const [configResponse, settingsResponse, breakglassResponse, otaResponse] = await Promise.all([
        getConfig(deviceUrl),
        getSettings(deviceUrl),
        getBreakglassAuthorizedKeys(deviceUrl),
        getOtaStatus(deviceUrl),
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
