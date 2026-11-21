/**
 * Design tokens (Option A · Lavender Light). Every color is a CSS variable
 * defined in src/index.css, so a theme is just a different set of variables.
 * Use these semantic names in components, never raw palette colors.
 */
const token = (name: string) => `rgb(var(--c-${name}) / <alpha-value>)`

export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      fontFamily: {
        sans: ['Inter', '-apple-system', 'BlinkMacSystemFont', 'Segoe UI', 'Roboto', 'Helvetica Neue', 'Arial', 'sans-serif'],
        mono: ['"JetBrains Mono"', 'ui-monospace', 'SFMono-Regular', 'Menlo', 'monospace'],
      },
      colors: {
        canvas: token('canvas'),
        surface: token('surface'),
        subtle: token('subtle'),
        line: { DEFAULT: token('line'), strong: token('line-strong') },
        ink: token('ink'),
        muted: token('muted'),
        primary: {
          DEFAULT: token('primary'),
          hover: token('primary-hover'),
          soft: token('primary-soft'),
          ink: token('primary-ink'),
        },
        attention: {
          DEFAULT: token('attention'),
          hover: token('attention-hover'),
          soft: token('attention-soft'),
          line: token('attention-line'),
          ink: token('attention-ink'),
        },
        danger: {
          DEFAULT: token('danger'),
          hover: token('danger-hover'),
          soft: token('danger-soft'),
          ink: token('danger-ink'),
        },
        success: {
          DEFAULT: token('success'),
          soft: token('success-soft'),
          ink: token('success-ink'),
        },
      },
      borderRadius: {
        control: '10px',
      },
      boxShadow: {
        card: '0 1px 2px rgba(28, 22, 49, 0.04)',
        pop: '0 8px 24px -6px rgba(28, 22, 49, 0.16)',
      },
      animation: {
        'fade-in': 'fadeIn 0.2s ease-out',
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0', transform: 'translateY(4px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' },
        },
      },
    },
  },
  plugins: [],
}
