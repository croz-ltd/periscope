// Display settings mapped to PatternFly 6's three native theming axes, persisted
// to localStorage and applied as classes on <html>:
//   - color scheme : light | dark | system   -> pf-v6-theme-dark
//   - theme        : default | felt          -> pf-v6-theme-felt
//   - contrast     : default | high-contrast | glass
//                       -> pf-v6-theme-high-contrast / pf-v6-theme-glass (exclusive)

export type ColorScheme = 'light' | 'dark' | 'system'
export type ThemeStyle = 'default' | 'felt'
export type Contrast = 'default' | 'high-contrast' | 'glass'

const KEY = {
  colorScheme: 'cc.colorScheme',
  theme: 'cc.theme',
  contrast: 'cc.contrast',
} as const

const DARK_CLASS = 'pf-v6-theme-dark'
const FELT_CLASS = 'pf-v6-theme-felt'
const HC_CLASS = 'pf-v6-theme-high-contrast'
const GLASS_CLASS = 'pf-v6-theme-glass'

const COLOR_SCHEMES: readonly ColorScheme[] = ['light', 'dark', 'system']
const THEMES: readonly ThemeStyle[] = ['default', 'felt']
const CONTRASTS: readonly Contrast[] = ['default', 'high-contrast', 'glass']

const mql = window.matchMedia('(prefers-color-scheme: dark)')

function read<T extends string>(key: string, fallback: T, allowed: readonly T[]): T {
  const v = localStorage.getItem(key)
  return v && (allowed as readonly string[]).includes(v) ? (v as T) : fallback
}

export const getColorScheme = (): ColorScheme => read(KEY.colorScheme, 'system', COLOR_SCHEMES)
export const getTheme = (): ThemeStyle => read(KEY.theme, 'default', THEMES)
export const getContrast = (): Contrast => read(KEY.contrast, 'default', CONTRASTS)

const resolveDark = (cs: ColorScheme): boolean =>
  cs === 'dark' || (cs === 'system' && mql.matches)

const root = () => document.documentElement

export function applyColorScheme(cs: ColorScheme): void {
  root().classList.toggle(DARK_CLASS, resolveDark(cs))
}
export function applyTheme(t: ThemeStyle): void {
  root().classList.toggle(FELT_CLASS, t === 'felt')
}
export function applyContrast(c: Contrast): void {
  // Mutually exclusive: at most one of high-contrast / glass.
  root().classList.toggle(HC_CLASS, c === 'high-contrast')
  root().classList.toggle(GLASS_CLASS, c === 'glass')
}

export function setColorScheme(cs: ColorScheme): void {
  localStorage.setItem(KEY.colorScheme, cs)
  applyColorScheme(cs)
}
export function setTheme(t: ThemeStyle): void {
  localStorage.setItem(KEY.theme, t)
  applyTheme(t)
}
export function setContrast(c: Contrast): void {
  localStorage.setItem(KEY.contrast, c)
  applyContrast(c)
}

/** Apply all persisted settings once at startup. */
export function applyInitialSettings(): void {
  applyColorScheme(getColorScheme())
  applyTheme(getTheme())
  applyContrast(getContrast())
}

/** Keep the 'system' color scheme live as the OS preference changes. */
export function watchSystemTheme(): void {
  mql.addEventListener('change', () => {
    if (getColorScheme() === 'system') applyColorScheme('system')
  })
}
