import { motion } from 'framer-motion'

const variantBase = {
  primary: 'bg-brand-600 hover:bg-brand-500 text-white shadow-lg shadow-brand-600/20',
  secondary: 'bg-surface-700 hover:bg-surface-600 text-surface-200 border border-surface-600',
  danger: 'bg-danger/15 hover:bg-danger/25 text-danger border border-danger/30',
  ghost: 'hover:bg-surface-700/50 text-surface-300',
  outline: 'bg-transparent border-2 border-current text-brand-400 hover:bg-brand-500/10',
  gradient: 'bg-gradient-to-r from-brand-600 to-brand-500 hover:from-brand-500 hover:to-brand-400 text-white shadow-lg shadow-brand-600/20',
}

const focusRing = {
  primary: 'focus-visible:ring-brand-400/70',
  secondary: 'focus-visible:ring-surface-400/70',
  danger: 'focus-visible:ring-danger/70',
  ghost: 'focus-visible:ring-surface-400/70',
  outline: 'focus-visible:ring-brand-400/70',
  gradient: 'focus-visible:ring-brand-400/70',
}

const sizes = {
  sm: 'px-3 py-1.5 text-xs min-h-9',
  md: 'px-4 py-2 text-sm min-h-10',
  lg: 'px-5 py-3 text-base min-h-12',
}

export function Button({
  children,
  variant = 'primary',
  size = 'md',
  disabled = false,
  loading = false,
  className = '',
  type = 'button',
  icon = false,
  shine = false,
  ...props
}) {
  const isIconOnly = icon && !children

  return (
    <motion.button
      type={type}
      whileTap={{ scale: disabled || loading ? 1 : 0.96 }}
      transition={{ type: 'spring', stiffness: 500, damping: 30 }}
      className={`
        relative inline-flex items-center justify-center gap-2 font-medium rounded-lg shrink-0
        overflow-hidden
        transition-all duration-150 active:scale-[0.97]
        focus-visible:outline-none focus-visible:ring-2 ${focusRing[variant]} focus-visible:ring-offset-2 focus-visible:ring-offset-surface-950
        disabled:opacity-50 disabled:cursor-not-allowed disabled:active:scale-100
        ${isIconOnly ? 'w-10 h-10 p-0' : sizes[size]}
        ${variantBase[variant]}
        ${shine ? 'shine' : ''}
        ${className}
      `}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...props}
    >
      {loading && (
        <span className="absolute inset-0 flex items-center justify-center">
          <span
            className="w-4 h-4 border-2 border-current border-t-transparent rounded-full animate-spin"
            aria-hidden="true"
          />
        </span>
      )}
      <span className={`flex items-center justify-center gap-2 ${loading ? 'opacity-0' : 'opacity-100'} transition-opacity duration-150`}>
        {children}
      </span>
    </motion.button>
  )
}
