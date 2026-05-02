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
    <Card border="1px solid" borderColor="whiteAlpha.300" bg="whiteAlpha.100">
      <CardHeader>
        <Flex align="center" justify="space-between" gap={4}>
          <VStack align="start" spacing={1}>
            <Heading size="md">Device endpoint</Heading>
            <Text color="gray.300" fontSize="sm">
              Point the control plane at your device API for live data.
            </Text>
          </VStack>
          <Badge colorScheme="purple">Targeted API mode</Badge>
        </Flex>
      </CardHeader>
      <CardBody pt={0}>
        <HStack spacing={3} align="stretch">
          <VStack align="stretch" spacing={1}>
            <Text as="label" htmlFor="device-input" color="gray.200" fontSize="sm">
              Base URL
            </Text>
            <Input
              id="device-input"
              value={rawValue}
              onChange={(event) => setRawValue(event.target.value)}
              placeholder="http://192.168.1.10:8080"
              _focus={{ boxShadow: 'none', borderColor: 'teal.300' }}
            />
          </VStack>
          <VStack align="stretch" spacing={2}>
            <Button colorScheme="teal" onClick={save}>
              Save
            </Button>
            <Button variant="outline" colorScheme="teal" onClick={useCurrentHost}>
              Use this host
            </Button>
            <Button variant="ghost" onClick={clearDeviceUrl}>
              Clear
            </Button>
          </VStack>
        </HStack>
        <Text mt={3} color="gray.300" fontSize="sm">
          Active target:&nbsp;
          <Box as="span" fontFamily="mono" color="white">
            {deviceUrl || '(not set)'}
          </Box>
        </Text>
        <HStack mt={3} spacing={2} wrap="wrap">
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
    <Card bg="yellow.900" color="yellow.100" border="1px solid" borderColor="yellow.500">
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
    <Box
      minH="100vh"
      bgGradient="linear(to-br, #020617, #020617 35%, #0b1630)"
      color="gray.100"
      px={{ base: 4, md: 6 }}
      py={{ base: 4, md: 6 }}
    >
      <Container maxW="6xl">
        <VStack align="stretch" spacing={4}>
          <Card
            bgGradient="linear(to-r, #111827, #1f2937)"
            border="1px solid"
            borderColor="whiteAlpha.300"
          >
            <CardBody>
              <Heading size="lg" letterSpacing="wide">
                Photo Backup Station
              </Heading>
              <Text color="gray.300" mt={2}>
                Remote status dashboard and sync configuration for your device.
              </Text>
            </CardBody>
          </Card>

          <DeviceSwitcher />

          <Card bg="whiteAlpha.80" p={2} border="1px solid" borderColor="whiteAlpha.200">
            <TabsRoot
              value={activeRoute.path}
              onValueChange={(event) => navigateRoute(event.value)}
              variant="line"
            >
              <TabsList>
                {pageRegistry.map((route) => (
                  <TabsTrigger key={route.path} value={route.path} _selected={{ color: 'white', borderColor: 'teal.300' }} gap={2}>
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
