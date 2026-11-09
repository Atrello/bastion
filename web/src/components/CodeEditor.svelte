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
      '&': { backgroundColor: '#0b0d11', color: '#c9d1de', fontSize: '12.5px', height: '100%' },
      '.cm-content': { fontFamily: 'var(--mono)', caretColor: '#fff' },
      '.cm-gutters': { backgroundColor: '#0b0d11', color: '#4b5263', border: 'none' },
      '.cm-activeLine, .cm-activeLineGutter': { backgroundColor: '#141821' },
      '&.cm-focused .cm-selectionBackground, .cm-selectionBackground': { backgroundColor: '#25324f !important' },
    },
    { dark: true },
  );
  // Colours for the code itself, readable on the dark background (CodeMirror's own are for light ones).
  const colours = HighlightStyle.define([
    { tag: [tags.propertyName, tags.definition(tags.propertyName)], color: '#7aa2ff' },
    { tag: [tags.string, tags.special(tags.string)], color: '#9fd18b' },
    { tag: [tags.number, tags.bool, tags.null], color: '#e6a23c' },
    { tag: tags.comment, color: '#5d6678', fontStyle: 'italic' },
    { tag: [tags.keyword, tags.labelName, tags.typeName], color: '#c792ea' },
    { tag: [tags.punctuation, tags.separator], color: '#8b93a3' },
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
