import type { ReactNode } from 'react'

interface PageHeaderProps {
  title: string
  sub?: ReactNode
  children?: ReactNode
}

export function PageHeader({ title, sub, children }: PageHeaderProps) {
  return (
    <div className="page-header">
      <div>
        <h1>{title}</h1>
        {sub && <p className="sub">{sub}</p>}
      </div>
      {children && (
        <>
          <span className="grow" />
          {children}
        </>
      )}
    </div>
  )
}
