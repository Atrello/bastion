<!-- A code editor (CodeMirror) for compose files and .env files: YAML highlighting for compose. -->
<script lang="ts">
  import { yaml } from '@codemirror/lang-yaml';
  import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
  import { EditorState } from '@codemirror/state';
  import { tags } from '@lezer/highlight';
  import { EditorView, basicSetup } from 'codemirror';

  let { value = $bindable(), language = 'text', label }: { value: string; language?: 'yaml' | 'text'; label: string } = $props();
  let host: HTMLDivElement;

  const theme = EditorView.theme(
    {
      '&': { backgroundColor: '#0b0d0f', color: '#d8d2c8', fontSize: '12.5px', height: '100%' },
      '.cm-content': { fontFamily: 'var(--mono)', caretColor: '#f08c4f' },
      '.cm-gutters': { backgroundColor: '#0b0d0f', color: '#4d5156', border: 'none' },
      '.cm-activeLine, .cm-activeLineGutter': { backgroundColor: '#15181b' },
      '&.cm-focused .cm-selectionBackground, .cm-selectionBackground': { backgroundColor: '#3a2a20 !important' },
    },
    { dark: true },
  );
  // Colours for the code itself, readable on the dark background (CodeMirror's own are for light ones).
  const colours = HighlightStyle.define([
    { tag: [tags.propertyName, tags.definition(tags.propertyName)], color: '#f0a06a' },
    { tag: [tags.string, tags.special(tags.string)], color: '#a9d18e' },
    { tag: [tags.number, tags.bool, tags.null], color: '#e8c547' },
    { tag: tags.comment, color: '#6b6862', fontStyle: 'italic' },
    { tag: [tags.keyword, tags.labelName, tags.typeName], color: '#d4a5e8' },
    { tag: [tags.punctuation, tags.separator], color: '#8d8a84' },
  ]);

  $effect(() => {
    const view = new EditorView({
      parent: host,
      state: EditorState.create({
        doc: value,
        extensions: [
          basicSetup,
          theme,
          syntaxHighlighting(colours),
          language === 'yaml' ? yaml() : [],
          EditorView.contentAttributes.of({ 'aria-label': label }),
          EditorView.updateListener.of((u) => {
            if (u.docChanged) value = u.state.doc.toString();
          }),
        ],
      }),
    });
    return () => view.destroy();
  });
</script>

<div class="editor" bind:this={host}></div>

<style>
  .editor {
    height: 520px;
    border: 1px solid var(--line);
    border-radius: 8px;
    overflow: hidden;
  }
  .editor :global(.cm-editor) {
    height: 100%;
  }
</style>
