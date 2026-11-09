<!-- Backup storage: S3 buckets (AWS, or a service that speaks S3: Backblaze B2, Cloudflare R2, Wasabi,
     MinIO...) that game servers can keep their backups in, rather than on their own server. Each game
     chooses in its Settings. Saving checks Bastion can write there. The secret key is never shown again. -->
<script lang="ts">
  import Modal from '../components/Modal.svelte';
  import { sure } from '../lib/ask.svelte';
  import { api } from '../lib/api';
  import { load } from '../lib/load.svelte';
  import type { BackupStore } from '../lib/types';

  const stores = load(api.backupStores);
  let error = $state<string>();
  let editing = $state<(Omit<BackupStore, 'id' | 'backups'> & { id?: number; backups: number }) | undefined>();
  let saving = $state(false);
  let formError = $state<string>();

  const blank = () => ({ name: '', endpoint: '', region: '', bucket: '', prefix: 'bastion/', accessKey: '', secretKey: '', pathStyle: false, plainHttp: false, backups: 0 });

  async function save(e: SubmitEvent) {
    e.preventDefault();
    if (!editing) return;
    saving = true;
    formError = undefined;
    const { id, backups: _, ...store } = editing;
    try {
      await api.saveBackupStore(store, id);
      editing = undefined;
      await stores.reload();
    } catch (err) {
      formError = (err as Error).message;
    } finally {
      saving = false;
    }
  }

  async function remove(s: BackupStore) {
    if (!(await sure(`Remove ${s.name}?`, "Bastion forgets it. Nothing in the bucket is touched. Games keeping their backups there go back to keeping them on their own server."))) return;
    error = undefined;
    try {
      await api.deleteBackupStore(s.id);
      await stores.reload();
    } catch (err) {
      error = (err as Error).message;
    }
  }
</script>

<div class="page-head">
  <h1>Backup storage</h1>
  <button class="primary" onclick={() => ((editing = blank()), (formError = undefined))}>Add storage</button>
</div>
<p class="muted intro">
  Game servers keep their backups on their own server unless their Settings say to keep them here instead. A backup
  is made on the server as usual, then uploaded, and the copy on the server removed.
</p>
{#if error}<p class="error">{error}</p>{/if}

<div class="card table-card">
  <table>
    <thead><tr><th>Name</th><th>Where</th><th>Backups</th><th></th></tr></thead>
    <tbody>
      {#each stores.data ?? [] as s (s.id)}
        <tr>
          <td><b>{s.name}</b></td>
          <td class="mono muted where">{s.plainHttp ? 'http://' : ''}{s.endpoint}/{s.bucket}/{s.prefix}</td>
          <td>{s.backups}</td>
          <td class="buttons">
            <button onclick={() => ((editing = { ...s, secretKey: '' }), (formError = undefined))}>Edit</button>
            <button class="danger" onclick={() => remove(s)}>Remove</button>
          </td>
        </tr>
      {:else}
        {#if stores.data}<tr><td colspan="4" class="empty">None yet: backups stay on each game's own server.</td></tr>{/if}
      {/each}
    </tbody>
  </table>
</div>

{#if editing}
  {@const s = editing}
  <Modal title={s.id ? `Edit ${s.name}` : 'Add backup storage'} onclose={() => (editing = undefined)}>
    <form id="store" class="form" onsubmit={save}>
      <label>Name <input required maxlength="60" placeholder="e.g. Backblaze" bind:value={s.name} /></label>
      <div class="pair">
        <label>
          Address
          <input required class="mono" placeholder="s3.eu-west-2.amazonaws.com" bind:value={s.endpoint} disabled={!!s.id && s.backups > 0} />
        </label>
        <label>Region <input class="mono" placeholder="eu-west-2 (if it asks)" bind:value={s.region} /></label>
      </div>
      <div class="pair">
        <label>Bucket <input required class="mono" bind:value={s.bucket} disabled={!!s.id && s.backups > 0} /></label>
        <label>Folder in it <input class="mono" placeholder="bastion/" bind:value={s.prefix} disabled={!!s.id && s.backups > 0} /></label>
      </div>
      {#if s.id && s.backups > 0}
        <p class="muted small">It has backups in it, so where it is can't change: add new storage for somewhere else.</p>
      {/if}
      <div class="pair">
        <label>Access key <input required class="mono" autocomplete="off" bind:value={s.accessKey} /></label>
        <label>
          Secret key
          <input
            type="password"
            class="mono"
            autocomplete="new-password"
            required={!s.id}
            placeholder={s.id ? 'Leave empty to keep it' : ''}
            bind:value={s.secretKey}
          />
        </label>
      </div>
      <label class="check"><input type="checkbox" bind:checked={s.pathStyle} /> The bucket goes in the path (MinIO and some others need this)</label>
      <label class="check"><input type="checkbox" bind:checked={s.plainHttp} /> Plain http, not https (only for storage on your own network)</label>
      <p class="muted small">
        Give Bastion keys of its own that can only reach this bucket. Saving checks they work by writing a small file
        and deleting it. Backups are kept in the folder you give, under game-&lt;number&gt;/.
      </p>
      {#if formError}<p class="error">{formError}</p>{/if}
    </form>
    {#snippet footer()}
      <button onclick={() => (editing = undefined)}>Cancel</button>
      <button type="submit" form="store" class="primary" disabled={saving}>{saving ? 'Checking…' : 'Save'}</button>
    {/snippet}
  </Modal>
{/if}

<style>
  .intro {
    margin: -8px 0 16px;
    max-width: 70ch;
  }
  .where {
    overflow-wrap: anywhere;
  }
  .buttons {
    text-align: right;
    white-space: nowrap;
  }
  .buttons > * {
    margin-left: 6px;
  }
  .form {
    display: grid;
    gap: 12px;
  }
  .form label:not(.check) {
    display: grid;
    gap: 6px;
  }
  .check {
    display: flex;
    gap: 8px;
    align-items: center;
  }
</style>
