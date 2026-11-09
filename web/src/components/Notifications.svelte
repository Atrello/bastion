<!-- Custom notifications, on the Alerts page: when something happens to a game server (a backup made or
     failed, the game starting, stopping or crashing), for any game or one, send a message in your own
     words to a channel. A backup made can carry a download link that works for a few days. -->
<script lang="ts">
  import Modal from './Modal.svelte';
  import { sure } from '../lib/ask.svelte';
  import { api } from '../lib/api';
  import { load } from '../lib/load.svelte';
  import type { NotificationRule } from '../lib/types';

  let { channels, onnote }: { channels: { id: number; name: string; kind: string }[]; onnote: (ok: boolean, text: string) => void } = $props();

  const rules = load(api.notifications);
  const games = load(api.games);

  // When, with the words a new one starts with.
  const EVENTS: Record<NotificationRule['event'], { label: string; title: string; message: string }> = {
    backup_done: { label: 'A backup is made', title: '{game} backed up', message: 'Backup "{backup}" of {game} is done ({size}).\nDownload it: {link}' },
    backup_failed: { label: 'A backup fails', title: '{game}: backup failed', message: 'Backup "{backup}" of {game} on {server} failed: {error}' },
    game_started: { label: 'A game server starts', title: '{game} is up', message: '{game} on {server} has started. Come and play!' },
    game_stopped: { label: 'A game server stops', title: '{game} stopped', message: '{game} on {server} has stopped.' },
    game_crashed: { label: 'A game server crashes', title: '{game} crashed', message: '{game} on {server} stopped by itself at {time}.' },
  };
  const FILLS: Record<NotificationRule['event'], string> = {
    backup_done: '{game} {server} {backup} {size} {time} {link}',
    backup_failed: '{game} {server} {backup} {error} {time}',
    game_started: '{game} {server} {time}',
    game_stopped: '{game} {server} {time}',
    game_crashed: '{game} {server} {time}',
  };

  let editing = $state<(Omit<NotificationRule, 'id'> & { id?: number }) | undefined>();
  let withLink = $state(false);
  let error = $state<string>();

  function start(n?: NotificationRule) {
    error = undefined;
    const e = EVENTS.backup_done;
    editing = n ? { ...n } : { name: '', event: 'backup_done', gameId: 0, channelId: channels[0]?.id ?? 0, title: e.title, message: e.message, linkDays: 7, enabled: true };
    withLink = (editing.linkDays ?? 0) > 0;
  }

  // Changing when it's sent starts its words again, unless they've been written already.
  function changeEvent(event: NotificationRule['event']) {
    if (!editing) return;
    const was = EVENTS[editing.event];
    if (editing.title === was.title && editing.message === was.message) {
      editing.title = EVENTS[event].title;
      editing.message = EVENTS[event].message;
    }
    editing.event = event;
    if (event !== 'backup_done') withLink = false;
  }

  async function save(e: SubmitEvent) {
    e.preventDefault();
    if (!editing) return;
    const { id, ...n } = editing;
    n.linkDays = withLink && n.event === 'backup_done' ? Number(n.linkDays) || 7 : 0;
    try {
      await api.saveNotification(n, id);
      editing = undefined;
      await rules.reload();
    } catch (err) {
      error = (err as Error).message;
    }
  }

  async function act(work: () => Promise<unknown>, done?: string) {
    try {
      await work();
      if (done) onnote(true, done);
      await rules.reload();
    } catch (err) {
      onnote(false, (err as Error).message);
    }
  }

  const gameName = (id: number) => (id ? (games.data?.find((g) => g.id === id)?.name ?? 'A deleted game') : 'Any game server');
  const channelName = (id: number) => channels.find((c) => c.id === id)?.name ?? '?';
</script>

<div class="row head">
  <h2 class="grow">Custom notifications</h2>
  <button onclick={() => start()} disabled={!channels.length} title={channels.length ? undefined : 'Add a channel first'}>New notification</button>
</div>
<div class="card table-card">
  <table>
    <thead><tr><th>Notification</th><th>When</th><th>For</th><th>Sent to</th><th></th></tr></thead>
    <tbody>
      {#each rules.data ?? [] as n (n.id)}
        <tr class:off={!n.enabled}>
          <td>
            <b>{n.name}</b>
            {#if !n.enabled}<span class="badge">Off</span>{/if}
            {#if n.linkDays}<span class="badge blue">Download link, {n.linkDays} day{n.linkDays === 1 ? '' : 's'}</span>{/if}
          </td>
          <td>{EVENTS[n.event].label}</td>
          <td>{gameName(n.gameId)}</td>
          <td>{channelName(n.channelId)}</td>
          <td class="buttons">
            <button onclick={() => act(() => api.testNotification(n.id), `Sent a test of "${n.name}", with made-up details.`)}>Test</button>
            <button onclick={() => start(n)}>Edit</button>
            <button class="danger" onclick={async () => (await sure(`Delete "${n.name}"?`, 'It stops being sent.')) && act(() => api.deleteNotification(n.id))}>Delete</button>
          </td>
        </tr>
      {:else}
        {#if rules.data}<tr><td colspan="5" class="empty">None yet. Make one to say, in your own words, when a game's backed up, starts, stops or crashes.</td></tr>{/if}
      {/each}
    </tbody>
  </table>
</div>

{#if editing}
  {@const n = editing}
  <Modal title={n.id ? `Edit "${n.name}"` : 'New notification'} onclose={() => (editing = undefined)}>
    <form id="notification" class="form" onsubmit={save}>
      <label>Name <input required maxlength="80" placeholder="e.g. Survival backups to Discord" bind:value={n.name} /></label>
      <div class="three">
        <label>
          When
          <select value={n.event} onchange={(e) => changeEvent(e.currentTarget.value as NotificationRule['event'])}>
            {#each Object.entries(EVENTS) as [k, e] (k)}<option value={k}>{e.label}</option>{/each}
          </select>
        </label>
        <label>
          For
          <select bind:value={n.gameId}>
            <option value={0}>Any game server</option>
            {#each games.data ?? [] as g (g.id)}<option value={g.id}>{g.name} ({g.serverName})</option>{/each}
          </select>
        </label>
        <label>
          Sent to
          <select bind:value={n.channelId}>
            {#each channels as c (c.id)}<option value={c.id}>{c.name} ({c.kind})</option>{/each}
          </select>
        </label>
      </div>
      <label>Title <input required maxlength="200" bind:value={n.title} /></label>
      <label>Message <textarea required rows="4" maxlength="2000" bind:value={n.message}></textarea></label>
      <p class="muted small">Fill-ins: <span class="mono">{FILLS[n.event]}</span></p>
      {#if n.event === 'backup_done'}
        <label class="check">
          <input type="checkbox" bind:checked={withLink} />
          Include a link that downloads the backup, without signing in, for
          <input class="days" type="number" min="1" max="30" bind:value={n.linkDays} disabled={!withLink} /> days
        </label>
        {#if withLink}
          <p class="muted small">
            Anyone with the link can download that backup until it runs out, so send it only where you'd share the world. Links
            stop when the backup's deleted, or with "Stop links" on its row in Backups. It goes where {'{link}'} is, or at the end.
          </p>
        {/if}
      {/if}
      <label class="check"><input type="checkbox" bind:checked={n.enabled} /> On</label>
      {#if error}<p class="error">{error}</p>{/if}
    </form>
    {#snippet footer()}
      <button onclick={() => (editing = undefined)}>Cancel</button>
      <button type="submit" form="notification" class="primary">Save</button>
    {/snippet}
  </Modal>
{/if}

<style>
  .head {
    margin: 22px 0 8px;
  }
  .head h2 {
    margin: 0;
  }
  .buttons {
    text-align: right;
    white-space: nowrap;
  }
  .buttons > * {
    margin-left: 6px;
  }
  .off td {
    opacity: 0.6;
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
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
  }
  /* The form makes every box full width: this one sits in its sentence. */
  .form label > input.days {
    width: 70px;
    flex: none;
  }
  textarea {
    resize: vertical;
  }
</style>
