<!-- One line of a server's health: a label, the figures, and a bar that turns amber past 80%. -->
<script lang="ts">
  let { label, value, percent }: { label: string; value: string; percent: number } = $props();
</script>

<div class="meter">
  <div class="row small"><span class="muted grow">{label}</span><span>{value}</span></div>
  <div class="bar" role="meter" aria-label={label} aria-valuenow={percent} aria-valuemin={0} aria-valuemax={100}>
    <span class:high={percent >= 80} style:width="{percent}%"></span>
  </div>
</div>

<style>
  .bar {
    height: 5px;
    margin: 4px 0 8px;
    background: #252a35;
    border-radius: 3px;
    overflow: hidden;
  }
  .bar span {
    display: block;
    height: 100%;
    background: var(--accent);
    border-radius: 3px;
  }
  .bar span.high {
    background: var(--amber);
  }
</style>
