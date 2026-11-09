<!-- Uploads in progress, in the corner of every page, so you can carry on elsewhere while they go
     (lib/uploads.svelte.ts). Each can be cancelled; finished ones stay until cleared. -->
<script lang="ts">
  import { busy, cancel, clearFinished, uploads } from '../lib/uploads.svelte';

  let open = $state(true);
  const left = $derived(uploads.filter((u) => u.state === 'waiting' || u.state === 'sending').length);
  const failed = $derived(uploads.filter((u) => u.state === 'failed').length);
  const label = { waiting: 'Waiting', sending: '', done: 'Done', failed: 'Failed', cancelled: 'Cancelled' };
</script>

{#if uploads.length}
  <section class="uploads card" aria-label="Uploads">
    <div class="row head">
      <button class="link grow" onclick={() => (open = !open)} aria-expanded={open}>
        <b>
          {#if left}Uploading {left} file{left === 1 ? '' : 's'}{:else if failed}{failed} upload{failed === 1 ? '' : 's'} failed{:else}Uploads done{/if}
        </b>
      </button>
      {#if !busy()}<button class="small" onclick={clearFinished}>Clear</button>{/if}
    </div>
    {#if open}
      <ul>
        {#each uploads as u (u.key)}
          <li>
            <div class="row">
              <span class="grow name mono" title="{u.gameName}: {u.path}">{u.name}</span>
              {#if u.state === 'sending'}
                <span class="muted small">{Math.round(u.done * 100)}%</span>
              {:else}
                <span class="small" class:muted={u.state !== 'failed'} class:error={u.state === 'failed'}>{label[u.state]}</span>
              {/if}
              {#if u.state === 'waiting' || u.state === 'sending'}
                <button class="small" onclick={() => cancel(u.key)} aria-label="Cancel uploading {u.name}">Cancel</button>
              {/if}
            </div>
            <span class="muted small where">To {u.gameName}, {u.path}</span>
            {#if u.state === 'sending'}<span class="bar"><span style:width="{Math.round(u.done * 100)}%"></span></span>{/if}
            {#if u.error && u.state === 'failed'}<span class="error small">{u.error}</span>{/if}
          </li>
        {/each}
      </ul>
    {/if}
  </section>
{/if}

<style>
  .uploads {
    position: fixed;
    right: 16px;
    bottom: 16px;
    z-index: 40;
    width: min(360px, calc(100vw - 32px));
    max-height: min(60vh, 420px);
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
</style>
