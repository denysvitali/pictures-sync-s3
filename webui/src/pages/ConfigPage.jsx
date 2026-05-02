import { useEffect, useState } from 'react'
import {
  Alert,
  AlertDescription,
  AlertIndicator,
  Badge,
  Button,
  Card,
  CardBody,
  Checkbox,
  Heading,
  HStack,
  Input,
  List,
  ListItem,
  Spinner,
  Stack,
  Text,
  Wrap,
  WrapItem
} from '@chakra-ui/react'
import { getConfig, getSettings, saveSettings, testConfig } from '../api'

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

  const load = async () => {
    if (!deviceUrl) return
    setLoading(true)
    setError('')
    try {
      const [configResponse, settingsResponse] = await Promise.all([
        getConfig(deviceUrl),
        getSettings(deviceUrl)
      ])
      setConfig(configResponse || { configured: false, remotes: [] })
      setRemoteName(settingsResponse?.remote_name || '')
      setRemotePath(settingsResponse?.remote_path || '')
      setReformatThreshold(toNumberOrDefault(settingsResponse?.reformat_threshold, 0.3))
      setTransfers(toNumberOrDefault(settingsResponse?.transfers, 4))
      setCheckers(toNumberOrDefault(settingsResponse?.checkers, 8))
      setGooglePhotosEnabled(Boolean(settingsResponse?.google_photos_enabled))
      setGooglePhotosRemoteName(settingsResponse?.google_photos_remote_name || '')
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
        google_photos_remote_name: googlePhotosRemoteName
      })
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
                  <Checkbox
                    isChecked={googlePhotosEnabled}
                    onChange={(event) => setGooglePhotosEnabled(event.target.checked)}
                  >
                    Enable Google Photos
                  </Checkbox>
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
        </Stack>
      </CardBody>
    </Card>
  )
}
