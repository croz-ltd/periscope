import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '@patternfly/react-core/dist/styles/base.css'
import './styles.css'
import App from './App'
import { applyInitialSettings, watchSystemPreferences } from './settings'

// Apply persisted color-scheme/theme/contrast before first paint, then keep the
// 'system' options in sync with the OS preferences.
applyInitialSettings()
watchSystemPreferences()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
