// Display settings (theme / contrast / drift color scheme), persisted to
// localStorage and applied as classes on <html>. PatternFly reads the theme
// class (pf-v6-theme-dark); the contrast and color-scheme classes drive our
// own CSS overrides in styles.css.

export type Theme = 'light' | 'dark' | 'system'
export type Contrast = 'standard' | 'high'
export type ColorScheme = 'default' | 'colorblind'

const KEY = {
  theme: 'cc.theme',
  contrast: 'cc.contrast',
  colorScheme: 'cc.colorScheme',
} as const

const DARK_CLASS = 'pf-v6-theme-dark'
const HC_CLASS = 'cc-contrast-high'
const CB_CLASS = 'cc-cb'

const THEMES: readonly Theme[] = ['light', 'dark', 'system']
const CONTRASTS: readonly Contrast[] = ['standard', 'high']
const SCHEMES: readonly ColorScheme[] = ['default', 'colorblind']

const mql = window.matchMedia('(prefers-color-scheme: dark)')

function read<T extends string>(key: string, fallback: T, allowed: readonly T[]): T {
  const v = localStorage.getItem(key)
  return v && (allowed as readonly string[]).includes(v) ? (v as T) : fallback
}

export const getTheme = (): Theme => read(KEY.theme, 'system', THEMES)
export const getContrast = (): Contrast => read(KEY.contrast, 'standard', CONTRASTS)
export const getColorScheme = (): ColorScheme => read(KEY.colorScheme, 'default', SCHEMES)

const resolveDark = (theme: Theme): boolean =>
  theme === 'dark' || (theme === 'system' && mql.matches)

const root = () => document.documentElement

export function applyTheme(theme: Theme): void {
  root().classList.toggle(DARK_CLASS, resolveDark(theme))
}
export function applyContrast(c: Contrast): void {
  root().classList.toggle(HC_CLASS, c === 'high')
}
export function applyColorScheme(cs: ColorScheme): void {
  root().classList.toggle(CB_CLASS, cs === 'colorblind')
}

export function setTheme(theme: Theme): void {
  localStorage.setItem(KEY.theme, theme)
  applyTheme(theme)
}
export function setContrast(c: Contrast): void {
  localStorage.setItem(KEY.contrast, c)
  applyContrast(c)
}
export function setColorScheme(cs: ColorScheme): void {
  localStorage.setItem(KEY.colorScheme, cs)
  applyColorScheme(cs)
}

/** Apply all persisted settings once at startup. */
export function applyInitialSettings(): void {
  applyTheme(getTheme())
  applyContrast(getContrast())
  applyColorScheme(getColorScheme())
}

/** Keep the 'system' theme live as the OS preference changes. */
export function watchSystemTheme(): void {
  mql.addEventListener('change', () => {
    if (getTheme() === 'system') applyTheme('system')
  })
}
