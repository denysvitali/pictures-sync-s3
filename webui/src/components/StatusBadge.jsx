const variants = {
  success: 'bg-success/15 text-success border-success/30',
  warning: 'bg-warning/15 text-warning border-warning/30',
  danger: 'bg-danger/15 text-danger border-danger/30',
  info: 'bg-info/15 text-info border-info/30',
  neutral: 'bg-surface-700/50 text-surface-300 border-surface-600/50',
}

const dotColors = {
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-danger',
  info: 'bg-info',
  neutral: 'bg-surface-300',
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
  const dotColor = dotColors[variant] || dotColors.neutral

  if (dot) {
    return (
      <span
        className={`relative inline-flex shrink-0`}
        aria-label={typeof children === 'string' ? children : undefined}
        title={typeof children === 'string' ? children : undefined}
      >
        {pulse && (
          <span
            className={`absolute inline-flex h-full w-full animate-ping rounded-full opacity-60 ${dotColor}`}
            aria-hidden="true"
          />
        )}
        <span className={`relative inline-block rounded-full ${dotSizes[size]} ${dotColor}`} />
      </span>
    )
  }

  return (
    <span
      className={`inline-flex shrink-0 items-center whitespace-nowrap rounded-full border font-medium tracking-wide ${sizeClasses[size]} ${
        variants[variant] || variants.neutral
      }`}
    >
      {pulse && (
        <span className="relative inline-flex shrink-0">
          <span className={`absolute inline-flex h-full w-full animate-ping rounded-full bg-current opacity-60`} aria-hidden="true" />
          <span className={`relative inline-block rounded-full bg-current ${dotSizes[size]}`} />
        </span>
      )}
      {children}
    </span>
  )
}
