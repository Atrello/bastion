<!-- One stack: what it's running, its compose file and .env to edit, its logs, and earlier versions. -->
<script lang="ts">
  import StatusDot from '../components/StatusDot.svelte';
  import Tabs from '../components/Tabs.svelte';
  import { api, type StackAction } from '../lib/api';
  import { ago } from '../lib/format';
  import { load } from '../lib/load.svelte';
  import { containerTone, stackStatus } from '../lib/status';

  let { serverId, name }: { serverId: number; name: string } = $props();

  // The editor is most of the panel's code, so it's only loaded on a stack's page.
  const editor = import('../components/CodeEditor.svelte');

  const stacks = load(api.stacks);
  const files = load(() => api.stackFiles(serverId, name));
  const containers = load(api.containers);
  const logs = load(() => api.stackLogs(serverId, name));
  const history = load(() => api.stackHistory(serverId, name));

  const stack = $derived(stacks.data?.find((s) => s.serverId === serverId && s.name === name));
  const mine = $derived(containers.data?.filter((c) => c.serverId === serverId && c.stack === name) ?? []);

  let tab = $state('compose');
  let compose = $state('');
  let env = $state('');
  let busy = $state<string>();
  let message = $state<{ ok: boolean; text: string }>();

  // The editors start from the files as they are on the server.
  $effect(() => {
    if (files.data) {
      compose = files.data.compose;
      env = files.data.env ?? '';
    }
  });
  const changed = $derived(!!files.data && (compose !== files.data.compose || env !== (files.data.env ?? '')));

  async function run(label: string, work: () => Promise<unknown>, done: string) {
    busy = label;
    message = undefined;
    try {
      await work();
      message = { ok: true, text: done };
      await Promise.all([stacks.reload(), containers.reload(), files.reload(), history.reload()]);
    } catch (err) {
      message = { ok: false, text: (err as Error).message };
    } finally {
      busy = undefined;
    }
  }

  const action = (a: StackAction, label: string, done: string) => run(label, () => api.stackAction(serverId, name, a), done);
  const save = (deploy: boolean) =>
    run(
      deploy ? 'Saving and deploying' : 'Saving',
      async () => {
        await api.saveStack(serverId, name, { compose, env: files.data?.env === null && !env ? null : env });
        if (deploy) await api.stackAction(serverId, name, 'up');
      },
      deploy ? 'Saved and deployed.' : 'Saved. Deploy when you’re ready.',
    );
</script>

<p class="crumbs small"><a href="/stacks">Stacks</a> / {stack?.serverName ?? ''}</p>
<div class="page-head">
  <h1>{name}</h1>
  {#if stack}
    {@const status = stackStatus[stack.state]}
    <span class="row status"><StatusDot tone={status.tone} />{status.label} · {stack.running} of {stack.total} containers</span>
  {/if}
  <button disabled={!!busy} onclick={() => action('update', 'Updating', 'Pulled the latest images and recreated what changed.')}>Update images</button>
  <button disabled={!!busy} onclick={() => action('restart', 'Restarting', 'Restarted.')}>Restart</button>
  {#if stack?.state === 'stopped' || stack?.state === 'new'}
    <button class="primary" disabled={!!busy} onclick={() => action('up', 'Starting', 'Started.')}>Start</button>
  {:else}
    <button class="danger" disabled={!!busy} onclick={() => action('stop', 'Stopping', 'Stopped.')}>Stop</button>
  {/if}
</div>

{#if busy}<p class="note">{busy}…</p>{/if}
{#if message}<p class="note" class:error={!message.ok}>{message.text}</p>{/if}

<Tabs
  bind:selected={tab}
  tabs={[
    { id: 'compose', label: 'Compose file' },
    { id: 'env', label: '.env' },
    { id: 'containers', label: `Containers (${mine.length})` },
    { id: 'logs', label: 'Logs' },
    { id: 'history', label: 'History' },
  ]}
/>

{#if tab === 'compose' || tab === 'env'}
  {#if files.error}<p class="error">{files.error}</p>{/if}
  {#if files.data}
    <p class="muted small file">
      {#if tab === 'compose'}
        {stack?.configFile}
      {:else}
        The .env file beside the compose file. It's read from and saved to the server; Bastion doesn't keep a copy.
      {/if}
    </p>
    {#await editor then { default: CodeEditor }}
      {#if tab === 'compose'}
        <CodeEditor bind:value={compose} language="yaml" label="Compose file" />
      {:else}
        <CodeEditor bind:value={env} label=".env file" />
      {/if}
    {/await}
    <div class="row actions">
      <span class="muted small grow">{changed ? 'Unsaved changes. Docker checks them before anything is saved.' : 'No changes.'}</span>
      <button disabled={!changed || !!busy} onclick={() => files.reload()}>Discard</button>
      <button disabled={!changed || !!busy} onclick={() => save(false)}>Save</button>
      <button class="primary" disabled={!changed || !!busy} onclick={() => save(true)}>Save and deploy</button>
    </div>
  {/if}
{:else if tab === 'containers'}
  <div class="card table-card">
    <table>
      <thead><tr><th>Container</th><th>Service</th><th>Image</th><th>Status</th><th>Ports</th></tr></thead>
      <tbody>
        {#each mine as c (c.id)}
          <tr>
            <td><span class="row"><StatusDot tone={containerTone(c.state)} label={c.state} /><b>{c.name}</b></span></td>
            <td>{c.service}</td>
            <td class="mono muted">{c.image}</td>
            <td>{c.status}</td>
            <td class="mono muted">{c.ports}</td>
          </tr>
        {:else}
          <tr><td colspan="5" class="empty">No containers. Start the stack to create them.</td></tr>
        {/each}
      </tbody>
    </table>
  </div>
{:else if tab === 'logs'}
  <div class="row actions top">
    <span class="muted small grow">The last 200 lines from every container in the stack.</span>
    <button onclick={() => logs.reload()}>Refresh</button>
  </div>
  <pre class="logs mono">{#each logs.data?.lines ?? [] as line}<span class:warn={/\bWARN|\bERROR/.test(line)}>{line}</span>
{/each}</pre>
{:else if tab === 'history'}
  <div class="card table-card">
    <table>
      <thead><tr><th>Version</th><th>Saved</th><th>By</th><th>Note</th></tr></thead>
      <tbody>
        {#each history.data ?? [] as v (v.id)}
          <tr>
            <td class="mono">#{v.id}</td>
            <td>{ago(v.savedAt)}</td>
            <td>{v.savedBy}</td>
            <td class="muted">{v.note || '–'}</td>
          </tr>
        {:else}
          <tr><td colspan="4" class="empty">No earlier versions yet. Each save keeps the compose file it replaced.</td></tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<style>
  .crumbs {
    margin: 0 0 4px;
    color: var(--muted);
  }
  .status {
    color: var(--muted);
    margin-right: auto;
  }
  .page-head h1 {
    flex: none;
  }
  .note {
    margin: -8px 0 12px;
    color: var(--green);
  }
  .note.error {
    color: var(--red);
  }
  .file {
    margin: 0 0 8px;
  }
  .actions {
    margin-top: 12px;
  }
  .actions.top {
    margin: 0 0 8px;
  }
  .logs {
    margin: 0;
    height: 520px;
    overflow: auto;
    padding: 12px 14px;
    background: #0b0d11;
    border: 1px solid var(--line);
    border-radius: 8px;
    line-height: 1.6;
    color: #b7c0cf;
  }
  .warn {
    color: var(--amber);
  }
</style>
