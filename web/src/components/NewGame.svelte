<!-- A new game server: which egg, which server it runs on, its image, limits and ports. Its egg's
     installer runs straight away. -->
<script lang="ts">
  import Modal from './Modal.svelte';
  import { api } from '../lib/api';
  import { load } from '../lib/load.svelte';
  import { navigate } from '../lib/router.svelte';
  import type { EggDetail } from '../lib/types';

  let { onclose }: { onclose: () => void } = $props();
  const eggs = load(api.eggs);
  const servers = load(api.servers);

  let name = $state('');
  let eggId = $state('');
  let serverId = $state('');
  let egg = $state<EggDetail>();
  let image = $state('');
  let memoryMb = $state(4096);
  let cpuPercent = $state(200);
  let diskMb = $state(20480);
  let ports = $state('25565');
  let error = $state<string>();
  let busy = $state(false);

  $effect(() => {
    if (!eggId) return;
    void api.egg(Number(eggId)).then((e) => {
      egg = e;
      image = e.images[0]?.image ?? '';
    });
  });

  async function create(e: SubmitEvent) {
    e.preventDefault();
    busy = true;
    error = undefined;
    try {
      const game = await api.createGame({
        name: name.trim(),
        eggId: Number(eggId),
        serverId: Number(serverId),
        image,
        memoryMb,
        cpuPercent,
        diskMb,
        ports: ports.split(/[\s,]+/).filter(Boolean).map(Number),
      });
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
    <div class="pair">
      <label>
        Game
        <select required bind:value={eggId}>
          <option value="" disabled>Pick an egg</option>
          {#each eggs.data ?? [] as e (e.id)}<option value={String(e.id)}>{e.name} ({e.category})</option>{/each}
        </select>
      </label>
      <label>
        Runs on
        <select required bind:value={serverId}>
          <option value="" disabled>Pick a server</option>
          {#each (servers.data ?? []).filter((s) => s.online) as s (s.id)}<option value={String(s.id)}>{s.name}</option>{/each}
        </select>
      </label>
    </div>
    {#if egg && egg.images.length > 1}
      <label>
        Image
        <select bind:value={image}>{#each egg.images as i (i.image)}<option value={i.image}>{i.label}</option>{/each}</select>
      </label>
    {/if}
    <div class="three">
      <label>Memory (MB) <input type="number" min="256" step="256" required bind:value={memoryMb} /></label>
      <label>CPU (%) <input type="number" min="0" max="6400" step="50" bind:value={cpuPercent} /></label>
      <label>Disk (MB) <input type="number" min="0" step="1024" bind:value={diskMb} /></label>
    </div>
    <label>Ports <span class="muted small">(the first is the game's; add more with commas)</span> <input required class="mono" bind:value={ports} /></label>
    <p class="muted small">100% CPU is one core; 0 means no limit, for CPU and disk. The egg's installer runs first, so it can take a few minutes before it can start.</p>
    {#if !eggs.data?.length && eggs.data}<p class="error">Import an egg first, from Eggs.</p>{/if}
    {#if error}<p class="error">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <button onclick={onclose}>Cancel</button>
    <button type="submit" form="new-game" class="primary" disabled={busy}>{busy ? 'Creating…' : 'Create and install'}</button>
  {/snippet}
</Modal>

