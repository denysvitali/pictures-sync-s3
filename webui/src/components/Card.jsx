export function Card({ children, className = '', ...props }) {
  return (
    <div
      className={`bg-surface-800/50 border border-surface-700/50 rounded-xl p-4 ${className}`}
      {...props}
    >
      {children}
    </div>
  )
}

export function CardHeader({ children, className = '' }) {
  return (
    <div className={`flex items-center justify-between mb-3 ${className}`}>
      {children}
    </div>
  )
}

export function CardTitle({ children, className = '' }) {
  return (
    <h3 className={`text-sm font-semibold text-surface-200 uppercase tracking-wider ${className}`}>
      {children}
    </h3>
  )
}
