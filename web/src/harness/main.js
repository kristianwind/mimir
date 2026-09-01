import { mount } from 'svelte'
import '../app.css'
import ArtifactGrid from '../lib/ArtifactGrid.svelte'
mount(ArtifactGrid, {
  target: document.getElementById('app'),
  props: {
    account: { id: 1 },
    characters: [
      { key: 'Arlecchino' }, { key: 'Sandrone' }, { key: 'Linnea' },
      { key: 'Nilou' }, { key: 'RaidenShogun' }, { key: 'Ayaka' },
    ],
  },
})
