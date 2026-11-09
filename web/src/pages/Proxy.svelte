<!-- Nginx Proxy Manager, managed from here: its proxy hosts, redirects, streams, 404 hosts, access lists
     and certificates, one tab each, with the panel's health check beside each proxy host. -->
<script lang="ts">
  import Modal from '../components/Modal.svelte';
  import ProxyForm from '../components/ProxyForm.svelte';
  import StatusDot from '../components/StatusDot.svelte';
  import Tabs from '../components/Tabs.svelte';
  import { sure } from '../lib/ask.svelte';
  import { api, ApiError } from '../lib/api';
  import { ago } from '../lib/format';
  import { load } from '../lib/load.svelte';
  import { navigate } from '../lib/router.svelte';
  import type { NpmItem, NpmKind } from '../lib/types';

  let { kind }: { kind: NpmKind } = $props();

  const TABS: { id: NpmKind; label: string; one: string }[] = [
    { id: 'proxy-hosts', label: 'Proxy hosts', one: 'proxy host' },
    { id: 'redirection-hosts', label: 'Redirects', one: 'redirect' },
    { id: 'streams', label: 'Streams', one: 'stream' },
    { id: 'dead-hosts', label: '404 hosts', one: '404 host' },
    { id: 'access-lists', label: 'Access lists', one: 'access list' },
    { id: 'certificates', label: 'Certificates', one: 'certificate' },
  ];
  // Each tab is its own address (/proxy/streams), so it can be linked to and Back works.
  // svelte-ignore state_referenced_locally
  let tab = $state<string>(kind);
  $effect(() => {
    if (tab !== kind) navigate(`/proxy/${tab}`);
  });
  $effect.pre(() => {
    tab = kind; // Back or a link changed the address
  });
  const instances = load(api.npmInstances);
  let npmId = $state(0);
  $effect(() => {
    if (!npmId && instances.data?.length) npmId = instances.data[0].id;
  });

  let items = $state<NpmItem[]>();
  let certificates = $state<NpmItem[]>([]);
  let accessLists = $state<NpmItem[]>([]);
  let error = $state<string>();
  async function reload() {
    if (!npmId) return;
    try {
      // Hosts' forms need the certificates and access lists to choose from.
      const [list, certs, lists] = await Promise.all([
        api.npmList(npmId, kind),
        kind === 'certificates' ? null : api.npmList(npmId, 'certificates'),
        kind === 'proxy-hosts' ? api.npmList(npmId, 'access-lists') : null,
      ]);
      items = list;
      certificates = certs ?? list;
      accessLists = lists ?? [];
      error = undefined;
    } catch (err) {
      error = (err as Error).message;
    }
  }
  $effect(() => {
    items = undefined;
    void reload();
  });

  let editing = $state<{ item?: NpmItem }>();
  let linking = $state(false);
  let managing = $state(false);
  let busy = $state(0);

  async function act(item: NpmItem, action: 'enable' | 'disable' | 'renew') {
    busy = item.id;
    try {
      await api.npmAction(npmId, kind, item.id, action);
      await reload();
    } catch (err) {
      error = (err as Error).message;
    } finally {
      busy = 0;
    }
  }
  async function remove(item: NpmItem) {
    if (!(await sure(`Delete ${describe(item)}?`, "It's deleted from Nginx Proxy Manager."))) return;
    try {
      await api.npmDelete(npmId, kind, item.id);
      await reload();
    } catch (err) {
      error = (err as Error).message;
    }
  }

  const describe = (item: NpmItem) => item.domain_names?.join(', ') || item.name || item.nice_name || `port ${item.incoming_port}`;
  const enabled = (item: NpmItem) => item.enabled === true || item.enabled === 1;
  const certName = (item: NpmItem) => {
    if (!item.certificate_id) return 'HTTP only';
    const cert = item.certificate ?? certificates.find((c) => c.id === item.certificate_id);
    return cert ? cert.nice_name || cert.domain_names?.[0] : 'Certificate';
  };
  const daysLeft = (cert: NpmItem) => Math.floor((Date.parse(String(cert.expires_on).replace(' ', 'T') + 'Z') - Date.now()) / 86_400_000);

  // Linking an NPM.
  let link = $state({ name: '', url: '', email: '', password: '' });
  let linkError = $state<string>();
  let linkBusy = $state(false);
  // An NPM with a self-signed certificate: the certificate it showed, to trust or not.
  let untrusted = $state<{ fingerprint: string; subject: string; expires: string }>();
  async function linkNpm(e: SubmitEvent) {
    e.preventDefault();
    linkBusy = true;
    linkError = undefined;
    try {
      try {
        await api.linkNpm({ ...link, trustCertificate: untrusted?.fingerprint });
      } catch (err) {
        if (err instanceof ApiError && err.status === 409 && err.body.certificate) {
          untrusted = err.body.certificate;
          return;
        }
        throw err;
      }
      untrusted = undefined;
      link = { name: '', url: '', email: '', password: '' };
      linking = false;
      await instances.reload();
      npmId = instances.data?.at(-1)?.id ?? 0;
    } catch (err) {
      linkError = (err as Error).message;
    } finally {
      linkBusy = false;
    }
  }
  async function unlink(id: number) {
    if (!(await sure('Unlink this NPM?', 'Nothing in NPM changes; Bastion just stops managing it.', 'Unlink'))) return;
    await api.unlinkNpm(id);
    if (npmId === id) npmId = 0;
    await instances.reload();
  }
</script>

<div class="page-head">
  <h1>Proxy</h1>
  {#if (instances.data?.length ?? 0) > 1}
    <select aria-label="NPM" bind:value={npmId}>
      {#each instances.data ?? [] as i (i.id)}<option value={i.id}>{i.name}</option>{/each}
    </select>
  {/if}
  {#if instances.data?.length}
    <button onclick={() => (managing = true)}>Linked NPMs</button>
    <button class="primary" onclick={() => (editing = {})}>
      New {TABS.find((t) => t.id === kind)?.one}
    </button>
  {/if}
</div>

{#if instances.data && !instances.data.length}
  <section class="card intro">
    <h2>Link Nginx Proxy Manager</h2>
    <p class="muted">Bastion signs in to NPM as one of its users and manages it through NPM's own API. NPM keeps doing the proxying.</p>
    <button class="primary" onclick={() => (linking = true)}>Link an NPM</button>
  </section>
{:else if instances.data}
  <Tabs tabs={TABS} bind:selected={tab} />
  {#if error}<p class="error">{error}</p>{/if}
  <div class="card table-card">
    <table>
      {#if kind === 'proxy-hosts'}
        <thead><tr><th>Domains</th><th>Forwards to</th><th>SSL</th><th>Access</th><th>Answering</th><th></th></tr></thead>
      {:else if kind === 'redirection-hosts'}
        <thead><tr><th>Domains</th><th>Redirects to</th><th>SSL</th><th></th></tr></thead>
      {:else if kind === 'streams'}
        <thead><tr><th>Incoming port</th><th>Forwards to</th><th>Protocols</th><th></th></tr></thead>
      {:else if kind === 'dead-hosts'}
        <thead><tr><th>Domains</th><th>SSL</th><th></th></tr></thead>
      {:else if kind === 'access-lists'}
        <thead><tr><th>Name</th><th>Users</th><th>Addresses</th><th>Lets in</th><th></th></tr></thead>
      {:else}
        <thead><tr><th>Name</th><th>Domains</th><th>From</th><th>Expires</th><th></th></tr></thead>
      {/if}
      <tbody>
        {#each items ?? [] as item (item.id)}
          <tr class:off={'enabled' in item && !enabled(item)}>
            {#if kind === 'proxy-hosts'}
              <td><b>{item.domain_names.join(', ')}</b></td>
              <td class="muted"><span class="address">{item.forward_scheme}://{item.forward_host}:{item.forward_port}</span></td>
              <td>{certName(item)}</td>
              <td>{item.access_list?.name ?? 'Public'}</td>
              <td>
                {#if !enabled(item)}
                  <span class="badge">Disabled</span>
                {:else if item.bastion_health}
                  {@const h = item.bastion_health}
                  <span class="row" title="Checked {ago(h.checkedAt)}{h.error ? `: ${h.error}` : ''}">
                    <StatusDot tone={h.status && h.status < 500 ? 'green' : 'red'} />
                    {h.status ? `${h.status} in ${h.ms} ms` : 'No answer'}
                  </span>
                {:else}
                  <span class="muted">Not checked yet</span>
                {/if}
              </td>
            {:else if kind === 'redirection-hosts'}
              <td><b>{item.domain_names.join(', ')}</b></td>
              <td class="mono muted">{item.forward_http_code} → {item.forward_scheme === 'auto' ? '' : `${item.forward_scheme}://`}{item.forward_domain_name}</td>
              <td>{certName(item)}</td>
            {:else if kind === 'streams'}
              <td><b>{item.incoming_port}</b></td>
              <td class="mono muted">{item.forwarding_host}:{item.forwarding_port}</td>
              <td>{[item.tcp_forwarding && 'TCP', item.udp_forwarding && 'UDP'].filter(Boolean).join(' and ')}</td>
            {:else if kind === 'dead-hosts'}
              <td><b>{item.domain_names.join(', ')}</b></td>
              <td>{certName(item)}</td>
            {:else if kind === 'access-lists'}
              <td><b>{item.name}</b></td>
              <td>{item.items?.length ?? 0}</td>
              <td>{item.clients?.length ?? 0}</td>
              <td>{item.satisfy_any ? 'Either check' : 'Both checks'}</td>
            {:else}
              {@const days = daysLeft(item)}
              <td><b>{item.nice_name || item.domain_names?.[0]}</b></td>
              <td class="muted">{item.domain_names?.join(', ')}</td>
              <td>{item.provider === 'letsencrypt' ? "Let's Encrypt" : 'Uploaded'}</td>
              <td><span class="badge {days < 0 ? 'red' : days < 14 ? 'amber' : 'green'}">{days < 0 ? 'Expired' : `In ${days} days`}</span></td>
            {/if}
            <td class="buttons">
              {#if kind === 'certificates'}
                {#if item.provider === 'letsencrypt'}<button disabled={busy === item.id} onclick={() => act(item, 'renew')}>{busy === item.id ? 'Renewing…' : 'Renew'}</button>{/if}
              {:else}
                <button onclick={() => (editing = { item })}>Edit</button>
                {#if 'enabled' in item}
                  <button disabled={busy === item.id} onclick={() => act(item, enabled(item) ? 'disable' : 'enable')}>{enabled(item) ? 'Disable' : 'Enable'}</button>
                {/if}
              {/if}
              <button class="danger" onclick={() => remove(item)}>Delete</button>
            </td>
          </tr>
        {:else}
          {#if items}<tr><td colspan="6" class="empty">None yet.</td></tr>{/if}
        {/each}
      </tbody>
    </table>
  </div>
{/if}

{#if editing}
  <ProxyForm {npmId} {kind} item={editing.item} {certificates} {accessLists} onclose={() => (editing = undefined)} onsaved={reload} />
{/if}

{#if managing}
  <Modal title="Linked NPMs" onclose={() => (managing = false)}>
    <ul class="instances">
      {#each instances.data ?? [] as i (i.id)}
        <li class="row">
          <span class="grow"><b>{i.name}</b> <span class="muted small">{i.url} as {i.email}</span></span>
          <button class="danger" onclick={() => unlink(i.id)}>Unlink</button>
        </li>
      {/each}
    </ul>
    {#snippet footer()}
      <button onclick={() => ((managing = false), (linking = true))}>Link another</button>
    {/snippet}
  </Modal>
{/if}

{#if linking}
  <Modal title="Link Nginx Proxy Manager" onclose={() => (linking = false)}>
    <form id="link-npm" class="form" onsubmit={linkNpm}>
      <label>Name <input placeholder="e.g. Home proxy" maxlength="60" bind:value={link.name} /></label>
      <label>Its admin page <input required class="mono" placeholder="http://192.168.1.20:81" bind:value={link.url} oninput={() => (untrusted = undefined)} /></label>
      <div class="pair">
        <label>NPM user's email <input required type="email" autocomplete="off" bind:value={link.email} /></label>
        <label>Password <input required type="password" autocomplete="new-password" bind:value={link.password} /></label>
      </div>
      <p class="muted small">Bastion signs in to check these work. The password is kept in Bastion's database to sign in again later, and never shown.</p>
      {#if untrusted}
        <div class="untrusted">
          <p><b>NPM's certificate can't be checked the usual way.</b> It's probably self-signed. If this is your NPM, you can trust this one certificate: Bastion then accepts only it, and refuses NPM if it ever changes.</p>
          <p class="small">For <span class="mono">{untrusted.subject}</span>, until {new Date(untrusted.expires).toLocaleDateString('en-GB', { day: 'numeric', month: 'short', year: 'numeric' })}</p>
          <p class="mono small fingerprint">{untrusted.fingerprint}</p>
          <p class="muted small">Or use <span class="mono">http://</span> if NPM's admin page is plain HTTP, as it is by default on port 81.</p>
        </div>
      {/if}
      {#if linkError}<p class="error">{linkError}</p>{/if}
    </form>
    {#snippet footer()}
      <button onclick={() => (linking = false)}>Cancel</button>
      <button type="submit" form="link-npm" class="primary" disabled={linkBusy}>
        {linkBusy ? 'Checking…' : untrusted ? 'Trust this certificate and link' : 'Link'}
      </button>
    {/snippet}
  </Modal>
{/if}

<style>
  .intro {
    max-width: 560px;
  }
  .buttons {
    text-align: right;
  }
  .buttons button {
    margin-left: 6px;
    padding: 4px 10px;
  }
  .address {
    font-family: var(--mono);
    font-size: 12.5px;
    word-break: break-all;
  }
  tr.off td {
    opacity: 0.6;
  }
  .untrusted {
    display: grid;
    gap: 6px;
    padding: 12px;
    border: 1px solid var(--amber);
    border-radius: 8px;
  }
  .fingerprint {
    word-break: break-all;
    color: var(--soft);
  }
  .instances {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 10px;
  }
</style>
