export function LoadingSpinner({ size = 'md', variant = 'circular', className = '', label = 'Loading…' }) {
  const sizeClasses = {
    sm: 'w-4 h-4',
    md: 'w-8 h-8',
    lg: 'w-12 h-12',
  }

  const dotSizes = {
    sm: 'w-1 h-1',
    md: 'w-1.5 h-1.5',
    lg: 'w-2 h-2',
  }

  const barSizes = {
    sm: { w: 'w-0.5', h: 'h-3' },
    md: { w: 'w-1', h: 'h-5' },
    lg: { w: 'w-1.5', h: 'h-7' },
  }

  return (
    <div
      role="status"
      aria-busy="true"
      aria-live="polite"
      className={`flex items-center justify-center ${className}`}
    >
      {variant === 'circular' && (
        <div className={`relative ${sizeClasses[size]}`} aria-hidden="true">
          <div className="absolute inset-0 rounded-full border-2 border-surface-700/70" />
          <div className="absolute inset-0 animate-spin rounded-full border-2 border-transparent border-t-brand-400 border-r-brand-400/40" />
        </div>
      )}
      {variant === 'dots' && (
        <div className="flex items-center gap-1" aria-hidden="true">
          <div className={`${dotSizes[size]} bg-brand-400 rounded-full animate-bounce`} style={{ animationDelay: '0ms' }} />
          <div className={`${dotSizes[size]} bg-brand-400 rounded-full animate-bounce`} style={{ animationDelay: '150ms' }} />
          <div className={`${dotSizes[size]} bg-brand-400 rounded-full animate-bounce`} style={{ animationDelay: '300ms' }} />
        </div>
      )}
      {variant === 'bars' && (
        <div className="flex items-end gap-0.5" aria-hidden="true">
          {[0, 1, 2, 3, 4].map((i) => (
            <div
              key={i}
              className={`${barSizes[size].w} ${barSizes[size].h} bg-brand-400 rounded-full animate-pulse`}
              style={{ animationDelay: `${i * 120}ms`, animationDuration: '1s' }}
            />
          ))}
        </div>
      )}
      <span className="sr-only">{label}</span>
    </div>
  )
}

export function PageLoader({ label = 'Loading…' }) {
  return (
    <div className="flex min-h-[50vh] flex-col items-center justify-center gap-4">
      <div className="relative flex items-center justify-center">
        <span className="absolute h-16 w-16 rounded-full bg-brand-500/10 blur-xl pulse-ring" aria-hidden="true" />
        <LoadingSpinner size="lg" label={label} />
      </div>
      <p className="text-sm text-surface-500">{label}</p>
    </div>
  )
}

export function Skeleton({ className = '', width, height, circle = false }) {
  const style = {}
  if (width) style.width = width
  if (height) style.height = height

  return (
    <div
      className={`bg-surface-700/50 animate-pulse ${circle ? 'rounded-full' : 'rounded-md'} ${className}`}
      style={style}
      aria-hidden="true"
    />
  )
}

export function SkeletonText({ lines = 1, className = '' }) {
  return (
    <div className={`flex flex-col gap-2 ${className}`} aria-hidden="true">
      {Array.from({ length: lines }).map((_, i) => (
        <Skeleton
          key={i}
          className={`h-4 ${i === lines - 1 && lines > 1 ? 'w-3/4' : 'w-full'}`}
        />
      ))}
    </div>
  )
}

export function SkeletonCard({ className = '' }) {
  return (
    <div className={`rounded-lg p-4 bg-surface-800/55 border border-surface-700/60 ${className}`} aria-hidden="true">
      <div className="flex items-center gap-3 mb-4">
        <Skeleton circle className="w-10 h-10" />
        <div className="flex-1">
          <Skeleton className="h-4 w-1/3 mb-2" />
          <Skeleton className="h-3 w-1/2" />
        </div>
      </div>
      <SkeletonText lines={3} />
    </div>
  )
}
