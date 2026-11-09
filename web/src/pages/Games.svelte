<!-- Game servers: each one's state, players and memory, and starting a new one from an egg. -->
<script lang="ts">
  import Meter from '../components/Meter.svelte';
  import NewGame from '../components/NewGame.svelte';
  import StatusDot from '../components/StatusDot.svelte';
  import { api } from '../lib/api';
  import { bytes, duration, percent } from '../lib/format';
  import { load } from '../lib/load.svelte';
  import { gameStatus } from '../lib/status';

  const games = load(api.games);
  let { owner = true }: { owner?: boolean } = $props();
  let creating = $state(false);
</script>

<div class="page-head">
  <h1>Game servers</h1>
  {#if owner}<button class="primary" onclick={() => (creating = true)}>New game server</button>{/if}
</div>

{#if games.error}<p class="error">{games.error}</p>{/if}

<div class="games">
  {#each games.data ?? [] as g (g.id)}
    {@const status = gameStatus[g.state]}
    <a class="card game" href="/games/{g.id}">
      <div class="row">
        <StatusDot tone={status.tone} label={status.label} />
        <h2 class="grow">{g.name}</h2>
        {#if g.players}
          <span class="badge green">{g.players.online} / {g.players.max} players</span>
        {:else}
          <span class="badge" class:red={g.state === 'crashed'}>{status.label}</span>
        {/if}
      </div>
      <p class="muted small">{g.egg} · {g.serverName}</p>
      <p class="mono address">{g.address}</p>
      {#if g.state === 'running'}
        <Meter label="Memory" value="{bytes(g.memUsed)} / {bytes(g.memLimit)}" percent={percent(g.memUsed, g.memLimit)} />
        <p class="muted small">CPU {Math.round(g.cpuPercent)}% · up {duration(g.uptimeSecs)}</p>
      {:else}
        <p class="muted small stopped">
          {g.state === 'crashed' ? 'Stopped unexpectedly. Its console shows why.' : g.state === 'installing' ? 'Installing…' : `Not running · ${bytes(g.diskUsed)} on disk`}
        </p>
      {/if}
    </a>
  {:else}
    {#if games.data}<p class="empty">{owner ? 'No game servers yet. Import an egg, then start one from it.' : 'Nothing has been shared with you yet.'}</p>{/if}
  {/each}
</div>

{#if creating}
  <NewGame onclose={() => (creating = false)} />
{/if}

<style>
  .games {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
    gap: 16px;
  }
  .game {
    color: inherit;
  }
  .game:hover {
    text-decoration: none;
    border-color: #343b4a;
  }
  .game p {
    margin: 2px 0 0;
  }
  .game .address {
    margin: 10px 0 12px;
  }
  .game .stopped {
    padding: 14px 0 8px;
  }
</style>
