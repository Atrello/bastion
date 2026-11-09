<!-- Picks one of the online servers, remembering the choice in the address (?server=). -->
<script lang="ts">
  import { api } from '../lib/api';
  import { load } from '../lib/load.svelte';

  let { value = $bindable() }: { value: number } = $props();
  const servers = load(api.servers);

  $effect(() => {
    const online = servers.data?.filter((s) => s.online) ?? [];
    if (!value && online.length) value = Number(new URLSearchParams(location.search).get('server')) || online[0].id;
  });
  $effect(() => {
    if (value) history.replaceState(null, '', `?server=${value}`);
  });
</script>

<select aria-label="Server" bind:value>
  {#each (servers.data ?? []).filter((s) => s.online) as s (s.id)}<option value={s.id}>{s.name}</option>{/each}
</select>
