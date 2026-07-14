import { Form, FormGroup, ToggleGroup, ToggleGroupItem } from '@patternfly/react-core'
import {
  COLOR_SCHEME_OPTIONS,
  THEME_OPTIONS,
  CONTRAST_OPTIONS,
} from './settings'
import type { DisplaySettings } from './useDisplaySettings'

// The three display-preference groups, rendered as labeled toggle groups.
// Shares state with the masthead settings menu via the DisplaySettings hook.
export function DisplayPreferences({ settings }: { settings: DisplaySettings }) {
  const { colorScheme, theme, contrast, changeColorScheme, changeTheme, changeContrast } = settings
  return (
    <Form>
      <FormGroup label="Color scheme" fieldId="pref-color-scheme">
        <ToggleGroup aria-label="Color scheme">
          {COLOR_SCHEME_OPTIONS.map(([value, label]) => (
            <ToggleGroupItem
              key={value}
              text={label}
              buttonId={`pref-cs-${value}`}
              isSelected={colorScheme === value}
              onChange={() => changeColorScheme(value)}
            />
          ))}
        </ToggleGroup>
      </FormGroup>

      <FormGroup label="Theme" fieldId="pref-theme">
        <ToggleGroup aria-label="Theme">
          {THEME_OPTIONS.map(([value, label]) => (
            <ToggleGroupItem
              key={value}
              text={label}
              buttonId={`pref-theme-${value}`}
              isSelected={theme === value}
              onChange={() => changeTheme(value)}
            />
          ))}
        </ToggleGroup>
      </FormGroup>

      <FormGroup label="Contrast" fieldId="pref-contrast">
        <ToggleGroup aria-label="Contrast">
          {CONTRAST_OPTIONS.map(([value, label]) => (
            <ToggleGroupItem
              key={value}
              text={label}
              buttonId={`pref-contrast-${value}`}
              isSelected={contrast === value}
              onChange={() => changeContrast(value)}
            />
          ))}
        </ToggleGroup>
      </FormGroup>
    </Form>
  )
}
