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
  NativeSelectField,
  NativeSelectRoot,
  Spinner,
  Stack,
  TableBody,
  TableCell,
  TableColumnHeader,
  TableHeader,
  TableRoot,
  TableRow,
  Text
} from '@chakra-ui/react'
import {
  connectWifi,
  disconnectWifi,
  getWifiNetworks,
  getWifiStatus,
  reorderWifi,
  scanWifi
} from '../api'

function networkId(network, index) {
  return network.ssid || network.SSID || network.path || network.networkId || network.id || index
}

export function WifiPage({ deviceUrl }) {
  const [status, setStatus] = useState(null)
  const [networks, setNetworks] = useState([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [scanBusy, setScanBusy] = useState(false)
  const [reorderBusy, setReorderBusy] = useState(false)
  const [sortBy, setSortBy] = useState('signal')
  const [ssid, setSsid] = useState('')
  const [password, setPassword] = useState('')
  const [connecting, setConnecting] = useState(false)
  const [disconnecting, setDisconnecting] = useState('')

  const load = async () => {
    if (!deviceUrl) return
    setLoading(true)
    setError('')
    try {
      const [statusResponse, networksResponse] = await Promise.all([getWifiStatus(deviceUrl), getWifiNetworks(deviceUrl)])
      setStatus(statusResponse)
      setNetworks(Array.isArray(networksResponse?.networks) ? networksResponse.networks : [])
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [deviceUrl])

  const handleScan = async () => {
    setScanBusy(true)
    try {
      const response = await scanWifi(deviceUrl, sortBy)
      setNetworks(Array.isArray(response?.networks) ? response.networks : [])
    } catch (err) {
      setError(err.message)
    } finally {
      setScanBusy(false)
    }
  }

  const handleConnect = async (event) => {
    event.preventDefault()
    setConnecting(true)
    setError('')
    try {
      const result = await connectWifi(deviceUrl, ssid, password)
      if (result?.success) {
        setSsid('')
        setPassword('')
        await load()
      } else {
        setError(result?.error || 'Failed to connect')
      }
    } catch (err) {
      setError(err.message)
    } finally {
      setConnecting(false)
    }
  }

  const handleDisconnect = async (networkSsid) => {
    setDisconnecting(networkSsid)
    setError('')
    try {
      const response = await disconnectWifi(deviceUrl, networkSsid)
      if (response?.success) {
        await load()
      } else {
        setError(response?.error || 'Failed to remove network')
      }
    } catch (err) {
      setError(err.message)
    } finally {
      setDisconnecting('')
    }
  }

  const move = (index, direction) => {
    const next = [...networks]
    const target = index + direction
    if (target < 0 || target >= next.length) return
    const temp = next[index]
    next[index] = next[target]
    next[target] = temp
    setNetworks(next)
  }

  const saveOrder = async () => {
    setReorderBusy(true)
    setError('')
    try {
      const response = await reorderWifi(deviceUrl, networks.map((network) => network.ssid))
      if (!response?.success) {
        setError(response?.error || 'Failed to reorder')
      }
    } catch (err) {
      setError(err.message)
    } finally {
      setReorderBusy(false)
    }
  }

  return (
    <Stack spacing={4} align="stretch">
      <Card variant="panel">
        <CardBody>
          <Heading size="sm">Wi-Fi status</Heading>
          {loading ? (
            <Spinner mt={2} />
          ) : (
            <HStack mt={2} spacing={3} wrap="wrap">
              <Badge colorScheme={status?.connected ? 'green' : 'red'}>
                {status?.connected ? `Connected: ${status.ssid}` : 'Not connected'}
              </Badge>
              <Badge colorScheme="purple">
                signal: {status?.signal ?? 'n/a'}
              </Badge>
            </HStack>
          )}
        </CardBody>
      </Card>

      <Card variant="panel">
        <CardBody>
          <Heading size="sm" mb={3}>
            Saved networks
          </Heading>
          <form onSubmit={handleConnect}>
            <Stack spacing={2}>
              <HStack spacing={2}>
                <Input
                  value={ssid}
                  onChange={(event) => setSsid(event.target.value)}
                  placeholder="SSID"
                  size="sm"
                />
                <Input
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  placeholder="Password (optional)"
                  size="sm"
                  type="password"
                />
                <Button type="submit" variant="brand" size="sm" isLoading={connecting}>
                  Connect
                </Button>
              </HStack>
            </Stack>
          </form>

          <HStack mt={3} spacing={2} wrap="wrap">
            <NativeSelectRoot size="sm" width="180px">
              <NativeSelectField value={sortBy} onChange={(event) => setSortBy(event.target.value)}>
                <option value="signal">Signal</option>
                <option value="name">Name</option>
                <option value="security">Security</option>
              </NativeSelectField>
            </NativeSelectRoot>
            <Button size="sm" variant="outline" onClick={handleScan} isLoading={scanBusy}>
              Scan saved networks
            </Button>
            <Button size="sm" variant="brand" onClick={saveOrder} isLoading={reorderBusy} isDisabled={networks.length < 2}>
              Save order
            </Button>
            <Button size="sm" onClick={load} isLoading={loading} variant="ghost">
              Refresh
            </Button>
          </HStack>

          {error ? (
            <Alert status="error" mt={3}>
              <AlertIndicator />
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}

          <TableRoot mt={3} size="sm" variant="simple">
            <TableHeader>
              <TableRow>
                <TableColumnHeader>SSID</TableColumnHeader>
                <TableColumnHeader>Has password</TableColumnHeader>
                <TableColumnHeader>Actions</TableColumnHeader>
                <TableColumnHeader>Reorder</TableColumnHeader>
              </TableRow>
            </TableHeader>
            <TableBody>
              {networks.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} color="gray.300">
                    No saved networks found.
                  </TableCell>
                </TableRow>
              ) : null}
              {networks.map((network, index) => (
                <TableRow key={`${networkId(network, index)}`}>
                  <TableCell>{network.ssid || network.SSID || 'Unknown network'}</TableCell>
                  <TableCell>{network.has_password ? 'yes' : 'no'}</TableCell>
                  <TableCell>
                    <Button
                      size="xs"
                      colorScheme="red"
                      variant="outline"
                      onClick={() => handleDisconnect(network.ssid)}
                      isLoading={disconnecting === network.ssid}
                    >
                      Remove
                    </Button>
                  </TableCell>
                  <TableCell>
                    <HStack>
                      <Button size="xs" onClick={() => move(index, -1)} isDisabled={index === 0}>
                        ↑
                      </Button>
                      <Button size="xs" onClick={() => move(index, 1)} isDisabled={index === networks.length - 1}>
                        ↓
                      </Button>
                    </HStack>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </TableRoot>
        </CardBody>
      </Card>
    </Stack>
  )
}
