<!-- A game's console: its recent output, new lines live while it runs, and a line to type commands into
     (up and down go through the ones sent before). While it installs, the installer's output instead.
     Along the top: whether it's connected, a search that shows only matching lines, and the text size. -->
<script lang="ts">
  import LogLine from './LogLine.svelte';
  import StatusDot from './StatusDot.svelte';
  import { api } from '../lib/api';
  import { openStream } from '../lib/stream';
  import type { GameServer } from '../lib/types';

  // oneula: the game has said it needs Minecraft's EULA accepted before it will run.
  let { game, onerror, oneula }: { game: GameServer; onerror: (message: string) => void; oneula?: (needed: boolean) => void } = $props();

  const KEEP = 1000;
  let lines = $state<string[]>([]);
  let box: HTMLPreElement;
  let follow = true;
  let command = $state('');
  let sent: string[] = [];
  let back = 0;
  let connected = $state(false);
  let search = $state('');
  let searching = $state(false);
  // The text size, remembered in this browser.
  let size = $state(readSize());
  function readSize() {
    try {
      return Math.min(20, Math.max(10, Number(localStorage.getItem('bastion.console.size')) || 13));
    } catch {
      return 13;
    }
  }
  function resize(by: number) {
    size = Math.min(20, Math.max(10, size + by));
    try {
      localStorage.setItem('bastion.console.size', String(size));
    } catch {
      // not kept: fine
    }
  }
  const shown = $derived(search.trim() ? lines.filter((l) => l.toLowerCase().includes(search.trim().toLowerCase())) : lines);

  // Only a change of state reopens the console, not each fresh copy of the game the page loads.
  const gameState = $derived(game.state);
  const id = $derived(game.id);
  const running = $derived(gameState === 'running' || gameState === 'starting' || gameState === 'stopping');
  const installing = $derived(gameState === 'installing');

  function add(text: string) {
    lines = [...lines, ...text.replace(/\n$/, '').split('\n')].slice(-KEEP);
  }

  // Reload the history when the game starts, stops or installs, and follow it live while it runs.
  $effect(() => {
    const gameId = id;
    let stream: ReturnType<typeof openStream> | undefined;
    let poll: ReturnType<typeof setInterval> | undefined;
    const fetchLines = () =>
      api
        .gameConsole(gameId)
        .then((r) => (lines = r.lines.slice(-KEEP)))
        .catch(() => {});
    void fetchLines();
    connected = false;
    if (running) {
      stream = openStream(
        `game?id=${gameId}`,
        add,
        () => (connected = false),
        () => (connected = true),
      );
    } else if (installing) {
      poll = setInterval(fetchLines, 3000);
    }
    return () => {
      stream?.close();
      clearInterval(poll);
      connected = false;
    };
  });

  $effect(() => {
    oneula?.(!running && lines.slice(-20).some((l) => /agree to the EULA/i.test(l)));
  });

  $effect(() => {
    void shown.length;
    if (box && follow) queueMicrotask(() => (box.scrollTop = box.scrollHeight));
  });

  async function send(e: SubmitEvent) {
    e.preventDefault();
    const line = command.trim();
    if (!line) return;
    sent = [...sent.filter((s) => s !== line), line].slice(-50);
    back = 0;
    command = '';
    try {
      await api.gameCommand(id, line);
    } catch (err) {
      onerror((err as Error).message);
    }
  }

  function keys(e: KeyboardEvent) {
    if (e.key === 'ArrowUp' && back < sent.length) back++;
    else if (e.key === 'ArrowDown' && back > 0) back--;
    else return;
    e.preventDefault();
    command = back ? sent[sent.length - back] : '';
  }
</script>

<div class="console-box">
<div class="bar row small">
  <span class="row grow state">
    {#if running}
      <StatusDot tone={connected ? 'green' : 'amber'} />{connected ? 'Live' : 'Connecting…'}
    {:else if installing}
      <StatusDot tone="amber" />Installing
    {:else}
      <StatusDot tone="grey" />Not running
    {/if}
  </span>
  {#if searching}
    <!-- svelte-ignore a11y_autofocus -->
    <input class="search" placeholder="Show lines with…" aria-label="Search the console" autofocus bind:value={search} onkeydown={(e) => e.key === 'Escape' && ((search = ''), (searching = false))} />
    {#if search.trim()}<span class="muted">{shown.length} of {lines.length}</span>{/if}
  {/if}
  <button class="tool" aria-label={searching ? 'Close the search' : 'Search the console'} aria-pressed={searching} onclick={() => ((searching = !searching), (search = ''))}>
    <svg viewBox="0 0 24 24" aria-hidden="true"><path d={searching ? 'M6 6l12 12M18 6L6 18' : 'M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM20 20l-4-4'} /></svg>
  </button>
  <button class="tool" aria-label="Smaller text" onclick={() => resize(-1)} disabled={size <= 10}>
    <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12h14" /></svg>
  </button>
  <span class="size muted">{size}px</span>
  <button class="tool" aria-label="Larger text" onclick={() => resize(1)} disabled={size >= 20}>
    <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12h14M12 5v14" /></svg>
  </button>
</div>
<pre class="console mono" style:font-size="{size}px" bind:this={box} onscroll={() => (follow = box.scrollTop + box.clientHeight >= box.scrollHeight - 8)}>{#each shown as line, i (i)}<span class:warn={/\bWARN\b/.test(line)} class:bad={/\bERROR\b|Exception/.test(line)}><LogLine {line} /></span>
{:else}<span class="muted">{search.trim() ? 'No lines match.' : installing ? 'Installing…' : running ? 'Nothing yet.' : 'Not running. Start it to see its console.'}</span>{/each}</pre>
<form class="row command" onsubmit={send}>
  <input
    class="grow mono"
    aria-label="Command"
    placeholder={running ? 'Type a command, like: say Restarting at 19:00' : 'Start it to send commands'}
    disabled={!running}
    autocomplete="off"
    onkeydown={keys}
    bind:value={command}
  />
  <button type="submit" disabled={!running || !command.trim()}>Send</button>
</form>
</div>

<style>
  .console-box {
    background: var(--sunken);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    padding: 8px 10px 10px;
    display: grid;
    gap: 8px;
  }
  .bar {
    gap: 6px;
    min-height: 30px;
  }
  .state {
    gap: 8px;
    color: var(--soft);
  }
  .search {
    flex: 0 1 220px;
    width: 0;
    min-width: 80px;
    padding: 4px 8px;
  }
  .state {
    min-width: 0;
    white-space: nowrap;
  }
  .tool {
    padding: 4px;
    width: 28px;
    height: 28px;
    display: grid;
    place-items: center;
    background: none;
    border-color: transparent;
  }
  .tool:hover:not(:disabled) {
    background: var(--hover);
  }
  .tool svg {
    width: 16px;
    height: 16px;
    fill: none;
    stroke: currentColor;
    stroke-width: 2;
    stroke-linecap: round;
  }
  .size {
    font-variant-numeric: tabular-nums;
    min-width: 34px;
    text-align: center;
  }
  .console {
    margin: 0;
    height: 520px;
    overflow: auto;
    padding: 4px 4px 0;
    line-height: 1.6;
    color: var(--soft);
    white-space: pre-wrap;
    word-break: break-word;
  }
  .warn {
    color: var(--amber);
  }
  .bad {
    color: var(--red);
  }
  .command input {
    background: var(--bg);
  }
</style>
