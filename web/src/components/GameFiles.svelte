<!-- A game's files: browse folders, edit text files, upload, download, rename, delete and make folders. -->
<script lang="ts">
  import Modal from './Modal.svelte';
  import { api } from '../lib/api';
  import { ago, bytes } from '../lib/format';
  import type { GameFile } from '../lib/types';

  let { id, onerror, onnote }: { id: number; onerror: (message: string) => void; onnote: (message: string) => void } = $props();

  const editor = () => import('./CodeEditor.svelte');
  // Files that open in the editor rather than downloading.
  const TEXT = /\.(txt|properties|ya?ml|json|toml|ini|cfg|conf|log|sh|md|xml|csv|env|lua|js|ts|py|txt)$|^[^.]+$/i;

  let path = $state('/');
  let files = $state<GameFile[]>();
  let editing = $state<{ path: string; content: string; saved: string }>();
  let renaming = $state<{ from: string; to: string }>();
  let uploading = $state('');
  let input: HTMLInputElement;

  async function list() {
    try {
      files = await api.gameFiles(id, path);
    } catch (err) {
      onerror((err as Error).message);
    }
  }
  $effect(() => {
    void path;
    void list();
  });

  const join = (name: string) => path + name;
  const crumbs = $derived(path.split('/').filter(Boolean));

  async function attempt(work: () => Promise<unknown>, note?: string) {
    try {
      await work();
      if (note) onnote(note);
      await list();
    } catch (err) {
      onerror((err as Error).message);
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

  async function upload() {
    const chosen = [...(input.files ?? [])];
    input.value = '';
    for (const file of chosen) {
      uploading = file.name;
      try {
        await api.uploadGameFile(id, join(file.name), file);
      } catch (err) {
        onerror(`${file.name}: ${(err as Error).message}`);
        break;
      }
    }
    uploading = '';
    await list();
  }

  function newFolder() {
    const name = prompt('Name of the new folder')?.trim();
    if (name) void attempt(() => api.makeGameFolder(id, join(name)));
  }

  function remove(f: GameFile) {
    if (confirm(`Delete ${f.name}${f.dir ? ' and everything in it' : ''}? This can't be undone.`)) {
      void attempt(() => api.deleteGameFile(id, join(f.name)), `Deleted ${f.name}.`);
    }
  }
</script>

<div class="row top">
  <nav class="crumbs mono grow" aria-label="Folder">
    <button class="link" onclick={() => (path = '/')}>home</button>
    {#each crumbs as part, i (i)}
      / <button class="link" onclick={() => (path = `/${crumbs.slice(0, i + 1).join('/')}/`)}>{part}</button>
    {/each}
  </nav>
  {#if uploading}<span class="muted small">Uploading {uploading}…</span>{/if}
  <button onclick={newFolder}>New folder</button>
  <button onclick={() => input.click()} disabled={!!uploading}>Upload</button>
  <input bind:this={input} type="file" multiple hidden onchange={upload} />
</div>

<div class="card table-card">
  <table>
    <thead><tr><th>Name</th><th>Size</th><th>Changed</th><th></th></tr></thead>
    <tbody>
      {#each files ?? [] as f (f.name)}
        <tr>
          <td>
            <button class="link name" onclick={() => open(f)}>
              <span class="icon" aria-hidden="true">{f.dir ? '▸' : ''}</span>{f.name}{f.dir ? '/' : ''}
            </button>
          </td>
          <td class="muted">{f.dir ? '' : bytes(f.size)}</td>
          <td class="muted">{ago(f.modified)}</td>
          <td class="buttons">
            {#if !f.dir}<a class="button" href={api.gameFileDownload(id, join(f.name))}>Download</a>{/if}
            <button onclick={() => (renaming = { from: join(f.name), to: join(f.name) })}>Rename</button>
            <button class="danger" onclick={() => remove(f)}>Delete</button>
          </td>
        </tr>
      {:else}
        {#if files}<tr><td colspan="4" class="empty">This folder is empty.</td></tr>{/if}
      {/each}
    </tbody>
  </table>
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
          attempt(async () => {
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
        void attempt(() => api.renameGameFile(id, r.from, r.to)).then(() => (renaming = undefined));
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
    font-family: inherit;
  }
  .icon {
    display: inline-block;
    width: 14px;
    color: var(--muted);
  }
  .buttons {
    text-align: right;
  }
  .buttons > * {
    margin-left: 6px;
    padding: 3px 10px;
  }
</style>
