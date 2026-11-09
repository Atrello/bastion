<!-- Starting a new game server: which egg, which server it runs on, its memory and port. -->
<script lang="ts">
  import Modal from './Modal.svelte';
  import { api } from '../lib/api';
  import { load } from '../lib/load.svelte';
  import { navigate } from '../lib/router.svelte';

  let { onclose }: { onclose: () => void } = $props();
  const eggs = load(api.eggs);
  const servers = load(api.servers);

  let name = $state('');
  let eggId = $state('');
  let serverId = $state('');
  let memoryMb = $state(4096);
  let port = $state(25565);
  let error = $state<string>();
  let busy = $state(false);

  async function create(e: SubmitEvent) {
    e.preventDefault();
    busy = true;
    try {
      const game = await api.createGame({ name: name.trim(), eggId: Number(eggId), serverId: Number(serverId), memoryMb, port });
      navigate(`/games/${game.id}`);
    } catch (err) {
      error = (err as Error).message;
      busy = false;
    }
  }
</script>

<Modal title="New game server" {onclose}>
  <form id="new-game" class="form" onsubmit={create}>
    <label>Name <input required maxlength="60" placeholder="e.g. Survival SMP" bind:value={name} /></label>
    <label>
      Game
      <select required bind:value={eggId}>
        <option value="" disabled>Pick an egg</option>
        {#each eggs.data ?? [] as egg (egg.id)}<option value={String(egg.id)}>{egg.name} ({egg.category})</option>{/each}
      </select>
    </label>
    <label>
      Runs on
      <select required bind:value={serverId}>
        <option value="" disabled>Pick a server</option>
        {#each (servers.data ?? []).filter((s) => s.online) as s (s.id)}<option value={String(s.id)}>{s.name}</option>{/each}
      </select>
    </label>
    <div class="pair">
      <label>Memory (MB) <input type="number" min="256" step="256" required bind:value={memoryMb} /></label>
      <label>Port <input type="number" min="1024" max="65535" required bind:value={port} /></label>
    </div>
    <p class="muted small">The egg's installer runs first, so it can take a few minutes before the server starts.</p>
    {#if error}<p class="error">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <button onclick={onclose}>Cancel</button>
    <button type="submit" form="new-game" class="primary" disabled={busy}>{busy ? 'Creating…' : 'Create'}</button>
  {/snippet}
</Modal>

<style>
  .form {
    display: grid;
    gap: 12px;
  }
  label {
    display: grid;
    gap: 6px;
  }
  .pair {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }
</style>
