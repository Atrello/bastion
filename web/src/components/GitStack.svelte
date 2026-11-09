<!-- A stack following a compose file in a Git repository: where it is, the deploy key to add to the
     repository, and deploying what's new. The .env file stays on the server. -->
<script lang="ts">
  import { api } from '../lib/api';
  import { ago } from '../lib/format';
  import { load } from '../lib/load.svelte';

  let { serverId, name, ondeployed }: { serverId: number; name: string; ondeployed: () => void } = $props();
  const git = load(() => api.git(serverId, name));
  let repo = $state('');
  let branch = $state('main');
  let path = $state('compose.yaml');
  let autoDeploy = $state(false);
  let editing = $state(false);
  let busy = $state(false);
  let message = $state<{ ok: boolean; text: string }>();
  let copied = $state(false);

  $effect(() => {
    const g = git.data;
    if (g) ({ repo, branch, path, autoDeploy } = g);
  });

  async function attempt(work: () => Promise<unknown>, done: string) {
    busy = true;
    message = undefined;
    try {
      await work();
      message = { ok: true, text: done };
      await git.reload();
      editing = false;
    } catch (err) {
      message = { ok: false, text: (err as Error).message };
    } finally {
      busy = false;
    }
  }
  const short = (commit: string) => commit.slice(0, 7);
</script>

{#if message}<p class="note" class:error={!message.ok}>{message.text}</p>{/if}

{#if git.data && !editing}
  {@const g = git.data}
  <div class="card stack">
    <div class="row">
      <p class="grow"><span class="mono">{g.repo}</span> · <b>{g.branch}</b> · <span class="mono">{g.path}</span></p>
      <button onclick={() => (editing = true)}>Change</button>
      <button class="danger" disabled={busy} onclick={() => attempt(() => api.unlinkGit(serverId, name), 'No longer following the repository. The stack is as it was.')}>Stop following</button>
    </div>
    {#if g.error}
      <p class="error">{g.error}</p>
    {:else if g.remoteCommit && g.remoteCommit !== g.deployedCommit}
      <p class="row"><span class="badge amber">New commit</span> {short(g.remoteCommit)} on {g.branch}{g.deployedCommit ? `, deployed is ${short(g.deployedCommit)}` : ''}</p>
    {:else if g.deployedCommit}
      <p class="muted">Up to date at {short(g.deployedCommit)}.</p>
    {/if}
    <p class="muted small">
      {g.autoDeploy ? 'New commits deploy by themselves.' : 'New commits wait for Deploy.'}
      {g.checkedAt ? `Checked ${ago(g.checkedAt)}; checked every 5 minutes.` : ''}
    </p>
    <div class="row">
      <button class="primary" disabled={busy} onclick={() => attempt(async () => { await api.deployGit(serverId, name); ondeployed(); }, 'Deployed from the repository.')}>
        {busy ? 'Deploying…' : 'Deploy from the repository'}
      </button>
    </div>
  </div>
  <div class="card stack">
    <h2>Deploy key</h2>
    <p class="muted small">Add this to the repository as a read-only deploy key (on GitHub: the repository's Settings, Deploy keys). Public repositories over https:// don't need it.</p>
    <pre class="mono key">{g.publicKey}</pre>
    <div><button onclick={async () => { await navigator.clipboard.writeText(g.publicKey); copied = true; }}>{copied ? 'Copied' : 'Copy key'}</button></div>
  </div>
{:else if git.data !== undefined}
  <form class="card stack" onsubmit={(e) => { e.preventDefault(); void attempt(() => api.setGit(serverId, name, { repo, branch, path, autoDeploy }), 'Saved. Add the deploy key to the repository if it needs one, then Deploy.'); }}>
    <p class="muted">Follow a compose file in a Git repository. Deploying copies it over this stack's compose file; the .env file stays as it is.</p>
    <label>Repository <input required class="mono" placeholder="git@github.com:you/homelab.git or https://…" bind:value={repo} /></label>
    <div class="pair">
      <label>Branch <input required class="mono" bind:value={branch} /></label>
      <label>Compose file in it <input required class="mono" placeholder="stacks/{name}/compose.yaml" bind:value={path} /></label>
    </div>
    <label class="row check"><input type="checkbox" bind:checked={autoDeploy} /> Deploy new commits by themselves</label>
    <div class="row">
      <button type="submit" class="primary" disabled={busy}>Save</button>
      {#if git.data}<button type="button" onclick={() => (editing = false)}>Cancel</button>{/if}
    </div>
  </form>
{/if}

<style>
  .stack {
    display: grid;
    gap: 10px;
    margin-bottom: 16px;
  }
  .stack p {
    margin: 0;
  }
  label {
    display: grid;
    gap: 6px;
  }
  .check {
    display: flex;
  }
  .key {
    margin: 0;
    white-space: pre-wrap;
    word-break: break-all;
    background: var(--sunken);
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 10px 12px;
  }
  .note {
    color: var(--green);
    margin: 0 0 12px;
  }
  .note.error {
    color: var(--red);
  }
</style>
