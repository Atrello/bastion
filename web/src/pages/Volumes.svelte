<!-- A server's volumes: how big each is, and which containers use it. -->
<script lang="ts">
  import Cleanup from '../components/Cleanup.svelte';
  import ServerPicker from '../components/ServerPicker.svelte';
  import { api } from '../lib/api';
  import { bytes } from '../lib/format';
  import type { Volume } from '../lib/types';

  let serverId = $state(0);
  let volumes = $state<Volume[]>();
  let error = $state<string>();
  let cleaning = $state(false);

  async function reload() {
    try {
      volumes = (await api.volumes(serverId)).sort((a, b) => b.size - a.size);
      error = undefined;
    } catch (err) {
      error = (err as Error).message;
    }
  }
  $effect(() => {
    if (serverId) void reload();
  });
  const total = $derived(volumes?.reduce((sum, v) => sum + v.size, 0) ?? 0);
</script>

<div class="page-head">
  <h1>Volumes</h1>
  {#if volumes}<span class="muted">{bytes(total)} in all</span>{/if}
  <ServerPicker bind:value={serverId} />
  <button disabled={!serverId} onclick={() => (cleaning = true)}>Clean up</button>
</div>

{#if error}<p class="error">{error}</p>{/if}
<div class="card table-card">
  <table>
    <thead><tr><th>Volume</th><th>Stack</th><th>Used by</th><th class="num">Size</th></tr></thead>
    <tbody>
      {#each volumes ?? [] as v (v.name)}
        <tr>
          <td class="mono">{v.name}</td>
          <td>{#if v.stack}<a href="/stacks/{serverId}/{v.stack}">{v.stack}</a>{/if}</td>
          <td>{#if v.containers}{v.usedBy?.join(', ') ?? v.containers}{:else}<span class="badge amber">Unused</span>{/if}</td>
          <td class="num">{bytes(v.size)}</td>
        </tr>
      {:else}
        {#if volumes}<tr><td colspan="4" class="empty">No volumes on this server.</td></tr>{/if}
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
