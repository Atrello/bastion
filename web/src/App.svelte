<!-- The panel's frame: setup or sign-in until someone's signed in, then the menu and the page for the
     current address. -->
<script lang="ts">
  import Sidebar from './components/Sidebar.svelte';
  import { api, type AuthState } from './lib/api';
  import Account from './pages/Account.svelte';
  import Setup from './pages/Setup.svelte';
  import SignIn from './pages/SignIn.svelte';
  import { match, navigate, route } from './lib/router.svelte';
  import Containers from './pages/Containers.svelte';
  import Alerts from './pages/Alerts.svelte';
  import Eggs from './pages/Eggs.svelte';
  import Images from './pages/Images.svelte';
  import Volumes from './pages/Volumes.svelte';
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

  let auth = $state<AuthState>();
  async function checkAuth() {
    auth = await api.authState();
    if (auth.signedIn && route.path === '/setup') navigate('/');
  }
  void checkAuth();
  addEventListener('bastion:signed-out', () => void checkAuth());

  async function signOut() {
    await api.signOut();
    await checkAuth();
  }

  const stack = $derived(match('/stacks/:server/:name'));
  const game = $derived(match('/games/:id'));
</script>

<svelte:document onclick={followLink} />

{#if !auth}
  <!-- Checking whether anyone's signed in: a moment, so nothing is shown. -->
{:else if auth.setupNeeded}
  <Setup passkeys={auth.passkeys} ondone={checkAuth} />
{:else if !auth.signedIn}
  <SignIn passkeys={auth.passkeys} ondone={checkAuth} />
{:else}
<div class="frame">
  <Sidebar username={auth.username ?? ''} onsignout={signOut} />
  <main>
    {#if route.path === '/'}
      <Overview />
    {:else if route.path === '/servers'}
      <Servers />
    {:else if route.path === '/alerts'}
      <Alerts />
    {:else if route.path === '/stacks'}
      <Stacks />
    {:else if stack}
      {#key route.path}<Stack serverId={Number(stack.server)} name={stack.name} />{/key}
    {:else if route.path === '/containers'}
      <Containers />
    {:else if route.path === '/volumes'}
      <Volumes />
    {:else if route.path === '/images'}
      <Images />
    {:else if route.path === '/games'}
      <Games />
    {:else if game}
      {#key route.path}<Game id={Number(game.id)} />{/key}
    {:else if route.path === '/eggs'}
      <Eggs />
    {:else if route.path === '/account'}
      <Account passkeys={auth.passkeys} />
    {:else}
      <p class="empty">There's nothing at this address. <a href="/">Back to the overview</a></p>
    {/if}
  </main>
</div>
{/if}

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
