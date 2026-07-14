import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '@patternfly/react-core/dist/styles/base.css'
import './styles.css'
import App from './App'
import { applyInitialSettings, watchSystemTheme } from './settings'

// Apply persisted theme/contrast/color-scheme before first paint, then keep the
// 'system' theme in sync with the OS preference.
applyInitialSettings()
watchSystemTheme()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
