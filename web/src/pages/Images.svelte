<!-- A server's images: how much each takes up, and whether anything uses it. -->
<script lang="ts">
  import Cleanup from '../components/Cleanup.svelte';
  import ServerPicker from '../components/ServerPicker.svelte';
  import { api } from '../lib/api';
  import { bytes } from '../lib/format';
  import type { Image } from '../lib/types';

  let serverId = $state(0);
  let images = $state<Image[]>();
  let error = $state<string>();
  let cleaning = $state(false);

  async function reload() {
    try {
      images = (await api.images(serverId)).sort((a, b) => b.size - a.size);
      error = undefined;
    } catch (err) {
      error = (err as Error).message;
    }
  }
  $effect(() => {
    if (serverId) void reload();
  });
  const unused = $derived(images?.filter((i) => !i.containers).reduce((sum, i) => sum + i.uniqueSize, 0) ?? 0);
</script>

<div class="page-head">
  <h1>Images</h1>
  {#if images && unused}<span class="muted">{bytes(unused)} in images nothing uses</span>{/if}
  <ServerPicker bind:value={serverId} />
  <button disabled={!serverId} onclick={() => (cleaning = true)}>Clean up</button>
</div>

{#if error}<p class="error">{error}</p>{/if}
<div class="card table-card">
  <table>
    <thead><tr><th>Image</th><th>Tag</th><th>ID</th><th>Made</th><th>Used by</th><th class="num">Size</th></tr></thead>
    <tbody>
      {#each images ?? [] as i (i.id + i.tag)}
        <tr>
          <td class="mono">{i.repository}</td>
          <td class="mono">{i.tag}</td>
          <td class="mono muted">{i.id}</td>
          <td class="muted">{i.created}</td>
          <td>{#if i.containers}{i.containers === 1 ? '1 container' : `${i.containers} containers`}{:else}<span class="badge amber">Unused</span>{/if}</td>
          <td class="num">{bytes(i.size)}</td>
        </tr>
      {:else}
        {#if images}<tr><td colspan="6" class="empty">No images on this server.</td></tr>{/if}
      {/each}
    </tbody>
  </table>
</div>

{#if cleaning}<Cleanup {serverId} onclose={() => (cleaning = false)} ondone={reload} />{/if}

<style>
  .page-head h1 {
    flex: none;
    margin-right: auto;
  }
  .num {
    text-align: right;
  }
</style>
