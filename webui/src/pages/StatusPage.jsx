import { useEffect, useMemo, useState } from 'react'
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
  WrapItem,
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
    <VStack align="stretch" gap={4}>
      <Card variant="panel">
        <CardHeader>
          <Flex justify="space-between" align="center" gap={3} wrap="wrap">
            <Heading size="sm">System status</Heading>
            <HStack gap={2}>
              <Button size="sm" variant="brand" onClick={load} loading={loading}>
                Refresh
              </Button>
              <Button
                size="sm"
                variant="solid"
                bg="accent.alt"
                color="gray.900"
                onClick={start}
                loading={syncing}
                loadingText="Starting"
                disabled={isSyncing}
              >
                Start sync
              </Button>
              <Button
                size="sm"
                variant="outline"
                borderColor="danger"
                color="danger"
                onClick={cancel}
                loading={canceling}
                loadingText="Cancelling"
                disabled={!isSyncing}
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
          <VStack align="stretch" gap={4}>
            <Wrap gap={3}>
              <WrapItem>
                <Badge bg="accent.muted" color="accent">
                  State: {status?.status || 'unknown'}
                </Badge>
              </WrapItem>
              <WrapItem>
                <Badge variant="outline" borderColor="border.muted" color="fg.default">
                  SD card: {status?.sdcard_mounted ? 'mounted' : 'not mounted'}
                </Badge>
              </WrapItem>
              <WrapItem>
                <Badge variant="outline" borderColor="border.muted" color="fg.muted">
                  Device select: {status?.needs_device_select ? 'required' : 'not needed'}
                </Badge>
              </WrapItem>
            </Wrap>

            <TableRoot size="sm" variant="line">
              <TableHeader>
                <TableRow>
                  <TableColumnHeader color="fg.muted">Field</TableColumnHeader>
                  <TableColumnHeader color="fg.muted">Value</TableColumnHeader>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow>
                  <TableCell color="fg.muted">SD card mount point</TableCell>
                  <TableCell fontFamily="mono" color="fg.default">{status?.sdcard_path || 'n/a'}</TableCell>
                </TableRow>
                <TableRow>
                  <TableCell color="fg.muted">Current file</TableCell>
                  <TableCell fontFamily="mono" color="fg.default">{status?.current_sync?.current_file || 'n/a'}</TableCell>
                </TableRow>
                <TableRow>
                  <TableCell color="fg.muted">Synced</TableCell>
                  <TableCell color="fg.default">
                    {rowText(status?.current_sync?.files_synced)} / {rowText(status?.current_sync?.files_total)}
                  </TableCell>
                </TableRow>
                <TableRow>
                  <TableCell color="fg.muted">Transfer speed</TableCell>
                  <TableCell color="fg.default">
                    {status?.current_sync?.transfer_speed ? `${rowText(status.current_sync.transfer_speed)} B/s` : '—'}
                  </TableCell>
                </TableRow>
                <TableRow>
                  <TableCell color="fg.muted">ETA</TableCell>
                  <TableCell color="fg.default">{status?.current_sync?.eta || '—'}</TableCell>
                </TableRow>
              </TableBody>
            </TableRoot>
          </VStack>
        </CardBody>
      </Card>

      <Card variant="panel">
        <CardHeader>
          <Flex justify="space-between" align="center">
            <Heading size="sm">Recent history</Heading>
            <Badge bg="accentMuted" color="accent">{history.length}</Badge>
          </Flex>
        </CardHeader>
        <CardBody pt={0}>
          {loading && history.length === 0 ? (
            <Spinner size="sm" />
          ) : (
            <TableRoot variant="line" size="sm">
              <TableHeader>
                <TableRow>
                  <TableColumnHeader color="fg.muted">Started</TableColumnHeader>
                  <TableColumnHeader color="fg.muted">Status</TableColumnHeader>
                  <TableColumnHeader color="fg.muted">Message</TableColumnHeader>
                  <TableColumnHeader color="fg.muted" textAlign="right">Count</TableColumnHeader>
                </TableRow>
              </TableHeader>
              <TableBody>
                {latestHistory.map((row) => (
                  <TableRow key={row.id || row.start_time}>
                    <TableCell fontFamily="mono" color="fg.default">{prettyDate(row.start_time)}</TableCell>
                    <TableCell>
                      <Badge 
                        bg={row.status === 'success' ? 'success.bg' : row.status === 'error' ? 'danger.bg' : 'warning.bg'}
                        color={row.status === 'success' ? 'success' : row.status === 'error' ? 'danger' : 'warning'}
                      >
                        {row.status || 'n/a'}
                      </Badge>
                    </TableCell>
                    <TableCell color="fg.default">{row.error || 'ok'}</TableCell>
                    <TableCell textAlign="right" color="fg.default">
                      {row.files_synced != null ? `${row.files_synced}/${row.files_total}` : '—'}
                    </TableCell>
                  </TableRow>
                ))}
                {latestHistory.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={4}>
                      <Text color="fg.subtle">No history entries yet.</Text>
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
