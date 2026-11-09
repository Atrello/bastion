<!-- Your account: password, authenticator app, recovery codes, passkeys, and signing out elsewhere.
     Anything that changes how you sign in asks for your password again. -->
<script lang="ts">
  import Modal from '../components/Modal.svelte';
  import RecoveryCodes from '../components/RecoveryCodes.svelte';
  import { api } from '../lib/api';
  import { ago } from '../lib/format';
  import { load } from '../lib/load.svelte';
  import { createPasskey } from '../lib/webauthn';

  let { passkeys }: { passkeys: boolean } = $props();
  const account = load(api.account);

  // One dialog at a time: which one, and what it needs.
  let dialog = $state<'password' | 'authenticator' | 'codes' | 'passkey' | null>(null);
  let current = $state('');
  let next = $state('');
  let name = $state('');
  let code = $state('');
  let qr = $state<{ secret: string; qrCode: string }>();
  let codes = $state<string[]>([]);
  let error = $state<string>();
  let note = $state<string>();
  let busy = $state(false);

  function open(which: typeof dialog) {
    dialog = which;
    current = next = name = code = '';
    qr = undefined;
    codes = [];
    error = undefined;
  }

  async function attempt(work: () => Promise<void>) {
    error = undefined;
    busy = true;
    try {
      await work();
    } catch (err) {
      error = err instanceof DOMException ? 'The passkey was cancelled.' : (err as Error).message;
    } finally {
      busy = false;
    }
  }

  const done = (message: string) => {
    dialog = null;
    note = message;
    void account.reload();
  };
</script>

<div class="page-head"><h1>Your account</h1></div>
{#if note}<p class="note">{note}</p>{/if}
{#if account.error}<p class="error">{account.error}</p>{/if}

{#if account.data}
  {@const a = account.data}
  <div class="sections">
    <section class="card">
      <h2>Password</h2>
      <p class="muted">Signed in as <b>{a.username}</b>.</p>
      <button onclick={() => open('password')}>Change password</button>
    </section>

    <section class="card">
      <h2>Authenticator app</h2>
      <p class="muted">{a.hasAuthenticator ? 'Set up. Codes from it finish signing in.' : 'Not set up.'}</p>
      <button onclick={() => open('authenticator')}>{a.hasAuthenticator ? 'Move to a new app' : 'Set one up'}</button>
    </section>

    <section class="card">
      <h2>Recovery codes</h2>
      <p class="muted">{a.recoveryCodesLeft} unused. Each signs you in once without the authenticator app.</p>
      <button onclick={() => open('codes')}>Make new codes</button>
    </section>

    <section class="card wide">
      <div class="row">
        <h2 class="grow">Passkeys</h2>
        {#if passkeys}<button onclick={() => open('passkey')}>Add a passkey</button>{/if}
      </div>
      {#if !passkeys}
        <p class="muted">Passkeys need the panel to be reached by a name, like https://bastion.home.lab, not an IP address.</p>
      {:else if a.passkeys.length === 0}
        <p class="muted">None yet. A passkey signs you in with your fingerprint, face or a security key, and can't be phished.</p>
      {:else}
        <table>
          <thead><tr><th>Name</th><th>Added</th><th>Last used</th><th></th></tr></thead>
          <tbody>
            {#each a.passkeys as key (key.id)}
              <tr>
                <td><b>{key.name}</b></td>
                <td class="muted">{ago(key.createdAt)}</td>
                <td class="muted">{key.lastUsedAt ? ago(key.lastUsedAt) : 'Never'}</td>
                <td class="end">
                  <button class="danger" onclick={() => attempt(async () => { await api.deletePasskey(key.id); done(`Removed ${key.name}.`); })}>Remove</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <section class="card wide row">
      <div class="grow">
        <h2>Other browsers</h2>
        <p class="muted">Sign out everywhere else you're signed in, like a browser you've left signed in.</p>
      </div>
      <button onclick={() => attempt(async () => { await api.signOutEverywhere(); done('Signed out everywhere else.'); })}>Sign out everywhere else</button>
    </section>
  </div>
{/if}

{#if dialog === 'password'}
  <Modal title="Change password" onclose={() => (dialog = null)}>
    <form id="dialog-form" class="form" onsubmit={(e) => { e.preventDefault(); void attempt(async () => { await api.changePassword(current, next); done('Password changed. Other browsers have been signed out.'); }); }}>
      <label>Current password <input required type="password" autocomplete="current-password" bind:value={current} /></label>
      <label>New password <input required type="password" minlength="12" autocomplete="new-password" bind:value={next} /></label>
      {#if error}<p class="error">{error}</p>{/if}
    </form>
    {#snippet footer()}<button type="submit" form="dialog-form" class="primary" disabled={busy}>Change password</button>{/snippet}
  </Modal>
{:else if dialog === 'authenticator'}
  <Modal title="Authenticator app" onclose={() => (dialog = null)}>
    {#if codes.length}
      <RecoveryCodes {codes} ondone={() => done('Authenticator app changed.')} />
    {:else if !qr}
      <form id="dialog-form" class="form" onsubmit={(e) => { e.preventDefault(); void attempt(async () => { qr = await api.authenticatorBegin(current); }); }}>
        <p class="muted">Your current app stops working once the new one is set up.</p>
        <label>Your password <input required type="password" autocomplete="current-password" bind:value={current} /></label>
        {#if error}<p class="error">{error}</p>{/if}
      </form>
    {:else}
      <form id="dialog-form" class="form" onsubmit={(e) => { e.preventDefault(); void attempt(async () => { codes = (await api.authenticatorFinish(code)).recoveryCodes; }); }}>
        <img class="qr" src={qr.qrCode} alt="QR code for your authenticator app" width="200" height="200" />
        <p class="muted small">Or enter this key by hand: <span class="mono">{qr.secret}</span></p>
        <label>The 6-digit code it shows <input required inputmode="numeric" autocomplete="one-time-code" maxlength="6" bind:value={code} /></label>
        {#if error}<p class="error">{error}</p>{/if}
      </form>
    {/if}
    {#snippet footer()}
      {#if !codes.length}<button type="submit" form="dialog-form" class="primary" disabled={busy}>{qr ? 'Finish' : 'Next'}</button>{/if}
    {/snippet}
  </Modal>
{:else if dialog === 'codes'}
  <Modal title="New recovery codes" onclose={() => (dialog = null)}>
    {#if codes.length}
      <RecoveryCodes {codes} ondone={() => done('New recovery codes made. The old ones no longer work.')} />
    {:else}
      <form id="dialog-form" class="form" onsubmit={(e) => { e.preventDefault(); void attempt(async () => { codes = (await api.newRecoveryCodes(current)).recoveryCodes; }); }}>
        <p class="muted">Your old codes stop working.</p>
        <label>Your password <input required type="password" autocomplete="current-password" bind:value={current} /></label>
        {#if error}<p class="error">{error}</p>{/if}
      </form>
    {/if}
    {#snippet footer()}
      {#if !codes.length}<button type="submit" form="dialog-form" class="primary" disabled={busy}>Make new codes</button>{/if}
    {/snippet}
  </Modal>
{:else if dialog === 'passkey'}
  <Modal title="Add a passkey" onclose={() => (dialog = null)}>
    <form id="dialog-form" class="form" onsubmit={(e) => { e.preventDefault(); void attempt(async () => { await api.passkeyRegisterFinish(await createPasskey(await api.passkeyRegisterBegin(name, current))); done(`Added ${name}.`); }); }}>
      <label>Name <input required maxlength="60" placeholder="e.g. Phone" bind:value={name} /></label>
      <label>Your password <input required type="password" autocomplete="current-password" bind:value={current} /></label>
      {#if error}<p class="error">{error}</p>{/if}
    </form>
    {#snippet footer()}<button type="submit" form="dialog-form" class="primary" disabled={busy}>Add passkey</button>{/snippet}
  </Modal>
{/if}

<style>
  .sections {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 16px;
  }
  .sections .card {
    display: grid;
    gap: 10px;
    align-content: start;
  }
  .sections .card p {
    margin: 0;
  }
  .sections button {
    justify-self: start;
  }
  .wide {
    grid-column: 1 / -1;
  }
  .end {
    text-align: right;
  }
  .note {
    color: var(--green);
    margin: -8px 0 12px;
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
  .qr {
    justify-self: center;
    border-radius: 8px;
    background: #fff;
    padding: 8px;
  }
</style>
