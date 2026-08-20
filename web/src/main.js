import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'
import { applyTheme, storedMode, storedTheme } from './lib/theme.js'
import { applyLang, lang } from './lib/lang.svelte.js'

// Both applied before the app mounts, for the same reason: a flash of the
// wrong element is jarring, and a flash of the wrong language is worse.
applyTheme(storedTheme(), storedMode())
applyLang(lang())

export default mount(App, { target: document.getElementById('app') })
