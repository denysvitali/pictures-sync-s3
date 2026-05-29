import { motion } from 'framer-motion'

export function Card({ children, className = '', glow = false, animate = false, ...props }) {
  const Wrapper = animate ? motion.div : 'div'
  const wrapperProps = animate
    ? {
        whileHover: { y: -3 },
        transition: { type: 'spring', stiffness: 380, damping: 26 },
      }
    : {}

  return (
    <Wrapper
      className={`group/card relative rounded-xl p-px bg-gradient-to-b from-surface-600/40 via-surface-700/20 to-transparent transition-shadow duration-300 ${
        glow ? 'hover:shadow-glow' : ''
      }`}
      {...wrapperProps}
      {...props}
    >
      <div
        className={`relative h-full overflow-hidden rounded-[11px] border border-surface-700/50 bg-surface-800/55 p-4 shadow-card backdrop-blur-sm ${className}`}
      >
        {/* Subtle top sheen for depth */}
        <div
          className="pointer-events-none absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-white/10 to-transparent"
          aria-hidden="true"
        />
        {children}
      </div>
    </Wrapper>
  )
}

export function CardHeader({ children, className = '' }) {
  return (
    <div
      className={`relative mb-3 -ml-3 flex items-center justify-between gap-3 pl-3 ${className}`}
    >
      <span
        className="absolute left-0 top-1/2 h-5 w-1 -translate-y-1/2 rounded-full bg-gradient-to-b from-brand-400 to-brand-600"
        aria-hidden="true"
      />
      {children}
    </div>
  )
}

export function CardTitle({ children, className = '' }) {
  return (
    <h3 className={`text-[0.95rem] font-semibold tracking-tight text-surface-100 ${className}`}>
      {children}
    </h3>
  )
}
