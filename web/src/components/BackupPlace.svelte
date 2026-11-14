<!-- Where a backup goes: the server it's made on, or one of the backup storages (S3). With `usual`, the
     first choice is "wherever the settings say" (null), named so it's clear where that is. -->
<script lang="ts">
  import type { BackupStore } from '../lib/types';

  let {
    value = $bindable(),
    stores,
    server,
    usual,
  }: {
    /** 0 for the server, a storage's ID, or null for the usual place. */
    value: number | null;
    stores: BackupStore[];
    /** The server's name: "On atlas". */
    server: string;
    /** Where the usual place is, for its choice; left out, there's no such choice. */
    usual?: string;
  } = $props();
</script>

<label>
  Kept on
  <select bind:value={() => value ?? -1, (v: number) => (value = v === -1 ? null : v)}>
    {#if usual !== undefined}<option value={-1}>The usual place ({usual})</option>{/if}
    <option value={0}>{server}</option>
    {#each stores as s (s.id)}<option value={s.id}>{s.name} (S3)</option>{/each}
  </select>
</label>
