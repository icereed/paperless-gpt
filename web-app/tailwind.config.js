/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        canvas: 'var(--color-canvas)',
        surface: {
          DEFAULT: 'var(--color-surface)',
          2: 'var(--color-surface-2)',
        },
        line: 'var(--color-line)',
        ink: 'var(--color-ink)',
        muted: 'var(--color-muted)',
        faint: 'var(--color-faint)',
        primary: {
          DEFAULT: 'var(--color-primary)',
          hover: 'var(--color-primary-hover)',
          ink: 'var(--color-primary-ink)',
          tint: 'var(--color-primary-tint)',
        },
        pos: {
          DEFAULT: 'var(--color-pos)',
          tint: 'var(--color-pos-tint)',
        },
        neg: {
          DEFAULT: 'var(--color-neg)',
          tint: 'var(--color-neg-tint)',
        },
        warn: {
          DEFAULT: 'var(--color-warn)',
          tint: 'var(--color-warn-tint)',
        },
      },
      boxShadow: {
        card: 'var(--shadow-card)',
        raised: 'var(--shadow-raised)',
      },
      fontSize: {
        // Fixed rem scale, ~1.2 ratio, five roles: metadata / secondary / body / section / page.
        xs: ['0.75rem', { lineHeight: '1rem' }],
        sm: ['0.875rem', { lineHeight: '1.25rem' }],
        base: ['1rem', { lineHeight: '1.5rem' }],
        lg: ['1.1875rem', { lineHeight: '1.75rem' }],
        xl: ['1.4375rem', { lineHeight: '2rem' }],
      },
      transitionTimingFunction: {
        'out-quart': 'cubic-bezier(0.25, 1, 0.5, 1)',
      },
      zIndex: {
        dropdown: '30',
        sticky: '40',
        'modal-backdrop': '50',
        modal: '60',
        toast: '70',
      },
    },
  },
  plugins: [],
};
