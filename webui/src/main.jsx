import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { DeviceProvider } from './DeviceContext.jsx'
import { WebSocketProvider } from './WebSocketContext.jsx'
import App from './App.jsx'
import './index.css'

createRoot(document.getElementById('root')).render(
  <StrictMode>
    <DeviceProvider>
      <WebSocketProvider>
        <App />
      </WebSocketProvider>
    </DeviceProvider>
  </StrictMode>
)
