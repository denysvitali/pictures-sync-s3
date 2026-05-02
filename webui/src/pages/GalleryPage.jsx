import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  AlertDescription,
  AlertIndicator,
  Box,
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  Button,
  ButtonGroup,
  Card,
  CardBody,
  Flex,
  Heading,
  HStack,
  IconButton,
  Spinner,
  TableBody,
  TableCell,
  TableColumnHeader,
  TableHeader,
  TableRoot,
  TableRow,
  Text,
  VStack
} from '@chakra-ui/react'
import { getFiles, getFilesPaginated, getFileViewUrl } from '../api'

function isImage(name) {
  const value = String(name || '').toLowerCase()
  return value.endsWith('.jpg') || value.endsWith('.jpeg') || value.endsWith('.png') || value.endsWith('.gif') || value.endsWith('.webp')
}

function formatSize(size) {
  if (!size || size <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let index = 0
  let value = Number(size)
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024
    index += 1
  }
  return `${value.toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

function joinPath(base, next) {
  if (!base) return next
  if (!next) return base
  return `${base}/${next}`
}

export function GalleryPage({ deviceUrl }) {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [path, setPath] = useState('')
  const [files, setFiles] = useState([])
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(60)
  const [total, setTotal] = useState(0)
  const [totalPages, setTotalPages] = useState(1)
  const [hasMore, setHasMore] = useState(false)
  const [usePagination, setUsePagination] = useState(false)

  const breadcrumbPath = useMemo(() => path.split('/').filter(Boolean), [path])

  const load = async (nextPath = path, nextPage = page) => {
    if (!deviceUrl) return
    setLoading(true)
    setError('')
    try {
      if (usePagination) {
        const response = await getFilesPaginated(deviceUrl, {
          path: nextPath,
          page: nextPage,
          pageSize
        })
        setFiles(response?.files || [])
        setPath(response?.path || nextPath || '')
        setTotal(response?.total || 0)
        setTotalPages(response?.total_pages || 1)
        setHasMore(response?.has_more || false)
      } else {
        const response = await getFiles(deviceUrl, nextPath)
        setFiles(response?.files || [])
        setPath(response?.path || nextPath || '')
        setTotal(response?.files?.length || 0)
        setTotalPages(1)
        setHasMore(false)
      }
    } catch (err) {
      setError(err.message)
      setFiles([])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load('', 1)
  }, [deviceUrl, usePagination])

  const breadcrumbs = [
    { path: '', label: 'root' },
    ...breadcrumbPath.map((segment, index) => ({
      label: segment,
      path: breadcrumbPath.slice(0, index + 1).join('/')
    }))
  ]

  const openDir = (entryPath) => {
    setPage(1)
    load(entryPath, 1)
  }

  const goUp = () => {
    if (!path) return
    const parent = path.split('/').slice(0, -1).join('/')
    setPage(1)
    load(parent, 1)
  }

  const goNext = () => {
    const next = page + 1
    setPage(next)
    load(path, next)
  }

  const goPrevious = () => {
    const previous = Math.max(1, page - 1)
    setPage(previous)
    load(path, previous)
  }

  return (
    <VStack align="stretch" spacing={4}>
      <Card bg="whiteAlpha.50">
        <CardBody>
          <Flex gap={3} align="center" justify="space-between" wrap="wrap">
            <Heading size="sm">Gallery</Heading>
            <Button size="sm" colorScheme="teal" onClick={() => load(path, 1)} isLoading={loading}>
              Refresh
            </Button>
          </Flex>

          <HStack mt={3} align="center" justify="space-between">
            <Breadcrumb separator="/" fontSize="sm">
              {breadcrumbs.map((crumb) => (
                <BreadcrumbItem key={`${crumb.path}-${crumb.label}`}>
                  <BreadcrumbLink
                    onClick={(event) => {
                      event.preventDefault()
                      setPage(1)
                      load(crumb.path, 1)
                    }}
                    href="#"
                  >
                    {crumb.label}
                  </BreadcrumbLink>
                </BreadcrumbItem>
              ))}
            </Breadcrumb>
            <Button size="sm" variant="outline" onClick={() => setUsePagination((value) => !value)}>
              {usePagination ? 'Use flat listing' : 'Use paginated listing'}
            </Button>
          </HStack>

          {path ? (
            <Button size="sm" mt={2} onClick={goUp} variant="ghost">
              Up one level
            </Button>
          ) : null}

          {error ? (
            <Alert status="error" mt={3}>
              <AlertIndicator />
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}

          {loading ? (
            <Box py={6}>
              <Spinner />
            </Box>
          ) : (
            <TableRoot mt={4} size="sm" variant="simple">
              <TableHeader>
                <TableRow>
                  <TableColumnHeader>Name</TableColumnHeader>
                  <TableColumnHeader>Type</TableColumnHeader>
                  <TableColumnHeader textAlign="right">Size</TableColumnHeader>
                  <TableColumnHeader>Modified</TableColumnHeader>
                  <TableColumnHeader>Preview</TableColumnHeader>
                </TableRow>
              </TableHeader>
              <TableBody>
                {files.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={5}>
                      <Text color="gray.300">No files here.</Text>
                    </TableCell>
                  </TableRow>
                ) : null}
                {files.map((item) => (
                  <TableRow key={item.path || item.name}>
                    <TableCell>
                      {item.is_dir ? (
                        <Button variant="link" onClick={() => openDir(joinPath(path, item.name))}>
                          {item.name}
                        </Button>
                      ) : (
                        <Text>{item.name}</Text>
                      )}
                    </TableCell>
                    <TableCell>{item.is_dir ? 'Directory' : 'File'}</TableCell>
                    <TableCell textAlign="right">{item.is_dir ? '—' : formatSize(item.size)}</TableCell>
                    <TableCell>{item.mod_time ? new Date(item.mod_time).toLocaleString() : '—'}</TableCell>
                    <TableCell>
                      {!item.is_dir && isImage(item.name) ? (
                        <Button
                          as="a"
                          href={getFileViewUrl(deviceUrl, item.path)}
                          target="_blank"
                          rel="noreferrer"
                          size="xs"
                          variant="outline"
                        >
                          View
                        </Button>
                      ) : (
                        <Text color="gray.400" fontSize="sm">
                          n/a
                        </Text>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </TableRoot>
          )}
        </CardBody>
      </Card>

      {usePagination ? (
        <Flex justify="space-between" align="center" gap={2}>
          <Text color="gray.200" fontSize="sm">
            Total: {total} • Page {page} of {Math.max(totalPages, 1)}
          </Text>
          <ButtonGroup size="sm" isAttached>
            <IconButton
              icon={<Text as="span">←</Text>}
              aria-label="Previous page"
              onClick={goPrevious}
              isDisabled={loading || page <= 1}
            />
            <Button
              isDisabled={loading || !hasMore || totalPages <= page}
              onClick={goNext}
            >
              Next
            </Button>
          </ButtonGroup>
        </Flex>
      ) : null}
    </VStack>
  )
}
