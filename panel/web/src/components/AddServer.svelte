<!-- Adding a server: name it, then run the command shown on that server to install and join its agent. -->
<script lang="ts">
  import Modal from './Modal.svelte';
  import { api } from '../lib/api';

  let { onclose, onadded }: { onclose: () => void; onadded: () => void } = $props();
  let name = $state('');
  let command = $state<string>();
  let error = $state<string>();
  let copied = $state(false);

  async function add(e: SubmitEvent) {
    e.preventDefault();
    try {
      command = (await api.addServer(name.trim())).joinCommand;
      onadded();
    } catch (err) {
      error = (err as Error).message;
    }
  }

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
  {:else}
    <p>Run this on <b>{name}</b> as root. It installs the agent, joins it to this panel and starts it:</p>
    <pre class="mono">{command}</pre>
    <p class="muted small">
      The command works once, for the next hour. The agent connects out to the panel, so the server needs no open
      ports. It shows as online here as soon as it's connected.
    </p>
  {/if}
  {#snippet footer()}
    {#if !command}
      <button type="submit" form="add-server" class="primary" disabled={!name.trim()}>Add server</button>
    {:else}
      <button onclick={copy}>{copied ? 'Copied' : 'Copy command'}</button>
      <button class="primary" onclick={onclose}>Done</button>
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
    background: #0b0d11;
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 10px 12px;
  }
</style>
