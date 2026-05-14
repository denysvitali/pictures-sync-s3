export function LoadingSpinner({ size = 'md', className = '', label = 'Loading…' }) {
  const sizeClasses = {
    sm: 'w-4 h-4',
    md: 'w-8 h-8',
    lg: 'w-12 h-12',
  }

  return (
    <div
      role="status"
      aria-busy="true"
      aria-live="polite"
      className={`flex items-center justify-center ${className}`}
    >
      <div
        className={`${sizeClasses[size]} border-2 border-surface-600 border-t-brand-400 rounded-full animate-spin`}
        aria-hidden="true"
      />
      <span className="sr-only">{label}</span>
    </div>
  )
}

export function PageLoader({ label = 'Loading…' }) {
  return (
    <div className="flex items-center justify-center min-h-[50vh]">
      <LoadingSpinner size="lg" label={label} />
    </div>
  )
}
