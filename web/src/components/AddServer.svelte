<!-- Adding a server: name it, then run the command shown on that server to install and join its agent. It only
     shows in Bastion once its agent has connected, so this waits for that and says when it has. -->
<script lang="ts">
  import Modal from './Modal.svelte';
  import StatusDot from './StatusDot.svelte';
  import { api } from '../lib/api';
  import { navigate } from '../lib/router.svelte';

  let { onclose, onadded }: { onclose: () => void; onadded: () => void } = $props();
  let name = $state('');
  let command = $state<string>();
  let id = $state<number>();
  let connected = $state(false);
  let error = $state<string>();
  let copied = $state(false);

  async function add(e: SubmitEvent) {
    e.preventDefault();
    try {
      const added = await api.addServer(name.trim());
      command = added.joinCommand;
      id = added.server.id;
    } catch (err) {
      error = (err as Error).message;
    }
  }

  // Watch for its agent joining, until it has or the dialog is closed.
  $effect(() => {
    if (id === undefined || connected) return;
    const timer = setInterval(async () => {
      try {
        if ((await api.servers()).some((s) => s.id === id)) {
          connected = true;
          onadded();
        }
      } catch {
        // Tried again in a moment.
      }
    }, 3000);
    return () => clearInterval(timer);
  });

  async function copy() {
    await navigator.clipboard.writeText(command!);
    copied = true;
  }
</script>

<Modal title="Add a server" {onclose}>
  {#if !command}
    <form id="add-server" onsubmit={add}>
      <label>
        Name
        <input required maxlength="40" placeholder="e.g. atlas" bind:value={name} />
      </label>
      <p class="muted small">How it's shown in Bastion. It doesn't have to match the server's hostname.</p>
      {#if error}<p class="error">{error}</p>{/if}
    </form>
  {:else if connected}
    <p><b>{name}</b> has connected and is now in Bastion.</p>
  {:else}
    <p>Run this on <b>{name}</b> as root. It installs the agent, joins it to this panel and starts it:</p>
    <pre class="mono">{command}</pre>
    <p class="muted small">
      The command works once, for the next hour. The agent connects out to the panel, so the server needs no open
      ports. It's added to Bastion once it has connected: until then it isn't listed anywhere.
    </p>
    <p class="row small muted"><StatusDot tone="amber" />Waiting for it to connect…</p>
  {/if}
  {#snippet footer()}
    {#if !command}
      <button type="submit" form="add-server" class="primary" disabled={!name.trim()}>Add server</button>
    {:else if connected}
      <button onclick={onclose}>Done</button>
      <button class="primary" onclick={() => navigate(`/servers/${id}`)}>Open {name}</button>
    {:else}
      <button onclick={copy}>{copied ? 'Copied' : 'Copy command'}</button>
      <button class="primary" onclick={onclose}>Close</button>
    {/if}
  {/snippet}
</Modal>

<style>
  label {
    display: grid;
    gap: 6px;
  }
  pre {
    white-space: pre-wrap;
    word-break: break-all;
    background: var(--sunken);
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 10px 12px;
  }
</style>
