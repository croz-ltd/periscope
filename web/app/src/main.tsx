import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '@patternfly/react-core/dist/styles/base.css'
import './styles.css'
import App from './App'

// Follow the OS light/dark preference; PatternFly 6 reads the theme class on <html>.
const mql = window.matchMedia('(prefers-color-scheme: dark)')
const applyTheme = (dark: boolean) => {
  document.documentElement.classList.toggle('pf-v6-theme-dark', dark)
}
applyTheme(mql.matches)
mql.addEventListener('change', (e) => applyTheme(e.matches))

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
