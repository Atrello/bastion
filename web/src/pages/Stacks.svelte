<!-- Every compose stack on every server, to search or narrow to one server. -->
<script lang="ts">
  import StatusDot from '../components/StatusDot.svelte';
  import { api } from '../lib/api';
  import { load } from '../lib/load.svelte';
  import { stackStatus } from '../lib/status';

  const stacks = load(api.stacks);
  const servers = load(api.servers);
  let server = $state(new URLSearchParams(location.search).get('server') ?? '');
  let search = $state('');

  const shown = $derived(
    (stacks.data ?? []).filter(
      (s) => (!server || String(s.serverId) === server) && s.name.includes(search.trim().toLowerCase()),
    ),
  );

  // Keeps the chosen server in the address, so the page can be shared or come back to.
  $effect(() => {
    history.replaceState(null, '', server ? `?server=${server}` : location.pathname);
  });
</script>

<div class="page-head">
  <h1>Stacks</h1>
  <input type="search" placeholder="Search stacks" aria-label="Search stacks" bind:value={search} />
  <select aria-label="Server" bind:value={server}>
    <option value="">All servers</option>
    {#each servers.data ?? [] as s (s.id)}<option value={String(s.id)}>{s.name}</option>{/each}
  </select>
</div>

{#if stacks.error}<p class="error">{stacks.error}</p>{/if}

<div class="card table-card">
  <table>
    <thead>
      <tr><th>Stack</th><th>Server</th><th>State</th><th>Containers</th><th>Compose file</th></tr>
    </thead>
    <tbody>
      {#each shown as s (`${s.serverId}/${s.name}`)}
        {@const status = stackStatus[s.state]}
        <tr>
          <td><a href="/stacks/{s.serverId}/{s.name}"><b>{s.name}</b></a></td>
          <td class="muted">{s.serverName}</td>
          <td><span class="row"><StatusDot tone={status.tone} />{status.label}</span></td>
          <td>{s.running} / {s.total} running</td>
          <td class="mono muted">{s.configFile}</td>
        </tr>
      {:else}
        {#if stacks.data}<tr><td colspan="5" class="empty">No stacks match.</td></tr>{/if}
      {/each}
    </tbody>
  </table>
</div>
