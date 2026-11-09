<!-- A dialog over the page, using the browser's own <dialog>: Esc and the close button shut it. -->
<script lang="ts">
  import type { Snippet } from 'svelte';

  let { title, onclose, children, footer }: { title: string; onclose: () => void; children: Snippet; footer?: Snippet } = $props();
  let dialog: HTMLDialogElement;

  $effect(() => {
    dialog.showModal();
  });
</script>

<dialog bind:this={dialog} onclose={onclose} aria-label={title}>
  <header class="row">
    <h2 class="grow">{title}</h2>
    <button class="close" aria-label="Close" onclick={() => dialog.close()}>×</button>
  </header>
  <div class="body">{@render children()}</div>
  {#if footer}<footer class="row">{@render footer()}</footer>{/if}
</dialog>

<style>
  dialog {
    width: min(560px, calc(100vw - 32px));
    padding: 0;
    color: var(--text);
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
  }
  dialog::backdrop {
    background: rgb(0 0 0 / 55%);
  }
  header,
  footer,
  .body {
    padding: 14px 18px;
  }
  header {
    border-bottom: 1px solid var(--line);
  }
  footer {
    justify-content: flex-end;
    border-top: 1px solid var(--line);
  }
  .close {
    border: 0;
    background: none;
    font-size: 20px;
    line-height: 1;
    padding: 0 4px;
  }
</style>
