export function Skeleton({ className = '', ...props }) {
  return (
    <div
      className={`animate-shimmer rounded-md ${className}`}
      {...props}
    />
  )
}
