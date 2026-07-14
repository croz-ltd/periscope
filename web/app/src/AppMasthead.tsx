import { useState } from 'react'
import {
  Masthead,
  MastheadMain,
  MastheadToggle,
  MastheadBrand,
  MastheadLogo,
  MastheadContent,
  PageToggleButton,
  Toolbar,
  ToolbarContent,
  ToolbarGroup,
  ToolbarItem,
  ToggleGroup,
  ToggleGroupItem,
  Content,
} from '@patternfly/react-core'
import { BarsIcon, SunIcon, MoonIcon, DesktopIcon } from '@patternfly/react-icons'
import type { Theme, Contrast, ColorScheme } from './settings'
import {
  getTheme,
  getContrast,
  getColorScheme,
  setTheme,
  setContrast,
  setColorScheme,
} from './settings'

interface Props {
  isSidebarOpen: boolean
  onSidebarToggle: () => void
}

export function AppMasthead({ isSidebarOpen, onSidebarToggle }: Props) {
  const [theme, setThemeState] = useState<Theme>(getTheme)
  const [contrast, setContrastState] = useState<Contrast>(getContrast)
  const [scheme, setSchemeState] = useState<ColorScheme>(getColorScheme)

  const changeTheme = (t: Theme) => {
    setTheme(t)
    setThemeState(t)
  }
  const changeContrast = (c: Contrast) => {
    setContrast(c)
    setContrastState(c)
  }
  const changeScheme = (s: ColorScheme) => {
    setColorScheme(s)
    setSchemeState(s)
  }

  return (
    <Masthead>
      <MastheadMain>
        <MastheadToggle>
          <PageToggleButton
            variant="plain"
            aria-label="Global navigation"
            isSidebarOpen={isSidebarOpen}
            onSidebarToggle={onSidebarToggle}
            id="main-nav-toggle"
            icon={<BarsIcon />}
          />
        </MastheadToggle>
        <MastheadBrand>
          <MastheadLogo component="a" href="#">
            Cluster Comparator
          </MastheadLogo>
        </MastheadBrand>
      </MastheadMain>
      <MastheadContent>
        <Toolbar isFullHeight isStatic>
          <ToolbarContent>
            <ToolbarGroup align={{ default: 'alignEnd' }} columnGap={{ default: 'columnGapLg' }}>
              <ToolbarItem>
                <Content component="small" className="cc-ctl-label">
                  Theme
                </Content>
                <ToggleGroup aria-label="Theme">
                  <ToggleGroupItem
                    icon={<SunIcon />}
                    aria-label="Light theme"
                    buttonId="theme-light"
                    isSelected={theme === 'light'}
                    onChange={() => changeTheme('light')}
                  />
                  <ToggleGroupItem
                    icon={<MoonIcon />}
                    aria-label="Dark theme"
                    buttonId="theme-dark"
                    isSelected={theme === 'dark'}
                    onChange={() => changeTheme('dark')}
                  />
                  <ToggleGroupItem
                    icon={<DesktopIcon />}
                    aria-label="System theme"
                    buttonId="theme-system"
                    isSelected={theme === 'system'}
                    onChange={() => changeTheme('system')}
                  />
                </ToggleGroup>
              </ToolbarItem>

              <ToolbarItem>
                <Content component="small" className="cc-ctl-label">
                  Contrast
                </Content>
                <ToggleGroup aria-label="Contrast">
                  <ToggleGroupItem
                    text="Standard"
                    buttonId="contrast-standard"
                    isSelected={contrast === 'standard'}
                    onChange={() => changeContrast('standard')}
                  />
                  <ToggleGroupItem
                    text="High"
                    buttonId="contrast-high"
                    isSelected={contrast === 'high'}
                    onChange={() => changeContrast('high')}
                  />
                </ToggleGroup>
              </ToolbarItem>

              <ToolbarItem>
                <Content component="small" className="cc-ctl-label">
                  Colors
                </Content>
                <ToggleGroup aria-label="Drift color scheme">
                  <ToggleGroupItem
                    text="Default"
                    buttonId="scheme-default"
                    isSelected={scheme === 'default'}
                    onChange={() => changeScheme('default')}
                  />
                  <ToggleGroupItem
                    text="Colorblind-safe"
                    buttonId="scheme-cb"
                    isSelected={scheme === 'colorblind'}
                    onChange={() => changeScheme('colorblind')}
                  />
                </ToggleGroup>
              </ToolbarItem>
            </ToolbarGroup>
          </ToolbarContent>
        </Toolbar>
      </MastheadContent>
    </Masthead>
  )
}
