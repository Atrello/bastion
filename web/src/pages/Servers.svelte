<!-- Servers: each one with an agent, how it's doing, and adding another. A server's name opens its page.
     Agents older than the panel's can all be updated from here at once. -->
<script lang="ts">
  import AddServer from '../components/AddServer.svelte';
  import StatusDot from '../components/StatusDot.svelte';
  import { sure } from '../lib/ask.svelte';
  import { api } from '../lib/api';
  import { ago, bytes, percent } from '../lib/format';
  import { load } from '../lib/load.svelte';

  const servers = load(api.servers);
  let adding = $state(false);
  let updating = $state<number[]>([]);
  let error = $state('');
  const behind = $derived(servers.data?.filter((s) => s.online && s.agentUpdate) ?? []);

  async function updateAll() {
    const list = behind;
    const ok = await sure(
      `Update ${list.length} agent${list.length === 1 ? '' : 's'}?`,
      'Each replaces itself with the panel\'s agent and restarts, which takes a few seconds. Everything they run keeps running; live logs and terminals open on them reconnect.',
      'Update',
      false,
    );
    if (!ok) return;
    error = '';
    updating = list.map((s) => s.id);
    const failed: string[] = [];
    for (const s of list) {
      try {
        await api.updateAgent(s.id);
      } catch (err) {
        failed.push(`${s.name}: ${(err as Error).message}`);
      }
    }
    error = failed.join(' ');
    // They're back a few seconds later, with the new agent.
    setTimeout(async () => {
      await servers.reload();
      updating = [];
    }, 8000);
  }
</script>

<div class="page-head">
  <h1>Servers</h1>
  {#if behind.length}
    <button onclick={updateAll} disabled={!!updating.length}>
      {updating.length ? 'Updating…' : `Update ${behind.length} agent${behind.length === 1 ? '' : 's'}`}
    </button>
  {/if}
  <button class="primary" onclick={() => (adding = true)}>Add a server</button>
</div>

{#if servers.error}<p class="error">{servers.error}</p>{/if}
{#if error}<p class="error">{error}</p>{/if}

<div class="card table-card">
  <table>
    <thead>
      <tr><th>Server</th><th>System</th><th>CPU</th><th>Memory</th><th>Disk</th><th>Stacks</th><th>Agent</th><th>Last seen</th></tr>
    </thead>
    <tbody>
      {#each servers.data ?? [] as s (s.id)}
        <tr>
          <td><span class="row"><StatusDot tone={s.online ? 'green' : 'red'} label={s.online ? 'Online' : 'Offline'} /><a href="/servers/{s.id}"><b>{s.name}</b></a></span></td>
          <td class="muted">{s.os}</td>
          {#if s.metrics}
            <td>{s.metrics.cpuPercent}%</td>
            <td>{percent(s.metrics.memUsed, s.metrics.memTotal)}% <span class="muted">of {bytes(s.metrics.memTotal)}</span></td>
            <td>{percent(s.metrics.diskUsed, s.metrics.diskTotal)}% <span class="muted">of {bytes(s.metrics.diskTotal)}</span></td>
          {:else}
            <td class="muted" colspan="3">No figures while it's offline</td>
          {/if}
          <td><a href="/stacks?server={s.id}">{s.stacks}</a></td>
          <td>
            <span class="mono">{s.agentVersion}</span>
            {#if updating.includes(s.id)}<span class="badge amber">Updating…</span>{:else if s.agentUpdate && s.online}<span class="badge amber">Update</span>{/if}
          </td>
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
