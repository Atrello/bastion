<!-- Picking a folder in a game's files, to move things into: open folders to go in, the path at the top to
     go back, make a new one, then "Move here". The folders being moved can't be opened (nothing goes inside
     itself). -->
<script lang="ts">
  import Modal from './Modal.svelte';
  import { ask } from '../lib/ask.svelte';
  import { api } from '../lib/api';

  let {
    id,
    from,
    moving,
    onpick,
    onclose,
  }: { id: number; from: string; moving: string[]; onpick: (to: string) => void; onclose: () => void } = $props();

  let at = $state('/');
  let folders = $state<string[]>();
  let error = $state('');

  async function list() {
    folders = undefined;
    error = '';
    try {
      folders = (await api.gameFiles(id, at)).filter((f) => f.dir).map((f) => f.name);
    } catch (err) {
      error = (err as Error).message;
    }
  }
  $effect(() => {
    void at;
    void list();
  });

  const crumbs = $derived(at.split('/').filter(Boolean));
  // One of the things being moved: going into it would put it inside itself.
  const isMoving = (name: string) => at === from && moving.includes(name);

  async function newFolder() {
    const name = await ask({ title: 'New folder', message: `In ${at}`, input: { label: 'Its name', required: true }, confirm: 'Make it' });
    if (!name) return;
    try {
      await api.makeGameFolder(id, at + name);
      at = `${at}${name}/`;
    } catch (err) {
      error = (err as Error).message;
    }
  }
</script>

<Modal title="Move {moving.length === 1 ? moving[0] : `${moving.length} items`}" {onclose}>
  <nav class="crumbs mono" aria-label="Folder">
    <button class="link" onclick={() => (at = '/')}>home</button>
    {#each crumbs as part, i (i)}
      / <button class="link" onclick={() => (at = `/${crumbs.slice(0, i + 1).join('/')}/`)}>{part}</button>
    {/each}
  </nav>
  <ul class="folders">
    {#each folders ?? [] as name (name)}
      <li>
        <button class="folder" disabled={isMoving(name)} onclick={() => (at = `${at}${name}/`)}>
          <svg class="icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" /></svg>
          <span>{name}</span>
          {#if isMoving(name)}<span class="muted small">being moved</span>{/if}
        </button>
      </li>
    {:else}
      {#if folders}<li class="muted small none">No folders in here.</li>{/if}
    {/each}
  </ul>
  {#if error}<p class="error">{error}</p>{/if}
  {#snippet footer()}
    <button class="grow-right" onclick={newFolder}>New folder</button>
    <button onclick={onclose}>Cancel</button>
    <button class="primary" disabled={at === from} title={at === from ? "They're in this folder already" : undefined} onclick={() => onpick(at)}>
      Move to {at}
    </button>
  {/snippet}
</Modal>

<style>
  .crumbs {
    margin: 0 0 10px;
    overflow-wrap: anywhere;
    color: var(--muted);
  }
  button.link {
    border: 0;
    background: none;
    padding: 0;
    color: var(--accent);
    font-weight: 400;
  }
  .folders {
    list-style: none;
    margin: 0;
    padding: 0;
    border: 1px solid var(--line);
    border-radius: 8px;
    max-height: 320px;
    overflow-y: auto;
  }
  .folders li + li {
    border-top: 1px solid var(--line);
  }
  .folder {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    border-radius: 0;
    padding: 9px 12px;
  }
  .folder:hover:not(:disabled) {
    background: var(--sunken);
  }
  .folder span:first-of-type {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .icon {
    width: 16px;
    height: 16px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.8;
    flex: none;
  }
  .none {
    padding: 10px 12px;
  }
  .grow-right {
    margin-right: auto;
  }
</style>
