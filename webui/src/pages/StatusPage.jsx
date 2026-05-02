import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  AlertDescription,
  AlertIndicator,
  Badge,
  Button,
  Card,
  CardBody,
  CardHeader,
  Flex,
  HStack,
  Heading,
  Spinner,
  TableBody,
  TableCell,
  TableColumnHeader,
  TableHeader,
  TableRoot,
  TableRow,
  Text,
  VStack,
  Wrap,
  WrapItem
} from '@chakra-ui/react'
import { cancelSync, getHistory, getStatus, startSync } from '../api'

function prettyDate(value) {
  if (!value) return 'n/a'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toLocaleString()
}

function rowText(value) {
  if (value === undefined || value === null) return '—'
  if (typeof value === 'number') return value.toLocaleString()
  return String(value)
}

export function StatusPage({ deviceUrl }) {
  const [status, setStatus] = useState(null)
  const [history, setHistory] = useState([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [actionMessage, setActionMessage] = useState('')
  const [syncing, setSyncing] = useState(false)
  const [canceling, setCanceling] = useState(false)

  const isSyncing = status?.status === 'syncing'

  const load = async () => {
    if (!deviceUrl) return
    setLoading(true)
    setError('')
    try {
      const [statusResponse, historyResponse] = await Promise.all([getStatus(deviceUrl), getHistory(deviceUrl)])
      setStatus(statusResponse)
      setHistory(Array.isArray(historyResponse) ? historyResponse : [])
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [deviceUrl])

  const start = async () => {
    setSyncing(true)
    setActionMessage('')
    try {
      await startSync(deviceUrl)
      setActionMessage('Sync started.')
      await load()
    } catch (err) {
      setActionMessage(err.message)
    } finally {
      setSyncing(false)
    }
  }

  const cancel = async () => {
    setCanceling(true)
    setActionMessage('')
    try {
      await cancelSync(deviceUrl)
      setActionMessage('Sync cancelled.')
      await load()
    } catch (err) {
      setActionMessage(err.message)
    } finally {
      setCanceling(false)
    }
  }

  const latestHistory = useMemo(() => history.slice(0, 6), [history])

  return (
    <VStack spacing={4} align="stretch">
      <Card bg="whiteAlpha.50">
        <CardHeader>
          <Flex justify="space-between" align="center" gap={3} wrap="wrap">
            <Heading size="sm">System status</Heading>
            <HStack spacing={2}>
              <Button size="sm" colorScheme="teal" onClick={load} isLoading={loading}>
                Refresh
              </Button>
              <Button
                size="sm"
                colorScheme="green"
                onClick={start}
                isLoading={syncing}
                loadingText="Starting"
                isDisabled={isSyncing}
              >
                Start sync
              </Button>
              <Button
                size="sm"
                colorScheme="red"
                onClick={cancel}
                isLoading={canceling}
                loadingText="Cancelling"
                isDisabled={!isSyncing}
                variant="outline"
              >
                Cancel sync
              </Button>
            </HStack>
          </Flex>
        </CardHeader>
        <CardBody pt={0}>
          {actionMessage ? (
            <Alert status={error ? 'error' : 'info'} mb={4}>
              <AlertIndicator />
              <AlertDescription>{actionMessage || error}</AlertDescription>
            </Alert>
          ) : null}
          {error ? (
            <Alert status="error" mb={4}>
              <AlertIndicator />
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
          <VStack align="stretch" spacing={4}>
            <Wrap spacing={3}>
              <WrapItem>
                <Badge size="lg" variant="solid" colorScheme="teal">
                  State: {status?.status || 'unknown'}
                </Badge>
              </WrapItem>
              <WrapItem>
                <Badge size="lg" variant="outline" colorScheme="blue">
                  SD card: {status?.sdcard_mounted ? 'mounted' : 'not mounted'}
                </Badge>
              </WrapItem>
              <WrapItem>
                <Badge size="lg" variant="outline" colorScheme="purple">
                  Device select: {status?.needs_device_select ? 'required' : 'not needed'}
                </Badge>
              </WrapItem>
            </Wrap>

            <TableRoot size="sm" variant="simple">
              <TableHeader>
                <TableRow>
                  <TableColumnHeader>Field</TableColumnHeader>
                  <TableColumnHeader>Value</TableColumnHeader>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow>
                  <TableCell>SD card mount point</TableCell>
                  <TableCell fontFamily="mono">{status?.sdcard_path || 'n/a'}</TableCell>
                </TableRow>
                <TableRow>
                  <TableCell>Current file</TableCell>
                  <TableCell fontFamily="mono">{status?.current_sync?.current_file || 'n/a'}</TableCell>
                </TableRow>
                <TableRow>
                  <TableCell>Synced</TableCell>
                  <TableCell>
                    {rowText(status?.current_sync?.files_synced)} / {rowText(status?.current_sync?.files_total)}
                  </TableCell>
                </TableRow>
                <TableRow>
                  <TableCell>Transfer speed</TableCell>
                  <TableCell>
                    {status?.current_sync?.transfer_speed ? `${rowText(status.current_sync.transfer_speed)} B/s` : '—'}
                  </TableCell>
                </TableRow>
                <TableRow>
                  <TableCell>ETA</TableCell>
                  <TableCell>{status?.current_sync?.eta || '—'}</TableCell>
                </TableRow>
              </TableBody>
            </TableRoot>
          </VStack>
        </CardBody>
      </Card>

      <Card bg="whiteAlpha.30">
        <CardHeader>
          <Flex justify="space-between" align="center">
            <Heading size="sm">Recent history</Heading>
            <Badge colorScheme="teal" variant="subtle">
              {history.length}
            </Badge>
          </Flex>
        </CardHeader>
        <CardBody pt={0}>
          {loading && history.length === 0 ? (
            <Spinner size="sm" />
          ) : (
            <TableRoot variant="simple" size="sm">
              <TableHeader>
                <TableRow>
                  <TableColumnHeader>Started</TableColumnHeader>
                  <TableColumnHeader>Status</TableColumnHeader>
                  <TableColumnHeader>Message</TableColumnHeader>
                  <TableColumnHeader textAlign="right">Count</TableColumnHeader>
                </TableRow>
              </TableHeader>
              <TableBody>
                {latestHistory.map((row) => (
                  <TableRow key={row.id || row.start_time}>
                    <TableCell fontFamily="mono">{prettyDate(row.start_time)}</TableCell>
                    <TableCell>
                      <Badge colorScheme={row.status === 'success' ? 'green' : row.status === 'error' ? 'red' : 'yellow'}>
                        {row.status || 'n/a'}
                      </Badge>
                    </TableCell>
                    <TableCell>{row.error || 'ok'}</TableCell>
                    <TableCell textAlign="right">
                      {row.files_synced != null ? `${row.files_synced}/${row.files_total}` : '—'}
                    </TableCell>
                  </TableRow>
                ))}
                {latestHistory.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={4}>
                      <Text color="gray.300">No history entries yet.</Text>
                    </TableCell>
                  </TableRow>
                ) : null}
              </TableBody>
            </TableRoot>
          )}
        </CardBody>
      </Card>
    </VStack>
  )
}
