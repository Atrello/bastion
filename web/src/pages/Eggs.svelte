<!-- The egg library: the games Bastion can run, imported from Pterodactyl's egg files. -->
<script lang="ts">
  import { api } from '../lib/api';
  import { load } from '../lib/load.svelte';

  const eggs = load(api.eggs);
  let picker: HTMLInputElement;
  let error = $state<string>();

  const categories = $derived([...new Set((eggs.data ?? []).map((e) => e.category))].sort());

  async function importFile(file: File) {
    error = undefined;
    try {
      await api.importEgg(await file.text());
      await eggs.reload();
    } catch (err) {
      error = `${file.name}: ${(err as Error).message}`;
    }
  }
</script>

<div class="page-head">
  <h1>Eggs</h1>
  <button class="primary" onclick={() => picker.click()}>Import an egg</button>
  <input
    bind:this={picker}
    type="file"
    accept=".json,application/json"
    hidden
    onchange={(e) => {
      const file = e.currentTarget.files?.[0];
      e.currentTarget.value = '';
      if (file) void importFile(file);
    }}
  />
</div>

<p class="muted intro">
  An egg tells Bastion how to install and run a game. Pterodactyl's eggs work as they are: export one from
  Pterodactyl, or download one from a community collection, and import its JSON file here.
</p>
{#if error}<p class="error">{error}</p>{/if}

{#each categories as category (category)}
  <h2 class="category">{category}</h2>
  <div class="card table-card">
    <table>
      <thead><tr><th>Egg</th><th>What it is</th><th>Game servers</th><th>By</th></tr></thead>
      <tbody>
        {#each (eggs.data ?? []).filter((e) => e.category === category) as egg (egg.id)}
          <tr>
            <td><b>{egg.name}</b></td>
            <td class="muted description">{egg.description}</td>
            <td>{egg.servers}</td>
            <td class="muted small">{egg.author}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/each}

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
</style>
