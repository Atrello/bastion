<!-- Alerts: where they go (ntfy, Discord, email), which kinds are on, and what's been raised. -->
<script lang="ts">
  import Modal from '../components/Modal.svelte';
  import { api } from '../lib/api';
  import { ago } from '../lib/format';
  import { load } from '../lib/load.svelte';

  const page = load(api.alerts);
  const KINDS: [string, string][] = [
    ['offline', 'A server goes offline (for 2 minutes)'],
    ['crash', 'A container keeps restarting'],
    ['unhealthy', 'A container fails its health check'],
    ['disk', "A server's disk is over 90% full"],
    ['update', 'An image update is available'],
  ];
  let adding = $state(false);
  let kind = $state<'ntfy' | 'discord' | 'email'>('ntfy');
  let name = $state('');
  let cfg = $state<Record<string, string>>({ url: 'https://ntfy.sh', topic: '', token: '', host: '', port: '587', username: '', password: '', from: '', to: '' });
  let error = $state<string>();
  let note = $state<{ ok: boolean; text: string }>();

  async function attempt(work: () => Promise<unknown>, done?: string) {
    note = undefined;
    try {
      await work();
      if (done) note = { ok: true, text: done };
      await page.reload();
    } catch (err) {
      note = { ok: false, text: (err as Error).message };
    }
  }

  function toggle(k: string, on: boolean) {
    const kinds = on ? [...(page.data?.kinds ?? []), k] : (page.data?.kinds ?? []).filter((x) => x !== k);
    void attempt(() => api.setAlertKinds(kinds));
  }

  async function add(e: SubmitEvent) {
    e.preventDefault();
    error = undefined;
    const config: Record<string, string | number> =
      kind === 'ntfy' ? { url: cfg.url, topic: cfg.topic, token: cfg.token }
      : kind === 'discord' ? { url: cfg.url }
      : { host: cfg.host, port: Number(cfg.port), username: cfg.username, password: cfg.password, from: cfg.from, to: cfg.to };
    try {
      await api.addChannel(kind, name, config);
      adding = false;
      await page.reload();
    } catch (err) {
      error = (err as Error).message;
    }
  }
</script>

<div class="page-head">
  <h1>Alerts</h1>
  <button class="primary" onclick={() => ((adding = true), (cfg.url = kind === 'ntfy' ? 'https://ntfy.sh' : ''))}>Add a channel</button>
</div>
{#if note}<p class="note" class:error={!note.ok}>{note.text}</p>{/if}

{#if page.data}
  {@const p = page.data}
  <div class="grid">
    <section class="card">
      <h2>Tell me when</h2>
      {#each KINDS as [k, label] (k)}
        <label class="row check"><input type="checkbox" checked={p.kinds.includes(k)} onchange={(e) => toggle(k, e.currentTarget.checked)} /> {label}</label>
      {/each}
      <p class="muted small">Each alert is sent once, and again when it's resolved.</p>
    </section>
    <section class="card">
      <h2>Sent to</h2>
      {#each p.channels as c (c.id)}
        <div class="row channel">
          <span class="badge blue">{c.kind}</span>
          <span class="grow"><b>{c.name}</b> <span class="muted small">{c.detail}</span></span>
          <button onclick={() => attempt(() => api.testChannel(c.id), `Sent a test to ${c.name}.`)}>Test</button>
          <button class="danger" onclick={() => attempt(() => api.deleteChannel(c.id))}>Remove</button>
        </div>
      {:else}
        <p class="muted">Nowhere yet: add ntfy, a Discord webhook or email.</p>
      {/each}
    </section>
  </div>

  <h2 class="recent">Recent</h2>
  <div class="card table-card">
    <table>
      <thead><tr><th>Alert</th><th>Details</th><th>Raised</th><th>Resolved</th></tr></thead>
      <tbody>
        {#each p.recent as a (a.id)}
          <tr>
            <td><b>{a.title}</b></td>
            <td class="muted wrap">{a.detail}</td>
            <td>{ago(a.raisedAt)}</td>
            <td>{#if a.clearedAt}{ago(a.clearedAt)}{:else}<span class="badge amber">Open</span>{/if}</td>
          </tr>
        {:else}
          <tr><td colspan="4" class="empty">Nothing's been raised.</td></tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

{#if adding}
  <Modal title="Add a channel" onclose={() => (adding = false)}>
    <form id="channel" class="form" onsubmit={add}>
      <div class="pair">
        <label>Kind
          <select bind:value={kind} onchange={() => (cfg.url = kind === 'ntfy' ? 'https://ntfy.sh' : '')}>
            <option value="ntfy">ntfy</option><option value="discord">Discord</option><option value="email">Email</option>
          </select>
        </label>
        <label>Name <input maxlength="60" placeholder="e.g. Phone" bind:value={name} /></label>
      </div>
      {#if kind === 'ntfy'}
        <div class="pair">
          <label>Server <input required bind:value={cfg.url} /></label>
          <label>Topic <input required bind:value={cfg.topic} /></label>
        </div>
        <label>Access token <span class="muted small">(if the topic is protected)</span> <input type="password" autocomplete="off" bind:value={cfg.token} /></label>
      {:else if kind === 'discord'}
        <label>Webhook address <input required placeholder="https://discord.com/api/webhooks/…" bind:value={cfg.url} /></label>
      {:else}
        <div class="pair">
          <label>SMTP server <input required placeholder="smtp.example.com" bind:value={cfg.host} /></label>
          <label>Port <input required inputmode="numeric" bind:value={cfg.port} /></label>
          <label>Username <input autocomplete="off" bind:value={cfg.username} /></label>
          <label>Password <input type="password" autocomplete="off" bind:value={cfg.password} /></label>
          <label>From <input required type="email" bind:value={cfg.from} /></label>
          <label>To <input required type="email" bind:value={cfg.to} /></label>
        </div>
      {/if}
      {#if error}<p class="error">{error}</p>{/if}
    </form>
    {#snippet footer()}<button type="submit" form="channel" class="primary">Add</button>{/snippet}
  </Modal>
{/if}

<style>
  .grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 16px;
  }
  .grid .card {
    display: grid;
    gap: 10px;
    align-content: start;
  }
  .grid p {
    margin: 0;
  }
  .check {
    display: flex;
  }
  .channel {
    padding: 6px 0;
    border-top: 1px solid var(--line-soft);
  }
  .recent {
    margin: 22px 0 8px;
  }
  .wrap {
    white-space: normal;
  }
  .note {
    color: var(--green);
    margin: -8px 0 12px;
  }
  .note.error {
    color: var(--red);
  }
  .form {
    display: grid;
    gap: 12px;
  }
  .form label {
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
</style>
