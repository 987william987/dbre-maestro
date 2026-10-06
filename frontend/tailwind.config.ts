import type { Config } from 'tailwindcss'

// Monochrome palette per docs/explanation/ui-design-guidelines.md; token names keep existing pages stable.
export default {
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        page:          'rgb(var(--page) / <alpha-value>)',
        panel:         'rgb(var(--panel) / <alpha-value>)',
        'panel-muted': 'rgb(var(--panel-muted) / <alpha-value>)',
        'panel-soft':  'rgb(var(--panel-soft) / <alpha-value>)',
        sidebar:       'rgb(var(--sidebar) / <alpha-value>)',
        editor:        'rgb(var(--editor) / <alpha-value>)',
        accent:        'rgb(var(--accent) / <alpha-value>)',
        'accent-soft': 'rgb(var(--accent-soft) / <alpha-value>)',
        brand:         'rgb(var(--brand) / <alpha-value>)',
        success:       'rgb(var(--success) / <alpha-value>)',
        'success-soft':'rgb(var(--success-soft) / <alpha-value>)',
        warning:       'rgb(var(--warning) / <alpha-value>)',
        danger:        'rgb(var(--danger) / <alpha-value>)',
        border:        'rgb(var(--border) / <alpha-value>)',
        'border-strong':'rgb(var(--border-strong) / <alpha-value>)',
        ink:           'rgb(var(--text) / <alpha-value>)',
        muted:         'rgb(var(--text-muted) / <alpha-value>)',
        faint:         'rgb(var(--faint) / <alpha-value>)',
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
        card: 'var(--shadow)',
        soft: 'var(--shadow-soft)',
      },
    },
  },
  plugins: [],
} satisfies Config
