<script lang="ts">
  import { highlightParts, splitMentions } from './format';
  import { parseMarkdown, type Block, type Inline } from './markdown';
  import type { Seat } from './types';

  let { text, seats, query, onMentionClick }: {
    text: string;
    seats: Seat[];
    query: string;
    onMentionClick: (seat: Seat) => void;
  } = $props();

  const blocks = $derived(parseMarkdown(text));
</script>

{#snippet plainText(value: string)}{#each value.split('\n') as line, li (li)}{#if li > 0}<br>{/if}{#each highlightParts(line, query) as part, pi (pi)}{#if typeof part === 'string'}{part}{:else}<mark class="search-mark">{part.hit}</mark>{/if}{/each}{/each}{/snippet}

{#snippet inlines(nodes: Inline[])}{#each nodes as node, ni (ni)}{#if node.type === 'text'}{#each splitMentions(node.text, seats) as segment, si (si)}{#if segment.seat}<button class="mention-link" type="button" title="Lihat info {segment.seat.name}" onclick={() => { const mentioned = segment.seat; if (mentioned) onMentionClick(mentioned); }}>{segment.text}</button>{:else}{@render plainText(segment.text)}{/if}{/each}{:else if node.type === 'strong'}<strong>{@render inlines(node.children)}</strong>{:else if node.type === 'em'}<em>{@render inlines(node.children)}</em>{:else if node.type === 'del'}<del>{@render inlines(node.children)}</del>{:else if node.type === 'code'}<code class="md-inline-code">{node.text}</code>{:else if node.type === 'link'}<a href={node.href} target="_blank" rel="noopener noreferrer">{node.text}</a>{/if}{/each}{/snippet}

{#snippet blockList(list: Block[])}{#each list as block, bi (bi)}{#if block.type === 'p'}<p class="md-p">{@render inlines(block.children)}</p>{:else if block.type === 'h'}<p class="md-h md-h{block.level}">{@render inlines(block.children)}</p>{:else if block.type === 'code'}<pre class="md-code"><code>{block.text}</code></pre>{:else if block.type === 'quote'}<blockquote class="md-quote">{@render blockList(block.blocks)}</blockquote>{:else if block.type === 'ul'}<ul class="md-list">{#each block.items as item, ii (ii)}<li>{@render inlines(item)}</li>{/each}</ul>{:else if block.type === 'ol'}<ol class="md-list">{#each block.items as item, ii (ii)}<li>{@render inlines(item)}</li>{/each}</ol>{:else if block.type === 'hr'}<hr class="md-hr">{/if}{/each}{/snippet}

{@render blockList(blocks)}
