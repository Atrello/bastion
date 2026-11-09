<!-- The menu down the left: the logo and each area of the panel. On a phone it's a bar along the top, and
     the menu slides in from the left when its button is pressed. -->
<script lang="ts">
  import { route } from '../lib/router.svelte';

  let { username, version = '', friend = false, onsignout }: { username: string; version?: string; friend?: boolean; onsignout: () => void } = $props();

  type Link = { href: string; label: string; icon: string };
  const groups: { title?: string; links: Link[] }[] = [
    {
      links: [
        { href: '/', label: 'Overview', icon: 'M3 3h7v7H3zM14 3h7v4h-7zM14 11h7v10h-7zM3 14h7v7H3z' },
        { href: '/servers', label: 'Servers', icon: 'M3 4h18v6H3zM3 14h18v6H3zM7 7h.01M7 17h.01' },
        { href: '/alerts', label: 'Alerts', icon: 'M6 8a6 6 0 1 1 12 0c0 7 3 9 3 9H3s3-2 3-9M10.3 21a1.9 1.9 0 0 0 3.4 0' },
      ],
    },
    {
      title: 'Docker',
      links: [
        { href: '/stacks', label: 'Stacks', icon: 'M12 3 3 8l9 5 9-5-9-5zM3 13l9 5 9-5M3 18l9 5 9-5' },
        { href: '/containers', label: 'Containers', icon: 'M4 6h16v12H4zM8 6v12M12 6v12M16 6v12' },
        { href: '/volumes', label: 'Volumes', icon: 'M4 6c0-1.7 3.6-3 8-3s8 1.3 8 3v12c0 1.7-3.6 3-8 3s-8-1.3-8-3zM4 6c0 1.7 3.6 3 8 3s8-1.3 8-3M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3' },
        { href: '/images', label: 'Images', icon: 'M21 16V8l-9-5-9 5v8l9 5 9-5zM3.3 7 12 12l8.7-5M12 22V12' },
      ],
    },
    {
      title: 'Network',
      links: [
        { href: '/proxy', label: 'Proxy', icon: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM3 12h18M12 3c2.5 2.7 3.8 5.7 3.8 9s-1.3 6.3-3.8 9c-2.5-2.7-3.8-5.7-3.8-9s1.3-6.3 3.8-9z' },
      ],
    },
    {
      title: 'Games',
      links: [
        { href: '/games', label: 'Game servers', icon: 'M6 9h4M8 7v4M15 8h.01M17 10h.01M7 4h10a5 5 0 0 1 5 5v3a5 5 0 0 1-9 3h-2a5 5 0 0 1-9-3V9a5 5 0 0 1 5-5z' },
        { href: '/eggs', label: 'Eggs', icon: 'M12 3c4 0 7 6 7 11a7 7 0 0 1-14 0c0-5 3-11 7-11z' },
      ],
    },
  ];
  // A friend has only the game servers shared with them.
  const shown = $derived(friend ? [{ links: groups.flatMap((g) => g.links).filter((l) => l.href === '/games') }] : groups);
  let open = $state(false);
  // Going somewhere closes the menu on a phone.
  $effect(() => {
    void route.path;
    open = false;
  });

  const current = (href: string) => (href === '/' ? route.path === '/' : route.path === href || route.path.startsWith(href + '/'));
</script>

<header class="topbar">
  <button class="menu" aria-label="Menu" aria-expanded={open} onclick={() => (open = !open)}>
    <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M4 12h16M4 17h16" /></svg>
  </button>
  <a class="logo" href="/"><img src="/favicon.svg" alt="" width="24" height="24" /> Bastion</a>
</header>
{#if open}<button class="backdrop" aria-label="Close the menu" onclick={() => (open = false)}></button>{/if}

<nav class:open>
  <a class="logo" href="/">
    <img src="/favicon.svg" alt="" width="26" height="26" />
    Bastion
  </a>
  {#each shown as group, i (i)}
    {#if group.title}<p class="group">{group.title}</p>{/if}
    {#each group.links as link (link.href)}
      <a class="link" class:on={current(link.href)} href={link.href} aria-current={current(link.href) ? 'page' : undefined}>
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d={link.icon} /></svg>
        {link.label}
      </a>
    {/each}
  {/each}
  <div class="foot">
    <a class="link" class:on={current('/account')} href="/account">
      <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM4 21a8 8 0 0 1 16 0" /></svg>
      {username}
    </a>
    <button class="signout" onclick={onsignout}>Sign out</button>
    {#if version}<span class="version">Bastion {version}</span>{/if}
  </div>
</nav>

<style>
  nav {
    position: sticky;
    top: 0;
    height: 100vh;
    width: 220px;
    flex: none;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 16px 12px;
    background: var(--panel);
    border-right: 1px solid var(--line);
  }
  .topbar,
  .backdrop {
    display: none;
  }
  .logo {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 4px 8px 18px;
    color: var(--text);
    font-weight: 700;
    font-size: 17px;
    /* The wordmark: spaced capitals, like a name cut in stone. */
    text-transform: uppercase;
    letter-spacing: 0.14em;
  }
  .group {
    margin: 16px 0 4px;
    padding: 0 10px;
    font-size: 11px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--muted);
  }
  .link {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 10px;
    border-radius: 7px;
    color: var(--soft);
  }
  .link:hover {
    background: var(--raised);
    text-decoration: none;
  }
  .link.on {
    background: var(--accent-bg);
    color: var(--bright);
    box-shadow: inset 2px 0 0 var(--accent);
  }
  svg {
    width: 17px;
    height: 17px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.8;
    stroke-linejoin: round;
    stroke-linecap: round;
  }
  .foot {
    margin-top: auto;
    padding-top: 12px;
    border-top: 1px solid var(--line);
    display: grid;
    gap: 4px;
  }
  .version {
    color: var(--muted);
    font-size: 12px;
    padding: 2px 10px;
  }
  .signout {
    border: 0;
    background: none;
    color: var(--muted);
    text-align: left;
    padding: 4px 10px;
    font-weight: 400;
  }

  @media (max-width: 760px) {
    .topbar {
      position: sticky;
      top: 0;
      z-index: 20;
      display: flex;
      align-items: center;
      gap: 8px;
      padding: 8px 12px;
      background: var(--panel);
      border-bottom: 1px solid var(--line);
    }
    .topbar .logo {
      padding: 0;
      font-size: 15px;
    }
    .menu {
      border: 0;
      background: none;
      padding: 6px;
      display: flex;
    }
    .menu svg {
      width: 22px;
      height: 22px;
    }
    nav {
      position: fixed;
      left: 0;
      z-index: 40;
      transform: translateX(-100%);
      transition: transform 0.2s ease;
      box-shadow: 0 0 40px rgb(0 0 0 / 50%);
    }
    nav.open {
      transform: none;
    }
    .backdrop {
      display: block;
      position: fixed;
      inset: 0;
      z-index: 30;
      border: 0;
      border-radius: 0;
      background: rgb(0 0 0 / 50%);
    }
  }
</style>
