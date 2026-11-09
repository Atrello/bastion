<!-- A game's console: its recent output, new lines live while it runs, and a line to type commands into
     (up and down go through the ones sent before). While it installs, the installer's output instead. -->
<script lang="ts">
  import { api } from '../lib/api';
  import { openStream } from '../lib/stream';
  import type { GameServer } from '../lib/types';

  let { game, onerror }: { game: GameServer; onerror: (message: string) => void } = $props();

  const KEEP = 1000;
  let lines = $state<string[]>([]);
  let box: HTMLPreElement;
  let follow = true;
  let command = $state('');
  let sent: string[] = [];
  let back = 0;

  // Only a change of state reopens the console, not each fresh copy of the game the page loads.
  const gameState = $derived(game.state);
  const id = $derived(game.id);
  const running = $derived(gameState === 'running' || gameState === 'stopping');
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
    if (running) {
      stream = openStream(`game?id=${gameId}`, add, () => {});
    } else if (installing) {
      poll = setInterval(fetchLines, 3000);
    }
    return () => {
      stream?.close();
      clearInterval(poll);
    };
  });

  $effect(() => {
    void lines.length;
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

<pre class="console mono" bind:this={box} onscroll={() => (follow = box.scrollTop + box.clientHeight >= box.scrollHeight - 8)}>{#each lines as line, i (i)}<span class:warn={/\bWARN\b/.test(line)} class:bad={/\bERROR\b|Exception/.test(line)}>{line}</span>
{:else}<span class="muted">{installing ? 'Installing…' : running ? 'Nothing yet.' : 'Not running. Start it to see its console.'}</span>{/each}</pre>
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

<style>
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
    word-break: break-word;
  }
  .warn {
    color: var(--amber);
  }
  .bad {
    color: var(--red);
  }
  .command {
    margin-top: 10px;
  }
</style>
