<!-- Things under way, in the corner of every page: uploads, and jobs the panel runs on a server (unpacking,
     archiving, moving files). Each shows its progress and can be stopped; ended ones stay until cleared.
     The list itself is lib/tasks.svelte.ts. -->
<script lang="ts">
  import { bytes } from '../lib/format';
  import { cancel, clearFinished, jobs, stopJob, uploads } from '../lib/tasks.svelte';

  // Uploads and jobs, shown alike.
  type Row = {
    key: string;
    title: string;
    detail: string;
    /** 0 to 1, or null when how far isn't known. */
    done: number | null;
    amount: string;
    status: 'waiting' | 'running' | 'stopping' | 'done' | 'failed' | 'stopped';
    error?: string;
    stop: () => void;
  };

  const rows = $derived<Row[]>([
    ...uploads.map((u) => ({
      key: `u${u.key}`,
      title: `Uploading ${u.name}`,
      detail: `${u.gameName}, ${u.path.slice(0, u.path.lastIndexOf('/') + 1)}`,
      done: u.done,
      amount: '',
      status: ({ waiting: 'waiting', sending: 'running', done: 'done', failed: 'failed', cancelled: 'stopped' } as const)[u.state],
      error: u.state === 'failed' ? u.error : undefined,
      stop: () => cancel(u.key),
    })),
    ...jobs.map((j) => ({
      key: `j${j.id}`,
      title: j.title,
      detail: j.detail,
      done: j.total > 0 ? Math.min(1, j.done / j.total) : null,
      // A move counts items; the others bytes.
      amount: j.total > 0 && !j.title.startsWith('Moving') ? `${bytes(j.done)} of ${bytes(j.total)}` : '',
      status: j.state,
      error: j.state === 'failed' || j.state === 'stopped' ? j.error : undefined,
      stop: () => void stopJob(j.id),
    })),
  ]);

  let open = $state(true);
  const going = $derived(rows.filter((r) => r.status === 'waiting' || r.status === 'running' || r.status === 'stopping').length);
  const failed = $derived(rows.filter((r) => r.status === 'failed').length);
  const label = { waiting: 'Waiting', running: '', stopping: 'Stopping…', done: 'Done', failed: 'Failed', stopped: 'Stopped' };
</script>

{#if rows.length}
  <section class="tasks card" aria-label="Under way">
    <div class="row head">
      <button class="link grow" onclick={() => (open = !open)} aria-expanded={open}>
        <b>{#if going}{going} under way{:else if failed}{failed} failed{:else}All done{/if}</b>
      </button>
      {#if rows.length > going}<button class="small" onclick={clearFinished}>Clear</button>{/if}
    </div>
    {#if open}
      <ul>
        {#each rows as r (r.key)}
          <li>
            <div class="row">
              <span class="grow name" title={r.title}>{r.title}</span>
              {#if r.status === 'running'}
                <span class="muted small">{r.done === null ? '' : `${Math.round(r.done * 100)}%`}</span>
              {:else}
                <span class="small" class:muted={r.status !== 'failed'} class:error={r.status === 'failed'}>{label[r.status]}</span>
              {/if}
              {#if r.status === 'waiting' || r.status === 'running'}
                <button class="small" onclick={r.stop} aria-label="Stop {r.title}">Stop</button>
              {/if}
            </div>
            <span class="muted small where">{r.detail}{r.amount && r.status === 'running' ? ` · ${r.amount}` : ''}</span>
            {#if r.status === 'running' || r.status === 'stopping'}
              <span class="bar" class:unknown={r.done === null}><span style:width={r.done === null ? '' : `${Math.round(r.done * 100)}%`}></span></span>
            {/if}
            {#if r.error}<span class="small" class:error={r.status === 'failed'} class:muted={r.status !== 'failed'}>{r.error}</span>{/if}
          </li>
        {/each}
      </ul>
    {/if}
  </section>
{/if}

<style>
  .tasks {
    position: fixed;
    right: 16px;
    bottom: 16px;
    z-index: 40;
    width: min(380px, calc(100vw - 32px));
    max-height: min(60vh, 440px);
    display: flex;
    flex-direction: column;
    padding: 0;
    box-shadow: 0 8px 28px rgb(0 0 0 / 0.28);
  }
  .head {
    padding: 10px 12px;
    border-bottom: 1px solid var(--line);
  }
  .head .link {
    text-align: left;
    color: inherit;
    background: none;
    border: none;
    padding: 0;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 4px 12px 10px;
    overflow-y: auto;
  }
  li {
    display: grid;
    gap: 4px;
    padding: 8px 0;
    border-bottom: 1px solid var(--line);
  }
  li:last-child {
    border-bottom: none;
  }
  .name,
  .where {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .error {
    margin: 0;
  }
  button.small {
    padding: 2px 10px;
  }
  .bar {
    display: block;
    height: 6px;
    border-radius: 3px;
    background: var(--line);
    overflow: hidden;
  }
  .bar span {
    display: block;
    height: 100%;
    background: var(--accent);
    transition: width 0.2s;
  }
  /* How far isn't known yet: a stripe going back and forth. */
  .bar.unknown span {
    width: 30%;
    animation: along 1.2s ease-in-out infinite alternate;
  }
  @keyframes along {
    from {
      transform: translateX(-10%);
    }
    to {
      transform: translateX(245%);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .bar.unknown span {
      animation: none;
      width: 100%;
      opacity: 0.4;
    }
  }
</style>
