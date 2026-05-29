import { motion } from 'framer-motion'

const variantBase = {
  primary:
    'bg-gradient-to-b from-brand-500 to-brand-600 hover:from-brand-400 hover:to-brand-500 text-white shadow-lg shadow-brand-900/40 ring-1 ring-inset ring-white/10',
  secondary:
    'bg-surface-700/70 hover:bg-surface-600/80 text-surface-100 border border-surface-600/70 shadow-sm shadow-black/20',
  danger:
    'bg-danger/15 hover:bg-danger/25 text-danger border border-danger/30 hover:border-danger/40',
  ghost: 'text-surface-300 hover:text-surface-100 hover:bg-surface-700/50',
  outline:
    'bg-transparent border-2 border-brand-400/60 text-brand-300 hover:bg-brand-500/10 hover:border-brand-400',
  gradient:
    'bg-gradient-to-r from-brand-600 via-brand-500 to-brand-400 hover:brightness-110 text-white shadow-lg shadow-brand-900/40 ring-1 ring-inset ring-white/10',
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
  const isDisabled = disabled || loading

  return (
    <motion.button
      type={type}
      whileTap={{ scale: isDisabled ? 1 : 0.96 }}
      transition={{ type: 'spring', stiffness: 500, damping: 30 }}
      className={`
        group relative inline-flex shrink-0 items-center justify-center gap-2 overflow-hidden rounded-lg font-medium
        transition-[background,border-color,box-shadow,filter,transform] duration-200 ease-[cubic-bezier(0.16,1,0.3,1)]
        focus-visible:outline-none focus-visible:ring-2 ${focusRing[variant]} focus-visible:ring-offset-2 focus-visible:ring-offset-surface-950
        disabled:cursor-not-allowed disabled:opacity-50 disabled:shadow-none
        ${isIconOnly ? 'h-10 w-10 p-0' : sizes[size]}
        ${variantBase[variant]}
        ${className}
      `}
      disabled={isDisabled}
      aria-busy={loading || undefined}
      {...props}
    >
      {/* Animated shine sweep on hover (decorative). */}
      {shine && !isDisabled && (
        <span
          className="pointer-events-none absolute inset-0 -translate-x-full bg-gradient-to-r from-transparent via-white/25 to-transparent transition-transform duration-700 ease-out group-hover:translate-x-full motion-reduce:hidden"
          aria-hidden="true"
        />
      )}
      {loading && (
        <span className="absolute inset-0 flex items-center justify-center">
          <span
            className="h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent"
            aria-hidden="true"
          />
        </span>
      )}
      <span
        className={`flex items-center justify-center gap-2 transition-opacity duration-150 ${
          loading ? 'opacity-0' : 'opacity-100'
        }`}
      >
        {children}
      </span>
    </motion.button>
  )
}
