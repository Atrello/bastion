<!-- The centred card the setup and sign-in screens sit in, with the logo above. -->
<script lang="ts">
  import { asset } from '../lib/router.svelte';
  import type { Snippet } from 'svelte';

  let { title, children }: { title: string; children: Snippet } = $props();
</script>

<main class="auth">
  <div class="logo"><img src={asset('favicon.svg')} alt="" width="30" height="30" /> Bastion</div>
  <section class="card">
    <h1>{title}</h1>
    {@render children()}
  </section>
</main>

<style>
  .auth {
    min-height: 100vh;
    display: grid;
    place-content: center;
    gap: 18px;
    padding: 24px;
  }
  .logo {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    font-weight: 700;
    font-size: 19px;
    /* The wordmark: spaced capitals, like a name cut in stone. */
    text-transform: uppercase;
    letter-spacing: 0.14em;
  }
  .card {
    width: min(400px, calc(100vw - 48px));
    display: grid;
    gap: 14px;
    padding: 24px;
  }
  h1 {
    font-size: 18px;
  }
  .card :global(form) {
    display: grid;
    gap: 12px;
  }
  .card :global(label) {
    display: grid;
    gap: 6px;
  }
  .card :global(p) {
    margin: 0;
  }
</style>
