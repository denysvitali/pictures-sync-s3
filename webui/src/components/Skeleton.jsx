export function Skeleton({ className = '', ...props }) {
  return (
    <div
      className={`animate-shimmer rounded-md ${className}`}
      {...props}
    />
  )
}

export function SkeletonCard({ className = '' }) {
  return (
    <div className={`rounded-lg p-px bg-gradient-to-br from-brand-500/5 to-transparent ${className}`}>
      <div className="bg-surface-800/55 border border-surface-700/60 rounded-lg p-4 shadow-sm shadow-black/10 h-full space-y-3">
        <div className="flex items-center gap-3">
          <Skeleton className="h-8 w-8 rounded-full" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-3 w-24" />
            <Skeleton className="h-2 w-16" />
          </div>
        </div>
        <Skeleton className="h-16 w-full" />
        <div className="flex gap-2">
          <Skeleton className="h-8 w-20" />
          <Skeleton className="h-8 w-20" />
        </div>
      </div>
    </div>
  )
}

export function SkeletonStatus() {
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <Skeleton className="h-12 w-12 rounded-xl" />
        <div className="flex-1 space-y-2">
          <Skeleton className="h-4 w-32" />
          <Skeleton className="h-3 w-20" />
        </div>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <SkeletonCard />
        <SkeletonCard />
      </div>
      <Skeleton className="h-32 w-full rounded-lg" />
    </div>
  )
}

export function SkeletonGallery({ count = 6 }) {
  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-3">
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className="aspect-square rounded-lg overflow-hidden">
          <Skeleton className="h-full w-full" />
        </div>
      ))}
    </div>
  )
}
