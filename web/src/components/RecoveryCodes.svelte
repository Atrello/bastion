<!-- Recovery codes, shown once: to keep somewhere safe for when the authenticator app isn't to hand. -->
<script lang="ts">
  let { codes, ondone }: { codes: string[]; ondone: () => void } = $props();
  let copied = $state(false);
</script>

<p>
  Keep these recovery codes somewhere safe, like your password manager. Each one signs you in once if your
  authenticator app isn't to hand. They won't be shown again.
</p>
<ol class="codes mono">
  {#each codes as code (code)}<li>{code}</li>{/each}
</ol>
<div class="row">
  <button
    onclick={async () => {
      await navigator.clipboard.writeText(codes.join('\n'));
      copied = true;
    }}>{copied ? 'Copied' : 'Copy them'}</button
  >
  <span class="grow"></span>
  <button class="primary" onclick={ondone}>I've saved them</button>
</div>

<style>
  .codes {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 6px 24px;
    margin: 0;
    padding: 12px 16px 12px 36px;
    background: #0b0d11;
    border: 1px solid var(--line);
    border-radius: 8px;
  }
</style>
