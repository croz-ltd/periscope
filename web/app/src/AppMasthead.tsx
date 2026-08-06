import { useEffect, useState } from 'react'
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
  Dropdown,
  DropdownGroup,
  DropdownList,
  DropdownItem,
  Divider,
  MenuToggle,
  Modal,
  ModalHeader,
  ModalBody,
} from '@patternfly/react-core'
import {
  BarsIcon,
  QuestionCircleIcon,
  CogIcon,
  UserIcon,
  ExternalLinkAltIcon,
} from '@patternfly/react-icons'
import { COLOR_SCHEME_OPTIONS, THEME_OPTIONS, CONTRAST_OPTIONS } from './settings'
import { useDisplaySettings } from './useDisplaySettings'
import { DisplayPreferences } from './DisplayPreferences'
import { PeriscopeLogo } from './PeriscopeLogo'
import { REPO_URL } from './project'
import { fetchUser } from './api'

interface Props {
  isSidebarOpen: boolean
  onSidebarToggle: () => void
  onAbout: () => void
}

export function AppMasthead({ isSidebarOpen, onSidebarToggle, onAbout }: Props) {
  const settings = useDisplaySettings()

  const [helpOpen, setHelpOpen] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [userOpen, setUserOpen] = useState(false)
  const [prefsOpen, setPrefsOpen] = useState(false)
  const [user, setUser] = useState('')

  useEffect(() => {
    void fetchUser().then((u) => setUser(u.user))
  }, [])

  const help = (
    <Dropdown
      isOpen={helpOpen}
      onOpenChange={setHelpOpen}
      onSelect={() => setHelpOpen(false)}
      popperProps={{ position: 'right' }}
      toggle={(toggleRef) => (
        <MenuToggle
          ref={toggleRef}
          aria-label="Help"
          variant="plain"
          isExpanded={helpOpen}
          onClick={() => setHelpOpen((v) => !v)}
        >
          <QuestionCircleIcon />
        </MenuToggle>
      )}
    >
      <DropdownList>
        <DropdownItem key="about" onClick={onAbout}>
          About
        </DropdownItem>
        <DropdownItem
          key="source"
          component="a"
          href={REPO_URL}
          target="_blank"
          rel="noreferrer"
          icon={<ExternalLinkAltIcon />}
        >
          Source
        </DropdownItem>
      </DropdownList>
    </Dropdown>
  )

  const renderGroup = <T extends string>(
    label: string,
    options: [T, string][],
    current: T,
    onChange: (v: T) => void,
  ) => (
    <DropdownGroup label={label}>
      <DropdownList>
        {options.map(([value, text]) => (
          <DropdownItem key={value} isSelected={current === value} onClick={() => onChange(value)}>
            {text}
          </DropdownItem>
        ))}
      </DropdownList>
    </DropdownGroup>
  )

  const settingsMenu = (
    <Dropdown
      isOpen={settingsOpen}
      onOpenChange={setSettingsOpen}
      popperProps={{ position: 'right' }}
      toggle={(toggleRef) => (
        <MenuToggle
          ref={toggleRef}
          aria-label="Settings"
          variant="plain"
          isExpanded={settingsOpen}
          onClick={() => setSettingsOpen((v) => !v)}
        >
          <CogIcon />
        </MenuToggle>
      )}
    >
      {renderGroup('Color scheme', COLOR_SCHEME_OPTIONS, settings.colorScheme, settings.changeColorScheme)}
      {renderGroup('Theme', THEME_OPTIONS, settings.theme, settings.changeTheme)}
      {renderGroup('Contrast', CONTRAST_OPTIONS, settings.contrast, settings.changeContrast)}
      <Divider component="li" />
      <DropdownList>
        <DropdownItem
          key="more"
          onClick={() => {
            setSettingsOpen(false)
            setPrefsOpen(true)
          }}
        >
          More display preferences
        </DropdownItem>
      </DropdownList>
    </Dropdown>
  )

  const userMenu = user ? (
    <Dropdown
      isOpen={userOpen}
      onOpenChange={setUserOpen}
      onSelect={() => setUserOpen(false)}
      popperProps={{ position: 'right' }}
      toggle={(toggleRef) => (
        <MenuToggle
          ref={toggleRef}
          aria-label="User menu"
          variant="plainText"
          icon={<UserIcon />}
          isExpanded={userOpen}
          onClick={() => setUserOpen((v) => !v)}
        >
          {user}
        </MenuToggle>
      )}
    >
      <DropdownList>
        <DropdownItem key="logout" component="a" href="/oauth/sign_out?rd=/">
          Log out
        </DropdownItem>
      </DropdownList>
    </Dropdown>
  ) : null

  return (
    <>
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
            <MastheadLogo component="a" href="#" className="cc-brand">
              <PeriscopeLogo className="cc-brand-mark" />
              <span className="cc-brand-text">
                <span className="cc-brand-eyebrow">OpenShift fleet</span>
                <span className="cc-brand-name">Periscope</span>
              </span>
            </MastheadLogo>
          </MastheadBrand>
        </MastheadMain>
        <MastheadContent>
          <Toolbar isFullHeight isStatic>
            <ToolbarContent>
              <ToolbarGroup
                variant="action-group-plain"
                align={{ default: 'alignEnd' }}
                gap={{ default: 'gapNone' }}
              >
                <ToolbarItem>{settingsMenu}</ToolbarItem>
                <ToolbarItem>{help}</ToolbarItem>
                {userMenu && <ToolbarItem>{userMenu}</ToolbarItem>}
              </ToolbarGroup>
            </ToolbarContent>
          </Toolbar>
        </MastheadContent>
      </Masthead>

      <Modal
        variant="small"
        isOpen={prefsOpen}
        onClose={() => setPrefsOpen(false)}
        aria-labelledby="cc-prefs-title"
      >
        <ModalHeader title="Display preferences" labelId="cc-prefs-title" />
        <ModalBody>
          <DisplayPreferences settings={settings} />
        </ModalBody>
      </Modal>
    </>
  )
}
