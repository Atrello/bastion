<!-- A coloured dot for how something is: green running, amber partly, red failed, grey off. -->
<script lang="ts">
  let { tone, label }: { tone: 'green' | 'amber' | 'red' | 'grey'; label?: string } = $props();
</script>

<span class="dot {tone}" title={label} aria-label={label}></span>

<style>
  .dot {
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex: none;
  }
  .green {
    background: var(--green);
  }
  .amber {
    background: var(--amber);
  }
  .red {
    background: var(--red);
  }
  .grey {
    background: var(--grey);
  }
</style>
