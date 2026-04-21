import Link from 'next/link'

export const BackToManager = () => (
  <Link
    href="/manager"
    style={{
      display: 'flex',
      alignItems: 'center',
      gap: 8,
      padding: '12px 20px',
      margin: '0 0 8px 0',
      color: 'var(--theme-text)',
      textDecoration: 'none',
      fontSize: 13,
      fontWeight: 500,
      borderBottom: '1px solid var(--theme-elevation-100)',
      background: 'var(--theme-elevation-50)',
    }}
  >
    <span aria-hidden>{'←'}</span>
    <span>{'返回管理后台'}</span>
  </Link>
)
