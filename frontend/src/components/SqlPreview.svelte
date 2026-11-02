<script lang="ts">
  // Read-only SQL with syntax colors (apply previews). Statements are
  // shown one after another, each ending with ';'.
  import { highlightCode, tagHighlighter, tags as t } from '@lezer/highlight'
  import { sqlDialect } from '../lib/cm'

  let { sql, driver = '' }: { sql: string[]; driver?: string } = $props()

  const hl = tagHighlighter([
    { tag: [t.keyword, t.operatorKeyword, t.modifier, t.bool, t.null], class: 'k' },
    { tag: [t.string, t.special(t.string)], class: 's' },
    { tag: t.number, class: 'n' },
    { tag: [t.comment, t.lineComment, t.blockComment], class: 'c' },
    { tag: [t.typeName, t.standard(t.name)], class: 't' },
    { tag: [t.function(t.variableName), t.function(t.name), t.special(t.name), t.quote], class: 'f' },
    { tag: [t.operator, t.punctuation, t.bracket], class: 'o' },
  ])

  type Seg = { text: string; cls: string } | null // null = line break

  let segs = $derived.by(() => {
    const code = sql.map((s) => s.trim().replace(/;$/, '') + ';').join('\n')
    const tree = sqlDialect(driver).language.parser.parse(code)
    const out: Seg[] = []
    highlightCode(
      code,
      tree,
      hl,
      (text, cls) => out.push({ text, cls }),
      () => out.push(null),
    )
    return out
  })
</script>

<pre class="sql mono">{#each segs as s, i (i)}{#if s}<span class={s.cls}>{s.text}</span>{:else}{'\n'}{/if}{/each}</pre>

<style>
  .sql {
    max-height: 340px;
    overflow: auto;
    margin: 0;
    padding: 10px;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg);
    color: var(--syn-ident);
    white-space: pre-wrap;
    user-select: text;
    -webkit-user-select: text;
  }
  .k {
    color: var(--syn-keyword);
  }
  .s {
    color: var(--syn-string);
  }
  .n {
    color: var(--syn-number);
  }
  .c {
    color: var(--syn-comment);
    font-style: italic;
  }
  .t {
    color: var(--syn-type);
  }
  .f {
    color: var(--syn-fn);
  }
  .o {
    color: var(--syn-op);
  }
</style>
