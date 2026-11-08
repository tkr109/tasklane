import type { SVGProps } from 'react'

type IconProps = SVGProps<SVGSVGElement>

const stroke = { fill: 'none', stroke: 'currentColor', strokeWidth: 1.5 } as const

export function Logo(props: IconProps) {
  return (
    <svg width="26" height="26" viewBox="0 0 26 26" aria-hidden="true" {...props}>
      <rect width="26" height="26" rx="7" fill="var(--accent)" />
      <path d="M7 9h12M7 13h8M7 17h10" stroke="var(--on-accent)" strokeWidth="2" strokeLinecap="round" />
      <circle cx="19" cy="13" r="1.6" fill="var(--on-accent)" />
    </svg>
  )
}

export function OverviewIcon(props: IconProps) {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" {...stroke} {...props}>
      <rect x="2" y="2" width="5" height="5" rx="1" />
      <rect x="9" y="2" width="5" height="5" rx="1" />
      <rect x="2" y="9" width="5" height="5" rx="1" />
      <rect x="9" y="9" width="5" height="5" rx="1" />
    </svg>
  )
}

export function JobsIcon(props: IconProps) {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" {...stroke} strokeLinecap="round" {...props}>
      <path d="M5 4h9M5 8h9M5 12h9" />
      <circle cx="2.2" cy="4" r=".6" />
      <circle cx="2.2" cy="8" r=".6" />
      <circle cx="2.2" cy="12" r=".6" />
    </svg>
  )
}

export function WorkersIcon(props: IconProps) {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" {...stroke} {...props}>
      <rect x="2" y="3" width="12" height="4" rx="1" />
      <rect x="2" y="9" width="12" height="4" rx="1" />
      <path d="M5 5h.01M5 11h.01" strokeLinecap="round" strokeWidth="2" />
    </svg>
  )
}

export function TeamsIcon(props: IconProps) {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" {...stroke} {...props}>
      <circle cx="6" cy="6" r="2.5" />
      <path d="M1.8 13.5c.6-2.2 2.2-3.3 4.2-3.3s3.6 1.1 4.2 3.3" />
      <circle cx="11.5" cy="5.5" r="2" />
      <path d="M11 9.7c1.6 0 2.8.9 3.3 2.6" />
    </svg>
  )
}

export function SchedulesIcon(props: IconProps) {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" {...stroke} strokeLinecap="round" {...props}>
      <circle cx="8" cy="8" r="6" />
      <path d="M8 4.5V8l2.3 1.5" />
    </svg>
  )
}

export function DeadIcon(props: IconProps) {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" {...stroke} strokeLinecap="round" {...props}>
      <circle cx="8" cy="8" r="6" />
      <path d="M8 4.8v3.6M8 11h.01" />
    </svg>
  )
}

export function MoonIcon(props: IconProps) {
  return (
    <svg width="17" height="17" viewBox="0 0 20 20" aria-hidden="true" {...stroke} strokeWidth="1.6" {...props}>
      <path d="M16.5 12.2A7 7 0 0 1 7.8 3.5a7 7 0 1 0 8.7 8.7z" />
    </svg>
  )
}

export function DownloadIcon(props: IconProps) {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true" {...stroke} strokeWidth="1.8" strokeLinecap="round" {...props}>
      <path d="M8 2v9M4 7l4 4 4-4M3 14h10" />
    </svg>
  )
}

export function BackIcon(props: IconProps) {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true" {...stroke} strokeWidth="1.8" strokeLinecap="round" {...props}>
      <path d="M10 3L5 8l5 5" />
    </svg>
  )
}
