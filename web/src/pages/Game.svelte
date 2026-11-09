<!-- One game server: its console, files, schedules, backups and settings, with power buttons at the top.
     A stop or restart warns the players first when the game can be told. -->
<script lang="ts">
  import GameBackups from '../components/GameBackups.svelte';
  import GameConsole from '../components/GameConsole.svelte';
  import GameFiles from '../components/GameFiles.svelte';
  import GameSchedules from '../components/GameSchedules.svelte';
  import GameSettings from '../components/GameSettings.svelte';
  import Meter from '../components/Meter.svelte';
  import Modal from '../components/Modal.svelte';
  import StatusDot from '../components/StatusDot.svelte';
  import Tabs from '../components/Tabs.svelte';
  import { sure } from '../lib/ask.svelte';
  import { api, type GamePower } from '../lib/api';
  import { bytes, duration, percent } from '../lib/format';
  import { load } from '../lib/load.svelte';
  import { gameStatus } from '../lib/status';

  let { id }: { id: number } = $props();

  const game = load(() => api.game(id));
  // Its state and use, kept fresh while the page is open.
  $effect(() => {
    const timer = setInterval(() => void game.reload(), 5000);
    return () => clearInterval(timer);
  });

  let tab = $state('console');
  let message = $state<{ ok: boolean; text: string }>();
  let stopping = $state<'stop' | 'restart'>();
  let warn = $state(60);
  let eula = $state(false);

  // Minecraft won't run until its EULA is accepted: this writes eula.txt, as Pterodactyl's prompt does.
  async function acceptEula() {
    try {
      await api.writeGameFile(id, 'eula.txt', 'eula=true\n');
      eula = false;
      await power('start', 'EULA accepted. Starting.');
    } catch (err) {
      problem((err as Error).message);
    }
  }

  const running = $derived(['running', 'starting', 'stopping'].includes(game.data?.state ?? ''));

  async function kill() {
    if (await sure('Kill it?', "It's stopped at once, without saving. Use this only if it won't stop the normal way.", 'Kill')) {
      await power('kill', 'Killed.');
    }
  }
  const note = (text: string) => (message = { ok: true, text });
  const problem = (text: string) => (message = { ok: false, text });

  async function power(action: GamePower, done: string, warnSecs = 0) {
    message = undefined;
    stopping = undefined;
    try {
      await api.gamePower(id, action, warnSecs);
      note(done);
    } catch (err) {
      problem((err as Error).message);
    }
    await game.reload();
  }
</script>

<p class="crumbs small"><a href="/games">Game servers</a> / {game.data?.serverName ?? ''}</p>
{#if game.error}<p class="error">{game.error}</p>{/if}
{#if game.data}
  {@const g = game.data}
  {@const status = gameStatus[g.state]}
  <div class="page-head">
    <h1>{g.name}</h1>
    <span class="row status"><StatusDot tone={status.tone} />{status.label} · {g.egg}</span>
    <!-- The first button says what it's doing: Start, then Starting… until the game is ready, then Running. -->
    {#if g.state === 'starting'}
      <button class="primary state" disabled><span class="spinner" aria-hidden="true"></span>Starting…</button>
    {:else if g.state === 'running'}
      <button class="state running" disabled>Running</button>
    {:else if g.state === 'stopping'}
      <button class="state" disabled><span class="spinner" aria-hidden="true"></span>Stopping…</button>
    {:else if g.state === 'installing'}
      <button class="state" disabled><span class="spinner" aria-hidden="true"></span>Installing…</button>
    {:else}
      <button class="primary" disabled={g.state === 'offline'} onclick={() => power('start', 'Starting.')}>Start</button>
    {/if}
    {#if g.state === 'running' || g.state === 'starting'}
      <button onclick={() => (stopping = 'restart')}>Restart</button>
      <button onclick={() => (stopping = 'stop')}>Stop</button>
    {/if}
    {#if running}<button class="danger" onclick={kill}>Kill</button>{/if}
  </div>

  {#if message}<p class="note" class:error={!message.ok}>{message.text}</p>{/if}

  <Tabs
    bind:selected={tab}
    tabs={[
      { id: 'console', label: 'Console' },
      { id: 'files', label: 'Files' },
      { id: 'schedules', label: 'Schedules' },
      { id: 'backups', label: 'Backups' },
      { id: 'settings', label: 'Settings' },
    ]}
  />

  {#if tab === 'console'}
    <div class="console-layout">
      <div>
        {#if eula}
          <div class="card eula row">
            <span class="grow">
              Minecraft needs its <a href="https://aka.ms/MinecraftEULA" target="_blank" rel="noreferrer">end user licence agreement</a>
              accepted before this server will run.
            </span>
            <button class="primary" onclick={acceptEula}>I accept: start it</button>
          </div>
        {/if}
        <GameConsole game={g} onerror={problem} oneula={(needed) => (eula = needed)} />
      </div>
      <aside class="card stats">
        <p class="label muted small">Address</p>
        <p class="mono">{g.address}</p>
        <Meter label="CPU" value="{Math.round(g.cpuPercent)}%{g.cpuLimit ? ` of ${g.cpuLimit}%` : ''}" percent={g.cpuLimit ? percent(g.cpuPercent, g.cpuLimit) : g.cpuPercent} />
        <Meter label="Memory" value="{bytes(g.memUsed)} / {bytes(g.memLimit)}" percent={percent(g.memUsed, g.memLimit)} />
        {#if g.diskLimit}
          <Meter label="Disk" value="{bytes(g.diskUsed)} / {bytes(g.diskLimit)}" percent={percent(g.diskUsed, g.diskLimit)} />
        {:else}
          <p class="small"><span class="muted">Disk</span> {bytes(g.diskUsed)}</p>
        {/if}
        {#if g.state === 'running'}<p class="small"><span class="muted">Up</span> {duration(g.uptimeSecs)}</p>{/if}
      </aside>
    </div>
  {:else if tab === 'files'}
    <GameFiles {id} name={game.data?.name ?? ''} onerror={problem} onnote={note} />
  {:else if tab === 'schedules'}
    <GameSchedules {id} onerror={problem} onnote={note} />
  {:else if tab === 'backups'}
    <GameBackups game={g} onerror={problem} onnote={note} />
  {:else}
    <GameSettings game={g} onerror={problem} onnote={note} onchanged={game.reload} />
  {/if}

  {#if stopping}
    {@const action = stopping}
    <Modal title="{action === 'stop' ? 'Stop' : 'Restart'} {g.name}" onclose={() => (stopping = undefined)}>
      {#if g.warnCommand}
        <label class="row warn-row">
          Warn players first, then wait
          <select bind:value={warn}>
            <option value={0}>no time: now</option>
            <option value={10}>10 seconds</option>
            <option value={30}>30 seconds</option>
            <option value={60}>1 minute</option>
            <option value={300}>5 minutes</option>
          </select>
        </label>
        <p class="muted small">They see "Server {action === 'stop' ? 'stopping' : 'restarting'} in …" in the game.</p>
      {:else}
        <p class="muted">This game can't be told to warn its players (set a warning command in Settings if it has one).</p>
      {/if}
      {#snippet footer()}
        <button onclick={() => (stopping = undefined)}>Cancel</button>
        <button
          class="danger"
          onclick={() => {
            const secs = g.warnCommand ? warn : 0;
            const verb = action === 'stop' ? 'Stopping' : 'Restarting';
            void power(action, secs ? `${verb} in ${secs} seconds: players have been told.` : `${verb}.`, secs);
          }}>{action === 'stop' ? 'Stop' : 'Restart'}</button
        >
      {/snippet}
    </Modal>
  {/if}
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
  @media (max-width: 900px) {
    .console-layout {
      grid-template-columns: 1fr;
    }
  }
  .stats p {
    margin: 0 0 10px;
  }
  .stats .label {
    margin: 0 0 2px;
  }
  .eula {
    margin-bottom: 10px;
    border-color: var(--accent);
  }
  .state {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }
  .state:disabled {
    opacity: 1;
  }
  button.primary.state:disabled {
    opacity: 0.85;
  }
  .state.running {
    color: var(--green);
    border-color: rgb(87 199 133 / 40%);
    background: rgb(87 199 133 / 10%);
  }
  .spinner {
    width: 13px;
    height: 13px;
    border: 2px solid currentColor;
    border-right-color: transparent;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  .warn-row {
    gap: 10px;
  }
</style>
