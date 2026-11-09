<!-- The menu down the left: the logo and each area of the panel. -->
<script lang="ts">
  import { route } from '../lib/router.svelte';

  const links = [
    { href: '/', label: 'Overview', icon: 'M3 3h7v7H3zM14 3h7v4h-7zM14 11h7v10h-7zM3 14h7v7H3z' },
    { href: '/servers', label: 'Servers', icon: 'M3 4h18v6H3zM3 14h18v6H3zM7 7h.01M7 17h.01' },
    { href: '/stacks', label: 'Stacks', icon: 'M12 3 3 8l9 5 9-5-9-5zM3 13l9 5 9-5M3 18l9 5 9-5' },
    { href: '/containers', label: 'Containers', icon: 'M4 6h16v12H4zM8 6v12M12 6v12M16 6v12' },
  ];
  const current = (href: string) => (href === '/' ? route.path === '/' : route.path.startsWith(href));
</script>

<nav>
  <a class="logo" href="/">
    <img src="/favicon.svg" alt="" width="26" height="26" />
    Bastion
  </a>
  {#each links as link (link.href)}
    <a class="link" class:on={current(link.href)} href={link.href} aria-current={current(link.href) ? 'page' : undefined}>
      <svg viewBox="0 0 24 24" aria-hidden="true"><path d={link.icon} /></svg>
      {link.label}
    </a>
  {/each}
  <p class="version muted small">Bastion 0.1</p>
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
  .logo {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 4px 8px 18px;
    color: var(--text);
    font-weight: 700;
    font-size: 17px;
  }
  .link {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 10px;
    border-radius: 7px;
    color: #c4c9d3;
  }
  .link:hover {
    background: var(--raised);
    text-decoration: none;
  }
  .link.on {
    background: #22283a;
    color: #fff;
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
  .version {
    margin: auto 0 0;
    padding: 0 10px;
  }
</style>
