<!-- The overview: every server's health at a glance, and anything that needs looking at. -->
<script lang="ts">
  import Meter from '../components/Meter.svelte';
  import StatusDot from '../components/StatusDot.svelte';
  import { api } from '../lib/api';
  import { ago, bytes, count, duration, percent } from '../lib/format';
  import { load } from '../lib/load.svelte';

  const servers = load(api.servers);
  const stacks = load(api.stacks);

  const online = $derived(servers.data?.filter((s) => s.online).length ?? 0);
  // Stacks with something not running, and servers whose agent has gone quiet.
  const attention = $derived([
    ...(servers.data ?? [])
      .filter((s) => !s.online)
      .map((s) => ({ key: `server-${s.id}`, href: '/servers', title: s.name, detail: `Agent offline, last seen ${ago(s.lastSeen)}` })),
    ...(stacks.data ?? [])
      .filter((s) => s.state === 'partial')
      .map((s) => ({
        key: `stack-${s.serverId}-${s.name}`,
        href: `/stacks/${s.serverId}/${s.name}`,
        title: s.name,
        detail: `${s.running} of ${s.total} containers running on ${s.serverName}`,
      })),
    ...(stacks.data ?? [])
      .filter((s) => s.updates > 0)
      .map((s) => ({
        key: `update-${s.serverId}-${s.name}`,
        href: `/stacks/${s.serverId}/${s.name}`,
        title: s.name,
        detail: `${s.updates === 1 ? 'An image update is' : `${s.updates} image updates are`} available on ${s.serverName}`,
      })),
  ]);
</script>

<div class="page-head">
  <h1>Overview</h1>
  {#if servers.data}
    <span class="badge {online === servers.data.length ? 'green' : 'amber'}">{online} of {servers.data.length} servers online</span>
  {/if}
</div>

{#if servers.error}<p class="error">{servers.error}</p>{/if}

<div class="servers">
  {#each servers.data ?? [] as server (server.id)}
    <a class="card server" href="/stacks?server={server.id}">
      <div class="row">
        <StatusDot tone={server.online ? 'green' : 'red'} label={server.online ? 'Online' : 'Offline'} />
        <h2 class="grow">{server.name}</h2>
        {#if server.online && server.metrics}
          <span class="muted small">up {duration(server.metrics.uptimeSecs)}</span>
        {/if}
      </div>
      <p class="muted small os">{server.os}</p>
      {#if server.online && server.metrics}
        {@const m = server.metrics}
        <Meter label="CPU" value="{m.cpuPercent}%" percent={m.cpuPercent} />
        <Meter label="Memory" value="{bytes(m.memUsed)} / {bytes(m.memTotal)}" percent={percent(m.memUsed, m.memTotal)} />
        <Meter label="Disk" value="{bytes(m.diskUsed)} / {bytes(m.diskTotal)}" percent={percent(m.diskUsed, m.diskTotal)} />
      {:else}
        <p class="offline">Agent offline since {ago(server.lastSeen)}</p>
      {/if}
      <p class="small counts">{count(server.stacks, 'stack')} · {count(server.containers, 'container')}</p>
    </a>
  {/each}
</div>

<section class="card">
  <h2>Needs attention</h2>
  {#if attention.length}
    <ul>
      {#each attention as item (item.key)}
        <li class="row">
          <StatusDot tone="amber" />
          <a href={item.href}>{item.title}</a>
          <span class="muted">{item.detail}</span>
        </li>
      {/each}
    </ul>
  {:else if servers.data}
    <p class="muted">Everything's running.</p>
  {/if}
</section>

<style>
  .servers {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(270px, 1fr));
    gap: 16px;
    margin-bottom: 16px;
  }
  .server {
    color: inherit;
  }
  .server:hover {
    text-decoration: none;
    border-color: #343b4a;
  }
  .os {
    margin: 2px 0 12px;
  }
  .offline {
    margin: 22px 0;
    color: var(--red);
  }
  .counts {
    margin: 4px 0 0;
    color: var(--muted);
  }
  ul {
    list-style: none;
    margin: 10px 0 0;
    padding: 0;
  }
  li {
    padding: 7px 0;
    border-top: 1px solid var(--line-soft);
  }
</style>
