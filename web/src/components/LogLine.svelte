<!-- One line of a log or console, with its terminal colours shown as colours. -->
<script lang="ts">
  import { colourPieces } from '../lib/ansi';

  let { line }: { line: string } = $props();
  const pieces = $derived(line.includes('\x1b') ? colourPieces(line) : [{ text: line, className: '' }]);
</script>

{#each pieces as piece, i (i)}<span class={piece.className}>{piece.text}</span>{/each}
