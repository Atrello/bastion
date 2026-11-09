<!-- A new stack on a server: a compose file written here (and a .env if it needs one), or one that
     follows a compose file in a Git repository. -->
<script lang="ts">
  import Modal from './Modal.svelte';
  import { api } from '../lib/api';
  import { load } from '../lib/load.svelte';
  import { navigate } from '../lib/router.svelte';

  let { onclose }: { onclose: () => void } = $props();
  const servers = load(api.servers);
  let serverId = $state('');
  let name = $state('');
  let from = $state<'write' | 'git'>('write');
  let compose = $state('services:\n  app:\n    image: \n    restart: unless-stopped\n');
  let env = $state('');
  let repo = $state('');
  let branch = $state('main');
  let path = $state('compose.yaml');
  let busy = $state(false);
  let error = $state<string>();

  async function create(e: SubmitEvent) {
    e.preventDefault();
    busy = true;
    error = undefined;
    const id = Number(serverId);
    try {
      if (from === 'write') {
        await api.createStack(id, name, compose, env.trim() ? env : null);
      } else {
        await api.setGit(id, name, { repo, branch, path, autoDeploy: false });
        await api.deployGit(id, name);
      }
      navigate(`/stacks/${id}/${name}`);
    } catch (err) {
      error = (err as Error).message;
      busy = false;
    }
  }
</script>

<Modal title="New stack" {onclose}>
  <form id="new-stack" class="form" onsubmit={create}>
    <div class="pair">
      <label>
        Server
        <select required bind:value={serverId}>
          <option value="" disabled>Pick a server</option>
          {#each (servers.data ?? []).filter((s) => s.online) as s (s.id)}<option value={String(s.id)}>{s.name}</option>{/each}
        </select>
      </label>
      <label>Name <input required pattern="[a-z0-9][a-z0-9_\-]*" maxlength="63" placeholder="e.g. jellyfin" bind:value={name} /></label>
    </div>
    <div class="segmented" role="radiogroup" aria-label="Compose file">
      <button type="button" role="radio" aria-checked={from === 'write'} class:on={from === 'write'} onclick={() => (from = 'write')}>Write it here</button>
      <button type="button" role="radio" aria-checked={from === 'git'} class:on={from === 'git'} onclick={() => (from = 'git')}>From a Git repository</button>
    </div>
    {#if from === 'write'}
      <label>Compose file <textarea class="mono" rows="10" bind:value={compose}></textarea></label>
      <label>.env <span class="muted small">(optional)</span> <textarea class="mono" rows="3" bind:value={env}></textarea></label>
    {:else}
      <label>Repository <input required class="mono" placeholder="https://github.com/you/homelab.git" bind:value={repo} /></label>
      <div class="pair">
        <label>Branch <input required class="mono" bind:value={branch} /></label>
        <label>Compose file in it <input required class="mono" bind:value={path} /></label>
      </div>
      <p class="muted small">A private repository needs the stack's deploy key: it's shown on the stack's Git tab once it's made.</p>
    {/if}
    <p class="muted small">It's made in /opt/stacks/{name || '…'} and started.</p>
    {#if error}<p class="error">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <button onclick={onclose}>Cancel</button>
    <button type="submit" form="new-stack" class="primary" disabled={busy}>{busy ? 'Starting…' : 'Create and start'}</button>
  {/snippet}
</Modal>

<style>
  .form {
    display: grid;
    gap: 12px;
  }
  label {
    display: grid;
    gap: 6px;
  }
  .form p {
    margin: 0;
  }
  .pair {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }
  .segmented {
    display: flex;
    gap: 4px;
  }
  .segmented button.on {
    border-color: var(--accent);
    color: #fff;
  }
</style>
