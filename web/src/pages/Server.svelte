<!-- One server: how it's doing, what it is, and what runs on it, each linking to the page that manages it
     (stacks, game servers, containers, volumes and images live on their own pages, not here). Also a new
     join command, for reinstalling its agent, updating its agent from here, and removing it from Bastion. -->
<script lang="ts">
  import Meter from '../components/Meter.svelte';
  import Modal from '../components/Modal.svelte';
  import StatusDot from '../components/StatusDot.svelte';
  import { sure } from '../lib/ask.svelte';
  import { api } from '../lib/api';
  import { ago, bytes, duration, percent } from '../lib/format';
  import { load } from '../lib/load.svelte';
  import { navigate } from '../lib/router.svelte';
  import { gameStatus, stackStatus } from '../lib/status';

  let { id }: { id: number } = $props();

  const servers = load(api.servers);
  const stacks = load(api.stacks);
  const containers = load(api.containers);
  const games = load(api.games);
  // Its use, kept fresh while the page is open.
  $effect(() => {
    const timer = setInterval(() => void servers.reload(), 10000);
    return () => clearInterval(timer);
  });

  const server = $derived(servers.data?.find((s) => s.id === id));
  const mine = $derived(stacks.data?.filter((s) => s.serverId === id) ?? []);
  const myGames = $derived(games.data?.filter((g) => g.serverId === id) ?? []);
  // Containers shown nowhere else in Bastion: not in a compose stack, and not a game server.
  const loose = $derived(containers.data?.filter((c) => c.serverId === id && !c.stack && !c.game) ?? []);

  let error = $state<string>();
  let joinCommand = $state<string>();
  let updating = $state(false);

  // The agent replaces itself and restarts: watch until it's back running the new one (a minute at most).
  async function updateAgent() {
    error = undefined;
    updating = true;
    try {
      await api.updateAgent(id);
      for (let i = 0; i < 30; i++) {
        await new Promise((r) => setTimeout(r, 2000));
        await servers.reload();
        if (server?.online && !server.agentUpdate) break;
      }
      if (server?.agentUpdate) error = "The agent hasn't come back updated yet: it may still be restarting.";
    } catch (err) {
      error = (err as Error).message;
    } finally {
      updating = false;
    }
  }

  async function newCommand() {
    error = undefined;
    try {
      joinCommand = (await api.newJoinCommand(id)).joinCommand;
    } catch (err) {
      error = (err as Error).message;
    }
  }

  async function remove() {
    if (!server) return;
    const ok = await sure(
      `Remove ${server.name}?`,
      'Bastion forgets it and disconnects its agent. Everything it runs keeps running. To take the agent off the server too, see "Removing an agent" in docs/install.md.',
      'Remove',
    );
    if (!ok) return;
    try {
      await api.deleteServer(id);
      navigate('/servers');
    } catch (err) {
      error = (err as Error).message;
    }
  }
</script>

<p class="crumbs small"><a href="/servers">Servers</a></p>
{#if servers.data && !server}
  <p class="empty">There's no such server. <a href="/servers">Back to servers</a></p>
{:else if server}
  {@const s = server}
  <div class="page-head">
    <h1>{s.name}</h1>
    <span class="row status">
      <StatusDot tone={s.online ? 'green' : 'red'} />{s.online ? 'Online' : `Offline since ${ago(s.lastSeen)}`}
    </span>
    <button onclick={newCommand}>New join command</button>
    <button class="danger" onclick={remove}>Remove</button>
  </div>
  {#if error}<p class="error">{error}</p>{/if}

  <div class="top">
    <section class="card">
      <h2>Use</h2>
      {#if s.metrics}
        {@const m = s.metrics}
        <Meter label="CPU" value="{m.cpuPercent}%" percent={m.cpuPercent} />
        <Meter label="Memory" value="{bytes(m.memUsed)} / {bytes(m.memTotal)}" percent={percent(m.memUsed, m.memTotal)} />
        <Meter label="Disk" value="{bytes(m.diskUsed)} / {bytes(m.diskTotal)}" percent={percent(m.diskUsed, m.diskTotal)} />
        <p class="small muted">Up {duration(m.uptimeSecs)}</p>
      {:else}
        <p class="muted">No figures while it's offline.</p>
      {/if}
    </section>
    <section class="card">
      <h2>System</h2>
      <dl>
        <dt>Hostname</dt>
        <dd class="mono">{s.hostname || '–'}</dd>
        <dt>Address</dt>
        <dd class="mono">{s.address || '–'}</dd>
        <dt>System</dt>
        <dd>{s.os || '–'}{s.arch ? ` (${s.arch})` : ''}</dd>
        <dt>CPUs</dt>
        <dd>{s.cpus || '–'}</dd>
        <dt>Docker</dt>
        <dd>{s.docker || '–'}</dd>
        <dt>Agent</dt>
        <dd>
          <span class="mono">{s.agentVersion || '–'}</span>
          {#if updating}
            <span class="badge amber">Updating…</span>
          {:else if s.agentUpdate && s.online}
            <button class="small" title="The panel has a newer agent" onclick={updateAgent}>Update agent</button>
          {/if}
        </dd>
      </dl>
    </section>
    <section class="card">
      <h2>Also on this server</h2>
      <ul class="links">
        <li><a href="/containers">Containers</a> <span class="muted">{s.containers}</span></li>
        <li><a href="/volumes">Volumes</a> <span class="muted">and clean-up</span></li>
        <li><a href="/images">Images</a></li>
      </ul>
    </section>
  </div>

  <section class="card table-card section">
    <div class="row head"><h2 class="grow">Stacks <span class="muted">{mine.length}</span></h2><a href="/stacks">All stacks</a></div>
    <table>
      <tbody>
        {#each mine as st (st.name)}
          {@const status = stackStatus[st.state]}
          <tr>
            <td><a href="/stacks/{id}/{st.name}"><b>{st.name}</b></a></td>
            <td><span class="row"><StatusDot tone={status.tone} />{status.label}</span></td>
            <td class="muted">{st.running} of {st.total} running</td>
            <td>{#if st.updates}<span class="badge amber">{st.updates} update{st.updates === 1 ? '' : 's'}</span>{/if}</td>
          </tr>
        {:else}
          <tr><td class="empty">No stacks on it yet.</td></tr>
        {/each}
      </tbody>
    </table>
  </section>

  <section class="card table-card section">
    <div class="row head"><h2 class="grow">Game servers <span class="muted">{myGames.length}</span></h2><a href="/games">All game servers</a></div>
    <table>
      <tbody>
        {#each myGames as g (g.id)}
          {@const status = gameStatus[g.state]}
          <tr>
            <td><a href="/games/{g.id}"><b>{g.name}</b></a></td>
            <td><span class="row"><StatusDot tone={status.tone} />{status.label}</span></td>
            <td class="muted">{g.egg}</td>
            <td class="mono muted">{g.address}</td>
          </tr>
        {:else}
          <tr><td class="empty">No game servers on it.</td></tr>
        {/each}
      </tbody>
    </table>
  </section>

  {#if loose.length}
    <section class="card table-card section">
      <div class="row head"><h2 class="grow">Containers outside stacks <span class="muted">{loose.length}</span></h2><a href="/containers">All containers</a></div>
      <table>
        <tbody>
          {#each loose as c (c.id)}
            <tr><td><b>{c.name}</b></td><td class="muted">{c.status}</td><td class="mono muted">{c.image}</td></tr>
          {/each}
        </tbody>
      </table>
    </section>
  {/if}
{/if}

{#if joinCommand}
  <Modal title="New join command" onclose={() => (joinCommand = undefined)}>
    <p>Run this on the server, as root, to reinstall or update its agent. It works once, for an hour, and keeps the server's place in Bastion. The agent there now keeps working until this is run.</p>
    <pre class="command mono">{joinCommand}</pre>
    {#snippet footer()}
      <button onclick={() => navigator.clipboard?.writeText(joinCommand ?? '')}>Copy</button>
      <button class="primary" onclick={() => (joinCommand = undefined)}>Done</button>
    {/snippet}
  </Modal>
{/if}

<style>
  .crumbs {
    margin: 0 0 4px;
  }
  .page-head h1 {
    flex: none;
  }
  .status {
    color: var(--muted);
    margin-right: auto;
  }
  .top {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 280px), 1fr));
    gap: 16px;
    margin-bottom: 16px;
  }
  h2 {
    margin: 0 0 12px;
  }
  dl {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 6px 14px;
    margin: 0;
  }
  dt {
    color: var(--muted);
  }
  dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  dd .small {
    padding: 2px 10px;
    margin-left: 4px;
  }
  .links {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 8px;
  }
  .section {
    margin-bottom: 16px;
  }
  .head {
    padding: 10px 10px 4px;
  }
  .head h2 {
    margin: 0;
  }
  .command {
    white-space: pre-wrap;
    word-break: break-all;
    background: var(--sunken);
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 12px;
  }
</style>
