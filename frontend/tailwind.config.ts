import type { Config } from 'tailwindcss'

// Monochrome palette per DESIGN.md — token names kept so existing pages restyle automatically.
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        page:          '#fafafa',
        panel:         '#ffffff',
        'panel-muted': '#fafafa',
        'panel-soft':  '#f4f4f5',
        accent:        '#18181b',
        'accent-soft': '#f4f4f5',
        brand:         '#18181b',
        success:       '#16a34a',
        'success-soft':'#f0fdf4',
        warning:       '#d97706',
        danger:        '#dc2626',
        border:        '#e4e4e7',
        'border-strong':'#d4d4d8',
        ink:           '#18181b',
        muted:         '#71717a',
        faint:         '#a1a1aa',
      },
      fontFamily: {
        sans:    ['Inter', 'Noto Sans TC', 'sans-serif'],
        display: ['Inter', 'Noto Sans TC', 'sans-serif'],
        mono:    ['JetBrains Mono', 'monospace'],
      },
      borderRadius: {
        card:    '12px',
        control: '8px',
        pill:    '999px',
      },
      boxShadow: {
        card: '0 1px 2px rgba(0,0,0,0.04)',
        soft: '0 1px 2px rgba(0,0,0,0.03)',
      },
    },
  },
  plugins: [],
} satisfies Config
