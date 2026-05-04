import { createSystem, defaultConfig } from '@chakra-ui/react'

export const appSystem = createSystem(
  defaultConfig,
  {
    theme: {
      tokens: {
        colors: {
          panel: { value: '#141c31' },
          panelSoft: { value: '#1a2238' },
          panelStrong: { value: '#101b34' },
          borderGlow: { value: 'rgba(148, 163, 184, 0.22)' },
          accent: { value: '#26b5ff' },
          accentAlt: { value: '#2ad4a6' },
          warningText: { value: '#fde68a' },
          dangerText: { value: '#fecaca' }
        }
      },
      semanticTokens: {
        colors: {
          panel: { value: '{colors.panel}' },
          panelSoft: { value: '{colors.panelSoft}' },
          panelStrong: { value: '{colors.panelStrong}' },
          borderGlow: { value: '{colors.borderGlow}' },
          accent: { value: '{colors.accent}' },
          accentAlt: { value: '{colors.accentAlt}' }
        }
      },
      recipes: {
        button: {
          base: {
            borderRadius: 'l2',
            fontWeight: 'semibold',
            minH: '2.5rem'
          },
          variants: {
            variant: {
              brand: {
                bg: 'var(--chakra-colors-accent)',
                color: 'gray.900',
                _hover: {
                  bg: 'rgba(38, 181, 255, 0.9)'
                },
                _active: {
                  bg: 'rgba(38, 181, 255, 0.8)'
                },
                _disabled: {
                  bg: 'rgba(148, 163, 184, 0.3)'
                }
              }
            }
          }
        },
        input: {
          base: {
            borderRadius: 'l2',
            borderColor: 'rgba(148, 163, 184, 0.32)'
          }
        },
        textarea: {
          base: {
            borderRadius: 'l2',
            borderColor: 'rgba(148, 163, 184, 0.32)'
          }
        }
      },
      slotRecipes: {
        card: {
          variants: {
            variant: {
              panel: {
                root: {
                  bg: 'var(--chakra-colors-panel)',
                  borderWidth: '1px',
                  borderColor: 'var(--chakra-colors-borderGlow)',
                  borderRadius: 'l2',
                  boxShadow: '0 16px 40px rgba(2, 8, 24, 0.45)'
                }
              }
            }
          }
        }
      }
    },
    globalCss: {
      '*': {
        boxSizing: 'border-box'
      },
      'html, body': {
        margin: 0,
        minHeight: '100%',
        background:
          'radial-gradient(circle at 15% 10%, rgba(102, 216, 255, 0.16), transparent 30rem), radial-gradient(circle at 85% 0%, rgba(42, 212, 188, 0.14), transparent 28rem), linear-gradient(140deg, #070d1a 0%, #0b1220 45%, #101b34 100%)',
        color: 'var(--chakra-colors-whiteAlpha-100)',
        fontFamily: "'Manrope', 'Trebuchet MS', 'Inter', sans-serif"
      },
      body: {
        minHeight: '100%'
      },
      '#root': {
        minHeight: '100%'
      },
      'button, input, select, textarea': {
        fontFamily: 'inherit'
      }
    }
  }
)
