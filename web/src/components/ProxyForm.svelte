<!-- Adding or editing one NPM item: a proxy host, redirect, 404 host, stream, access list or certificate.
     The fields are NPM's own, and what's sent is only what NPM's API accepts for that kind. -->
<script lang="ts">
  import Modal from './Modal.svelte';
  import Tabs from './Tabs.svelte';
  import { api } from '../lib/api';
  import type { NpmItem, NpmKind } from '../lib/types';

  let {
    npmId,
    kind,
    item,
    certificates,
    accessLists,
    onclose,
    onsaved,
  }: {
    npmId: number;
    kind: NpmKind;
    item?: NpmItem;
    certificates: NpmItem[];
    accessLists: NpmItem[];
    onclose: () => void;
    onsaved: () => void;
  } = $props();

  const NAMES: Record<NpmKind, string> = {
    'proxy-hosts': 'proxy host',
    'redirection-hosts': 'redirect',
    'dead-hosts': '404 host',
    streams: 'stream',
    'access-lists': 'access list',
    certificates: 'certificate',
  };
  // The form starts from the item being edited, so nothing it has is lost.
  // svelte-ignore state_referenced_locally
  const it: Record<string, any> = item ?? {};
  const meta = it.meta ?? {};

  let domains = $state((it.domain_names ?? []).join(', '));
  // svelte-ignore state_referenced_locally
  let scheme = $state(it.forward_scheme ?? (kind === 'redirection-hosts' ? 'auto' : 'http'));
  let host = $state(it.forward_host ?? '');
  let port = $state<number | undefined>(it.forward_port);
  let caching = $state(!!it.caching_enabled);
  let blockExploits = $state(it.block_exploits === undefined ? true : !!it.block_exploits);
  let websockets = $state(it.allow_websocket_upgrade === undefined ? true : !!it.allow_websocket_upgrade);
  let accessList = $state(String(it.access_list_id ?? 0));
  let locations = $state<{ path: string; forward_scheme: string; forward_host: string; forward_port: number | undefined; advanced_config: string }[]>(
    (it.locations ?? []).map((l: Record<string, any>) => ({
      path: l.path ?? '',
      forward_scheme: l.forward_scheme ?? 'http',
      forward_host: l.forward_host ?? '',
      forward_port: l.forward_port,
      advanced_config: l.advanced_config ?? '',
    })),
  );
  let advanced = $state(it.advanced_config ?? '');

  // Redirects.
  let redirectTo = $state(it.forward_domain_name ?? '');
  let httpCode = $state(String(it.forward_http_code ?? 301));
  let preservePath = $state(it.preserve_path === undefined ? true : !!it.preserve_path);

  // Streams.
  let incomingPort = $state<number | undefined>(it.incoming_port);
  let forwardingHost = $state(it.forwarding_host ?? '');
  let forwardingPort = $state<number | undefined>(it.forwarding_port);
  let tcp = $state(it.tcp_forwarding === undefined ? true : !!it.tcp_forwarding);
  let udp = $state(!!it.udp_forwarding);

  // Access lists. NPM keeps a user's password when it's sent back empty.
  let listName = $state(it.name ?? '');
  let satisfyAny = $state(!!it.satisfy_any);
  let passAuth = $state(!!it.pass_auth);
  let users = $state<{ username: string; password: string }[]>((it.items ?? []).map((u: Record<string, any>) => ({ username: u.username, password: '' })));
  let clients = $state<{ address: string; directive: string }[]>((it.clients ?? []).map((c: Record<string, any>) => ({ address: c.address, directive: c.directive })));

  // SSL, for hosts: an existing certificate, none, or a new one from Let's Encrypt.
  let certificate = $state(String(it.certificate_id ?? 0));
  let forceSsl = $state(!!it.ssl_forced);
  let http2 = $state(!!it.http2_support);
  let hsts = $state(!!it.hsts_enabled);
  let hstsSubdomains = $state(!!it.hsts_subdomains);
  let email = $state(meta.letsencrypt_email ?? '');
  let agree = $state(false);
  let dnsChallenge = $state(!!meta.dns_challenge);
  let dnsProvider = $state(meta.dns_provider ?? '');
  let dnsCredentials = $state(meta.dns_provider_credentials ?? '');
  let propagation = $state<number | undefined>(meta.propagation_seconds);

  // Certificates: from Let's Encrypt, or uploaded (a custom one, as NPM calls it).
  let upload = $state(false);
  let certName = $state('');
  let pem = $state({ certificate: '', certificateKey: '', intermediate: '' });
  async function readFile(e: Event, field: keyof typeof pem) {
    const file = (e.currentTarget as HTMLInputElement).files?.[0];
    if (file) pem[field] = await file.text();
  }

  const isHost = $derived(kind === 'proxy-hosts' || kind === 'redirection-hosts' || kind === 'dead-hosts');
  const newCertificate = $derived((kind === 'certificates' && !upload) || certificate === 'new');
  const tabs = $derived([
    { id: 'details', label: 'Details' },
    ...(kind === 'proxy-hosts' ? [{ id: 'locations', label: `Custom locations (${locations.length})` }] : []),
    ...(isHost ? [{ id: 'ssl', label: 'SSL' }] : []),
    ...(isHost ? [{ id: 'advanced', label: 'Advanced' }] : []),
  ]);
  let tab = $state('details');
  let busy = $state(false);
  let error = $state<string>();

  const domainList = () =>
    domains
      .split(/[\s,]+/)
      .map((d: string) => d.trim().toLowerCase())
      .filter(Boolean);

  function letsEncrypt() {
    return {
      letsencrypt_email: email.trim(),
      letsencrypt_agree: agree,
      dns_challenge: dnsChallenge,
      ...(dnsChallenge ? { dns_provider: dnsProvider.trim(), dns_provider_credentials: dnsCredentials, propagation_seconds: propagation || undefined } : {}),
    };
  }

  function ssl() {
    return {
      certificate_id: certificate === 'new' ? 'new' : Number(certificate),
      ssl_forced: forceSsl,
      http2_support: http2,
      hsts_enabled: hsts,
      hsts_subdomains: hstsSubdomains,
      meta: certificate === 'new' ? letsEncrypt() : { letsencrypt_agree: false, dns_challenge: false },
    };
  }

  function payload(): Record<string, unknown> {
    switch (kind) {
      case 'proxy-hosts':
        return {
          domain_names: domainList(),
          forward_scheme: scheme,
          forward_host: host.trim(),
          forward_port: port,
          caching_enabled: caching,
          block_exploits: blockExploits,
          allow_websocket_upgrade: websockets,
          access_list_id: Number(accessList),
          locations: locations.map((l) => ({ ...l, path: l.path.trim(), forward_host: l.forward_host.trim() })),
          advanced_config: advanced,
          ...ssl(),
        };
      case 'redirection-hosts':
        return {
          domain_names: domainList(),
          forward_scheme: scheme,
          forward_domain_name: redirectTo.trim(),
          forward_http_code: Number(httpCode),
          preserve_path: preservePath,
          block_exploits: blockExploits,
          advanced_config: advanced,
          ...ssl(),
        };
      case 'dead-hosts':
        return { domain_names: domainList(), advanced_config: advanced, ...ssl() };
      case 'streams':
        return { incoming_port: incomingPort, forwarding_host: forwardingHost.trim(), forwarding_port: forwardingPort, tcp_forwarding: tcp, udp_forwarding: udp, meta: {} };
      case 'access-lists':
        return {
          name: listName.trim(),
          satisfy_any: satisfyAny,
          pass_auth: passAuth,
          items: users.filter((u) => u.username.trim()).map((u) => ({ username: u.username.trim(), password: u.password })),
          clients: clients.filter((c) => c.address.trim()).map((c) => ({ address: c.address.trim(), directive: c.directive })),
        };
      case 'certificates':
        return { provider: 'letsencrypt', domain_names: domainList(), meta: letsEncrypt() };
    }
  }

  // A field that needs filling in may be on another tab: show that tab, so the browser can point at it.
  function showInvalid(e: Event) {
    const section = (e.target as Element).closest<HTMLElement>('[data-tab]');
    if (section?.dataset.tab) tab = section.dataset.tab;
  }

  async function save(e: SubmitEvent) {
    e.preventDefault();
    if (newCertificate && !agree) {
      tab = isHost ? 'ssl' : 'details';
      error = "Agree to Let's Encrypt's terms to get a certificate.";
      return;
    }
    busy = true;
    error = undefined;
    try {
      if (kind === 'certificates' && upload) {
        const made = await api.npmSave(npmId, kind, { provider: 'other', nice_name: certName.trim() });
        try {
          await api.npmUpload(npmId, made.id, pem);
        } catch (err) {
          await api.npmDelete(npmId, kind, made.id); // don't leave an empty certificate behind
          throw err;
        }
        onsaved();
        onclose();
        return;
      }
      await api.npmSave(npmId, kind, payload(), item?.id);
      onsaved();
      onclose();
    } catch (err) {
      error = (err as Error).message;
      busy = false;
    }
  }
</script>

{#snippet letsEncryptFields()}
  <label>Email for Let's Encrypt <input type="email" required bind:value={email} /></label>
  <label class="check"><input type="checkbox" bind:checked={dnsChallenge} /> Use a DNS challenge (needed for wildcard domains, or when port 80 isn't reachable)</label>
  {#if dnsChallenge}
    <label>DNS provider <input required class="mono" placeholder="e.g. cloudflare" bind:value={dnsProvider} /></label>
    <label>Its credentials <textarea required class="mono" rows="3" placeholder="dns_cloudflare_api_token = …" bind:value={dnsCredentials}></textarea></label>
    <label>Wait for DNS to update <span class="muted small">(seconds, optional)</span> <input type="number" min="0" max="600" bind:value={propagation} /></label>
  {/if}
  <label class="check"><input type="checkbox" bind:checked={agree} /> I agree to the Let's Encrypt terms of service</label>
{/snippet}

<Modal title="{item ? 'Edit' : 'New'} {NAMES[kind]}" {onclose}>
  {#if tabs.length > 1}<Tabs {tabs} bind:selected={tab} />{/if}
  <form id="npm-item" class="form" onsubmit={save} oninvalidcapture={showInvalid}>
    <div class="form" data-tab="details" hidden={tab !== 'details'}>
      {#if isHost || (kind === 'certificates' && !upload)}
        <label>Domain names <input required class="mono" placeholder="jellyfin.home.lab, media.home.lab" bind:value={domains} /></label>
      {/if}
      {#if kind === 'proxy-hosts'}
        <div class="forward">
          <label>Scheme <select bind:value={scheme}><option>http</option><option>https</option></select></label>
          <label>Forward to <input required class="mono" placeholder="192.168.1.20 or a container name" bind:value={host} /></label>
          <label>Port <input required type="number" min="1" max="65535" bind:value={port} /></label>
        </div>
        <label class="check"><input type="checkbox" bind:checked={caching} /> Cache assets</label>
        <label class="check"><input type="checkbox" bind:checked={blockExploits} /> Block common exploits</label>
        <label class="check"><input type="checkbox" bind:checked={websockets} /> Websockets support</label>
        <label>
          Access list
          <select bind:value={accessList}>
            <option value="0">Public: anyone can reach it</option>
            {#each accessLists as list (list.id)}<option value={String(list.id)}>{list.name}</option>{/each}
          </select>
        </label>
      {:else if kind === 'redirection-hosts'}
        <div class="forward">
          <label>Scheme <select bind:value={scheme}><option>auto</option><option>http</option><option>https</option></select></label>
          <label>Redirect to <input required class="mono" placeholder="example.com" bind:value={redirectTo} /></label>
          <label>
            Code
            <select bind:value={httpCode}>
              {#each [['300', 'Multiple choices'], ['301', 'Moved permanently'], ['302', 'Found'], ['303', 'See other'], ['307', 'Temporary'], ['308', 'Permanent']] as [code, label] (code)}
                <option value={code}>{code} {label}</option>
              {/each}
            </select>
          </label>
        </div>
        <label class="check"><input type="checkbox" bind:checked={preservePath} /> Keep the path (/photos goes to example.com/photos)</label>
        <label class="check"><input type="checkbox" bind:checked={blockExploits} /> Block common exploits</label>
      {:else if kind === 'dead-hosts'}
        <p class="muted small">These domains answer with "404 Not Found", for names you own but don't use.</p>
      {:else if kind === 'streams'}
        <label>Incoming port <input required type="number" min="1" max="65535" bind:value={incomingPort} /></label>
        <div class="pair">
          <label>Forward to <input required class="mono" bind:value={forwardingHost} /></label>
          <label>Port <input required type="number" min="1" max="65535" bind:value={forwardingPort} /></label>
        </div>
        <label class="check"><input type="checkbox" bind:checked={tcp} /> TCP</label>
        <label class="check"><input type="checkbox" bind:checked={udp} /> UDP</label>
      {:else if kind === 'access-lists'}
        <label>Name <input required bind:value={listName} /></label>
        <label class="check"><input type="checkbox" bind:checked={satisfyAny} /> Let in anyone who passes either check (otherwise both)</label>
        <label class="check"><input type="checkbox" bind:checked={passAuth} /> Pass the sign-in on to the app</label>
        <h3>Users</h3>
        {#each users as user, i (i)}
          <div class="line">
            <input aria-label="Username" placeholder="Username" bind:value={user.username} />
            <input aria-label="Password" type="password" autocomplete="new-password" placeholder={item ? 'Unchanged' : 'Password'} bind:value={user.password} />
            <button type="button" aria-label="Remove" onclick={() => users.splice(i, 1)}>×</button>
          </div>
        {/each}
        <button type="button" class="add" onclick={() => users.push({ username: '', password: '' })}>Add a user</button>
        <h3>Addresses</h3>
        {#each clients as client, i (i)}
          <div class="line">
            <select aria-label="Allow or deny" bind:value={client.directive}><option value="allow">Allow</option><option value="deny">Deny</option></select>
            <input aria-label="Address" class="mono" placeholder="192.168.1.0/24" bind:value={client.address} />
            <button type="button" aria-label="Remove" onclick={() => clients.splice(i, 1)}>×</button>
          </div>
        {/each}
        <button type="button" class="add" onclick={() => clients.push({ address: '', directive: 'allow' })}>Add an address</button>
      {:else if kind === 'certificates'}
        <div class="segmented" role="radiogroup" aria-label="Where it's from">
          <button type="button" role="radio" aria-checked={!upload} class:on={!upload} onclick={() => (upload = false)}>From Let's Encrypt</button>
          <button type="button" role="radio" aria-checked={upload} class:on={upload} onclick={() => (upload = true)}>Upload my own</button>
        </div>
        {#if upload}
          <label>Name <input required placeholder="e.g. Home CA" bind:value={certName} /></label>
          {#each [['certificate', 'Certificate'], ['certificateKey', 'Private key'], ['intermediate', 'Intermediate certificate (optional)']] as [field, label] (field)}
            {@const f = field as keyof typeof pem}
            <label>
              {label}
              <textarea class="mono" rows="3" required={f !== 'intermediate'} placeholder="-----BEGIN …" bind:value={pem[f]}></textarea>
              <input type="file" accept=".pem,.crt,.key,.cer" onchange={(e) => readFile(e, f)} />
            </label>
          {/each}
        {:else}
          {@render letsEncryptFields()}
        {/if}
      {/if}
    </div>

    {#if kind === 'proxy-hosts'}
      <div class="form" data-tab="locations" hidden={tab !== 'locations'}>
        {#each locations as location, i (i)}
          <div class="location">
            <div class="line">
              <input aria-label="Path" required class="mono" placeholder="/api" bind:value={location.path} />
              <button type="button" aria-label="Remove" onclick={() => locations.splice(i, 1)}>×</button>
            </div>
            <div class="forward">
              <select aria-label="Scheme" bind:value={location.forward_scheme}><option>http</option><option>https</option></select>
              <input aria-label="Forward to" required class="mono" placeholder="Forward to" bind:value={location.forward_host} />
              <input aria-label="Port" required type="number" min="1" max="65535" placeholder="Port" bind:value={location.forward_port} />
            </div>
            <textarea aria-label="Its advanced config" class="mono" rows="2" placeholder="Extra nginx config for this path (optional)" bind:value={location.advanced_config}></textarea>
          </div>
        {:else}
          <p class="muted small">Send some paths somewhere else, like /api to another container.</p>
        {/each}
        <button type="button" class="add" onclick={() => locations.push({ path: '', forward_scheme: 'http', forward_host: '', forward_port: undefined, advanced_config: '' })}>
          Add a location
        </button>
      </div>
    {/if}

    {#if isHost}
      <div class="form" data-tab="ssl" hidden={tab !== 'ssl'}>
        <label>
          Certificate
          <select bind:value={certificate}>
            <option value="0">None: HTTP only</option>
            <option value="new">A new one from Let's Encrypt</option>
            {#each certificates as cert (cert.id)}<option value={String(cert.id)}>{cert.nice_name || cert.domain_names?.join(', ')}</option>{/each}
          </select>
        </label>
        {#if certificate === 'new'}{@render letsEncryptFields()}{/if}
        {#if certificate !== '0'}
          <label class="check"><input type="checkbox" bind:checked={forceSsl} /> Force SSL</label>
          <label class="check"><input type="checkbox" bind:checked={http2} /> HTTP/2</label>
          <label class="check"><input type="checkbox" bind:checked={hsts} /> HSTS</label>
          {#if hsts}<label class="check"><input type="checkbox" bind:checked={hstsSubdomains} /> HSTS for subdomains too</label>{/if}
        {/if}
      </div>
      <div class="form" data-tab="advanced" hidden={tab !== 'advanced'}>
        <label>
          Custom nginx config
          <textarea class="mono" rows="10" placeholder="location / {'{'} … {'}'}" bind:value={advanced}></textarea>
        </label>
        <p class="muted small">Added to this host's server block as it is. A mistake here can stop NPM loading it.</p>
      </div>
    {/if}
    {#if error}<p class="error">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <button onclick={onclose}>Cancel</button>
    <button type="submit" form="npm-item" class="primary" disabled={busy}>
      {busy ? (newCertificate ? 'Getting the certificate…' : 'Saving…') : 'Save'}
    </button>
  {/snippet}
</Modal>

<style>
  .forward {
    display: grid;
    grid-template-columns: 90px 1fr 100px;
    gap: 8px;
  }
  .forward > :global(*),
  .forward input {
    min-width: 0;
  }
  .line {
    display: flex;
    gap: 8px;
  }
  .line input {
    flex: 1;
  }
  .location {
    display: grid;
    gap: 8px;
    padding-bottom: 12px;
    border-bottom: 1px solid var(--line-soft);
  }
  .add {
    justify-self: start;
  }
  h3 {
    margin: 6px 0 0;
  }
</style>
