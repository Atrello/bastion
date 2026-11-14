<!-- A stack's backups: its folder (compose file, .env, anything beside them) and the volumes it made, in one
     archive. Make one now, or on a schedule keeping the newest few; restore, download, lock or delete. -->
<script lang="ts">
  import BackupPlace from './BackupPlace.svelte';
  import Modal from './Modal.svelte';
  import { sure } from '../lib/ask.svelte';
  import { api } from '../lib/api';
  import { ago, bytes } from '../lib/format';
  import { load } from '../lib/load.svelte';
  import { followJob } from '../lib/tasks.svelte';
  import type { StackBackup } from '../lib/types';

  let { serverId, name, onerror, onnote }: { serverId: number; name: string; onerror: (m: string) => void; onnote: (m: string) => void } = $props();

  const backups = load(() => api.stackBackups(serverId, name));
  const stores = load(api.backupStores);
  // A backup being made shows as such until it's done.
  $effect(() => {
    if (!backups.data?.backups.some((b) => b.state === 'making')) return;
    const timer = setInterval(() => void backups.reload(), 3000);
    return () => clearInterval(timer);
  });

  const PRESETS: [string, string][] = [
    ['', 'Not on a schedule'],
    ['0 4 * * *', 'Every day at 4am'],
    ['0 4 * * 1', 'Mondays at 4am'],
    ['0 */6 * * *', 'Every 6 hours'],
  ];
  let settings = $state({ when: '', keep: 7, stop: true, storeId: 0 });
  let custom = $state(false);
  $effect(() => {
    const s = backups.data?.settings;
    if (s) {
      settings = { when: s.when, keep: s.keep, stop: s.stop, storeId: s.storeId };
      custom = !PRESETS.some(([cron]) => cron === s.when);
    }
  });

  async function attempt(work: () => Promise<unknown>, note?: string) {
    try {
      await work();
      if (note) onnote(note);
    } catch (err) {
      onerror((err as Error).message);
    }
    await backups.reload();
  }

  let making = $state<{ name: string; storeId: number | null }>();

  function make(e: SubmitEvent) {
    e.preventDefault();
    if (!making) return;
    const { name: label, storeId } = making;
    making = undefined;
    void attempt(() => api.makeStackBackup(serverId, name, label.trim(), storeId), 'Backing up: it shows here when done.');
  }

  async function restore(b: StackBackup) {
    if (
      await sure(
        `Restore "${b.name}"?`,
        `The stack is stopped, its folder and volumes are replaced by the backup's (anything made since is lost), and it's started again if it was running.`,
        'Restore',
        true,
      )
    ) {
      try {
        followJob(await api.restoreStackBackup(serverId, name, b.id));
      } catch (err) {
        onerror((err as Error).message);
      }
    }
  }

  const where = (id: number) => (id ? (stores.data?.find((s) => s.id === id)?.name ?? 'backup storage') : 'the server');
</script>

<div class="grid">
  <section class="card">
    <h2>Backed up</h2>
    <form
      class="form"
      onsubmit={(e) => {
        e.preventDefault();
        void attempt(() => api.saveStackBackupSettings(serverId, name, settings), 'Saved.');
      }}
    >
      <label>
        When
        <select
          value={custom ? 'custom' : settings.when}
          onchange={(e) => {
            const v = e.currentTarget.value;
            custom = v === 'custom';
            if (!custom) settings.when = v;
          }}
        >
          {#each PRESETS as [cron, label] (cron)}<option value={cron}>{label}</option>{/each}
          <option value="custom">Another time (cron)</option>
        </select>
      </label>
      {#if custom}
        <label>Cron <span class="muted small">(minute hour day month weekday)</span> <input class="mono" bind:value={settings.when} /></label>
      {/if}
      <div class="pair">
        <label>Keep the newest <input type="number" min="0" max="1000" bind:value={settings.keep} /></label>
        <label>
          Kept on
          <select bind:value={settings.storeId}>
            <option value={0}>The server</option>
            {#each stores.data ?? [] as s (s.id)}<option value={s.id}>{s.name} (S3)</option>{/each}
          </select>
        </label>
      </div>
      <label class="check"><input type="checkbox" bind:checked={settings.stop} /> Stop it while it's backed up, so databases are copied whole</label>
      <p class="muted small">
        Keeping 0 keeps them all; locked ones are always kept.
        {#if backups.data?.settings.nextRun}Next: {new Date(backups.data.settings.nextRun).toLocaleString('en-GB', { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })}.{/if}
        {#if backups.data?.settings.lastError}<span class="error">Last time: {backups.data.settings.lastError}</span>{/if}
      </p>
      <div class="row end"><button type="submit">Save</button></div>
    </form>
  </section>
  <section class="card about">
    <h2>What's in one</h2>
    <p class="muted small">
      The stack's folder (its compose file, .env, and anything kept beside them, like a data folder) and every
      volume Docker made for it, with their owners and permissions. Volumes it shares with other stacks, and
      folders elsewhere on the server, aren't included. New backups go to {where(settings.storeId)}.
    </p>
    <button class="primary" onclick={() => (making = { name: '', storeId: null })}>Back up now</button>
  </section>
</div>

<div class="card table-card">
  <table>
    <thead><tr><th>Backup</th><th>Size</th><th>Made</th><th></th></tr></thead>
    <tbody>
      {#each backups.data?.backups ?? [] as b (b.id)}
        <tr>
          <td>
            <b>{b.name}</b>
            {#if b.locked}<span class="badge blue">Locked</span>{/if}
            {#if b.store}<span class="badge">{b.store}</span>{/if}
            {#if b.state === 'making'}<span class="badge amber">Making…</span>{:else if b.state === 'failed'}<span class="badge red" title={b.error}>Failed</span>{/if}
            {#if b.state === 'failed' && b.error}<div class="small error">{b.error}</div>{/if}
          </td>
          <td class="nowrap">{b.state === 'done' ? bytes(b.size) : ''}</td>
          <td class="muted nowrap">{ago(b.createdAt)}</td>
          <td class="buttons">
            {#if b.state === 'done'}
              <button onclick={() => restore(b)}>Restore</button>
              <a class="button" href={api.stackBackupDownload(serverId, name, b.id)}>Download</a>
              <button onclick={() => attempt(() => api.lockStackBackup(serverId, name, b.id, !b.locked))}>{b.locked ? 'Unlock' : 'Lock'}</button>
            {/if}
            {#if !b.locked && b.state !== 'making'}
              <button
                class="danger"
                onclick={async () =>
                  (await sure(`Delete "${b.name}"?`, b.store ? `The backup is deleted from ${b.store}.` : "The backup's file is deleted from the server.")) &&
                  attempt(() => api.deleteStackBackup(serverId, name, b.id))}>Delete</button
              >
            {/if}
          </td>
        </tr>
      {:else}
        {#if backups.data}<tr><td colspan="4" class="empty">No backups yet.</td></tr>{/if}
      {/each}
    </tbody>
  </table>
</div>

{#if making}
  <Modal title="Back up now" onclose={() => (making = undefined)}>
    <form id="make-stack-backup" class="form" onsubmit={make}>
      <label>Name it <span class="muted small">(optional)</span> <input maxlength="60" placeholder="e.g. Before the upgrade" bind:value={making.name} /></label>
      <BackupPlace bind:value={making.storeId} stores={stores.data ?? []} server="The server" usual={where(backups.data?.settings.storeId ?? 0)} />
    </form>
    {#snippet footer()}
      <button onclick={() => (making = undefined)}>Cancel</button>
      <button type="submit" form="make-stack-backup" class="primary">Back up</button>
    {/snippet}
  </Modal>
{/if}

<style>
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 340px), 1fr));
    gap: 16px;
    margin-bottom: 16px;
  }
  .grid h2 {
    margin: 0 0 12px;
  }
  .form {
    display: grid;
    gap: 12px;
  }
  .form label:not(.check) {
    display: grid;
    gap: 6px;
  }
  .form p {
    margin: 0;
  }
  .check {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  .about {
    display: grid;
    align-content: start;
    gap: 12px;
  }
  .about p {
    margin: 0;
  }
  .about button {
    justify-self: start;
  }
  .nowrap {
    white-space: nowrap;
  }
  .buttons {
    text-align: right;
    white-space: nowrap;
  }
  .buttons > * {
    margin-left: 6px;
    padding: 3px 10px;
  }
</style>
