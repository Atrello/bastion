<!-- One game server: its console, files, schedules, backups and settings, with power buttons at the top.
     A stop or restart warns the players first when the game can be told. Beside the console, its figures
     (address, uptime, CPU, memory, disk, network); under it, charts of the last ten minutes. -->
<script lang="ts">
  import GameBackups from '../components/GameBackups.svelte';
  import GameConsole from '../components/GameConsole.svelte';
  import GameFiles from '../components/GameFiles.svelte';
  import GameSchedules from '../components/GameSchedules.svelte';
  import GameSettings from '../components/GameSettings.svelte';
  import Chart from '../components/Chart.svelte';
  import StatCard from '../components/StatCard.svelte';
  import Modal from '../components/Modal.svelte';
  import StatusDot from '../components/StatusDot.svelte';
  import Tabs from '../components/Tabs.svelte';
  import { sure } from '../lib/ask.svelte';
  import { api, type GamePower } from '../lib/api';
  import { bitRate, bytes, duration, percent } from '../lib/format';
  import { load } from '../lib/load.svelte';
  import { gameStatus } from '../lib/status';
  import type { GamePermission } from '../lib/types';

  let { id }: { id: number } = $props();

  const game = load(() => api.game(id));
  // Its state and use, kept fresh while the page is open.
  $effect(() => {
    const timer = setInterval(() => void game.reload(), 5000);
    return () => clearInterval(timer);
  });

  // Its use over the last ten minutes, for the charts and the network rates.
  const history = load(() => api.gameHistory(id));
  $effect(() => {
    const timer = setInterval(() => void history.reload(), 10000);
    return () => clearInterval(timer);
  });
  const samples = $derived(history.data ?? []);
  // Network rates (bytes a second) between samples; a game restarted starts its totals again, so a drop is 0.
  const rates = $derived(
    samples.slice(1).map((s, i) => {
      const before = samples[i];
      const secs = Math.max(1, s.t - before.t);
      return { in: Math.max(0, s.netIn - before.netIn) / secs, out: Math.max(0, s.netOut - before.netOut) / secs };
    }),
  );
  const rateNow = $derived(rates.at(-1) ?? { in: 0, out: 0 });

  const ICONS = {
    address: 'M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z',
    uptime: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 7v5l3 2',
    players: 'M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM2 21a7 7 0 0 1 14 0M17 11a3.5 3.5 0 1 0 0-7M22 21a6 6 0 0 0-4-5.7',
    cpu: 'M8 8h8v8H8zM9 3v3M15 3v3M9 18v3M15 18v3M3 9h3M3 15h3M18 9h3M18 15h3M6 6h12v12H6z',
    memory: 'M3 8h18v8H3zM7 8v8M11 8v8M15 8v8M19 8v8M6 16v3M18 16v3',
    disk: 'M4 6c0-1.7 3.6-3 8-3s8 1.3 8 3-3.6 3-8 3-8-1.3-8-3zM4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6',
    down: 'M12 4v12M7 11l5 5 5-5M5 20h14',
    up: 'M12 20V8M7 13l5-5 5 5M5 4h14',
  };

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
  // What the viewer may do with it: a friend sees only the tabs and buttons shared with them.
  const can = (p: GamePermission) => !!game.data?.can.includes(p);
  const tabs = $derived(
    [
      { id: 'console', label: 'Console', show: true },
      { id: 'files', label: 'Files', show: can('files') },
      { id: 'schedules', label: 'Schedules', show: can('schedules') },
      { id: 'backups', label: 'Backups', show: can('backups') },
      { id: 'settings', label: 'Settings', show: !!game.data?.owner || can('startup') },
    ].filter((t) => t.show),
  );

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
    {:else if can('power')}
      <button class="primary" disabled={g.state === 'offline'} onclick={() => power('start', 'Starting.')}>Start</button>
    {/if}
    {#if can('power')}
      {#if g.state === 'running' || g.state === 'starting'}
        <button onclick={() => (stopping = 'restart')}>Restart</button>
        <button onclick={() => (stopping = 'stop')}>Stop</button>
      {/if}
      {#if running}<button class="danger" onclick={kill}>Kill</button>{/if}
    {/if}
  </div>

  {#if message}<p class="note" class:error={!message.ok}>{message.text}</p>{/if}

  <Tabs bind:selected={tab} {tabs} />

  {#if tab === 'console'}
    {@const live = g.state === 'running' || g.state === 'starting'}
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
        {#if can('console')}
          <GameConsole game={g} onerror={problem} oneula={(needed) => (eula = needed)} />
        {:else}
          <p class="card muted">Its console isn't shared with you.</p>
        {/if}
      </div>
      <aside class="stats" aria-label="Its figures">
        <StatCard icon={ICONS.address} label="Address" value={g.address} mono />
        <StatCard icon={ICONS.uptime} label="Uptime" value={live ? duration(g.uptimeSecs) : 'Not running'} />
        {#if g.players}
          <StatCard icon={ICONS.players} label="Players" value={String(g.players.online)} of="/ {g.players.max}" detail={g.players.names.join(', ')} />
        {/if}
        <StatCard
          icon={ICONS.cpu}
          label="CPU"
          value="{live ? g.cpuPercent.toFixed(1) : 0}%"
          of={g.cpuLimit ? `/ ${g.cpuLimit}%` : ''}
          percent={g.cpuLimit ? percent(g.cpuPercent, g.cpuLimit) : undefined}
        />
        <StatCard icon={ICONS.memory} label="Memory" value={bytes(live ? g.memUsed : 0)} of="/ {bytes(g.memLimit)}" percent={live ? percent(g.memUsed, g.memLimit) : 0} />
        <StatCard
          icon={ICONS.disk}
          label="Disk"
          tone="amber"
          value={bytes(g.diskUsed)}
          of={g.diskLimit ? `/ ${bytes(g.diskLimit)}` : '(no limit)'}
          percent={g.diskLimit ? percent(g.diskUsed, g.diskLimit) : undefined}
        />
        <StatCard icon={ICONS.down} label="Network in" tone="blue" value={live ? bitRate(rateNow.in) : '0 bps'} note="{bytes(g.netIn)} in all" />
        <StatCard icon={ICONS.up} label="Network out" tone="blue" value={live ? bitRate(rateNow.out) : '0 bps'} note="{bytes(g.netOut)} in all" />
      </aside>
    </div>

    <div class="charts">
      <section class="card">
        <h2>CPU <span class="muted">{live ? g.cpuPercent.toFixed(1) : 0}%</span></h2>
        <Chart
          series={[{ label: 'CPU', values: samples.map((s) => s.cpu), colour: 'var(--accent)' }]}
          max={g.cpuLimit || undefined}
          format={(n) => `${Math.round(n)}%`}
        />
      </section>
      <section class="card">
        <h2>Memory <span class="muted">{bytes(live ? g.memUsed : 0)}</span></h2>
        <Chart
          series={[{ label: 'Memory', values: samples.map((s) => s.mem), colour: 'var(--accent)' }]}
          limit={{ value: g.memLimit, label: `Limit ${bytes(g.memLimit)}` }}
          format={(n) => bytes(n)}
        />
      </section>
      <section class="card">
        <h2>Network</h2>
        <p class="legend small">
          <span class="key out"></span>Out <span class="muted">{bitRate(rateNow.out)}</span>
          <span class="key in"></span>In <span class="muted">{bitRate(rateNow.in)}</span>
        </p>
        <Chart
          series={[
            { label: 'Out', values: rates.map((r) => r.out), colour: 'var(--blue)' },
            { label: 'In', values: rates.map((r) => r.in), colour: 'var(--amber)' },
          ]}
          format={(n) => bitRate(n)}
        />
      </section>
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
    grid-template-columns: minmax(0, 1fr) 300px;
    gap: 16px;
    align-items: start;
  }
  .stats {
    display: grid;
    gap: 10px;
  }
  .charts {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 300px), 1fr));
    gap: 16px;
    margin-top: 16px;
  }
  .charts h2 {
    margin: 0 0 12px;
    display: flex;
    gap: 8px;
    align-items: baseline;
  }
  .charts h2 .muted {
    font-weight: 400;
    font-size: 13px;
  }
  .legend {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: -6px 0 10px;
  }
  .key {
    width: 14px;
    height: 0;
    border-top: 2px solid var(--blue);
    margin-left: 6px;
  }
  .key.out {
    margin-left: 0;
  }
  .key.in {
    border-top: 2px dashed var(--amber);
  }
  @media (max-width: 1000px) {
    .console-layout {
      grid-template-columns: 1fr;
    }
    .stats {
      grid-template-columns: repeat(auto-fit, minmax(min(100%, 220px), 1fr));
    }
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
