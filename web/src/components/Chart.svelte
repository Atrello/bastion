<!-- A small line chart of recent values: the first series filled beneath, any others as dashed lines, an
     optional limit drawn across, and three labels up the side. Drawn as SVG, stretched to fit its box; the
     labels are text beside it, so they don't stretch. -->
<script lang="ts">
  type Series = { label: string; values: number[]; colour: string };

  let {
    series,
    max,
    limit,
    format,
    height = 160,
  }: {
    series: Series[];
    /** The top of the chart; by default a little above the highest value. */
    max?: number;
    /** A ceiling to draw across, like a memory limit. */
    limit?: { value: number; label: string };
    format: (n: number) => string;
    height?: number;
  } = $props();

  const W = 300;
  const H = 100;

  const top = $derived.by(() => {
    if (max) return max;
    const highest = Math.max(0, limit?.value ?? 0, ...series.flatMap((s) => s.values));
    return highest > 0 ? highest * 1.15 : 1;
  });

  function points(values: number[]): string {
    if (values.length < 2) return '';
    return values.map((v, i) => `${(i / (values.length - 1)) * W},${H - (Math.min(v, top) / top) * H}`).join(' ');
  }

  const first = $derived(series[0]);
  const area = $derived(first && first.values.length > 1 ? `0,${H} ${points(first.values)} ${W},${H}` : '');
</script>

<div class="chart" style:height="{height}px">
  <div class="axis small muted">
    <span>{format(top)}</span>
    <span>{format(top / 2)}</span>
    <span>{format(0)}</span>
  </div>
  <div class="plot">
    <svg viewBox="0 0 {W} {H}" preserveAspectRatio="none" role="img" aria-label={series.map((s) => s.label).join(', ')}>
      {#each [0, 0.5] as f (f)}
        <line class="grid" x1="0" x2={W} y1={H * f} y2={H * f} vector-effect="non-scaling-stroke" />
      {/each}
      {#if limit && limit.value <= top}
        <line class="limit" x1="0" x2={W} y1={H - (limit.value / top) * H} y2={H - (limit.value / top) * H} vector-effect="non-scaling-stroke" />
      {/if}
      {#if area}<polygon points={area} fill={first.colour} opacity="0.16" />{/if}
      {#each series as s, i (s.label)}
        {#if s.values.length > 1}
          <polyline points={points(s.values)} fill="none" stroke={s.colour} stroke-width="2" stroke-dasharray={i ? '5 4' : undefined} vector-effect="non-scaling-stroke" />
        {/if}
      {/each}
    </svg>
    {#if limit && limit.value <= top}
      <span class="limit-label small" style:top="{100 - (limit.value / top) * 100}%">{limit.label}</span>
    {/if}
    {#if !first || first.values.length < 2}<p class="waiting small muted">Gathering figures…</p>{/if}
  </div>
</div>

<style>
  .chart {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 8px;
  }
  .axis {
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    text-align: right;
    min-width: 52px;
    font-variant-numeric: tabular-nums;
  }
  .axis span {
    line-height: 1;
  }
  .plot {
    position: relative;
    min-width: 0;
  }
  svg {
    width: 100%;
    height: 100%;
    display: block;
    overflow: visible;
  }
  .grid {
    stroke: var(--line);
    stroke-width: 1;
  }
  .limit {
    stroke: var(--red);
    stroke-width: 1;
    stroke-dasharray: 4 4;
  }
  .limit-label {
    position: absolute;
    right: 4px;
    transform: translateY(2px);
    color: var(--red);
  }
  .waiting {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    margin: 0;
  }
</style>
