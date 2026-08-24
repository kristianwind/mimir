import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'
import { applyTheme, storedMode, storedTheme } from './lib/theme.js'

// Applied before the app mounts: a flash of the wrong element is jarring.
applyTheme(storedTheme(), storedMode())

export default mount(App, { target: document.getElementById('app') })
