<!-- The egg library: the games Bastion can run, imported by hand from Pterodactyl's egg files (a file,
     pasted JSON, or a link to one). An egg with a name already here replaces it only after asking. -->
<script lang="ts">
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { load } from '../lib/load.svelte';
  import type { Egg } from '../lib/types';

  const eggs = load(api.eggs);
  let error = $state<string>();
  let importing = $state(false);
  let from = $state<'file' | 'paste' | 'link'>('file');
  let json = $state('');
  let url = $state('');
  let category = $state('Minecraft');
  let busy = $state(false);
  let importError = $state<string>();

  const categories = $derived([...new Set((eggs.data ?? []).map((e) => e.category))].sort());

  async function readFile(e: Event) {
    const file = (e.currentTarget as HTMLInputElement).files?.[0];
    json = file ? await file.text() : '';
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    busy = true;
    importError = undefined;
    const egg = from === 'link' ? { url: url.trim(), category } : { json, category };
    try {
      try {
        await api.importEgg(egg);
      } catch (err) {
        // The same name as one already here: replace it only if they say so.
        if (!(err instanceof ApiError && err.status === 409)) throw err;
        if (!confirm(`${err.message}. Replace it with this one? Its game servers use the new version from their next start.`)) return;
        await api.importEgg({ ...egg, replace: true });
      }
      importing = false;
      json = url = '';
      await eggs.reload();
    } catch (err) {
      importError = (err as Error).message;
    } finally {
      busy = false;
    }
  }

  async function remove(egg: Egg) {
    if (!confirm(`Delete the ${egg.name} egg?`)) return;
    try {
      await api.deleteEgg(egg.id);
      await eggs.reload();
    } catch (err) {
      error = (err as Error).message;
    }
  }
</script>

<div class="page-head">
  <h1>Eggs</h1>
  <button class="primary" onclick={() => (importing = true)}>Import an egg</button>
</div>

<p class="muted intro">
  An egg tells Bastion how to install and run a game. Pterodactyl's eggs work as they are: export one from
  Pterodactyl, or take one from a community collection, and import it here.
</p>
{#if error}<p class="error">{error}</p>{/if}
{#if eggs.data && !eggs.data.length}<p class="card empty">No eggs yet. Import one to make game servers from it.</p>{/if}

{#each categories as cat (cat)}
  <h2 class="category">{cat}</h2>
  <div class="card table-card">
    <table>
      <thead><tr><th>Egg</th><th>What it is</th><th>Game servers</th><th>By</th><th></th></tr></thead>
      <tbody>
        {#each (eggs.data ?? []).filter((e) => e.category === cat) as egg (egg.id)}
          <tr>
            <td><b>{egg.name}</b></td>
            <td class="muted description">{egg.description}</td>
            <td>{egg.servers}</td>
            <td class="muted small">{egg.author}</td>
            <td class="end">{#if !egg.servers}<button class="danger small-button" onclick={() => remove(egg)}>Delete</button>{/if}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/each}

{#if importing}
  <Modal title="Import an egg" onclose={() => (importing = false)}>
    <form id="import-egg" class="form" onsubmit={submit}>
      <div class="segmented" role="radiogroup" aria-label="From">
        {#each [['file', 'A file'], ['paste', 'Paste it'], ['link', 'A link']] as [value, label] (value)}
          <button type="button" role="radio" aria-checked={from === value} class:on={from === value} onclick={() => (from = value as typeof from)}>{label}</button>
        {/each}
      </div>
      {#if from === 'file'}
        <label>The egg's JSON file <input type="file" accept=".json,application/json" required onchange={readFile} /></label>
      {:else if from === 'paste'}
        <label>The egg's JSON <textarea class="mono" rows="8" required bind:value={json}></textarea></label>
      {:else}
        <label>Link to it <input class="mono" type="url" required placeholder="https://raw.githubusercontent.com/…/egg-paper.json" bind:value={url} /></label>
      {/if}
      <label>
        Kind of game
        <input list="egg-categories" required maxlength="40" bind:value={category} />
        <datalist id="egg-categories">{#each ['Minecraft', 'Steam', 'Voice', ...categories] as c (c)}<option value={c}></option>{/each}</datalist>
      </label>
      <p class="muted small">It's checked first: its images, installer, settings and config files.</p>
      {#if importError}<p class="error">{importError}</p>{/if}
    </form>
    {#snippet footer()}
      <button onclick={() => (importing = false)}>Cancel</button>
      <button type="submit" form="import-egg" class="primary" disabled={busy}>{busy ? 'Importing…' : 'Import'}</button>
    {/snippet}
  </Modal>
{/if}

<style>
  .intro {
    margin: -8px 0 20px;
    max-width: 760px;
  }
  .category {
    margin: 20px 0 8px;
  }
  .description {
    white-space: normal;
  }
  .end {
    text-align: right;
  }
  .small-button {
    padding: 3px 10px;
  }
</style>
