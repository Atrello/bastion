<!-- First-run setup: the owner's account, from the one-time link in the panel's log. A second factor
     (an authenticator app or a passkey) is set up before the account is saved. -->
<script lang="ts">
  import AuthCard from '../components/AuthCard.svelte';
  import RecoveryCodes from '../components/RecoveryCodes.svelte';
  import { api } from '../lib/api';
  import { createPasskey } from '../lib/webauthn';

  // invited: a friend's invite link to a shared game server, rather than the owner's setup link.
  let { passkeys, ondone, invited = false }: { passkeys: boolean; ondone: () => void; invited?: boolean } = $props();

  const token = location.hash.slice(1);
  let step = $state<'account' | 'factor' | 'authenticator' | 'codes'>('account');
  let username = $state('');
  let password = $state('');
  let repeat = $state('');
  let qr = $state<{ secret: string; qrCode: string }>();
  let code = $state('');
  let codes = $state<string[]>([]);
  let error = $state<string>();
  let busy = $state(false);

  async function attempt(work: () => Promise<void>) {
    error = undefined;
    busy = true;
    try {
      await work();
    } catch (err) {
      error = (err as Error).message;
    } finally {
      busy = false;
    }
  }

  const account = (e: SubmitEvent) => {
    e.preventDefault();
    if (password !== repeat) {
      error = "The passwords don't match.";
      return;
    }
    void attempt(async () => {
      await api.setup(token, username.trim(), password);
      step = 'factor';
    });
  };

  const useAuthenticator = () =>
    attempt(async () => {
      qr = await api.authenticatorBegin();
      step = 'authenticator';
    });

  const usePasskey = () =>
    attempt(async () => {
      await api.passkeyRegisterFinish(await createPasskey(await api.passkeyRegisterBegin('First passkey')));
      ondone();
    });

  const confirm = (e: SubmitEvent) => {
    e.preventDefault();
    void attempt(async () => {
      codes = (await api.authenticatorFinish(code)).recoveryCodes;
      step = 'codes';
    });
  };
</script>

{#if !token}
  <AuthCard title="Set up Bastion">
    <p>Open the setup link from the panel's log to make the owner's account. It looks like
      <span class="mono">https://…/setup#…</span></p>
  </AuthCard>
{:else if step === 'account'}
  <AuthCard title={invited ? "You've been invited" : 'Set up Bastion'}>
    <p class="muted">
      {invited ? 'A game server has been shared with you. Make an account to use it.' : "Make the owner's account."}
      You'll add a second way to sign in next: it's required.
    </p>
    <form onsubmit={account}>
      <label>Username <input required autocomplete="username" bind:value={username} /></label>
      <label>Password <input required type="password" minlength="12" autocomplete="new-password" bind:value={password} /></label>
      <label>Password again <input required type="password" autocomplete="new-password" bind:value={repeat} /></label>
      <p class="muted small">At least 12 characters. A few random words is good.</p>
      {#if error}<p class="error">{error}</p>{/if}
      <button type="submit" class="primary" disabled={busy}>Next</button>
    </form>
  </AuthCard>
{:else if step === 'factor'}
  <AuthCard title="A second way to sign in">
    <p class="muted">Each sign-in needs your password and one of these. You can add the other later.</p>
    <button class="primary" disabled={busy} onclick={useAuthenticator}>Use an authenticator app</button>
    {#if passkeys}
      <button disabled={busy} onclick={usePasskey}>Use a passkey (fingerprint, face or security key)</button>
    {/if}
    {#if error}<p class="error">{error}</p>{/if}
  </AuthCard>
{:else if step === 'authenticator' && qr}
  <AuthCard title="Scan this with your authenticator app">
    <img class="qr" src={qr.qrCode} alt="QR code for your authenticator app" width="200" height="200" />
    <p class="muted small">Or enter this key by hand: <span class="mono">{qr.secret}</span></p>
    <form onsubmit={confirm}>
      <label>The 6-digit code it shows <input required inputmode="numeric" autocomplete="one-time-code" maxlength="6" bind:value={code} /></label>
      {#if error}<p class="error">{error}</p>{/if}
      <button type="submit" class="primary" disabled={busy}>Finish setting up</button>
    </form>
  </AuthCard>
{:else if step === 'codes'}
  <AuthCard title="Your recovery codes">
    <RecoveryCodes {codes} ondone={() => ondone()} />
  </AuthCard>
{/if}

<style>
  .qr {
    justify-self: center;
    border-radius: 8px;
    background: #fff;
    padding: 8px;
  }
</style>
