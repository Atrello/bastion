<!-- The activity log: who did what, newest first, with whether it worked. Search it, or see one person's.
     Kept for 90 days; only the owner sees it. -->
<script lang="ts">
  import { api } from '../lib/api';
  import { ago } from '../lib/format';
  import type { ActivityEntry } from '../lib/types';

  let entries = $state<ActivityEntry[]>([]);
  let users = $state<string[]>([]);
  let user = $state('');
  let q = $state('');
  let error = $state<string>();
  let more = $state(false);
  let loading = $state(false);

  async function load(older = false) {
    loading = true;
    error = undefined;
    try {
      const page = await api.activity({ user, q: q.trim(), before: older ? entries.at(-1)?.id : undefined });
      entries = older ? [...entries, ...page.entries] : page.entries;
      users = page.users;
      more = page.entries.length === 100;
    } catch (err) {
      error = (err as Error).message;
    } finally {
      loading = false;
    }
  }

  // A moment after typing stops, or straight away when the person changes.
  $effect(() => {
    void user;
    void q;
    const timer = setTimeout(() => void load(), 250);
    return () => clearTimeout(timer);
  });

  const sentence = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);
  const when = (iso: string) => new Date(iso).toLocaleString('en-GB', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
</script>

<div class="page-head">
  <h1>Activity</h1>
  <input class="search" type="search" placeholder="Search" aria-label="Search the activity log" bind:value={q} />
  <select aria-label="Whose" bind:value={user}>
    <option value="">Everyone</option>
    {#each users as u (u)}<option value={u}>{u}</option>{/each}
  </select>
</div>
{#if error}<p class="error">{error}</p>{/if}

<div class="card table-card">
  <table>
    <thead><tr><th>When</th><th>Who</th><th>What</th><th>Details</th></tr></thead>
    <tbody>
      {#each entries as e (e.id)}
        <tr>
          <td class="nowrap" title={when(e.at)}>{ago(e.at)}</td>
          <td><b>{e.user}</b></td>
          <td>{sentence(e.action)} {#if !e.ok}<span class="badge red">Failed</span>{/if}</td>
          <td class="details muted">
            {#each Object.entries(e.details) as [k, v] (k)}<span><span class="key">{k}</span> {v}</span>{/each}
          </td>
        </tr>
      {:else}
        {#if !loading}<tr><td colspan="4" class="empty">{q || user ? 'Nothing matches.' : 'Nothing yet.'}</td></tr>{/if}
      {/each}
    </tbody>
  </table>
</div>
{#if more}<p class="more"><button onclick={() => load(true)} disabled={loading}>Show older</button></p>{/if}
<p class="muted small">Kept for 90 days.</p>

<style>
  .search {
    width: min(260px, 100%);
  }
  .nowrap {
    white-space: nowrap;
  }
  .details {
    white-space: normal;
    overflow-wrap: anywhere;
  }
  .details > span {
    margin-right: 12px;
    display: inline-block;
  }
  .key {
    color: var(--soft);
  }
  .more {
    text-align: center;
  }
</style>
