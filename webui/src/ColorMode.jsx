import { createContext, useContext, useEffect, useState } from 'react'

const STORAGE_KEY = 'photo-backup-color-mode'

const ColorModeContext = createContext({
  colorMode: 'dark',
  toggleColorMode: () => {},
  setColorMode: () => {},
})

export function ColorModeProvider({ children }) {
  const [colorMode, setColorModeState] = useState(() => {
    if (typeof window === 'undefined') return 'dark'
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved === 'light' || saved === 'dark') return saved
    // Check system preference
    if (window.matchMedia?.('(prefers-color-scheme: light)').matches) return 'light'
    return 'dark'
  })

  useEffect(() => {
    const root = document.documentElement
    
    // Set data attribute for CSS-based selectors
    root.setAttribute('data-color-mode', colorMode)
    root.style.colorScheme = colorMode
    
    // Persist to localStorage
    localStorage.setItem(STORAGE_KEY, colorMode)
    
    // Update body class for any CSS that needs it
    document.body.classList.remove('light-mode', 'dark-mode')
    document.body.classList.add(`${colorMode}-mode`)
  }, [colorMode])

  const toggleColorMode = () => {
    setColorModeState((prev) => (prev === 'dark' ? 'light' : 'dark'))
  }

  const setColorMode = (mode) => {
    if (mode === 'light' || mode === 'dark') {
      setColorModeState(mode)
    }
  }

  return (
    <ColorModeContext.Provider value={{ colorMode, toggleColorMode, setColorMode }}>
      {children}
    </ColorModeContext.Provider>
  )
}

export function useColorMode() {
  return useContext(ColorModeContext)
}
