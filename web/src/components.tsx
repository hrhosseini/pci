import type { ReactNode } from 'react'

export function Icon({ name, className }: { name: string; className?: string }) {
  const common = {
    className: className ?? 'icon',
    width: 16,
    height: 16,
    viewBox: '0 0 24 24',
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 1.75,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
    'aria-hidden': true as const,
  }
  switch (name) {
    case 'dashboard':
      return (
        <svg {...common}>
          <rect x="3" y="3" width="7" height="9" rx="1" />
          <rect x="14" y="3" width="7" height="5" rx="1" />
          <rect x="14" y="12" width="7" height="9" rx="1" />
          <rect x="3" y="16" width="7" height="5" rx="1" />
        </svg>
      )
    case 'topology':
      return (
        <svg {...common}>
          <circle cx="6" cy="6" r="2.5" />
          <circle cx="18" cy="6" r="2.5" />
          <circle cx="12" cy="18" r="2.5" />
          <path d="M8 7.5 10.5 15M16 7.5 13.5 15M8.5 6h7" />
        </svg>
      )
    case 'policies':
      return (
        <svg {...common}>
          <path d="M8 4h8l4 4v12a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2z" />
          <path d="M16 4v4h4M9 13h6M9 17h4" />
        </svg>
      )
    case 'results':
      return (
        <svg {...common}>
          <path d="M4 19V5M4 19h16M8 15v-4M12 15V8M16 15v-6" />
        </svg>
      )
    case 'findings':
      return (
        <svg {...common}>
          <circle cx="11" cy="11" r="7" />
          <path d="m20 20-3.5-3.5M11 8v3l2 2" />
        </svg>
      )
    case 'comparison':
      return (
        <svg {...common}>
          <path d="M8 5H5v14h3M16 5h3v14h-3M10 12h4" />
        </svg>
      )
    case 'snapshots':
      return (
        <svg {...common}>
          <rect x="4" y="5" width="16" height="14" rx="2" />
          <path d="M8 5V3h8v2M9 12h6" />
        </svg>
      )
    case 'verification':
      return (
        <svg {...common}>
          <path d="M12 3 4 7v5c0 5 3.5 8.5 8 9 4.5-.5 8-4 8-9V7l-8-4z" />
          <path d="m9 12 2 2 4-4" />
        </svg>
      )
    case 'reports':
      return (
        <svg {...common}>
          <path d="M7 4h7l5 5v11a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2z" />
          <path d="M14 4v5h5M8 13h8M8 17h5" />
        </svg>
      )
    case 'settings':
      return (
        <svg {...common}>
          <circle cx="12" cy="12" r="3" />
          <path d="M12 3v2M12 19v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M3 12h2M19 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
        </svg>
      )
    case 'refresh':
      return (
        <svg {...common}>
          <path d="M21 12a9 9 0 1 1-2.6-6.4M21 3v6h-6" />
        </svg>
      )
    default:
      return (
        <svg {...common}>
          <circle cx="12" cy="12" r="8" />
        </svg>
      )
  }
}

export function Badge({
  children,
  tone = 'neutral',
  title,
}: {
  children: ReactNode
  tone?: 'neutral' | 'info' | 'success' | 'warning' | 'danger' | 'muted'
  title?: string
}) {
  return (
    <span className={`badge badge-${tone}`} title={title}>
      {children}
    </span>
  )
}

export function Button({
  children,
  onClick,
  variant = 'secondary',
  disabled,
  type = 'button',
  title,
}: {
  children: ReactNode
  onClick?: () => void
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger'
  disabled?: boolean
  type?: 'button' | 'submit'
  title?: string
}) {
  return (
    <button type={type} className={`btn btn-${variant}`} onClick={onClick} disabled={disabled} title={title}>
      {children}
    </button>
  )
}

export function PageHeader({
  title,
  description,
  actions,
  breadcrumbs,
}: {
  title: string
  description?: string
  actions?: ReactNode
  breadcrumbs?: string[]
}) {
  return (
    <header className="page-header">
      <div className="page-header-text">
        {breadcrumbs && breadcrumbs.length > 0 && (
          <p className="breadcrumbs" aria-label="Breadcrumb">
            {breadcrumbs.join(' / ')}
          </p>
        )}
        <h2>{title}</h2>
        {description && <p className="page-description">{description}</p>}
      </div>
      {actions && <div className="page-actions">{actions}</div>}
    </header>
  )
}

export function EmptyState({
  title,
  body,
  action,
}: {
  title: string
  body?: string
  action?: ReactNode
}) {
  return (
    <div className="empty-state-card" role="status">
      <strong>{title}</strong>
      {body && <p>{body}</p>}
      {action}
    </div>
  )
}

export function ErrorBanner({
  title,
  message,
  detail,
  onRetry,
}: {
  title: string
  message: string
  detail?: string
  onRetry?: () => void
}) {
  return (
    <div className="error-banner" role="alert">
      <div>
        <strong>{title}</strong>
        <p>{message}</p>
        {detail && (
          <details>
            <summary>Technical details</summary>
            <pre>{detail}</pre>
          </details>
        )}
      </div>
      {onRetry && (
        <Button variant="secondary" onClick={onRetry}>
          Retry
        </Button>
      )}
    </div>
  )
}

export function Skeleton({ rows = 3 }: { rows?: number }) {
  return (
    <div className="skeleton-stack" aria-busy="true" aria-label="Loading">
      {Array.from({ length: rows }, (_, index) => (
        <div key={index} className="skeleton-row" />
      ))}
    </div>
  )
}

export function Panel({
  title,
  meta,
  children,
  className,
}: {
  title?: string
  meta?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <section className={`panel ${className ?? ''}`}>
      {(title || meta) && (
        <div className="panel-header">
          {title ? <h3>{title}</h3> : <span />}
          {meta}
        </div>
      )}
      {children}
    </section>
  )
}

export function ConnectionStrip({
  apiOk,
  apiLabel,
  discoveryLabel,
  discoveryTone,
  verificationLabel,
  verificationTone,
}: {
  apiOk: boolean
  apiLabel: string
  discoveryLabel: string
  discoveryTone: 'success' | 'warning' | 'muted' | 'danger'
  verificationLabel: string
  verificationTone: 'success' | 'warning' | 'muted' | 'danger'
}) {
  return (
    <div className="connection-strip" aria-label="Connection status">
      <span className={`conn-pill ${apiOk ? 'success' : 'danger'}`}>
        <span className="conn-dot" aria-hidden />
        API · {apiLabel}
      </span>
      <span className={`conn-pill ${discoveryTone}`}>
        <span className="conn-dot" aria-hidden />
        Discovery · {discoveryLabel}
      </span>
      <span className={`conn-pill ${verificationTone}`}>
        <span className="conn-dot" aria-hidden />
        Verification · {verificationLabel}
      </span>
    </div>
  )
}
