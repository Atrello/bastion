<!-- "Expose this": a proxy host in NPM for one of a container's published ports, at a domain, with a
     Let's Encrypt certificate if wanted. It points at the server's address and the port Docker publishes. -->
<script lang="ts">
  import Modal from './Modal.svelte';
  import { api } from '../lib/api';
  import { load } from '../lib/load.svelte';
  import type { Container } from '../lib/types';

  let { container, onclose }: { container: Container; onclose: () => void } = $props();

  const instances = load(api.npmInstances);
  const servers = load(api.servers);

  /** The TCP ports Docker publishes on the server's network (not ones bound to 127.0.0.1 only). */
  const ports = $derived([
    ...new Set(
      [...container.ports.matchAll(/(\S+):(\d+)->\d+\/tcp/g)].filter(([, address]) => !address.startsWith('127.')).map(([, , port]) => Number(port)),
    ),
  ]);

  let npmId = $state('');
  let domain = $state('');
  let port = $state('');
  let address = $state('');
  let certificate = $state(true);
  let email = $state('');
  let agree = $state(false);
  let busy = $state(false);
  let error = $state<string>();
  let made = $state<string>();

  $effect(() => {
    if (!npmId && instances.data?.length) npmId = String(instances.data[0].id);
    if (!port && ports.length) port = String(ports[0]);
    const server = servers.data?.find((s) => s.id === container.serverId);
    if (!address && server) address = server.address || server.hostname;
  });

  async function expose(e: SubmitEvent) {
    e.preventDefault();
    busy = true;
    error = undefined;
    try {
      const base = {
        domain_names: [domain.trim().toLowerCase()],
        forward_scheme: 'http',
        forward_host: address.trim(),
        forward_port: Number(port),
        certificate_id: certificate ? 'new' : 0,
        ssl_forced: certificate,
      };
      // Fields for NPM and NPMplus alike: the panel keeps only those this NPM accepts.
      await api.npmSave(Number(npmId), 'proxy-hosts', {
        ...base,
        block_exploits: true,
        allow_websocket_upgrade: true,
        access_list_id: 0,
        npmplus_access_list_ids: [],
        npmplus_access_list_type: 'public',
        http2_support: certificate,
        npmplus_http3_support: certificate,
        meta: certificate ? { letsencrypt_email: email.trim(), letsencrypt_agree: agree, dns_challenge: false } : { letsencrypt_agree: false, dns_challenge: false },
      });
      made = domain.trim().toLowerCase();
    } catch (err) {
      error = (err as Error).message;
    } finally {
      busy = false;
    }
  }
</script>

<Modal title="Expose {container.service || container.name}" {onclose}>
  {#if made}
    <p>
      <a href="{certificate ? 'https' : 'http'}://{made}" target="_blank" rel="noreferrer">{made}</a> now points at {address}:{port}.
      Its DNS needs to point at NPM, if it doesn't already.
    </p>
  {:else if instances.data && !instances.data.length}
    <p class="muted">Link Nginx Proxy Manager first, from <a href="/proxy">Proxy</a>.</p>
  {:else if !ports.length}
    <p class="muted">This container doesn't publish a TCP port on the server's network. Add one under <code>ports:</code> in the compose file first.</p>
  {:else}
    <form id="expose" class="form" onsubmit={expose}>
      <label>Domain <input required class="mono" placeholder="jellyfin.home.lab" bind:value={domain} /></label>
      <div class="pair">
        <label>Server's address <input required class="mono" bind:value={address} /></label>
        <label>
          Port
          <select bind:value={port}>{#each ports as p (p)}<option value={String(p)}>{p}</option>{/each}</select>
        </label>
      </div>
      {#if (instances.data?.length ?? 0) > 1}
        <label>
          In
          <select bind:value={npmId}>{#each instances.data ?? [] as i (i.id)}<option value={String(i.id)}>{i.name}</option>{/each}</select>
        </label>
      {/if}
      <label class="check"><input type="checkbox" bind:checked={certificate} /> Get a Let's Encrypt certificate and force HTTPS</label>
      {#if certificate}
        {#if instances.data?.find((i) => i.id === Number(npmId))?.flavour !== 'npmplus'}
          <label>Email for Let's Encrypt <input required type="email" bind:value={email} /></label>
        {/if}
        <label class="check"><input type="checkbox" required bind:checked={agree} /> I agree to the Let's Encrypt terms of service</label>
      {/if}
      <p class="muted small">Websockets and exploit blocking are on. Change anything else from Proxy afterwards.</p>
      {#if error}<p class="error">{error}</p>{/if}
    </form>
  {/if}
  {#snippet footer()}
    {#if made}
      <a class="button" href="/proxy">See it in Proxy</a>
      <button class="primary" onclick={onclose}>Done</button>
    {:else}
      <button onclick={onclose}>Cancel</button>
      <button type="submit" form="expose" class="primary" disabled={busy || !ports.length}>{busy ? (certificate ? 'Getting the certificate…' : 'Making it…') : 'Expose'}</button>
    {/if}
  {/snippet}
</Modal>
