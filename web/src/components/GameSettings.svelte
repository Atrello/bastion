<!-- A game's settings. Everyone it's shared with can change its startup variables (the ones its egg lets
     players change); the owner also sets its limits, ports and image, shares it, reinstalls and deletes it. -->
<script lang="ts">
  import { ask, sure } from '../lib/ask.svelte';
  import { api } from '../lib/api';
  import { load } from '../lib/load.svelte';
  import { navigate } from '../lib/router.svelte';
  import type { GameServer } from '../lib/types';

  let { game, onerror, onnote, onchanged }: { game: GameServer; onerror: (m: string) => void; onnote: (m: string) => void; onchanged: () => void } =
    $props();

  const startup = load(() => api.gameStartup(game.id));
  const shares = load(() => (game.owner ? api.gameShares(game.id) : Promise.resolve([])));
  const stores = load(() => (game.owner ? api.backupStores() : Promise.resolve([])));
  // svelte-ignore state_referenced_locally
  let backupStore = $state(game.backupStore);

  let values = $state<Record<string, string>>({});
  $effect(() => {
    if (startup.data) values = Object.fromEntries(startup.data.variables.map((v) => [v.key, v.value]));
  });

  // svelte-ignore state_referenced_locally
  let limits = $state({
    name: game.name,
    image: game.image,
    memoryMb: Math.round(game.memLimit / 1024 ** 2),
    cpuPercent: game.cpuLimit,
    diskMb: Math.round(game.diskLimit / 1024 ** 2),
    ports: game.ports.join(', '),
    warnCommand: game.warnCommand,
  });
  let invite = $state('');
  let friend = $state('');

  async function attempt(work: () => Promise<unknown>, note?: string) {
    try {
      await work();
      if (note) onnote(note);
      return true;
    } catch (err) {
      onerror((err as Error).message);
      return false;
    }
  }

  function saveLimits(e: SubmitEvent) {
    e.preventDefault();
    const ports = limits.ports.split(/[\s,]+/).filter(Boolean).map(Number);
    void attempt(() => api.updateGame(game.id, { ...limits, ports }), 'Saved. They apply from its next start.').then((ok) => ok && onchanged());
  }

  async function share(e: SubmitEvent) {
    e.preventDefault();
    if (await attempt(() => api.shareGame(game.id, friend.trim()), `Shared with ${friend}.`)) friend = '';
    await shares.reload();
  }
</script>

{#if startup.data}
  <section class="card">
    <h2>Startup</h2>
    <p class="mono command">{startup.data.command}</p>
    <form
      class="variables"
      onsubmit={(e) => {
        e.preventDefault();
        void attempt(() => api.saveGameStartup(game.id, values), 'Saved. They apply from its next start.');
      }}
    >
      {#each startup.data.variables as v (v.key)}
        <label class="variable">
          <span class="row"><b class="grow">{v.name}</b><span class="mono muted small">{v.key}</span></span>
          <input class="mono" disabled={!v.editable} bind:value={values[v.key]} />
          <span class="muted small">{v.description}{v.editable ? '' : ' Only the panel\'s owner can change this.'}</span>
        </label>
      {:else}
        <p class="muted">Its egg has no settings.</p>
      {/each}
      {#if startup.data.variables.length}<div class="row end"><button type="submit" class="primary">Save</button></div>{/if}
    </form>
  </section>
{/if}

{#if game.owner}
  <section class="card">
    <h2>Limits</h2>
    <form class="form" onsubmit={saveLimits}>
      <div class="pair">
        <label>Name <input required maxlength="60" bind:value={limits.name} /></label>
        <label>
          Image
          <select bind:value={limits.image}>
            {#each startup.data?.images ?? [{ label: limits.image, image: limits.image }] as i (i.image)}<option value={i.image}>{i.label}</option>{/each}
          </select>
        </label>
      </div>
      <div class="three">
        <label>Memory (MB) <input type="number" min="256" required bind:value={limits.memoryMb} /></label>
        <label>CPU (%) <input type="number" min="0" max="6400" bind:value={limits.cpuPercent} /></label>
        <label>Disk (MB) <input type="number" min="0" bind:value={limits.diskMb} /></label>
      </div>
      <div class="pair">
        <label>Ports <input required class="mono" bind:value={limits.ports} /></label>
        <label>Warn players with <input class="mono" placeholder="say %s" bind:value={limits.warnCommand} /></label>
      </div>
      <p class="muted small">
        100% CPU is one core, and 0 means no limit for CPU or disk. A game over its disk limit is stopped within a
        minute (Docker can't cap a folder's size). The first port is the game's own; each is opened for TCP and UDP.
        The warning is typed into the game's console before a stop, with %s where the message goes; leave it
        empty if the game can't be told.
      </p>
      <div class="row end"><button type="submit" class="primary">Save</button></div>
    </form>
  </section>

  <section class="card">
    <h2>Backups</h2>
    <div class="row store">
      <label class="grow">
        Keep new backups on
        <select bind:value={backupStore}>
          <option value={0}>{game.serverName}, beside the game</option>
          {#each stores.data ?? [] as s (s.id)}<option value={s.id}>{s.name} (S3)</option>{/each}
        </select>
      </label>
      <button
        disabled={backupStore === game.backupStore}
        onclick={() =>
          attempt(() => api.setGameBackupStore(game.id, backupStore), 'Saved: new backups go there. Those made already stay where they are.').then(
            (ok) => ok && onchanged(),
          )}>Save</button
      >
    </div>
    <p class="muted small">Backup storage (S3) is added on the <a href="/backup-storage">Backup storage</a> page.</p>
  </section>

  <section class="card">
    <h2>Shared with</h2>
    <ul class="shares">
      {#each shares.data ?? [] as s (s.userId)}
        <li class="row">
          <span class="grow">{s.username}</span>
          <button class="danger" onclick={() => attempt(() => api.unshareGame(game.id, s.userId), `${s.username} can't see it now.`).then(() => shares.reload())}>Stop sharing</button>
        </li>
      {:else}
        <li class="muted">Nobody yet. A friend sees only the game servers shared with them, and can't change their limits or delete them.</li>
      {/each}
    </ul>
    <div class="row share">
      <form class="row grow" onsubmit={share}>
        <input class="grow" placeholder="A friend's username, if they have an account" bind:value={friend} />
        <button type="submit" disabled={!friend.trim()}>Share</button>
      </form>
      <button onclick={() => attempt(async () => (invite = (await api.shareGame(game.id)).link ?? ''))}>Make an invite link</button>
    </div>
    {#if invite}
      <p class="small">Send this to them. It works once, for a week: they make an account (with two-factor sign-in) and see this game.</p>
      <input class="mono invite" readonly value={invite} onfocus={(e) => e.currentTarget.select()} />
    {/if}
  </section>

  <section class="card danger-zone">
    <h2>Reinstall or delete</h2>
    <div class="row">
      <span class="grow muted small">Reinstalling runs its egg's installer again over its files (worlds are usually kept). Deleting removes its files and backups, wherever they're kept.</span>
      <button
        onclick={async () =>
          (await sure('Reinstall ' + game.name + '?', "Its egg's installer runs again over its files. Stop it first. Worlds are usually kept, but back up first if unsure.", 'Reinstall', false)) &&
          attempt(() => api.reinstallGame(game.id), 'Reinstalling: the console shows how it goes.').then(onchanged)}>Reinstall</button
      >
      <button
        class="danger"
        onclick={async () =>
          (await ask({ title: `Delete ${game.name}?`, message: 'This deletes it, its files and its backups (those in backup storage too). It can\'t be undone.', input: { label: 'Type its name to go ahead', mustMatch: game.name }, confirm: 'Delete it', danger: true })) !== null &&
          attempt(() => api.deleteGame(game.id)).then((ok) => ok && navigate('/games'))}>Delete</button
      >
    </div>
  </section>
{/if}

<style>
  .store {
    align-items: end;
  }
  .store label {
    display: grid;
    gap: 6px;
  }
  section {
    margin-bottom: 16px;
  }
  h2 {
    margin: 0 0 12px;
  }
  .command {
    margin: 0 0 14px;
    color: var(--muted);
    word-break: break-all;
  }
  .variables {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 300px), 1fr));
    gap: 14px;
  }
  .variable {
    display: grid;
    gap: 6px;
  }
  .end {
    grid-column: 1 / -1;
    justify-content: flex-end;
  }
  .shares {
    list-style: none;
    margin: 0 0 12px;
    padding: 0;
    display: grid;
    gap: 8px;
  }
  .share {
    gap: 8px;
  }
  .invite {
    width: 100%;
  }
</style>
