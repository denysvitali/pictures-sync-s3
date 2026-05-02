import { useEffect, useState } from 'react'
import {
  Alert,
  AlertDescription,
  AlertIndicator,
  Card,
  CardBody,
  Heading,
  Spinner,
  TableBody,
  TableCell,
  TableColumnHeader,
  TableHeader,
  TableRoot,
  TableRow,
  Text
} from '@chakra-ui/react'
import { getHistory } from '../api'

function formatDate(value) {
  if (!value) return 'n/a'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return value
  }
  return parsed.toLocaleString()
}

export function HistoryPage({ deviceUrl }) {
  const [history, setHistory] = useState([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!deviceUrl) return
    const load = async () => {
      setLoading(true)
      setError('')
      try {
        const response = await getHistory(deviceUrl)
        setHistory(Array.isArray(response) ? response : [])
      } catch (err) {
        setError(err.message)
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [deviceUrl])

  return (
    <Card bg="whiteAlpha.30">
      <CardBody>
        <Heading size="sm" mb={4}>
          Sync history
        </Heading>

        {error ? (
          <Alert status="error" mb={4}>
            <AlertIndicator />
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        {loading ? (
          <Spinner />
        ) : (
          <TableRoot size="sm" variant="simple">
            <TableHeader>
              <TableRow>
                <TableColumnHeader>Started</TableColumnHeader>
                <TableColumnHeader>Status</TableColumnHeader>
                <TableColumnHeader>Message</TableColumnHeader>
                <TableColumnHeader textAlign="right">Files</TableColumnHeader>
                <TableColumnHeader textAlign="right">Bytes</TableColumnHeader>
              </TableRow>
            </TableHeader>
            <TableBody>
              {history.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5}>
                    <Text color="gray.300">No history available.</Text>
                  </TableCell>
                </TableRow>
              ) : null}
              {history.map((item) => (
                <TableRow key={item.id || `${item.start_time}-${item.end_time}`}>
                  <TableCell fontFamily="mono">{formatDate(item.start_time)}</TableCell>
                  <TableCell>{item.status || 'n/a'}</TableCell>
                  <TableCell>{item.error || 'ok'}</TableCell>
                  <TableCell textAlign="right">
                    {item.files_synced != null ? `${item.files_synced}/${item.files_total}` : '—'}
                  </TableCell>
                  <TableCell textAlign="right">{item.bytes_synced != null ? item.bytes_synced : '—'}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </TableRoot>
        )}
      </CardBody>
    </Card>
  )
}
