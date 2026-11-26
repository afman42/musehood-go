<script lang="ts">
  import { cachedHighlight, highlightCode, type CodeToken } from './highlight';

  let { code, lang }: {
    code: string;
    lang: string;
  } = $props();

  // Render polos dulu (tanpa lompatan layout), lalu tukar ke token
  // berwarna begitu modul bahasanya selesai dimuat malas.
  let tokens = $state<CodeToken[] | undefined>(undefined);

  $effect(() => {
    const currentCode = code;
    const currentLang = lang;
    const cached = cachedHighlight(currentCode, currentLang);
    tokens = cached;
    if (cached || !currentLang) return;
    let cancelled = false;
    highlightCode(currentCode, currentLang).then(result => {
      if (!cancelled && result) tokens = result;
    });
    return () => { cancelled = true; };
  });
</script>

<pre class="md-code"><code>{#if tokens}{#each tokens as token, i (i)}{#if token.type}<span class="shj-syn-{token.type}">{token.text}</span>{:else}{token.text}{/if}{/each}{:else}{code}{/if}</code></pre>
