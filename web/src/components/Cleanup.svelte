<!-- Clean-up for one server: what nothing uses, and what removing it frees, before anything is deleted.
     Volumes can hold data, so they're only removed when ticked. -->
<script lang="ts">
  import Modal from './Modal.svelte';
  import { api } from '../lib/api';
  import { bytes } from '../lib/format';
  import { load } from '../lib/load.svelte';

  let { serverId, onclose, ondone }: { serverId: number; onclose: () => void; ondone: () => void } = $props();
  const preview = load(() => api.cleanupPreview(serverId));
  let images = $state(true);
  let volumes = $state(false);
  let buildCache = $state(true);
  let busy = $state(false);
  let result = $state<string>();
  let error = $state<string>();

  async function run() {
    busy = true;
    try {
      result = (await api.cleanup(serverId, { images, volumes, buildCache })).output || 'Done.';
      ondone();
    } catch (err) {
      error = (err as Error).message;
    } finally {
      busy = false;
    }
  }
</script>

<Modal title="Clean up" {onclose}>
  {#if result}
    <pre class="mono out">{result}</pre>
  {:else if preview.error}
    <p class="error">{preview.error}</p>
  {:else if preview.data}
    {@const p = preview.data}
    <div class="parts">
      <label class="part">
        <input type="checkbox" bind:checked={images} disabled={!p.images?.length} />
        <span class="grow"><b>Images nothing uses</b> <span class="muted">({p.images?.length ?? 0})</span></span>
        <span>{bytes(p.imagesSize)}</span>
      </label>
      {#if p.images?.length}<ul class="mono small muted">{#each p.images as i (i.name)}<li>{i.name}</li>{/each}</ul>{/if}
      <label class="part">
        <input type="checkbox" bind:checked={volumes} disabled={!p.volumes?.length} />
        <span class="grow"><b>Volumes nothing uses</b> <span class="muted">({p.volumes?.length ?? 0})</span></span>
        <span>{bytes(p.volumesSize)}</span>
      </label>
      {#if p.volumes?.length}
        <ul class="mono small muted">{#each p.volumes as v (v.name)}<li>{v.name} · {bytes(v.size)}</li>{/each}</ul>
        <p class="warn small">Volumes hold data, like databases and uploads. A stopped stack's volumes show here too: only tick this if none of them are wanted.</p>
      {/if}
      <label class="part">
        <input type="checkbox" bind:checked={buildCache} disabled={!p.buildCacheSize} />
        <span class="grow"><b>Build cache</b></span>
        <span>{bytes(p.buildCacheSize)}</span>
      </label>
    </div>
    {#if error}<p class="error">{error}</p>{/if}
  {/if}
  {#snippet footer()}
    {#if result}
      <button class="primary" onclick={onclose}>Done</button>
    {:else}
      <button onclick={onclose}>Cancel</button>
      <button class="primary" class:danger={volumes} disabled={busy || !preview.data || !(images || volumes || buildCache)} onclick={run}>
        {busy ? 'Cleaning up…' : volumes ? 'Remove, including volumes' : 'Remove'}
      </button>
    {/if}
  {/snippet}
</Modal>

<style>
  .parts {
    display: grid;
    gap: 8px;
  }
  .part {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  ul {
    margin: 0 0 6px 28px;
    padding: 0;
    list-style: none;
  }
  .warn {
    color: var(--amber);
    margin: 0 0 6px 28px;
  }
  .out {
    white-space: pre-wrap;
    margin: 0;
  }
  button.primary.danger {
    background: var(--red);
    border-color: var(--red);
    color: #fff;
  }
</style>
