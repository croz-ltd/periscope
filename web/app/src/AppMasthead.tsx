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
import type { ColorScheme, ThemeStyle, Contrast } from './settings'
import {
  getColorScheme,
  getTheme,
  getContrast,
  setColorScheme,
  setTheme,
  setContrast,
} from './settings'

interface Props {
  isSidebarOpen: boolean
  onSidebarToggle: () => void
}

export function AppMasthead({ isSidebarOpen, onSidebarToggle }: Props) {
  const [colorScheme, setColorSchemeState] = useState<ColorScheme>(getColorScheme)
  const [theme, setThemeState] = useState<ThemeStyle>(getTheme)
  const [contrast, setContrastState] = useState<Contrast>(getContrast)

  const changeColorScheme = (cs: ColorScheme) => {
    setColorScheme(cs)
    setColorSchemeState(cs)
  }
  const changeTheme = (t: ThemeStyle) => {
    setTheme(t)
    setThemeState(t)
  }
  const changeContrast = (c: Contrast) => {
    setContrast(c)
    setContrastState(c)
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
                  Color scheme
                </Content>
                <ToggleGroup aria-label="Color scheme">
                  <ToggleGroupItem
                    icon={<SunIcon />}
                    aria-label="Light color scheme"
                    buttonId="cs-light"
                    isSelected={colorScheme === 'light'}
                    onChange={() => changeColorScheme('light')}
                  />
                  <ToggleGroupItem
                    icon={<MoonIcon />}
                    aria-label="Dark color scheme"
                    buttonId="cs-dark"
                    isSelected={colorScheme === 'dark'}
                    onChange={() => changeColorScheme('dark')}
                  />
                  <ToggleGroupItem
                    icon={<DesktopIcon />}
                    aria-label="System color scheme"
                    buttonId="cs-system"
                    isSelected={colorScheme === 'system'}
                    onChange={() => changeColorScheme('system')}
                  />
                </ToggleGroup>
              </ToolbarItem>

              <ToolbarItem>
                <Content component="small" className="cc-ctl-label">
                  Theme
                </Content>
                <ToggleGroup aria-label="Theme">
                  <ToggleGroupItem
                    text="Default"
                    buttonId="theme-default"
                    isSelected={theme === 'default'}
                    onChange={() => changeTheme('default')}
                  />
                  <ToggleGroupItem
                    text="Felt"
                    buttonId="theme-felt"
                    isSelected={theme === 'felt'}
                    onChange={() => changeTheme('felt')}
                  />
                </ToggleGroup>
              </ToolbarItem>

              <ToolbarItem>
                <Content component="small" className="cc-ctl-label">
                  Contrast
                </Content>
                <ToggleGroup aria-label="Contrast">
                  <ToggleGroupItem
                    text="Default"
                    buttonId="contrast-default"
                    isSelected={contrast === 'default'}
                    onChange={() => changeContrast('default')}
                  />
                  <ToggleGroupItem
                    text="High Contrast"
                    buttonId="contrast-high"
                    isSelected={contrast === 'high-contrast'}
                    onChange={() => changeContrast('high-contrast')}
                  />
                  <ToggleGroupItem
                    text="Glass"
                    buttonId="contrast-glass"
                    isSelected={contrast === 'glass'}
                    onChange={() => changeContrast('glass')}
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
