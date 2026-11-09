<!-- A row of tabs. The page shows what goes with the selected one. -->
<script lang="ts">
  let { tabs, selected = $bindable() }: { tabs: { id: string; label: string }[]; selected: string } = $props();
</script>

<div class="tabs" role="tablist">
  {#each tabs as tab (tab.id)}
    <button role="tab" aria-selected={selected === tab.id} class:on={selected === tab.id} onclick={() => (selected = tab.id)}>
      {tab.label}
    </button>
  {/each}
</div>

<style>
  .tabs {
    display: flex;
    gap: 4px;
    border-bottom: 1px solid var(--line);
    margin-bottom: 16px;
  }
  button {
    background: none;
    border: 0;
    border-bottom: 2px solid transparent;
    border-radius: 0;
    padding: 8px 12px;
    color: var(--muted);
  }
  button.on {
    color: var(--text);
    border-bottom-color: var(--accent);
  }
</style>
