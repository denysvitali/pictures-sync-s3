import { Icon } from './Icons.jsx'
import { Button } from './Button.jsx'

export function EmptyState({
  icon,
  title,
  description,
  action,
  compact = false,
  children,
}) {
  if (compact) {
    return (
      <div className="flex items-center gap-3 py-4 px-3 rounded-lg bg-surface-900/40 border border-surface-700/40">
        <div className="relative flex items-center justify-center w-10 h-10 rounded-full bg-gradient-to-br from-brand-500/20 to-brand-700/10 shrink-0">
          {icon && <Icon name={icon} className="w-5 h-5 text-brand-400" />}
        </div>
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium text-surface-300">{title}</p>
          {description && (
            <p className="text-xs text-surface-500 mt-0.5">{description}</p>
          )}
        </div>
        {action && (
          <div className="shrink-0">
            <Button variant="secondary" size="sm" onClick={action.onClick}>
              {action.label}
            </Button>
          </div>
        )}
        {children}
      </div>
    )
  }

  return (
    <div className="flex flex-col items-center justify-center px-4 text-center" role="status" aria-live="polite">
      <div className="relative mb-5">
        {/* Soft gradient background circle */}
        <div className="absolute inset-0 -m-4 rounded-full bg-gradient-to-br from-brand-500/15 via-brand-600/10 to-transparent blur-sm" />
        <div className="relative flex h-20 w-20 items-center justify-center rounded-2xl bg-gradient-to-br from-brand-500/15 to-brand-700/5 border border-brand-400/10">
          {icon && (
            <Icon
              name={icon}
              className="h-10 w-10 text-brand-400 float-animation"
              aria-hidden="true"
            />
          )}
        </div>
      </div>
      <h3 className="text-base font-semibold text-surface-200 mb-1.5">{title}</h3>
      {description && (
        <p className="max-w-xs text-sm text-surface-500 leading-relaxed">{description}</p>
      )}
      {action && (
        <div className="mt-4">
          <Button variant="secondary" size="md" onClick={action.onClick}>
            {action.icon && <Icon name={action.icon} className="w-4 h-4" />}
            {action.label}
          </Button>
        </div>
      )}
      {children}
    </div>
  )
}
