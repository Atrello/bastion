<!-- A game's files: browse folders, edit text files, upload (by button or dropping files on the list; the
     uploads carry on if you leave the page, lib/uploads.svelte.ts), download, rename, delete, make folders,
     and pack files into an archive or unpack one. Tick
     files to archive or delete several at once. -->
<script lang="ts">
  import { untrack } from 'svelte';
  import Modal from './Modal.svelte';
  import { ask, sure } from '../lib/ask.svelte';
  import { api } from '../lib/api';
  import { ago, bytes } from '../lib/format';
  import { landed, startUploads } from '../lib/uploads.svelte';
  import type { GameFile } from '../lib/types';

  let { id, name, onerror, onnote }: { id: number; name: string; onerror: (message: string) => void; onnote: (message: string) => void } = $props();

  const editor = () => import('./CodeEditor.svelte');
  // Files that open in the editor rather than downloading.
  const TEXT = /\.(txt|properties|ya?ml|json|toml|ini|cfg|conf|log|sh|md|xml|csv|env|lua|js|ts|py)$|^[^.]+$/i;
  const ARCHIVE = /\.(zip|tar\.gz|tgz|tar)$/i;

  let path = $state('/');
  let files = $state<GameFile[]>();
  let selected = $state<string[]>([]);
  let editing = $state<{ path: string; content: string; saved: string }>();
  let renaming = $state<{ from: string; to: string }>();
  let working = $state(''); // what's being done right now, like "Unpacking mods.zip…"
  let dropping = $state(false);
  let input: HTMLInputElement;

  async function list() {
    try {
      files = await api.gameFiles(id, path);
      selected = selected.filter((n) => files?.some((f) => f.name === n));
    } catch (err) {
      onerror((err as Error).message);
    }
  }
  $effect(() => {
    void path;
    selected = [];
    void list();
  });

  const join = (name: string) => path + name;
  const crumbs = $derived(path.split('/').filter(Boolean));
  const allSelected = $derived(!!files?.length && selected.length === files.length);

  async function attempt(doing: string, work: () => Promise<unknown>, note?: string) {
    working = doing;
    try {
      await work();
      if (note) onnote(note);
    } catch (err) {
      onerror((err as Error).message);
    } finally {
      working = '';
      await list();
    }
  }

  async function open(f: GameFile) {
    if (f.dir) {
      path = `${join(f.name)}/`;
      return;
    }
    if (!TEXT.test(f.name) || f.size > 2 * 1024 * 1024) {
      location.href = api.gameFileDownload(id, join(f.name));
      return;
    }
    try {
      const { content } = await api.readGameFile(id, join(f.name));
      editing = { path: join(f.name), content, saved: content };
    } catch (err) {
      onerror((err as Error).message);
    }
  }

  function upload(chosen: File[]) {
    if (chosen.length) startUploads(id, name, path, chosen);
  }
  // A file landing from an upload (perhaps one started before you came back to this page) shows in the list.
  $effect(() => {
    if (landed[id]) void untrack(list);
  });

  function drop(e: DragEvent) {
    e.preventDefault();
    dropping = false;
    void upload([...(e.dataTransfer?.files ?? [])]);
  }

  async function newFolder() {
    const name = await ask({ title: 'New folder', input: { label: 'Its name', placeholder: 'e.g. plugins', required: true }, confirm: 'Make it' });
    if (name) await attempt('Making the folder…', () => api.makeGameFolder(id, join(name)));
  }

  async function remove(names: string[]) {
    const what = names.length === 1 ? names[0] : `${names.length} items`;
    if (!(await sure(`Delete ${what}?`, "Folders are deleted with everything in them. It can't be undone."))) return;
    await attempt('Deleting…', async () => {
      for (const n of names) await api.deleteGameFile(id, join(n));
    }, `Deleted ${what}.`);
    selected = [];
  }

  async function archive() {
    const suggested = selected.length === 1 ? selected[0].replace(/\.[^.]+$/, '') : 'archive';
    const name = await ask({
      title: `Archive ${selected.length === 1 ? selected[0] : `${selected.length} items`}`,
      message: 'Packed into a .tar.gz in this folder.',
      input: { label: 'Archive name', value: suggested, required: true },
      confirm: 'Archive',
    });
    if (!name) return;
    const file = name.endsWith('.tar.gz') ? name : `${name}.tar.gz`;
    const names = [...selected];
    await attempt('Archiving…', () => api.archiveGameFiles(id, path, names, join(file)), `Made ${file}.`);
    selected = [];
  }

  async function unarchive(f: GameFile) {
    await attempt(`Unpacking ${f.name}…`, () => api.unarchiveGameFile(id, join(f.name)), `Unpacked ${f.name} here.`);
  }

  function toggle(name: string, on: boolean) {
    selected = on ? [...selected, name] : selected.filter((n) => n !== name);
  }
</script>

<div class="row top">
  <nav class="crumbs mono grow" aria-label="Folder">
    <button class="link" onclick={() => (path = '/')}>home</button>
    {#each crumbs as part, i (i)}
      / <button class="link" onclick={() => (path = `/${crumbs.slice(0, i + 1).join('/')}/`)}>{part}</button>
    {/each}
  </nav>
  {#if selected.length}
    <span class="muted small">{selected.length} selected</span>
    <button onclick={archive} disabled={!!working}>Archive</button>
    <button class="danger" onclick={() => remove(selected)} disabled={!!working}>Delete</button>
  {:else}
    <button onclick={newFolder} disabled={!!working}>New folder</button>
    <button onclick={() => input.click()} disabled={!!working}>Upload</button>
  {/if}
  <input
    bind:this={input}
    type="file"
    multiple
    hidden
    onchange={() => {
      const chosen = [...(input.files ?? [])];
      input.value = '';
      void upload(chosen);
    }}
  />
</div>

{#if working}<p class="working small"><span class="spinner" aria-hidden="true"></span>{working}</p>{/if}

<div
  class="card table-card drop"
  class:dropping
  role="region"
  aria-label="Files: drop files here to upload them"
  ondragover={(e) => {
    e.preventDefault();
    dropping = true;
  }}
  ondragleave={() => (dropping = false)}
  ondrop={drop}
>
  <table>
    <thead>
      <tr>
        <th class="tick">
          <input
            type="checkbox"
            aria-label="Select all"
            checked={allSelected}
            indeterminate={!!selected.length && !allSelected}
            onchange={(e) => (selected = e.currentTarget.checked ? (files ?? []).map((f) => f.name) : [])}
          />
        </th>
        <th>Name</th>
        <th>Size</th>
        <th>Changed</th>
        <th></th>
      </tr>
    </thead>
    <tbody>
      {#each files ?? [] as f (f.name)}
        <tr class:chosen={selected.includes(f.name)}>
          <td class="tick">
            <input type="checkbox" aria-label="Select {f.name}" checked={selected.includes(f.name)} onchange={(e) => toggle(f.name, e.currentTarget.checked)} />
          </td>
          <td>
            <button class="link name" onclick={() => open(f)}>
              <svg class="icon" viewBox="0 0 24 24" aria-hidden="true">
                {#if f.dir}<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />{:else}<path d="M7 3h7l5 5v13H7zM14 3v5h5" />{/if}
              </svg>{f.name}
            </button>
          </td>
          <td class="muted">{f.dir ? '' : bytes(f.size)}</td>
          <td class="muted">{ago(f.modified)}</td>
          <td class="buttons">
            {#if !f.dir && ARCHIVE.test(f.name)}<button onclick={() => unarchive(f)} disabled={!!working}>Unarchive</button>{/if}
            {#if !f.dir}<a class="button" href={api.gameFileDownload(id, join(f.name))}>Download</a>{/if}
            <button onclick={() => (renaming = { from: join(f.name), to: join(f.name) })}>Rename</button>
            <button class="danger" onclick={() => remove([f.name])}>Delete</button>
          </td>
        </tr>
      {:else}
        {#if files}<tr><td colspan="5" class="empty">This folder is empty. Drop files here to upload them.</td></tr>{/if}
      {/each}
    </tbody>
  </table>
  {#if dropping}<div class="drop-hint">Drop to upload to {path}</div>{/if}
</div>

{#if editing}
  {@const file = editing}
  <Modal title={file.path} onclose={() => (editing = undefined)}>
    {#await editor() then { default: CodeEditor }}
      <CodeEditor label={file.path} language={/\.ya?ml$/.test(file.path) ? 'yaml' : 'text'} bind:value={file.content} />
    {/await}
    {#snippet footer()}
      <span class="muted small grow">{file.content === file.saved ? 'No changes.' : 'Unsaved changes. Most games read their files when they start.'}</span>
      <button onclick={() => (editing = undefined)}>Close</button>
      <button
        class="primary"
        disabled={file.content === file.saved}
        onclick={() =>
          attempt('Saving…', async () => {
            await api.writeGameFile(id, file.path, file.content);
            file.saved = file.content;
          }, 'Saved.')}>Save</button
      >
    {/snippet}
  </Modal>
{/if}

{#if renaming}
  {@const r = renaming}
  <Modal title="Rename or move" onclose={() => (renaming = undefined)}>
    <form
      id="rename"
      class="form"
      onsubmit={(e) => {
        e.preventDefault();
        void attempt('Renaming…', () => api.renameGameFile(id, r.from, r.to)).then(() => (renaming = undefined));
      }}
    >
      <label>New path <input class="mono" required bind:value={r.to} /></label>
      <p class="muted small">A path into another folder moves it there.</p>
    </form>
    {#snippet footer()}
      <button onclick={() => (renaming = undefined)}>Cancel</button>
      <button type="submit" form="rename" class="primary">Rename</button>
    {/snippet}
  </Modal>
{/if}

<style>
  .top {
    margin-bottom: 10px;
  }
  .crumbs {
    color: var(--muted);
  }
  button.link {
    border: 0;
    background: none;
    padding: 0;
    color: var(--accent);
    font-weight: 400;
  }
  .name {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    color: var(--text);
  }
  .name:hover {
    color: var(--accent);
  }
  .icon {
    width: 16px;
    height: 16px;
    flex: none;
    fill: none;
    stroke: var(--muted);
    stroke-width: 1.7;
    stroke-linejoin: round;
  }
  .tick {
    width: 1%;
  }
  tr.chosen td {
    background: var(--accent-bg);
  }
  .buttons {
    text-align: right;
  }
  .buttons > * {
    margin-left: 6px;
    padding: 3px 10px;
  }
  .drop {
    position: relative;
  }
  .drop.dropping {
    border-color: var(--accent);
  }
  .drop-hint {
    position: absolute;
    inset: 0;
    display: grid;
    place-content: center;
    background: rgb(16 18 20 / 80%);
    border-radius: var(--radius);
    color: var(--accent);
    font-weight: 600;
    pointer-events: none;
  }
  .working {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0 0 10px;
    color: var(--muted);
  }
  .spinner {
    width: 12px;
    height: 12px;
    border: 2px solid currentColor;
    border-right-color: transparent;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
