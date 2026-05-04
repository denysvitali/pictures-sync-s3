import { useEffect, useMemo, useState } from 'react'
import {
  AlertRoot as Alert,
  AlertDescription,
  AlertIndicator,
  AlertTitle,
  Badge,
  Box,
  Button,
  CardRoot as Card,
  CardBody,
  CardHeader,
  Container,
  Flex,
  Heading,
  HStack,
  Input,
  SimpleGrid,
  TabsList,
  TabsRoot,
  TabsTrigger,
  Text,
  VStack
} from '@chakra-ui/react'
import { useDeviceUrl, isHostedPagesUrl } from './DeviceContext'
import { getVersion } from './api'
import { navigateRoute, parseHashRoute, pageByPath, pageRegistry } from './routes'

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
    <Card className="endpoint-card" variant="panel">
      <CardHeader className="endpoint-header">
        <Flex align="center" justify="space-between" gap={4} wrap="wrap">
          <Box>
            <Heading size="md">Device connection</Heading>
            <Text className="muted-text" fontSize="sm">
              Tell this interface which backup device to control.
            </Text>
          </Box>
          <Badge className="mode-badge">Live</Badge>
        </Flex>
      </CardHeader>
      <CardBody className="endpoint-body">
        <Box className="endpoint-form">
          <Box className="field-stack">
            <Text as="label" htmlFor="device-input" className="field-label">
              Device address
            </Text>
            <Input
              id="device-input"
              value={rawValue}
              onChange={(event) => setRawValue(event.target.value)}
              placeholder="http://192.168.1.10"
              _focus={{ boxShadow: 'none', borderColor: 'teal.300' }}
            />
          </Box>
          <HStack className="endpoint-actions">
            <Button size="lg" variant="brand" onClick={save} className="primary-cta">
              Connect
            </Button>
            <Button variant="outline" onClick={useCurrentHost}>
              Use this page
            </Button>
            <Button variant="ghost" onClick={clearDeviceUrl}>
              Disconnect
            </Button>
          </HStack>
        </Box>
        <Box className="active-target">
          <Text className="field-label">Connected device</Text>
          <Box as="span" className="target-value">
            {deviceUrl || '(not set)'}
          </Box>
        </Box>
        <details className="advanced-connection">
          <summary>Advanced connection details</summary>
          <Text mt={2} fontSize="sm" className="muted-text">
            Shortcuts:
          </Text>
          <HStack className="quick-targets" mt={2}>
            {DEVICE_EXAMPLES.map((item) => (
              <Button size="sm" variant="outline" key={item} onClick={() => setDeviceUrl(item)}>
                {item}
              </Button>
            ))}
          </HStack>
          <Text className="advanced-note">This should normally be filled automatically when browsing from the device.</Text>
        </details>
      </CardBody>
    </Card>
  )
}

function DeviceMissingNotice() {
  return (
    <Card className="notice-card" variant="panel">
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
    <Box className="app-shell">
      <Container maxW="6xl">
        <VStack align="stretch" className="app-stack">
          <Box className="hero-panel">
            <Box>
              <Heading size="lg" className="hero-title">
                Photo Backup Station
              </Heading>
            <Text className="hero-copy">Keep your camera backups running and tuned.</Text>
            </Box>
            <Box className="hero-status">
              <Text className="field-label">Connected device</Text>
              <Text className="hero-target">{deviceUrl || 'Not connected'}</Text>
              <Text className="field-label version-label">Software</Text>
              <Text className={versionError ? 'hero-version unavailable' : 'hero-version'}>
                {versionError ? 'Unavailable' : formatVersion(versionInfo)}
              </Text>
            </Box>
          </Box>

          {isHostedPages && <DeviceSwitcher />}

            <Card className="nav-card" variant="panel">
            <TabsRoot
              value={activeRoute.path}
              onValueChange={(event) => navigateRoute(event.value)}
              variant="line"
              className="nav-root"
            >
              <TabsList className="nav-tabs">
                {pageRegistry.map((route) => (
                  <TabsTrigger key={route.path} value={route.path} className="nav-tab" _selected={{ color: 'white', borderColor: 'teal.300' }}>
                    <Text as="span">{route.icon}</Text>
                    <Text as="span">{quickRouteLabel(route)}</Text>
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
