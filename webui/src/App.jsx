import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  AlertDescription,
  AlertIndicator,
  AlertTitle,
  Badge,
  Box,
  Button,
  Card,
  CardBody,
  CardHeader,
  Container,
  Flex,
  Heading,
  HStack,
  IconButton,
  Input,
  SimpleGrid,
  TabsList,
  TabsRoot,
  TabsTrigger,
  Text,
  VStack,
} from '@chakra-ui/react'
import { useDeviceUrl, isHostedPagesUrl } from './DeviceContext'
import { useColorMode } from './ColorMode'
import { getVersion } from './api'
import { navigateRoute, parseHashRoute, pageByPath, pageRegistry } from './routes'

// Icon components using SVG
const SunIcon = () => (
  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <circle cx="12" cy="12" r="5"/>
    <line x1="12" y1="1" x2="12" y2="3"/>
    <line x1="12" y1="21" x2="12" y2="23"/>
    <line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/>
    <line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/>
    <line x1="1" y1="12" x2="3" y2="12"/>
    <line x1="21" y1="12" x2="23" y2="12"/>
    <line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/>
    <line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/>
  </svg>
)

const MoonIcon = () => (
  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>
  </svg>
)

const DEVICE_EXAMPLES = ['http://192.168.1.10:8080', 'http://localhost:8080']
const quickRouteLabel = (route) => route.label || route.path

function DeviceSwitcher() {
  const { deviceUrl, setDeviceUrl, clearDeviceUrl } = useDeviceUrl()
  const [rawValue, setRawValue] = useState(deviceUrl)

  useEffect(() => {
    setRawValue(deviceUrl)
  }, [deviceUrl])

  const save = () => {
    setDeviceUrl(rawValue)
  }

  const useCurrentHost = () => {
    setDeviceUrl(window.location.origin)
  }

  return (
    <Card variant="panel">
      <CardHeader>
        <Flex align="center" justify="space-between" gap={4} wrap="wrap">
          <Box>
            <Heading size="md">Device connection</Heading>
            <Text color="fg.muted" fontSize="sm">
              Tell this interface which backup device to control.
            </Text>
          </Box>
          <Badge bg="accent.muted" color="accent">Live</Badge>
        </Flex>
      </CardHeader>
      <CardBody>
        <VStack align="stretch" gap={3}>
          <Box>
            <Text as="label" htmlFor="device-input" fontSize="sm" fontWeight="medium" color="fg.muted" mb={1} display="block">
              Device address
            </Text>
            <Input
              id="device-input"
              value={rawValue}
              onChange={(event) => setRawValue(event.target.value)}
              placeholder="http://192.168.1.10"
            />
          </Box>
          <HStack gap={2} wrap="wrap">
            <Button size="lg" variant="brand" onClick={save}>Connect</Button>
            <Button variant="outline" onClick={useCurrentHost}>Use this page</Button>
            <Button variant="ghost" onClick={clearDeviceUrl}>Disconnect</Button>
          </HStack>
        </VStack>
        
        <Box mt={4} pt={4} borderTopWidth="1px" borderColor="border.subtle">
          <Text fontSize="sm" fontWeight="medium" color="fg.muted" mb={1}>Connected device</Text>
          <Text fontFamily="mono" fontSize="sm" color="fg.default">
            {deviceUrl || '(not set)'}
          </Text>
        </Box>

        <details mt={3} style={{ cursor: 'pointer' }}>
          <summary style={{ color: 'var(--chakra-colors-fg-subtle)', fontSize: '0.82rem', fontWeight: 600 }}>
            Advanced connection details
          </summary>
          <Box mt={3}>
            <Text fontSize="sm" color="fg.muted" mb={2}>Shortcuts:</Text>
            <HStack gap={2} wrap="wrap">
              {DEVICE_EXAMPLES.map((item) => (
                <Button size="sm" variant="outline" key={item} onClick={() => setDeviceUrl(item)}>
                  {item}
                </Button>
              ))}
            </HStack>
          </Box>
        </details>
      </CardBody>
    </Card>
  )
}

function DeviceMissingNotice() {
  return (
    <Card variant="panel" bg="warningBg">
      <CardBody>
        <Alert status="warning" variant="subtle">
          <AlertIndicator />
          <Box>
            <AlertTitle>No device URL configured.</AlertTitle>
            <AlertDescription>
              You are likely browsing static UI assets. Set a reachable device URL (for example
              <Box as="span" fontFamily="mono" mx={1}>
                http://192.168.1.10:8080
              </Box>
              or
              <Box as="span" fontFamily="mono" mx={1}>
                http://localhost:8080
              </Box>
              to load live API data.
            </AlertDescription>
          </Box>
        </Alert>
      </CardBody>
    </Card>
  )
}

function RouteRenderer({ route, deviceUrl }) {
  const PageComponent = route?.component
  if (!PageComponent) {
    return null
  }
  return <PageComponent deviceUrl={deviceUrl} />
}

function formatVersion(versionInfo) {
  if (!versionInfo) return 'Unknown'
  const rawVersion = versionInfo.version || 'dev'
  const version = /^[a-f0-9]{40}$/i.test(rawVersion) ? rawVersion.slice(0, 7) : rawVersion
  const commit = versionInfo.commit ? versionInfo.commit.slice(0, 7) : ''
  const suffix = versionInfo.dirty ? ' dirty' : ''
  if (commit && commit !== version && commit !== rawVersion) {
    return `${version} (${commit}${suffix})`
  }
  return `${version}${suffix}`
}

export default function App() {
  const [activeRoutePath, setActiveRoutePath] = useState(parseHashRoute())
  const [versionInfo, setVersionInfo] = useState(null)
  const [versionError, setVersionError] = useState('')
  const { deviceUrl } = useDeviceUrl()
  const { colorMode, toggleColorMode } = useColorMode()

  const isHostedPages = isHostedPagesUrl(window.location.origin)

  const activeRoute = useMemo(() => pageByPath(activeRoutePath), [activeRoutePath])

  useEffect(() => {
    const onHashChange = () => {
      setActiveRoutePath(parseHashRoute())
    }
    window.addEventListener('hashchange', onHashChange)
    return () => window.removeEventListener('hashchange', onHashChange)
  }, [])

  useEffect(() => {
    let cancelled = false

    if (!deviceUrl) {
      setVersionInfo(null)
      setVersionError('')
      return () => {
        cancelled = true
      }
    }

    getVersion(deviceUrl)
      .then((data) => {
        if (!cancelled) {
          setVersionInfo(data)
          setVersionError('')
        }
      })
      .catch((error) => {
        if (!cancelled) {
          setVersionInfo(null)
          setVersionError(error.message || 'Version unavailable')
        }
      })

    return () => {
      cancelled = true
    }
  }, [deviceUrl])

  return (
    <Box minH="100vh" py={6} px={4}>
      <Container maxW="6xl">
        <VStack align="stretch" gap={4}>
          {/* Hero Panel with Theme Toggle */}
          <Card variant="panel">
            <CardBody>
              <Flex justify="space-between" align="center" gap={4} wrap="wrap">
                <Box>
                  <Heading size="lg" lineHeight="1.2">
                    Photo Backup Station
                  </Heading>
                  <Text color="fg.muted" mt={2}>
                    Keep your camera backups running and tuned.
                  </Text>
                </Box>
                
                <HStack gap={4}>
                  <Box textAlign="right" display={{ base: 'none', md: 'block' }}>
                    <Text fontSize="sm" color="fg.muted">Connected device</Text>
                    <Text fontFamily="mono" fontSize="sm" color="fg.default">
                      {deviceUrl || 'Not connected'}
                    </Text>
                    <Text fontSize="sm" color="fg.muted" mt={1}>Software</Text>
                    <Text 
                      fontFamily="mono" 
                      fontSize="sm" 
                      color={versionError ? 'danger' : 'fg.default'}
                    >
                      {versionError ? 'Unavailable' : formatVersion(versionInfo)}
                    </Text>
                  </Box>
                  
                  <IconButton
                    aria-label={`Switch to ${colorMode === 'dark' ? 'light' : 'dark'} mode`}
                    onClick={toggleColorMode}
                    variant="ghost"
                    size="lg"
                    color="fg.muted"
                    _hover={{ bg: 'panelSoft', color: 'fg.default' }}
                  >
                    {colorMode === 'dark' ? <SunIcon /> : <MoonIcon />}
                  </IconButton>
                </HStack>
              </Flex>
            </CardBody>
          </Card>

          {isHostedPages && <DeviceSwitcher />}

          <Card variant="panel" p={2}>
            <TabsRoot
              value={activeRoute.path}
              onValueChange={(event) => navigateRoute(event.value)}
              variant="line"
            >
              <TabsList display="grid" gridTemplateColumns={{ base: 'repeat(2, 1fr)', md: 'repeat(5, 1fr)' }} gap={2} borderBottom="none">
                {pageRegistry.map((route) => (
                  <TabsTrigger 
                    key={route.path} 
                    value={route.path}
                    justifyContent="center"
                    minH="48px"
                    borderRadius="l2"
                    borderWidth="1px"
                    borderColor="border.subtle"
                    color="fg.muted"
                    _selected={{ 
                      bg: 'accent.muted', 
                      borderColor: 'accent',
                      color: 'accent',
                    }}
                  >
                    <HStack gap={2}>
                      <Text as="span">{route.icon}</Text>
                      <Text as="span">{quickRouteLabel(route)}</Text>
                    </HStack>
                  </TabsTrigger>
                ))}
              </TabsList>
            </TabsRoot>
          </Card>

          <SimpleGrid columns={{ base: 1, lg: 1 }}>
            {activeRoute.requiresDeviceUrl && !deviceUrl ? (
              <DeviceMissingNotice />
            ) : (
              <RouteRenderer route={activeRoute} deviceUrl={deviceUrl} />
            )}
          </SimpleGrid>
        </VStack>
      </Container>
    </Box>
  )
}
