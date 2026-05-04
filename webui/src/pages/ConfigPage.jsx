import { useEffect, useState } from 'react'
import {
  AlertRoot as Alert,
  AlertDescription,
  AlertIndicator,
  Badge,
  Button,
  CardRoot as Card,
  CardBody,
  CardHeader,
  Heading,
  HStack,
  Input,
  ListItem,
  ListRoot,
  Spinner,
  Stack,
  Text,
  Textarea,
  VStack
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
  testConfig
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
        getOtaStatus(deviceUrl)
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
        tailscale_auth_key: tailscaleAuthKey
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
    <Card bg="transparent">
      <CardHeader>
        <Heading size="lg" className="section-title">Sync settings</Heading>
      </CardHeader>
      <CardBody>
        <Card className="section-card" variant="panel">
          <CardBody>
            <HStack justify="space-between" align="center">
              <div>
                <Text className="section-title">Destination</Text>
                <Text className="form-subtitle">Choose where backups are uploaded.</Text>
              </div>
              <Button size="sm" onClick={load} isLoading={loading}>
                Refresh
              </Button>
            </HStack>

            <Card className="section-card" mt={4} variant="panel">
              <CardBody>
                <HStack justify="space-between" wrap="wrap">
                  <Text fontWeight="medium">Configured destination</Text>
                  <Badge colorScheme={config.configured ? 'green' : 'orange'}>
                    {config.configured ? 'Configured' : 'Not configured'}
                  </Badge>
                </HStack>
                <Text mt={2} fontSize="sm" color="gray.300">
                  Remotes detected: {config.remotes?.length || 0}
                </Text>
                <ListRoot mt={2} spacing={1} color="gray.100">
                  {(config.remotes || []).map((item) => (
                    <ListItem key={item}>• {item}</ListItem>
                  ))}
                  {(config.remotes || []).length === 0 ? <ListItem>No remote entries found.</ListItem> : null}
                </ListRoot>
              </CardBody>
            </Card>

            <form onSubmit={onSave}>
              <Stack mt={4} spacing={4}>
                <Stack spacing={1}>
                  <Text color="gray.200" fontSize="sm">
                    Remote name
                  </Text>
                  <Input value={remoteName} onChange={(event) => setRemoteName(event.target.value)} />
                </Stack>

                <Stack spacing={1}>
                  <Text color="gray.200" fontSize="sm">
                    Remote path
                  </Text>
                  <Input value={remotePath} onChange={(event) => setRemotePath(event.target.value)} />
                </Stack>

                <HStack>
                  <Stack spacing={1} flex={1}>
                    <Text color="gray.200" fontSize="sm">
                      Reformat threshold
                    </Text>
                    <Input
                      type="number"
                      value={reformatThreshold}
                      onChange={(event) => setReformatThreshold(event.target.valueAsNumber)}
                    />
                  </Stack>
                  <Stack spacing={1}>
                    <Text color="gray.200" fontSize="sm">
                      Transfers
                    </Text>
                    <Input type="number" value={transfers} onChange={(event) => setTransfers(event.target.valueAsNumber)} />
                  </Stack>
                  <Stack spacing={1}>
                    <Text color="gray.200" fontSize="sm">
                      Checkers
                    </Text>
                    <Input type="number" value={checkers} onChange={(event) => setCheckers(event.target.valueAsNumber)} />
                  </Stack>
                </HStack>

                <VStack align="start" spacing={1}>
                  <Text color="gray.200" fontSize="sm">
                    Google Photos sync
                  </Text>
                  <HStack>
                    <input
                      type="checkbox"
                      checked={googlePhotosEnabled}
                      onChange={(event) => setGooglePhotosEnabled(event.target.checked)}
                    />
                    <Text>Also upload a copy to Google Photos</Text>
                  </HStack>
                </VStack>

                <Stack spacing={1}>
                  <Text color="gray.200" fontSize="sm">
                    Google Photos remote
                  </Text>
                  <Input
                    isDisabled={!googlePhotosEnabled}
                    value={googlePhotosRemoteName}
                    onChange={(event) => setGooglePhotosRemoteName(event.target.value)}
                    placeholder="gphotos"
                  />
                </Stack>

                <HStack mt={2} spacing={2}>
                  <Button type="submit" variant="brand" isLoading={saving} className="primary-cta">
                    Save destination
                  </Button>
                  <Button type="button" onClick={onTest} isLoading={testing} variant="outline">
                    Test destination
                  </Button>
                </HStack>
              </Stack>
            </form>

            {loading && (
              <Stack mt={4} spacing={2}>
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

        <details className="advanced-connection">
          <summary>Advanced options</summary>
          <Card className="section-card" mt={3} variant="panel">
            <CardHeader>
              <Heading size="sm">Security and maintenance</Heading>
            </CardHeader>
            <CardBody>
              <Stack spacing={4}>
                <form onSubmit={onChangeGokrazyPassword}>
                  <VStack align="stretch" spacing={3}>
                    <Text fontWeight="medium">Gokrazy web password</Text>
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
                    <Button type="submit" size="sm" variant="brand" isLoading={savingGokrazyPassword}>
                      Update password
                    </Button>
                  </VStack>
                </form>

                <form onSubmit={(event) => {
                  event.preventDefault()
                  onSaveBreakglass()
                }}>
                  <VStack align="stretch" spacing={2}>
                    <Text fontWeight="medium">Breakglass SSH keys</Text>
                    <Text fontSize="sm" color="gray.300">
                      {breakglassPath}
                    </Text>
                    <Textarea
                      mt={2}
                      minH="120px"
                      fontFamily="mono"
                      value={breakglassKeys}
                      onChange={(event) => setBreakglassKeys(event.target.value)}
                      placeholder="ssh-ed25519 AAAA..."
                    />
                    <HStack spacing={2}>
                    <Button size="sm" variant="brand" type="submit" isLoading={savingBreakglass}>
                      Save keys
                    </Button>
                    </HStack>
                  </VStack>
                </form>

                <Stack spacing={2}>
                  <Text fontWeight="medium">Tailscale auth key</Text>
                  <HStack>
                    <Text fontSize="sm" color="gray.300">
                      Stored: {tailscaleAuthKeyConfigured ? 'configured' : 'not configured'}
                    </Text>
                    <Badge colorScheme={tailscaleAuthKeyConfigured ? 'green' : 'orange'}>
                      {tailscaleAuthKeyConfigured ? 'ready' : 'missing'}
                    </Badge>
                  </HStack>
                  <Text fontSize="xs" color="gray.400">
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

                <Stack spacing={2}>
                  <HStack justify="space-between" align="center">
                    <Text fontWeight="medium">System update</Text>
                    <Badge colorScheme={otaStatus?.state === 'failed' ? 'red' : otaBusy ? 'blue' : 'green'}>
                      {otaStatus?.state || 'idle'}
                    </Badge>
                  </HStack>
                  <Text fontSize="sm" color="gray.300">
                    {otaStatus?.release ? `${otaStatus.release} · ${otaStatus.asset || 'OTA image'}` : 'Latest GitHub release'}
                  </Text>
                  {otaStatus?.message ? <Text fontSize="sm">{otaStatus.message}</Text> : null}
                  {otaStatus?.error ? <Text color="red.200" fontSize="sm">{otaStatus.error}</Text> : null}
                  <HStack>
                    <Button size="sm" variant="brand" onClick={onInstallOta} isLoading={installingOta || otaBusy}>
                      Install latest OTA
                    </Button>
                    <Button size="sm" onClick={load} isLoading={loading} variant="outline">
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
