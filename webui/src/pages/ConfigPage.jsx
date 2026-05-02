import { useEffect, useState } from 'react'
import {
  AlertRoot as Alert,
  AlertDescription,
  AlertIndicator,
  Badge,
  Button,
  CardRoot as Card,
  CardBody,
  Heading,
  HStack,
  Input,
  ListRoot as List,
  ListItem,
  Spinner,
  Stack,
  Text,
  Textarea,
  VStack,
  Wrap,
  WrapItem
} from '@chakra-ui/react'
import {
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
      setMessage('Settings saved.')
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
        setMessage('Connection test succeeded.')
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
    <Card bg="whiteAlpha.50">
      <CardBody>
        <HStack justify="space-between" align="center">
          <Heading size="sm">Configuration</Heading>
          <Button size="sm" onClick={load} isLoading={loading}>
            Refresh
          </Button>
        </HStack>

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

        <Stack mt={4} spacing={5}>
          <Card bg="whiteAlpha.100" variant="outline">
            <CardBody>
              <HStack justify="space-between">
                <Text fontWeight="medium">Configured remote</Text>
                <Badge colorScheme={config.configured ? 'green' : 'orange'}>
                  {config.configured ? 'configured' : 'not configured'}
                </Badge>
              </HStack>
              <Text mt={2} fontSize="sm" color="gray.300">
                Remotes detected: {config.remotes?.length || 0}
              </Text>
              <List mt={2} spacing={1} color="gray.100">
                {(config.remotes || []).map((item) => (
                  <ListItem key={item}>• {item}</ListItem>
                ))}
                {(config.remotes || []).length === 0 ? <ListItem>No remotes found.</ListItem> : null}
              </List>
            </CardBody>
          </Card>

          <form onSubmit={onSave}>
            <Card variant="outline" bg="whiteAlpha.100">
              <CardBody>
                <Heading size="sm" mb={3}>
                  Sync settings
                </Heading>
                <Stack spacing={3}>
                  <VStack align="start" spacing={1}>
                    <Text color="gray.200" fontSize="sm">
                      Remote name
                    </Text>
                    <Input value={remoteName} onChange={(event) => setRemoteName(event.target.value)} />
                  </VStack>
                  <VStack align="start" spacing={1}>
                    <Text color="gray.200" fontSize="sm">
                      Remote path
                    </Text>
                    <Input value={remotePath} onChange={(event) => setRemotePath(event.target.value)} />
                  </VStack>
                  <VStack align="start" spacing={1}>
                    <Text color="gray.200" fontSize="sm">
                      Reformat threshold
                    </Text>
                    <Input
                      type="number"
                      value={reformatThreshold}
                      onChange={(event) => setReformatThreshold(event.target.valueAsNumber)}
                    />
                  </VStack>
                  <Wrap spacing={3}>
                    <WrapItem>
                      <VStack align="start" spacing={1}>
                        <Text color="gray.200" fontSize="sm">
                          Transfers
                        </Text>
                        <Input
                          type="number"
                          value={transfers}
                          onChange={(event) => setTransfers(event.target.valueAsNumber)}
                        />
                      </VStack>
                    </WrapItem>
                    <WrapItem>
                      <VStack align="start" spacing={1}>
                        <Text color="gray.200" fontSize="sm">
                          Checkers
                        </Text>
                        <Input
                          type="number"
                          value={checkers}
                          onChange={(event) => setCheckers(event.target.valueAsNumber)}
                        />
                      </VStack>
                    </WrapItem>
                  </Wrap>
                  <HStack as="label" spacing={2} color="gray.100">
                    <Input
                      type="checkbox"
                      width="auto"
                      checked={googlePhotosEnabled}
                      onChange={(event) => setGooglePhotosEnabled(event.target.checked)}
                    />
                    <Text>Enable Google Photos</Text>
                  </HStack>
                  <VStack align="start" spacing={1}>
                    <Text color="gray.200" fontSize="sm">
                      Google Photos remote
                    </Text>
                    <Input
                      isDisabled={!googlePhotosEnabled}
                      value={googlePhotosRemoteName}
                      onChange={(event) => setGooglePhotosRemoteName(event.target.value)}
                      placeholder="gphotos"
                    />
                  </VStack>
                  <VStack align="start" spacing={1}>
                    <HStack justify="space-between" width="100%">
                      <Text color="gray.200" fontSize="sm">
                        Tailscale auth key
                      </Text>
                      <Badge colorScheme={tailscaleAuthKeyConfigured ? 'green' : 'orange'}>
                        {tailscaleAuthKeyConfigured ? 'configured' : 'not configured'}
                      </Badge>
                    </HStack>
                    <Input
                      type="password"
                      autoComplete="off"
                      value={tailscaleAuthKey}
                      onChange={(event) => setTailscaleAuthKey(event.target.value)}
                      placeholder="tskey-auth-..."
                    />
                    <Text fontSize="xs" color="gray.400">
                      Stored at {tailscaleAuthKeyPath}
                    </Text>
                  </VStack>
                </Stack>
                <HStack mt={4} spacing={2}>
                  <Button type="submit" colorScheme="teal" isLoading={saving}>
                    Save settings
                  </Button>
                  <Button type="button" onClick={onTest} isLoading={testing} variant="outline">
                    Test connection
                  </Button>
                </HStack>
              </CardBody>
            </Card>
          </form>

          <Card variant="outline" bg="whiteAlpha.100">
            <CardBody>
              <HStack justify="space-between" align="start">
                <VStack align="start" spacing={1}>
                  <Heading size="sm">System update</Heading>
                  <Text fontSize="sm" color="gray.300">
                    {otaStatus?.release ? `${otaStatus.release} · ${otaStatus.asset || 'OTA image'}` : 'Latest GitHub release'}
                  </Text>
                </VStack>
                <Badge colorScheme={otaStatus?.state === 'failed' ? 'red' : otaBusy ? 'blue' : 'green'}>
                  {otaStatus?.state || 'idle'}
                </Badge>
              </HStack>
              {otaStatus?.message ? (
                <Text mt={3} fontSize="sm" color="gray.200">
                  {otaStatus.message}
                </Text>
              ) : null}
              {otaStatus?.error ? (
                <Text mt={3} fontSize="sm" color="red.200">
                  {otaStatus.error}
                </Text>
              ) : null}
              <HStack mt={4} spacing={2}>
                <Button colorScheme="teal" onClick={onInstallOta} isLoading={installingOta || otaBusy}>
                  Install latest OTA
                </Button>
                <Button variant="outline" onClick={load} isLoading={loading}>
                  Refresh
                </Button>
              </HStack>
            </CardBody>
          </Card>

          <Card variant="outline" bg="whiteAlpha.100">
            <CardBody>
              <HStack justify="space-between">
                <Heading size="sm">Breakglass SSH keys</Heading>
                <Badge colorScheme={breakglassKeys.trim() ? 'green' : 'orange'}>
                  {breakglassKeys.trim() ? 'configured' : 'not configured'}
                </Badge>
              </HStack>
              <Text mt={2} fontSize="sm" color="gray.300">
                {breakglassPath}
              </Text>
              <Textarea
                mt={3}
                minH="160px"
                fontFamily="mono"
                value={breakglassKeys}
                onChange={(event) => setBreakglassKeys(event.target.value)}
                placeholder="ssh-ed25519 AAAA..."
              />
              <HStack mt={4} spacing={2}>
                <Button colorScheme="teal" onClick={onSaveBreakglass} isLoading={savingBreakglass}>
                  Save keys
                </Button>
              </HStack>
            </CardBody>
          </Card>
        </Stack>
      </CardBody>
    </Card>
  )
}
