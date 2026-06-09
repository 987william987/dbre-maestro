import type { Config } from 'tailwindcss'

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        page:          '#f6f7fb',
        panel:         '#ffffff',
        'panel-muted': '#fbfcfe',
        'panel-soft':  '#f7f9fc',
        accent:        '#2563eb',
        'accent-soft': '#eff6ff',
        brand:         '#111827',
        success:       '#12b76a',
        'success-soft':'#ecfdf3',
        warning:       '#f79009',
        danger:        '#f04438',
        border:        '#e7ebf0',
        'border-strong':'#d0d5dd',
        ink:           '#101828',
        muted:         '#667085',
        faint:         '#98a2b3',
      },
      fontFamily: {
        sans:    ['Manrope', 'Noto Sans TC', 'sans-serif'],
        display: ['Sora', 'Manrope', 'sans-serif'],
        mono:    ['JetBrains Mono', 'monospace'],
      },
      borderRadius: {
        card:    '18px',
        control: '12px',
        pill:    '999px',
      },
      boxShadow: {
        card: '0 18px 38px rgba(16,24,40,0.06)',
        soft: '0 4px 14px rgba(16,24,40,0.05)',
      },
    },
  },
  plugins: [],
} satisfies Config
