<!-- One game server: its console, files, startup settings, schedules and backups. -->
<script lang="ts">
  import Meter from '../components/Meter.svelte';
  import StatusDot from '../components/StatusDot.svelte';
  import Tabs from '../components/Tabs.svelte';
  import { api, type GamePower } from '../lib/api';
  import { ago, bytes, duration, percent } from '../lib/format';
  import { load } from '../lib/load.svelte';
  import { gameStatus } from '../lib/status';

  let { id }: { id: number } = $props();

  const game = load(() => api.game(id));
  const consoleLines = load(() => api.gameConsole(id));
  const startup = load(() => api.gameStartup(id));
  const schedules = load(() => api.gameSchedules(id));
  const backups = load(() => api.gameBackups(id));

  let tab = $state('console');
  let command = $state('');
  let path = $state('/');
  let message = $state<{ ok: boolean; text: string }>();
  const files = load(() => api.gameFiles(id, path));

  const running = $derived(game.data?.state === 'running' || game.data?.state === 'starting');

  async function attempt(work: () => Promise<unknown>, done?: string) {
    message = undefined;
    try {
      await work();
      if (done) message = { ok: true, text: done };
    } catch (err) {
      message = { ok: false, text: (err as Error).message };
    }
  }

  const power = (action: GamePower, done: string) => attempt(() => api.gamePower(id, action).then(game.reload), done);

  function send(e: SubmitEvent) {
    e.preventDefault();
    const sent = command.trim();
    if (!sent) return;
    command = '';
    void attempt(() => api.gameCommand(id, sent));
  }

  function open(dir: string) {
    path = dir === '..' ? path.replace(/[^/]+\/$/, '') : `${path}${dir}/`;
    void files.reload();
  }

  // Startup values being edited, from the egg's variables.
  let values = $state<Record<string, string>>({});
  $effect(() => {
    if (startup.data) values = Object.fromEntries(startup.data.variables.map((v) => [v.key, v.value]));
  });
</script>

<p class="crumbs small"><a href="/games">Game servers</a> / {game.data?.serverName ?? ''}</p>
{#if game.data}
  {@const g = game.data}
  {@const status = gameStatus[g.state]}
  <div class="page-head">
    <h1>{g.name}</h1>
    <span class="row status"><StatusDot tone={status.tone} />{status.label} · {g.egg}</span>
    {#if running}
      <button onclick={() => power('restart', 'Restarting.')}>Restart</button>
      <button class="danger" onclick={() => power('stop', 'Stopping: players are warned first.')}>Stop</button>
    {:else}
      <button class="primary" onclick={() => power('start', 'Starting.')}>Start</button>
    {/if}
    {#if g.state === 'stopping'}<button class="danger" onclick={() => power('kill', 'Killed.')}>Kill</button>{/if}
  </div>
{/if}

{#if message}<p class="note" class:error={!message.ok}>{message.text}</p>{/if}

<Tabs
  bind:selected={tab}
  tabs={[
    { id: 'console', label: 'Console' },
    { id: 'files', label: 'Files' },
    { id: 'startup', label: 'Startup' },
    { id: 'schedules', label: 'Schedules' },
    { id: 'backups', label: 'Backups' },
  ]}
/>

{#if tab === 'console'}
  <div class="console-layout">
    <div>
      <pre class="console mono">{#each consoleLines.data?.lines ?? [] as line}<span class:warn={/ WARN\]| ERROR\]/.test(line)}>{line}</span>
{/each}</pre>
      <form class="row command" onsubmit={send}>
        <input class="grow mono" placeholder={running ? 'Type a command, e.g. say Restarting at 19:00' : 'Start the server to send commands'} disabled={!running} bind:value={command} aria-label="Command" />
        <button type="submit" disabled={!running || !command.trim()}>Send</button>
      </form>
    </div>
    {#if game.data}
      {@const g = game.data}
      <aside class="card stats">
        <p class="label muted small">Address</p>
        <p class="mono">{g.address}</p>
        {#if g.players}
          <p class="label muted small">Players</p>
          <p>{g.players.online} of {g.players.max} online</p>
        {/if}
        <Meter label="CPU" value="{g.cpuPercent}%" percent={g.cpuPercent} />
        <Meter label="Memory" value="{bytes(g.memUsed)} / {bytes(g.memLimit)}" percent={percent(g.memUsed, g.memLimit)} />
        <p class="small"><span class="muted">Disk</span> {bytes(g.diskUsed)}</p>
        {#if running}<p class="small"><span class="muted">Up</span> {duration(g.uptimeSecs)}</p>{/if}
      </aside>
    {/if}
  </div>
{:else if tab === 'files'}
  <p class="mono path">{path}</p>
  <div class="card table-card">
    <table>
      <thead><tr><th>Name</th><th>Size</th><th>Changed</th></tr></thead>
      <tbody>
        {#if path !== '/'}
          <tr><td colspan="3"><button class="link" onclick={() => open('..')}>.. (up a folder)</button></td></tr>
        {/if}
        {#each files.data ?? [] as f (f.name)}
          <tr>
            <td>
              {#if f.dir}
                <button class="link" onclick={() => open(f.name)}><span class="icon" aria-hidden="true">▸</span>{f.name}/</button>
              {:else}
                <span class="mono">{f.name}</span>
              {/if}
            </td>
            <td class="muted">{f.dir ? '' : bytes(f.size)}</td>
            <td class="muted">{ago(f.modified)}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{:else if tab === 'startup' && startup.data}
  <div class="card startup">
    <p class="label muted small">Startup command</p>
    <p class="mono">{startup.data.command}</p>
    <p class="label muted small">Image</p>
    <p class="mono">{startup.data.image}</p>
  </div>
  <form
    class="variables"
    onsubmit={(e) => {
      e.preventDefault();
      void attempt(() => api.saveGameStartup(id, values), 'Saved. They apply from the next restart.');
    }}
  >
    {#each startup.data.variables as v (v.key)}
      <label class="card variable">
        <span class="row"><b class="grow">{v.name}</b><span class="mono muted small">{v.key}</span></span>
        <input class="mono" disabled={!v.editable} bind:value={values[v.key]} />
        <span class="muted small">{v.description}{v.editable ? '' : ' Set by the egg.'}</span>
      </label>
    {/each}
    <div class="row"><span class="grow"></span><button type="submit" class="primary">Save</button></div>
  </form>
{:else if tab === 'schedules'}
  <div class="card table-card">
    <table>
      <thead><tr><th>Schedule</th><th>When</th><th>Does</th><th>Next</th><th>On</th></tr></thead>
      <tbody>
        {#each schedules.data ?? [] as s (s.id)}
          <tr class:off={!s.enabled}>
            <td><b>{s.name}</b></td>
            <td>{s.when}</td>
            <td class="muted">{s.action}</td>
            <td>{s.enabled ? new Date(s.nextRun).toLocaleString('en-GB', { weekday: 'short', hour: '2-digit', minute: '2-digit' }) : '–'}</td>
            <td>{s.enabled ? 'Yes' : 'No'}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{:else if tab === 'backups'}
  <div class="row top">
    <span class="muted small grow">Backups are kept on the server the game runs on. A locked one is never deleted automatically.</span>
    <button class="primary" onclick={() => attempt(() => api.createGameBackup(id).then(backups.reload), 'Backing up.')}>Back up now</button>
  </div>
  <div class="card table-card">
    <table>
      <thead><tr><th>Backup</th><th>Size</th><th>Made</th><th></th></tr></thead>
      <tbody>
        {#each backups.data ?? [] as b (b.id)}
          <tr>
            <td><b>{b.name}</b> {#if b.locked}<span class="badge blue">Locked</span>{/if}</td>
            <td>{bytes(b.size)}</td>
            <td class="muted">{ago(b.createdAt)}</td>
            <td class="row actions"><button class="link">Restore</button><button class="link">Download</button></td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<style>
  .crumbs {
    margin: 0 0 4px;
    color: var(--muted);
  }
  .page-head h1 {
    flex: none;
  }
  .status {
    color: var(--muted);
    margin-right: auto;
  }
  .note {
    margin: -8px 0 12px;
    color: var(--green);
  }
  .note.error {
    color: var(--red);
  }
  .console-layout {
    display: grid;
    grid-template-columns: 1fr 260px;
    gap: 16px;
    align-items: start;
  }
  .console {
    margin: 0;
    height: 480px;
    overflow: auto;
    padding: 12px 14px;
    background: #0b0d11;
    border: 1px solid var(--line);
    border-radius: 8px;
    line-height: 1.6;
    color: #b7c0cf;
    white-space: pre-wrap;
  }
  .warn {
    color: var(--amber);
  }
  .command {
    margin-top: 10px;
  }
  .stats p {
    margin: 0 0 10px;
  }
  .stats .label,
  .startup .label {
    margin: 0 0 2px;
  }
  .path {
    margin: 0 0 8px;
    color: var(--muted);
  }
  button.link {
    border: 0;
    background: none;
    padding: 0;
    color: var(--accent);
    font-weight: 400;
  }
  .icon {
    display: inline-block;
    width: 14px;
    color: var(--muted);
  }
  .startup {
    display: grid;
    gap: 4px;
    margin-bottom: 16px;
  }
  .startup p {
    margin: 0 0 8px;
    word-break: break-all;
  }
  .variables {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: 12px;
  }
  .variables > .row {
    grid-column: 1 / -1;
  }
  .variable {
    display: grid;
    gap: 6px;
  }
  .actions {
    justify-content: flex-end;
    gap: 14px;
  }
  .off td {
    color: var(--muted);
  }
  .top {
    margin-bottom: 10px;
  }
</style>
