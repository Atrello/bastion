<!-- Every container on every server, with its stack, image and ports. -->
<script lang="ts">
  import StatusDot from '../components/StatusDot.svelte';
  import { api } from '../lib/api';
  import { load } from '../lib/load.svelte';
  import { containerTone } from '../lib/status';

  const containers = load(api.containers);
  const servers = load(api.servers);
  let search = $state('');

  const serverName = (id: number) => servers.data?.find((s) => s.id === id)?.name ?? '';
  const shown = $derived.by(() => {
    const q = search.trim().toLowerCase();
    return (containers.data ?? []).filter((c) => !q || c.name.includes(q) || c.image.includes(q) || c.stack.includes(q));
  });
</script>

<div class="page-head">
  <h1>Containers</h1>
  <input type="search" placeholder="Search name, image or stack" aria-label="Search containers" bind:value={search} />
</div>

{#if containers.error}<p class="error">{containers.error}</p>{/if}

<div class="card table-card">
  <table>
    <thead>
      <tr><th>Container</th><th>Stack</th><th>Server</th><th>Image</th><th>Status</th><th>Ports</th></tr>
    </thead>
    <tbody>
      {#each shown as c (`${c.serverId}/${c.id}`)}
        <tr>
          <td><span class="row"><StatusDot tone={containerTone(c.state)} label={c.state} /><b>{c.name}</b></span></td>
          <td>{#if c.stack}<a href="/stacks/{c.serverId}/{c.stack}">{c.stack}</a>{/if}</td>
          <td class="muted">{serverName(c.serverId)}</td>
          <td class="mono muted">{c.image}</td>
          <td class:failed={c.state === 'exited' && /Exited \([1-9]/.test(c.status)}>{c.status}</td>
          <td class="mono muted">{c.ports}</td>
        </tr>
      {:else}
        {#if containers.data}<tr><td colspan="6" class="empty">No containers match.</td></tr>{/if}
      {/each}
    </tbody>
  </table>
</div>

<style>
  .failed {
    color: var(--red);
  }
</style>
