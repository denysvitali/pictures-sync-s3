import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { DeviceProvider } from './DeviceContext.jsx'
import App from './App.jsx'
import './index.css'

createRoot(document.getElementById('root')).render(
  <StrictMode>
    <DeviceProvider>
      <App />
    </DeviceProvider>
  </StrictMode>
)
