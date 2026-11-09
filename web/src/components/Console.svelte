<!-- A terminal in a container (its shell), through the panel to the server's agent. -->
<script lang="ts">
  import '@xterm/xterm/css/xterm.css';
  import { FitAddon } from '@xterm/addon-fit';
  import { Terminal } from '@xterm/xterm';
  import { openStream } from '../lib/stream';

  let { serverId, container }: { serverId: number; container: string } = $props();
  let host: HTMLDivElement;
  let status = $state('Connecting…');

  const toB64 = (s: string) => btoa(String.fromCharCode(...new TextEncoder().encode(s)));

  $effect(() => {
    const term = new Terminal({
      fontFamily: 'ui-monospace, "JetBrains Mono", Menlo, monospace',
      fontSize: 13,
      theme: { background: '#0b0d0f', foreground: '#d8d2c8', cursor: '#f08c4f', selectionBackground: '#3a2a20' },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host);
    fit.fit();
    const stream = openStream(
      `console?server=${serverId}&container=${encodeURIComponent(container)}&cols=${term.cols}&rows=${term.rows}`,
      (b64) => {
        status = 'Connected';
        term.write(Uint8Array.from(atob(b64), (c) => c.charCodeAt(0)));
      },
      (reason) => {
        status = reason ?? 'The shell has ended.';
        term.write('\r\n[ended]\r\n');
      },
    );
    term.onData((data) => stream.send({ type: 'input', data: toB64(data) }));
    const resize = new ResizeObserver(() => {
      fit.fit();
      stream.send({ type: 'resize', cols: term.cols, rows: term.rows });
    });
    resize.observe(host);
    term.focus();
    return () => {
      resize.disconnect();
      stream.close();
      term.dispose();
    };
  });
</script>

<p class="muted small">{status}</p>
<div class="term" bind:this={host}></div>

<style>
  p {
    margin: 0 0 8px;
  }
  .term {
    height: 480px;
    padding: 8px;
    background: var(--sunken);
    border: 1px solid var(--line);
    border-radius: 8px;
  }
</style>
