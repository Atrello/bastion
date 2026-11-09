<!-- The panel's frame: setup or sign-in until someone's signed in, then the menu and the page for the
     current address. -->
<script lang="ts">
  import Ask from './components/Ask.svelte';
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
  import Proxy from './pages/Proxy.svelte';
  import Server from './pages/Server.svelte';
  import type { NpmKind } from './lib/types';
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
    if (auth.signedIn && (route.path === '/setup' || route.path === '/invite')) navigate('/');
  }
  void checkAuth();
  addEventListener('bastion:signed-out', () => void checkAuth());
  let updated = $state(false);
  addEventListener('bastion:updated', () => (updated = true));

  async function signOut() {
    await api.signOut();
    await checkAuth();
  }

  const stack = $derived(match('/stacks/:server/:name'));
  const game = $derived(match('/games/:id'));
  const serverPage = $derived(match('/servers/:id'));
  const proxy = $derived(route.path === '/proxy' ? { kind: 'proxy-hosts' } : match('/proxy/:kind'));
  const NPM_KINDS = ['proxy-hosts', 'redirection-hosts', 'streams', 'dead-hosts', 'access-lists', 'certificates'];
</script>

<svelte:document onclick={followLink} />

<Ask />

{#if updated}
  <div class="updated" role="status">
    Bastion has been updated. Reload to use the new version.
    <button class="primary" onclick={() => location.reload()}>Reload</button>
  </div>
{/if}

{#if !auth}
  <!-- Checking whether anyone's signed in: a moment, so nothing is shown. -->
{:else if route.path === '/invite' && !auth.signedIn}
  <Setup invited passkeys={auth.passkeys} ondone={checkAuth} />
{:else if auth.setupNeeded}
  <Setup passkeys={auth.passkeys} ondone={checkAuth} />
{:else if !auth.signedIn}
  <SignIn passkeys={auth.passkeys} ondone={checkAuth} />
{:else}
<div class="frame">
  <Sidebar username={auth.username ?? ''} friend={auth.role === 'friend'} onsignout={signOut} />
  <main>
    {#if auth.role === 'friend'}
      <!-- A friend sees only the game servers shared with them, and their own account. -->
      {#if game}
        {#key route.path}<Game id={Number(game.id)} />{/key}
      {:else if route.path === '/account'}
        <Account passkeys={auth.passkeys} />
      {:else}
        <Games owner={false} />
      {/if}
    {:else if route.path === '/'}
      <Overview />
    {:else if route.path === '/servers'}
      <Servers />
    {:else if serverPage}
      {#key route.path}<Server id={Number(serverPage.id)} />{/key}
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
    {:else if proxy && NPM_KINDS.includes(proxy.kind)}
      <Proxy kind={proxy.kind as NpmKind} />
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
  .updated {
    position: fixed;
    right: 16px;
    bottom: 16px;
    z-index: 60;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 12px 10px 16px;
    background: var(--raised);
    border: 1px solid var(--accent);
    border-radius: var(--radius);
    box-shadow: 0 8px 30px rgb(0 0 0 / 40%);
  }
  .frame {
    display: flex;
    min-height: 100vh;
  }
  main {
    flex: 1;
    min-width: 0;
    padding: 20px 28px 40px;
  }
  /* On a phone the menu is a bar along the top. */
  @media (max-width: 760px) {
    .frame {
      flex-direction: column;
    }
    main {
      padding: 16px 14px 32px;
    }
  }
</style>
