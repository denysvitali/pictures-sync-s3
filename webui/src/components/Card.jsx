export function Card({ children, className = '', ...props }) {
  return (
    <div
      className={`bg-surface-800/55 border border-surface-700/60 rounded-lg p-4 shadow-sm shadow-black/10 ${className}`}
      {...props}
    >
      {children}
    </div>
  )
}

export function CardHeader({ children, className = '' }) {
  return (
    <div className={`flex items-center justify-between gap-3 mb-3 ${className}`}>
      {children}
    </div>
  )
}

export function CardTitle({ children, className = '' }) {
  return (
    <h3 className={`text-sm font-semibold text-surface-200 uppercase tracking-wide ${className}`}>
      {children}
    </h3>
  )
}
