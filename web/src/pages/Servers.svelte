<!-- Servers: each one with an agent, how it's doing, and adding another. -->
<script lang="ts">
  import AddServer from '../components/AddServer.svelte';
  import StatusDot from '../components/StatusDot.svelte';
  import { api } from '../lib/api';
  import { ago, bytes, percent } from '../lib/format';
  import { load } from '../lib/load.svelte';

  const servers = load(api.servers);
  let adding = $state(false);
</script>

<div class="page-head">
  <h1>Servers</h1>
  <button class="primary" onclick={() => (adding = true)}>Add a server</button>
</div>

{#if servers.error}<p class="error">{servers.error}</p>{/if}

<div class="card table-card">
  <table>
    <thead>
      <tr><th>Server</th><th>System</th><th>CPU</th><th>Memory</th><th>Disk</th><th>Stacks</th><th>Agent</th><th>Last seen</th></tr>
    </thead>
    <tbody>
      {#each servers.data ?? [] as s (s.id)}
        <tr>
          <td><span class="row"><StatusDot tone={s.online ? 'green' : 'red'} label={s.online ? 'Online' : 'Offline'} /><b>{s.name}</b></span></td>
          <td class="muted">{s.os}</td>
          {#if s.metrics}
            <td>{s.metrics.cpuPercent}%</td>
            <td>{percent(s.metrics.memUsed, s.metrics.memTotal)}% <span class="muted">of {bytes(s.metrics.memTotal)}</span></td>
            <td>{percent(s.metrics.diskUsed, s.metrics.diskTotal)}% <span class="muted">of {bytes(s.metrics.diskTotal)}</span></td>
          {:else}
            <td class="muted" colspan="3">No figures while it's offline</td>
          {/if}
          <td><a href="/stacks?server={s.id}">{s.stacks}</a></td>
          <td class="mono">{s.agentVersion}</td>
          <td class:offline={!s.online}>{s.online ? 'Now' : ago(s.lastSeen)}</td>
        </tr>
      {:else}
        {#if servers.data}<tr><td colspan="8" class="empty">No servers yet. Add one to get started.</td></tr>{/if}
      {/each}
    </tbody>
  </table>
</div>

{#if adding}
  <AddServer onclose={() => (adding = false)} onadded={servers.reload} />
{/if}

<style>
  .offline {
    color: var(--red);
  }
</style>
