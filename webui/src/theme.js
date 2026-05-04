import { createSystem, defaultConfig, defineConfig } from '@chakra-ui/react'

const config = defineConfig({
  theme: {
    tokens: {
      fonts: {
        heading: { value: "'Manrope', 'Trebuchet MS', sans-serif" },
        body: { value: "'Manrope', 'Trebuchet MS', sans-serif" },
      },
      colors: {
        // Primary accent color (cyan/teal)
        accent: {
          50: { value: '#e6f7ff' },
          100: { value: '#b3e7ff' },
          200: { value: '#80d7ff' },
          300: { value: '#4dc7ff' },
          400: { value: '#26b5ff' },
          500: { value: '#0099e6' },
          600: { value: '#0077b3' },
          700: { value: '#005580' },
          800: { value: '#00334d' },
          900: { value: '#001a26' },
          DEFAULT: { value: '#26b5ff' },
        },
        // Secondary accent (green/teal)
        accentAlt: {
          50: { value: '#e6fdf5' },
          100: { value: '#b3f5d6' },
          200: { value: '#80edb7' },
          300: { value: '#4de598' },
          400: { value: '#26d67a' },
          500: { value: '#10b981' },
          600: { value: '#0d9668' },
          700: { value: '#0a734f' },
          800: { value: '#065036' },
          900: { value: '#032d1d' },
          DEFAULT: { value: '#2ad4a6' },
        },
        brand: {
          50: { value: '#e6f7ff' },
          100: { value: '#b3e7ff' },
          200: { value: '#80d7ff' },
          300: { value: '#4dc7ff' },
          400: { value: '#26b5ff' },
          500: { value: '#0099e6' },
          600: { value: '#0077b3' },
          700: { value: '#005580' },
          800: { value: '#00334d' },
          900: { value: '#001a26' },
        },
      },
    },
    semanticTokens: {
      colors: {
        // Panel colors (card backgrounds)
        panel: {
          DEFAULT: { value: { _light: '{colors.white}', _dark: '#141c31' } },
          soft: { value: { _light: '#f1f5f9', _dark: '#1a2238' } },
          strong: { value: { _light: '#e2e8f0', _dark: '#101b34' } },
        },
        // Accent colors
        accent: {
          DEFAULT: { value: { _light: '#0099e6', _dark: '#26b5ff' } },
          muted: { value: { _light: 'rgba(0, 153, 230, 0.1)', _dark: 'rgba(38, 181, 255, 0.15)' } },
          alt: { value: { _light: '#10b981', _dark: '#2ad4a6' } },
        },
        // Danger/Error colors
        danger: {
          DEFAULT: { value: { _light: '#dc2626', _dark: '#fecaca' } },
          bg: { value: { _light: 'rgba(220, 38, 38, 0.1)', _dark: 'rgba(254, 202, 202, 0.15)' } },
        },
        // Success colors
        success: {
          DEFAULT: { value: { _light: '#16a34a', _dark: '#86efac' } },
          bg: { value: { _light: 'rgba(22, 163, 74, 0.1)', _dark: 'rgba(134, 239, 172, 0.15)' } },
        },
        // Warning colors
        warning: {
          DEFAULT: { value: { _light: '#ca8a04', _dark: '#fde68a' } },
          bg: { value: { _light: 'rgba(202, 138, 4, 0.1)', _dark: 'rgba(253, 230, 138, 0.15)' } },
        },
        // Border colors
        border: {
          subtle: { value: { _light: 'rgba(148, 163, 184, 0.2)', _dark: 'rgba(148, 163, 184, 0.15)' } },
          muted: { value: { _light: 'rgba(148, 163, 184, 0.3)', _dark: 'rgba(148, 163, 184, 0.22)' } },
          emphasized: { value: { _light: 'rgba(148, 163, 184, 0.4)', _dark: 'rgba(148, 163, 184, 0.32)' } },
        },
      },
    },
    recipes: {
      button: {
        base: {
          borderRadius: 'l2',
          fontWeight: 'semibold',
          minH: '2.5rem',
        },
        variants: {
          brand: {
            bg: 'accent',
            color: 'gray.900',
            _hover: {
              opacity: 0.9,
            },
            _active: {
              opacity: 0.8,
            },
          },
          outline: {
            borderWidth: '1px',
            borderColor: 'border.muted',
            bg: 'transparent',
            color: 'fg.default',
            _hover: {
              bg: 'panel.soft',
              borderColor: 'accent',
            },
          },
          ghost: {
            bg: 'transparent',
            color: 'fg.muted',
            _hover: {
              bg: 'panel.soft',
              color: 'fg.default',
            },
          },
        },
      },
      input: {
        base: {
          borderRadius: 'l2',
          borderWidth: '1px',
          borderColor: 'border.subtle',
          bg: 'panel',
          color: 'fg.default',
          _hover: {
            borderColor: 'border.muted',
          },
          _focus: {
            borderColor: 'accent',
            boxShadow: '0 0 0 1px var(--chakra-colors-accent)',
          },
          _placeholder: {
            color: 'fg.subtle',
          },
        },
      },
      textarea: {
        base: {
          borderRadius: 'l2',
          borderWidth: '1px',
          borderColor: 'border.subtle',
          bg: 'panel',
          color: 'fg.default',
        },
      },
      card: {
        base: {
          borderRadius: 'l3',
          borderWidth: '1px',
        },
        variants: {
          panel: {
            bg: 'panel',
            borderColor: 'border.muted',
            boxShadow: '0 16px 40px rgba(2, 8, 24, 0.2)',
          },
        },
      },
    },
  },
  globalCss: {
    '*': {
      boxSizing: 'border-box',
    },
    'html, body': {
      margin: 0,
      minHeight: '100vh',
      fontFamily: 'body',
    },
    body: {
      minHeight: '100vh',
    },
    '#root': {
      minHeight: '100vh',
    },
    'button, input, select, textarea': {
      fontFamily: 'inherit',
    },
    '::selection': {
      bg: 'accent.muted',
      color: 'accent',
    },
    '::-webkit-scrollbar': {
      width: '8px',
      height: '8px',
    },
    '::-webkit-scrollbar-track': {
      bg: 'panel',
    },
    '::-webkit-scrollbar-thumb': {
      bg: 'border.emphasized',
      borderRadius: 'full',
    },
    '::-webkit-scrollbar-thumb:hover': {
      bg: 'fg.subtle',
    },
  },
})

export const system = createSystem(defaultConfig, config)
