import React from 'react'
import { createRoot } from 'react-dom/client'
import { ChakraProvider, defaultSystem } from '@chakra-ui/react'
import App from './App'
import { DeviceUrlProvider } from './DeviceContext'
import './styles.css'

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <ChakraProvider value={defaultSystem}>
      <DeviceUrlProvider>
        <App />
      </DeviceUrlProvider>
    </ChakraProvider>
  </React.StrictMode>
)
