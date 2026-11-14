<!-- A game's backups, kept on the server it runs on or in backup storage: make one (the owner can choose
     where), lock it, restore it, download or delete it. -->
<script lang="ts">
  import BackupPlace from './BackupPlace.svelte';
  import Modal from './Modal.svelte';
  import { sure } from '../lib/ask.svelte';
  import { api } from '../lib/api';
  import { ago, bytes } from '../lib/format';
  import { load } from '../lib/load.svelte';
  import type { GameBackup, GameServer } from '../lib/types';

  let { game, onerror, onnote }: { game: GameServer; onerror: (message: string) => void; onnote: (message: string) => void } = $props();

  const backups = load(() => api.gameBackups(game.id));
  // Only the owner sees backup storage, so only they choose where one goes.
  const stores = load(() => (game.owner ? api.backupStores() : Promise.resolve([])));
  const usual = $derived(game.backupStore ? (stores.data?.find((s) => s.id === game.backupStore)?.name ?? 'backup storage') : game.serverName);
  // A backup being made shows as such until it's done.
  $effect(() => {
    if (!backups.data?.some((b) => b.state === 'making')) return;
    const timer = setInterval(() => void backups.reload(), 3000);
    return () => clearInterval(timer);
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

  async function restore(b: GameBackup) {
    if (game.state === 'running' || game.state === 'starting' || game.state === 'stopping') {
      onerror('Stop it before restoring a backup.');
      return;
    }
    if (await sure(`Restore "${b.name}"?`, "Everything in the game's files now is replaced by the backup's.", 'Restore', true)) {
      void attempt(() => api.restoreGameBackup(game.id, b.id), 'Restored. Start it when ready.');
    }
  }

  let making = $state<{ name: string; storeId: number | null }>();

  function make(e: SubmitEvent) {
    e.preventDefault();
    if (!making) return;
    const { name, storeId } = making;
    making = undefined;
    void attempt(() => api.createGameBackup(game.id, name.trim(), storeId), 'Backing up: it shows here when done.');
  }
</script>

<div class="row top">
  <span class="muted small grow">
    {game.backupStore ? 'New backups go to backup storage (set in Settings).' : `New backups are kept on ${game.serverName}, beside the game.`}
    A locked one can't be deleted.
  </span>
  <button class="primary" onclick={() => (making = { name: '', storeId: null })}>Back up now</button>
</div>
<div class="card table-card">
  <table>
    <thead><tr><th>Backup</th><th>Size</th><th>Made</th><th></th></tr></thead>
    <tbody>
      {#each backups.data ?? [] as b (b.id)}
        <tr>
          <td>
            <b>{b.name}</b>
            {#if b.locked}<span class="badge blue">Locked</span>{/if}
            {#if b.store}<span class="badge">{b.store}</span>{/if}
            {#if b.state === 'making'}<span class="badge amber">Making…</span>{:else if b.state === 'failed'}<span class="badge red">Failed</span>{/if}
          </td>
          <td class="nowrap">{b.state === 'done' ? bytes(b.size) : ''}</td>
          <td class="muted nowrap">{ago(b.createdAt)}</td>
          <td class="buttons">
            {#if b.state === 'done'}
              <button onclick={() => restore(b)}>Restore</button>
              <a class="button" href={api.gameBackupDownload(game.id, b.id)}>Download</a>
              <button onclick={() => attempt(() => api.lockGameBackup(game.id, b.id, !b.locked))}>{b.locked ? 'Unlock' : 'Lock'}</button>
              {#if b.links && game.owner}
                <button title="Download links to it were sent in a notification" onclick={async () => (await sure('Stop its download links?', `The ${b.links === 1 ? 'link' : `${b.links} links`} sent in notifications stop working now.`, 'Stop links')) && attempt(() => api.stopBackupLinks(game.id, b.id), 'Its links have stopped.')}>Stop links</button>
              {/if}
            {/if}
            {#if !b.locked && b.state !== 'making'}
              <button class="danger" onclick={async () => (await sure(`Delete "${b.name}"?`, b.store ? `The backup's file is deleted from ${b.store}.` : "The backup's file is deleted from the server.")) && attempt(() => api.deleteGameBackup(game.id, b.id))}>Delete</button>
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
    <form id="make-backup" class="form" onsubmit={make}>
      <label>Name it <span class="muted small">(optional)</span> <input maxlength="60" placeholder="e.g. Before the update" bind:value={making.name} /></label>
      {#if game.owner}
        <BackupPlace bind:value={making.storeId} stores={stores.data ?? []} server={`On ${game.serverName}`} {usual} />
      {/if}
    </form>
    {#snippet footer()}
      <button onclick={() => (making = undefined)}>Cancel</button>
      <button type="submit" form="make-backup" class="primary">Back up</button>
    {/snippet}
  </Modal>
{/if}

<style>
  .top {
    margin-bottom: 10px;
  }
  .buttons {
    text-align: right;
  }
  .nowrap {
    white-space: nowrap;
  }
  .buttons > * {
    margin-left: 6px;
    padding: 3px 10px;
  }
</style>
