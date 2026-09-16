import { mount } from 'svelte'
import '../app.css'
import ArtifactGrid from '../lib/ArtifactGrid.svelte'
import KvasirChat from '../lib/KvasirChat.svelte'
import System from '../lib/System.svelte'
import Kvasir from '../lib/Kvasir.svelte'
import Users from '../lib/Users.svelte'

const what = new URLSearchParams(location.search).get('c') ?? 'grid'
const target = document.getElementById('app')

if (what === 'opinion') {
  mount(Kvasir, { target, props: { account: { id: 1 }, surface: 'roster' } })
} else if (what === 'system') {
  mount(System, { target, props: { user: { role: 'admin' }, hosted: true } })
} else if (what === 'users') {
  // The sole administrator. This is the case that mattered: every control
  // that takes something away is correctly hidden for him, and "Give free
  // access" has to still be there — it is the only way an operator whose
  // trial has run out gets the product back.
  mount(Users, { target, props: { me: { id: 1, role: 'admin' } } })
} else if (what === 'chat') {
  mount(KvasirChat, { target, props: { account: { id: 1 } } })
} else {
  mount(ArtifactGrid, {
    target,
    props: {
      account: { id: 1 },
      characters: [
        { key: 'Arlecchino' }, { key: 'Sandrone' }, { key: 'Linnea' },
        { key: 'Nilou' }, { key: 'RaidenShogun' }, { key: 'Ayaka' },
      ],
    },
  })
}
