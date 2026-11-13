<!-- A game's schedules: a restart (players warned a minute before), a console command, or a backup, at
     times written as cron: minute, hour, day of the month, month, day of the week. -->
<script lang="ts">
  import Modal from './Modal.svelte';
  import { sure } from '../lib/ask.svelte';
  import { api } from '../lib/api';
  import { ago } from '../lib/format';
  import { load } from '../lib/load.svelte';
  import type { Schedule } from '../lib/types';

  let { id, onerror, onnote }: { id: number; onerror: (message: string) => void; onnote: (message: string) => void } = $props();

  const schedules = load(() => api.gameSchedules(id));
  const DOES = { restart: 'Restart', command: 'Run a command', backup: 'Back up' };
  // Common times, so cron needn't be learnt for them.
  const PRESETS: [string, string][] = [
    ['0 4 * * *', 'Every day at 4am'],
    ['0 */6 * * *', 'Every 6 hours'],
    ['0 * * * *', 'Every hour'],
    ['0 4 * * 1', 'Mondays at 4am'],
  ];

  let editing = $state<{ id?: number; name: string; when: string; action: Schedule['action']; payload: string; enabled: boolean; keep: number }>();
  let busy = $state(false);

  async function attempt(work: () => Promise<unknown>, note?: string) {
    try {
      await work();
      if (note) onnote(note);
      await schedules.reload();
      return true;
    } catch (err) {
      onerror((err as Error).message);
      return false;
    }
  }

  async function save(e: SubmitEvent) {
    e.preventDefault();
    if (!editing) return;
    busy = true;
    const { id: scheduleId, ...s } = editing;
    if (await attempt(() => api.saveSchedule(id, s, scheduleId), 'Saved.')) editing = undefined;
    busy = false;
  }
</script>

<div class="row top">
  <span class="muted small grow">Times are the panel's own clock. A restart warns players a minute before, if the game can be told.</span>
  <button class="primary" onclick={() => (editing = { name: '', when: '0 4 * * *', action: 'restart', payload: '', enabled: true, keep: 7 })}>New schedule</button>
</div>
<div class="card table-card">
  <table>
    <thead><tr><th>Schedule</th><th>When</th><th>Does</th><th>Next</th><th>Last</th><th></th></tr></thead>
    <tbody>
      {#each schedules.data ?? [] as s (s.id)}
        <tr class:off={!s.enabled}>
          <td><b>{s.name}</b></td>
          <td class="mono">{PRESETS.find(([c]) => c === s.when)?.[1] ?? s.when}</td>
          <td>{DOES[s.action]}{s.action === 'command' ? `: ${s.payload}` : ''}</td>
          <td>{s.enabled && s.nextRun ? new Date(s.nextRun).toLocaleString('en-GB', { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : 'Off'}</td>
          <td>
            {#if s.lastError}<span class="badge red" title={s.lastError}>Failed</span>
            {:else if s.lastRunAt}<span class="muted">{ago(s.lastRunAt)}</span>{/if}
          </td>
          <td class="buttons">
            <button onclick={() => attempt(() => api.runSchedule(id, s.id), 'Done.')}>Run now</button>
            <button onclick={() => (editing = { id: s.id, name: s.name, when: s.when, action: s.action, payload: s.payload, enabled: s.enabled, keep: s.keep })}>Edit</button>
            <button class="danger" onclick={async () => (await sure(`Delete ${s.name}?`, 'It stops running.')) && attempt(() => api.deleteSchedule(id, s.id))}>Delete</button>
          </td>
        </tr>
      {:else}
        {#if schedules.data}<tr><td colspan="6" class="empty">No schedules. A nightly restart and backup is a good start.</td></tr>{/if}
      {/each}
    </tbody>
  </table>
</div>

{#if editing}
  {@const s = editing}
  <Modal title={s.id ? 'Edit schedule' : 'New schedule'} onclose={() => (editing = undefined)}>
    <form id="schedule" class="form" onsubmit={save}>
      <label>Name <input required maxlength="60" placeholder="e.g. Nightly restart" bind:value={s.name} /></label>
      <label>
        Does
        <select bind:value={s.action}>
          {#each Object.entries(DOES) as [value, label] (value)}<option {value}>{label}</option>{/each}
        </select>
      </label>
      {#if s.action === 'command'}
        <label>Command <input required class="mono" placeholder="say Daily reminder: be nice" bind:value={s.payload} /></label>
      {/if}
      {#if s.action === 'backup'}
        <label>Keep the newest <span class="muted small">(0 keeps all; locked ones are always kept)</span> <input type="number" min="0" max="1000" bind:value={s.keep} /></label>
      {/if}
      <label>
        When <span class="muted small">(minute hour day month weekday)</span>
        <input required class="mono" bind:value={s.when} />
      </label>
      <div class="row presets">
        {#each PRESETS as [cron, label] (cron)}<button type="button" class:on={s.when === cron} onclick={() => (s.when = cron)}>{label}</button>{/each}
      </div>
      <label class="check"><input type="checkbox" bind:checked={s.enabled} /> On</label>
    </form>
    {#snippet footer()}
      <button onclick={() => (editing = undefined)}>Cancel</button>
      <button type="submit" form="schedule" class="primary" disabled={busy}>Save</button>
    {/snippet}
  </Modal>
{/if}

<style>
  .top {
    margin-bottom: 10px;
  }
  .off td {
    color: var(--muted);
  }
  .buttons {
    text-align: right;
  }
  .buttons button {
    margin-left: 6px;
    padding: 3px 10px;
  }
  .presets {
    flex-wrap: wrap;
    gap: 6px;
  }
  .presets button {
    padding: 3px 10px;
    font-weight: 400;
  }
  .presets button.on {
    border-color: var(--accent);
  }
</style>
