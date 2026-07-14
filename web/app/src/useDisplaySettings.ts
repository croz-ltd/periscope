import { useState } from 'react'
import type { ColorScheme, ThemeStyle, Contrast } from './settings'
import {
  getColorScheme,
  getTheme,
  getContrast,
  setColorScheme,
  setTheme,
  setContrast,
} from './settings'

export interface DisplaySettings {
  colorScheme: ColorScheme
  theme: ThemeStyle
  contrast: Contrast
  changeColorScheme: (v: ColorScheme) => void
  changeTheme: (v: ThemeStyle) => void
  changeContrast: (v: Contrast) => void
}

// Single source of truth for the display settings — instantiate ONCE (in the
// masthead) and share with the settings menu and the preferences modal so they
// stay in sync. Each change persists (via settings.ts) and applies to <html>.
export function useDisplaySettings(): DisplaySettings {
  const [colorScheme, setCs] = useState<ColorScheme>(getColorScheme)
  const [theme, setTh] = useState<ThemeStyle>(getTheme)
  const [contrast, setCo] = useState<Contrast>(getContrast)

  return {
    colorScheme,
    theme,
    contrast,
    changeColorScheme: (v) => {
      setColorScheme(v)
      setCs(v)
    },
    changeTheme: (v) => {
      setTheme(v)
      setTh(v)
    },
    changeContrast: (v) => {
      setContrast(v)
      setCo(v)
    },
  }
}
