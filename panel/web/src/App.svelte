<!-- The panel's frame: the menu, and the page for the current address. -->
<script lang="ts">
  import Sidebar from './components/Sidebar.svelte';
  import { match, navigate, route } from './lib/router.svelte';
  import Containers from './pages/Containers.svelte';
  import Eggs from './pages/Eggs.svelte';
  import Game from './pages/Game.svelte';
  import Games from './pages/Games.svelte';
  import Overview from './pages/Overview.svelte';
  import Servers from './pages/Servers.svelte';
  import Stack from './pages/Stack.svelte';
  import Stacks from './pages/Stacks.svelte';

  // Links inside the panel change the page without reloading it.
  function followLink(e: MouseEvent) {
    const a = (e.target as Element).closest('a');
    if (!a || a.origin !== location.origin || a.target || e.ctrlKey || e.metaKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    navigate(a.pathname + a.search);
  }

  const stack = $derived(match('/stacks/:server/:name'));
  const game = $derived(match('/games/:id'));
</script>

<svelte:document onclick={followLink} />

<div class="frame">
  <Sidebar />
  <main>
    {#if route.path === '/'}
      <Overview />
    {:else if route.path === '/servers'}
      <Servers />
    {:else if route.path === '/stacks'}
      <Stacks />
    {:else if stack}
      {#key route.path}<Stack serverId={Number(stack.server)} name={stack.name} />{/key}
    {:else if route.path === '/containers'}
      <Containers />
    {:else if route.path === '/games'}
      <Games />
    {:else if game}
      {#key route.path}<Game id={Number(game.id)} />{/key}
    {:else if route.path === '/eggs'}
      <Eggs />
    {:else}
      <p class="empty">There's nothing at this address. <a href="/">Back to the overview</a></p>
    {/if}
  </main>
</div>

<style>
  .frame {
    display: flex;
    min-height: 100vh;
  }
  main {
    flex: 1;
    min-width: 0;
    padding: 20px 28px 40px;
  }
</style>
