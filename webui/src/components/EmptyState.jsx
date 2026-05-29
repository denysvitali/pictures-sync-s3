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
      <div className="flex items-center gap-3 rounded-xl border border-surface-700/40 bg-surface-900/40 px-3 py-4">
        <div className="relative flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-gradient-to-br from-brand-500/20 to-brand-700/10 ring-1 ring-inset ring-brand-400/15">
          {icon && <Icon name={icon} className="h-5 w-5 text-brand-300" />}
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
    <div className="flex flex-col items-center justify-center px-4 py-8 text-center" role="status" aria-live="polite">
      <div className="relative mb-5">
        {/* Soft gradient halo */}
        <div className="absolute inset-0 -m-5 rounded-full bg-gradient-to-br from-brand-500/20 via-brand-600/10 to-transparent blur-xl" aria-hidden="true" />
        <div className="relative flex h-20 w-20 items-center justify-center rounded-2xl border border-brand-400/15 bg-gradient-to-br from-surface-800/80 to-surface-900/80 shadow-card ring-1 ring-inset ring-white/5">
          {icon && (
            <Icon
              name={icon}
              className="h-10 w-10 text-brand-300 float-animation"
              aria-hidden="true"
            />
          )}
        </div>
      </div>
      <h3 className="mb-1.5 text-lg font-semibold tracking-tight text-surface-100">{title}</h3>
      {description && (
        <p className="max-w-xs text-sm leading-relaxed text-surface-400">{description}</p>
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
