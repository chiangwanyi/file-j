/** @type {import('tailwindcss').Config} */
export default {
  darkMode: "class",
  content: ["./index.html", "./src/**/*.{vue,ts}"],
  theme: {
    extend: {
      colors: {
        bg: "rgb(var(--c-bg) / <alpha-value>)",
        panel: "rgb(var(--c-panel) / <alpha-value>)",
        elevated: "rgb(var(--c-elevated) / <alpha-value>)",
        sidebar: "rgb(var(--c-sidebar) / <alpha-value>)",
        line: "rgb(var(--c-line) / <alpha-value>)",
        ink: "rgb(var(--c-ink) / <alpha-value>)",
        muted: "rgb(var(--c-muted) / <alpha-value>)",
        accent: "rgb(var(--c-accent) / <alpha-value>)",
        danger: "rgb(var(--c-danger) / <alpha-value>)",
        success: "rgb(var(--c-success) / <alpha-value>)",
      },
      fontFamily: {
        sans: [
          "-apple-system",
          "BlinkMacSystemFont",
          "SF Pro Text",
          "PingFang SC",
          "Helvetica Neue",
          "Arial",
          "sans-serif",
        ],
        mono: ["SF Mono", "Menlo", "Monaco", "monospace"],
      },
      fontSize: {
        "2xs": ["10px", "13px"],
      },
      boxShadow: {
        pane: "0 1px 2px rgb(0 0 0 / 0.06), 0 0 0 0.5px rgb(0 0 0 / 0.08)",
        pop: "0 12px 32px rgb(0 0 0 / 0.18), 0 0 0 0.5px rgb(0 0 0 / 0.12)",
        dialog: "0 24px 64px rgb(0 0 0 / 0.28), 0 0 0 0.5px rgb(0 0 0 / 0.18)",
      },
      borderRadius: {
        mac: "10px",
      },
      keyframes: {
        "fade-in": {
          from: { opacity: "0" },
          to: { opacity: "1" },
        },
        "slide-up": {
          from: { opacity: "0", transform: "translateY(8px) scale(0.99)" },
          to: { opacity: "1", transform: "translateY(0) scale(1)" },
        },
        indeterminate: {
          "0%": { transform: "translateX(-100%)" },
          "100%": { transform: "translateX(300%)" },
        },
      },
      animation: {
        "fade-in": "fade-in 120ms ease-out",
        "slide-up": "slide-up 140ms ease-out",
        indeterminate: "indeterminate 1.4s ease-in-out infinite",
      },
    },
  },
  plugins: [],
};
