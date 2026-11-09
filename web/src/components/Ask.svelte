<!-- The app's own dialog for questions (lib/ask.svelte.ts), in place of the browser's pop-ups. -->
<script lang="ts">
  import Modal from './Modal.svelte';
  import { asking } from '../lib/ask.svelte';

  let value = $state('');
  $effect(() => {
    value = asking.question?.input?.value ?? '';
  });
  const q = $derived(asking.question);
  const ready = $derived(!q?.input || ((!q.input.required || value.trim() !== '') && (!q.input.mustMatch || value === q.input.mustMatch)));
</script>

{#if q}
  <Modal title={q.title} onclose={() => asking.answer?.(null)}>
    <form
      id="ask"
      class="form"
      onsubmit={(e) => {
        e.preventDefault();
        if (ready) asking.answer?.(q.input ? value.trim() : '');
      }}
    >
      {#if q.message}<p>{q.message}</p>{/if}
      {#if q.input}
        <label>
          {q.input.label}
          <!-- svelte-ignore a11y_autofocus -->
          <input autofocus placeholder={q.input.placeholder} bind:value />
        </label>
      {/if}
    </form>
    {#snippet footer()}
      <button onclick={() => asking.answer?.(null)}>Cancel</button>
      <button type="submit" form="ask" class={q.danger ? 'danger solid' : 'primary'} disabled={!ready}>{q.confirm}</button>
    {/snippet}
  </Modal>
{/if}
