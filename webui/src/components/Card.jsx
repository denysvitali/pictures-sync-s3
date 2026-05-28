import { motion } from 'framer-motion'

export function Card({ children, className = '', glow = false, animate = false, ...props }) {
  const Wrapper = animate ? motion.div : 'div'
  const wrapperProps = animate
    ? { whileHover: { y: -2, scale: 1.005 }, transition: { type: 'spring', stiffness: 400, damping: 25 } }
    : {}

  return (
    <Wrapper
      className={`rounded-lg p-px bg-gradient-to-br from-brand-500/10 to-transparent ${glow ? 'hover:shadow-lg hover:shadow-brand-500/10 transition-shadow duration-300' : ''}`}
      {...wrapperProps}
      {...props}
    >
      <div
        className={`bg-surface-800/55 border border-surface-700/60 rounded-lg p-4 shadow-sm shadow-black/10 h-full ${className}`}
      >
        {children}
      </div>
    </Wrapper>
  )
}

export function CardHeader({ children, className = '' }) {
  return (
    <div className={`flex items-center justify-between gap-3 mb-3 border-l-2 border-brand-500 pl-3 -ml-3 ${className}`}>
      {children}
    </div>
  )
}

export function CardTitle({ children, className = '' }) {
  return (
    <h3 className={`text-base font-semibold text-surface-200 tracking-tight ${className}`}>
      {children}
    </h3>
  )
}
