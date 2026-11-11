<!-- One figure about a game server beside its console: an icon, what it is, its value (and what it's out
     of), an optional note on the right, and a bar along the bottom when it has a limit. -->
<script lang="ts">
  let {
    icon,
    label,
    value,
    of = '',
    note = '',
    detail = '',
    percent,
    tone = 'accent',
    mono = false,
  }: {
    /** An SVG path, drawn in a 24 by 24 box. */
    icon: string;
    label: string;
    value: string;
    /** What it's out of: "/ 8 GiB". */
    of?: string;
    note?: string;
    /** A small line under the value, wrapping: players' names, say. */
    detail?: string;
    /** 0 to 100, for the bar along the bottom. */
    percent?: number;
    tone?: 'accent' | 'blue' | 'amber' | 'red';
    /** For an address: in the fixed-width font, smaller, and wrapping rather than cut short. */
    mono?: boolean;
  } = $props();

  const barTone = $derived(percent !== undefined && percent >= 90 ? 'red' : percent !== undefined && percent >= 75 ? 'amber' : tone);
</script>

<div class="stat card">
  <span class="icon {tone}" aria-hidden="true">
    <svg viewBox="0 0 24 24"><path d={icon} /></svg>
  </span>
  <div class="text">
    <span class="label small muted">{label}</span>
    <span class="value" class:mono>{value}{#if of}<span class="of muted">{' ' + of}</span>{/if}</span>
    {#if detail}<span class="detail small muted">{detail}</span>{/if}
  </div>
  {#if note}<span class="note small muted">{note}</span>{/if}
  {#if percent !== undefined}
    <span class="bar" role="meter" aria-label={label} aria-valuenow={Math.round(percent)} aria-valuemin="0" aria-valuemax="100">
      <span class={barTone} style:width="{Math.min(100, Math.max(0, percent))}%"></span>
    </span>
  {/if}
</div>

<style>
  .stat {
    position: relative;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 14px 14px 16px;
    overflow: hidden;
    min-width: 0;
  }
  .icon {
    flex: none;
    width: 40px;
    height: 40px;
    border-radius: 10px;
    display: grid;
    place-items: center;
    background: color-mix(in srgb, var(--accent) 18%, transparent);
    color: var(--accent);
  }
  .icon.blue {
    background: color-mix(in srgb, var(--blue) 18%, transparent);
    color: var(--blue);
  }
  .icon.amber {
    background: color-mix(in srgb, var(--amber) 18%, transparent);
    color: var(--amber);
  }
  .icon svg {
    width: 22px;
    height: 22px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.8;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .text {
    display: grid;
    gap: 2px;
    min-width: 0;
  }
  .value {
    font-size: 18px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .value.mono {
    font-family: var(--mono);
    font-size: 14px;
    white-space: normal;
    overflow-wrap: anywhere;
  }
  .detail {
    overflow-wrap: anywhere;
  }
  .of {
    font-size: 13px;
    font-weight: 400;
  }
  .note {
    margin-left: auto;
    align-self: flex-start;
    text-align: right;
    white-space: nowrap;
  }
  .bar {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    height: 4px;
    background: var(--line);
  }
  .bar span {
    display: block;
    height: 100%;
    background: var(--accent);
    transition: width 0.4s;
  }
  .bar .blue {
    background: var(--blue);
  }
  .bar .amber {
    background: var(--amber);
  }
  .bar .red {
    background: var(--red);
  }
</style>
