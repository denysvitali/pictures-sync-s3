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
import { useDeviceUrl } from './DeviceContext'
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
    <Card className="endpoint-card">
      <CardHeader className="endpoint-header">
        <Flex align="center" justify="space-between" gap={4} wrap="wrap">
          <Box>
            <Heading size="md">Device endpoint</Heading>
            <Text className="muted-text" fontSize="sm">
              Connect this dashboard to the photo backup device API.
            </Text>
          </Box>
          <Badge className="mode-badge">Targeted API</Badge>
        </Flex>
      </CardHeader>
      <CardBody className="endpoint-body">
        <Box className="endpoint-form">
          <Box className="field-stack">
            <Text as="label" htmlFor="device-input" className="field-label">
              Base URL
            </Text>
            <Input
              id="device-input"
              value={rawValue}
              onChange={(event) => setRawValue(event.target.value)}
              placeholder="http://192.168.1.10:8080"
              _focus={{ boxShadow: 'none', borderColor: 'teal.300' }}
            />
          </Box>
          <HStack className="endpoint-actions">
            <Button colorScheme="teal" onClick={save}>
              Save
            </Button>
            <Button variant="outline" colorScheme="teal" onClick={useCurrentHost}>
              Use this host
            </Button>
            <Button variant="ghost" onClick={clearDeviceUrl}>
              Clear
            </Button>
          </HStack>
        </Box>
        <Box className="active-target">
          <Text className="field-label">Active target</Text>
          <Box as="span" className="target-value">
            {deviceUrl || '(not set)'}
          </Box>
        </Box>
        <HStack className="quick-targets">
          {DEVICE_EXAMPLES.map((item) => (
            <Button size="sm" variant="outline" key={item} onClick={() => setDeviceUrl(item)}>
              {item}
            </Button>
          ))}
        </HStack>
      </CardBody>
    </Card>
  )
}

function DeviceMissingNotice() {
  return (
    <Card className="notice-card">
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

export default function App() {
  const [activeRoutePath, setActiveRoutePath] = useState(parseHashRoute())
  const { deviceUrl } = useDeviceUrl()

  const activeRoute = useMemo(() => pageByPath(activeRoutePath), [activeRoutePath])

  useEffect(() => {
    const onHashChange = () => {
      setActiveRoutePath(parseHashRoute())
    }
    window.addEventListener('hashchange', onHashChange)
    return () => window.removeEventListener('hashchange', onHashChange)
  }, [])

  return (
    <Box className="app-shell">
      <Container maxW="6xl">
        <VStack align="stretch" className="app-stack">
          <Box className="hero-panel">
            <Box>
              <Heading size="lg" className="hero-title">
                Photo Backup Station
              </Heading>
              <Text className="hero-copy">
                Remote status dashboard and sync configuration for your device.
              </Text>
            </Box>
            <Box className="hero-status">
              <Text className="field-label">Current target</Text>
              <Text className="hero-target">{deviceUrl || 'Not configured'}</Text>
            </Box>
          </Box>

          <DeviceSwitcher />

          <Card className="nav-card">
            <TabsRoot
              value={activeRoute.path}
              onValueChange={(event) => navigateRoute(event.value)}
              variant="line"
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
