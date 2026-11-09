<!-- A stack's logs, following live: the last 200 lines, then each new one as it's written. -->
<script lang="ts">
  import { api } from '../lib/api';
  import { openStream } from '../lib/stream';

  let { serverId, name }: { serverId: number; name: string } = $props();
  const MAX_LINES = 2000;
  let lines = $state<string[]>([]);
  let status = $state('Connecting…');
  let box: HTMLPreElement;
  let pinned = true; // keep scrolled to the bottom until the reader scrolls up

  $effect(() => {
    if (import.meta.env.VITE_DEMO === '1') {
      void api.stackLogs(serverId, name).then((r) => ((lines = r.lines), (status = 'Demo: not live')));
      return;
    }
    const stream = openStream(
      `logs?server=${serverId}&stack=${encodeURIComponent(name)}`,
      (text) => {
        status = 'Live';
        lines = lines.length >= MAX_LINES ? [...lines.slice(-MAX_LINES + 1), text] : [...lines, text];
      },
      (reason) => (status = reason ?? 'Stopped'),
    );
    return () => stream.close();
  });

  $effect(() => {
    void lines.length;
    if (pinned && box) queueMicrotask(() => (box.scrollTop = box.scrollHeight));
  });
</script>

<p class="muted small">{status} · the last {MAX_LINES} lines are kept here</p>
<pre class="logs mono" bind:this={box} onscroll={() => (pinned = box.scrollTop + box.clientHeight >= box.scrollHeight - 8)}>{#each lines as line, i (i)}<span class:warn={/\bWARN|\bERROR|\berror\b/.test(line)}>{line}</span>
{/each}</pre>

<style>
  p {
    margin: 0 0 8px;
  }
  .logs {
    margin: 0;
    height: 520px;
    overflow: auto;
    padding: 12px 14px;
    background: #0b0d11;
    border: 1px solid var(--line);
    border-radius: 8px;
    line-height: 1.6;
    color: #b7c0cf;
  }
  .warn {
    color: var(--amber);
  }
</style>
