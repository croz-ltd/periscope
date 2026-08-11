// Display settings mapped to PatternFly 6's three native theming axes, persisted
// to localStorage and applied as classes on <html>:
//   - color scheme : system | light | dark  -> pf-v6-theme-dark
//   - theme        : default | felt         -> pf-v6-theme-felt
//   - contrast     : system | default | high-contrast | glass
//                       -> pf-v6-theme-high-contrast / pf-v6-theme-glass (exclusive)

export type ColorScheme = 'system' | 'light' | 'dark'
export type ThemeStyle = 'default' | 'felt'
export type Contrast = 'system' | 'default' | 'high-contrast' | 'glass'

const KEY = {
  colorScheme: 'cc.colorScheme',
  theme: 'cc.theme',
  contrast: 'cc.contrast',
} as const

const DARK_CLASS = 'pf-v6-theme-dark'
const FELT_CLASS = 'pf-v6-theme-felt'
const HC_CLASS = 'pf-v6-theme-high-contrast'
const GLASS_CLASS = 'pf-v6-theme-glass'

const COLOR_SCHEMES: readonly ColorScheme[] = ['system', 'light', 'dark']
const THEMES: readonly ThemeStyle[] = ['default', 'felt']
const CONTRASTS: readonly Contrast[] = ['system', 'default', 'high-contrast', 'glass']

// Selectable options with human labels, shared by the settings menu and modal.
export const COLOR_SCHEME_OPTIONS: [ColorScheme, string][] = [
  ['system', 'System default'],
  ['light', 'Light'],
  ['dark', 'Dark'],
]
export const THEME_OPTIONS: [ThemeStyle, string][] = [
  ['default', 'Default'],
  ['felt', 'Project Felt'],
]
export const CONTRAST_OPTIONS: [Contrast, string][] = [
  ['system', 'System default'],
  ['default', 'Default'],
  ['high-contrast', 'High contrast'],
  ['glass', 'Glass'],
]

const darkMql = window.matchMedia('(prefers-color-scheme: dark)')
const contrastMql = window.matchMedia('(prefers-contrast: more)')

function read<T extends string>(key: string, fallback: T, allowed: readonly T[]): T {
  const v = localStorage.getItem(key)
  return v && (allowed as readonly string[]).includes(v) ? (v as T) : fallback
}

export const getColorScheme = (): ColorScheme => read(KEY.colorScheme, 'system', COLOR_SCHEMES)
export const getTheme = (): ThemeStyle => read(KEY.theme, 'default', THEMES)
export const getContrast = (): Contrast => read(KEY.contrast, 'system', CONTRASTS)

const resolveDark = (cs: ColorScheme): boolean =>
  cs === 'dark' || (cs === 'system' && darkMql.matches)

const root = () => document.documentElement

export function applyColorScheme(cs: ColorScheme): void {
  root().classList.toggle(DARK_CLASS, resolveDark(cs))
}
export function applyTheme(t: ThemeStyle): void {
  root().classList.toggle(FELT_CLASS, t === 'felt')
}
export function applyContrast(c: Contrast): void {
  // System follows the OS "prefers-contrast: more" query. Only one class at a time.
  const wantHighContrast = c === 'high-contrast' || (c === 'system' && contrastMql.matches)
  root().classList.toggle(HC_CLASS, wantHighContrast)
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

/** Keep 'system' color scheme and contrast live as OS preferences change. */
export function watchSystemPreferences(): void {
  darkMql.addEventListener('change', () => {
    if (getColorScheme() === 'system') applyColorScheme('system')
  })
  contrastMql.addEventListener('change', () => {
    if (getContrast() === 'system') applyContrast('system')
  })
}
