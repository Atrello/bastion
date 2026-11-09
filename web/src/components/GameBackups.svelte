<!-- A game's backups, kept on the server it runs on: make one, lock it, restore it, download or delete it. -->
<script lang="ts">
  import { ask, sure } from '../lib/ask.svelte';
  import { api } from '../lib/api';
  import { ago, bytes } from '../lib/format';
  import { load } from '../lib/load.svelte';
  import type { GameBackup, GameServer } from '../lib/types';

  let { game, onerror, onnote }: { game: GameServer; onerror: (message: string) => void; onnote: (message: string) => void } = $props();

  const backups = load(() => api.gameBackups(game.id));
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
    if (game.state === 'running' || game.state === 'stopping') {
      onerror('Stop it before restoring a backup.');
      return;
    }
    if (await sure(`Restore "${b.name}"?`, "Everything in the game's files now is replaced by the backup's.", 'Restore', true)) {
      void attempt(() => api.restoreGameBackup(game.id, b.id), 'Restored. Start it when ready.');
    }
  }

  async function make() {
    const name = await ask({ title: 'Back up now', input: { label: 'Name it (optional)', placeholder: 'e.g. Before the update' }, confirm: 'Back up' });
    if (name !== null) void attempt(() => api.createGameBackup(game.id, name.trim()), 'Backing up: it shows here when done.');
  }
</script>

<div class="row top">
  <span class="muted small grow">Backups are kept on {game.serverName}, beside the game. A locked one can't be deleted.</span>
  <button class="primary" onclick={make}>Back up now</button>
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
            {#if b.state === 'making'}<span class="badge amber">Making…</span>{:else if b.state === 'failed'}<span class="badge red">Failed</span>{/if}
          </td>
          <td>{b.state === 'done' ? bytes(b.size) : ''}</td>
          <td class="muted">{ago(b.createdAt)}</td>
          <td class="buttons">
            {#if b.state === 'done'}
              <button onclick={() => restore(b)}>Restore</button>
              <a class="button" href={api.gameBackupDownload(game.id, b.id)}>Download</a>
              <button onclick={() => attempt(() => api.lockGameBackup(game.id, b.id, !b.locked))}>{b.locked ? 'Unlock' : 'Lock'}</button>
            {/if}
            {#if !b.locked && b.state !== 'making'}
              <button class="danger" onclick={async () => (await sure(`Delete "${b.name}"?`, "The backup's file is deleted from the server.")) && attempt(() => api.deleteGameBackup(game.id, b.id))}>Delete</button>
            {/if}
          </td>
        </tr>
      {:else}
        {#if backups.data}<tr><td colspan="4" class="empty">No backups yet.</td></tr>{/if}
      {/each}
    </tbody>
  </table>
</div>

<style>
  .top {
    margin-bottom: 10px;
  }
  .buttons {
    text-align: right;
  }
  .buttons > * {
    margin-left: 6px;
    padding: 3px 10px;
  }
</style>
