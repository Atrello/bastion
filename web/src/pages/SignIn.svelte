<!-- Signing in: a password and then a second factor (a code from the authenticator app, a recovery code
     or a passkey), or a passkey on its own. -->
<script lang="ts">
  import AuthCard from '../components/AuthCard.svelte';
  import { api } from '../lib/api';
  import { usePasskey } from '../lib/webauthn';

  let { passkeys, ondone }: { passkeys: boolean; ondone: () => void } = $props();

  let step = $state<'password' | 'second'>('password');
  let methods = $state({ code: false, passkey: false });
  let username = $state('');
  let password = $state('');
  let code = $state('');
  let recovery = $state(false);
  let error = $state<string>();
  let busy = $state(false);

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

  const withPassword = (e: SubmitEvent) => {
    e.preventDefault();
    void attempt(async () => {
      methods = await api.signIn(username.trim(), password);
      password = '';
      step = 'second';
    });
  };

  const withCode = (e: SubmitEvent) => {
    e.preventDefault();
    void attempt(async () => {
      await (recovery ? api.signInWithRecoveryCode(code) : api.signInWithCode(code));
      ondone();
    });
  };

  const withPasskey = () =>
    attempt(async () => {
      await api.passkeyFinish(await usePasskey(await api.passkeyBegin()));
      ondone();
    });
</script>

{#if step === 'password'}
  <AuthCard title="Sign in">
    <form onsubmit={withPassword}>
      <label>Username <input required autocomplete="username webauthn" bind:value={username} /></label>
      <label>Password <input required type="password" autocomplete="current-password" bind:value={password} /></label>
      {#if error}<p class="error">{error}</p>{/if}
      <button type="submit" class="primary" disabled={busy}>Next</button>
    </form>
    {#if passkeys}
      <button disabled={busy} onclick={withPasskey}>Sign in with a passkey</button>
    {/if}
  </AuthCard>
{:else}
  <AuthCard title="Finish signing in">
    {#if methods.code || recovery}
      <form onsubmit={withCode}>
        <label>
          {recovery ? 'A recovery code' : 'The code from your authenticator app'}
          <input required autocomplete="one-time-code" inputmode={recovery ? 'text' : 'numeric'} bind:value={code} />
        </label>
        {#if error}<p class="error">{error}</p>{/if}
        <button type="submit" class="primary" disabled={busy}>Sign in</button>
      </form>
    {:else if error}
      <p class="error">{error}</p>
    {/if}
    {#if methods.passkey && passkeys}
      <button disabled={busy} onclick={withPasskey}>Use a passkey instead</button>
    {/if}
    <button class="link" onclick={() => ((recovery = !recovery), (code = ''))}>
      {recovery ? 'Use the authenticator app' : 'Use a recovery code'}
    </button>
  </AuthCard>
{/if}

<style>
  .link {
    border: 0;
    background: none;
    color: var(--accent);
    font-weight: 400;
    justify-self: start;
    padding: 0;
  }
</style>
