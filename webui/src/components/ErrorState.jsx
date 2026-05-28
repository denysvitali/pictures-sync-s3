import { useMemo } from 'react'
import { Icon } from './Icons.jsx'
import { Button } from './Button.jsx'

const TROUBLESHOOTING_TIPS = {
  network: [
    'Check that the device is powered on',
    'Verify the device is on the same network',
    'Try accessing the device IP directly in your browser',
    'Check your firewall settings',
  ],
  auth: [
    'Verify your credentials are correct',
    'Try logging in again',
    'Check if the device password was changed',
  ],
  timeout: [
    'The device may be under heavy load',
    'Try again in a few moments',
    'Check if the device has sufficient resources',
  ],
  default: [
    'Check that the device is powered on and connected',
    'Verify the web server is running on the device',
    'Try refreshing the page',
    'Check the device logs for more details',
  ],
}

function getErrorType(error) {
  if (!error) return 'default'
  const msg = String(error).toLowerCase()
  if (msg.includes('unauthorized') || msg.includes('401') || msg.includes('403') || msg.includes('auth')) {
    return 'auth'
  }
  if (msg.includes('timeout') || msg.includes('timed out')) {
    return 'timeout'
  }
  if (msg.includes('failed to fetch') || msg.includes('network') || msg.includes('unreachable') || msg.includes('refused')) {
    return 'network'
  }
  return 'default'
}

export function ErrorState({
  error,
  onRetry,
  compact = false,
  title = 'Something went wrong',
  children,
}) {
  const errorType = useMemo(() => getErrorType(error), [error])
  const tips = TROUBLESHOOTING_TIPS[errorType] || TROUBLESHOOTING_TIPS.default

  if (compact) {
    return (
      <div className="flex items-start gap-3 rounded-lg border border-danger/20 bg-danger/5 px-4 py-3" role="alert" aria-live="assertive">
        <div className="mt-0.5 shrink-0">
          <Icon name="exclamation-triangle" className="w-5 h-5 text-danger" />
        </div>
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium text-surface-200">{title}</p>
          {error && (
            <p className="mt-1 text-xs text-surface-400 break-words">{String(error)}</p>
          )}
          {onRetry && (
            <Button variant="ghost" size="sm" className="mt-2" onClick={onRetry}>
              <Icon name="arrow-path" className="w-4 h-4" />
              Retry
            </Button>
          )}
        </div>
        {children}
      </div>
    )
  }

  return (
    <div className="flex flex-col items-center justify-center px-4 text-center" role="alert" aria-live="assertive">
      <div className="relative mb-5">
        <div className="absolute inset-0 -m-4 rounded-full bg-gradient-to-br from-danger/15 via-danger/5 to-transparent blur-sm" />
        <div className="relative flex h-20 w-20 items-center justify-center rounded-2xl bg-gradient-to-br from-danger/15 to-danger/5 border border-danger/10">
          <Icon
            name="exclamation-triangle"
            className="h-10 w-10 text-danger shake-animation"
            aria-hidden="true"
          />
        </div>
      </div>
      <h3 className="text-base font-semibold text-surface-200 mb-1.5">{title}</h3>
      {error && (
        <p className="max-w-sm text-sm text-surface-400 mb-4 break-words">{String(error)}</p>
      )}
      {onRetry && (
        <Button variant="secondary" size="md" onClick={onRetry}>
          <Icon name="arrow-path" className="w-4 h-4" />
          Try Again
        </Button>
      )}
      <div className="mt-6 max-w-sm">
        <p className="text-xs font-medium text-surface-500 uppercase tracking-wider mb-2">Troubleshooting tips</p>
        <ul className="text-left space-y-1.5">
          {tips.map((tip, i) => (
            <li key={i} className="flex items-start gap-2 text-xs text-surface-400">
              <span className="mt-1 w-1 h-1 rounded-full bg-surface-500 shrink-0" />
              {tip}
            </li>
          ))}
        </ul>
      </div>
      {children}
    </div>
  )
}
