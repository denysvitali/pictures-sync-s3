const variants = {
  success: 'bg-success/15 text-success border-success/30',
  warning: 'bg-warning/15 text-warning border-warning/30',
  danger: 'bg-danger/15 text-danger border-danger/30',
  info: 'bg-info/15 text-info border-info/30',
  neutral: 'bg-surface-700/50 text-surface-300 border-surface-600/50',
}

const sizeClasses = {
  sm: 'px-2 py-0.5 text-[10px] gap-1',
  md: 'px-2.5 py-0.5 text-xs gap-1.5',
  lg: 'px-3 py-1 text-sm gap-2',
}

const dotSizes = {
  sm: 'w-1 h-1',
  md: 'w-1.5 h-1.5',
  lg: 'w-2 h-2',
}

export function StatusBadge({ variant = 'neutral', children, pulse = false, size = 'md', dot = false }) {
  if (dot) {
    return (
      <span
        className={`inline-block shrink-0 rounded-full ${dotSizes[size]} ${pulse ? 'animate-pulse' : ''} ${variants[variant]?.split(' ')[1]?.replace('text-', 'bg-') || 'bg-surface-300'}`}
        aria-label={typeof children === 'string' ? children : undefined}
        title={typeof children === 'string' ? children : undefined}
      />
    )
  }

  return (
    <span
      className={`inline-flex shrink-0 items-center rounded-full border whitespace-nowrap font-medium ${sizeClasses[size]} ${variants[variant] || variants.neutral}`}
    >
      {pulse && (
        <span className={`rounded-full bg-current animate-pulse ${dotSizes[size]}`} />
      )}
      {children}
    </span>
  )
}
