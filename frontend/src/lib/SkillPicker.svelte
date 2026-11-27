<script lang="ts">
  import type { SkillDef } from './types';

  let { value = $bindable(''), available, code }: {
    value?: string;
    available: SkillDef[];
    code: string;
  } = $props();

  let search = $state('');

  const selected = $derived(value.split(',').map((x) => x.trim()).filter(Boolean));
  const query = $derived(search.trim().toLowerCase());
  const results = $derived(available
    .filter((s) => !selected.some((name) => name.toLowerCase() === s.name.toLowerCase()))
    .filter((s) => !query || s.name.toLowerCase().includes(query) || (s.description || '').toLowerCase().includes(query))
    .slice(0, 8));
  const customName = $derived(search.trim());
  const showCustom = $derived(customName !== ''
    && !available.some((s) => s.name.toLowerCase() === customName.toLowerCase())
    && !selected.some((name) => name.toLowerCase() === customName.toLowerCase()));

  function setSelected(names: string[]) {
    value = names.join(', ');
  }

  function add(name: string) {
    if (selected.some((n) => n.toLowerCase() === name.toLowerCase())) return;
    setSelected([...selected, name]);
    search = '';
  }

  function remove(name: string) {
    setSelected(selected.filter((n) => n !== name));
  }
</script>

<div class="skill-picker">
  {#if selected.length}
    <div class="skill-chips">
      {#each selected as name (name)}
        <span class="skill-chip">{name}<button class="skill-chip-remove" type="button" aria-label={`Hapus skill ${name}`} onclick={() => remove(name)}>✕</button></span>
      {/each}
    </div>
  {/if}
  <input aria-label={`Cari skill ${code}`} data-skill-search={code} maxlength="80" placeholder="Cari skill lalu tekan untuk menempel…" autocomplete="off" spellcheck="false" bind:value={search}>
  {#if search.trim()}
    <div class="skill-results">
      {#each results as skill (skill.name)}
        <button class="skill-result" type="button" aria-label={`Tambah skill ${skill.name}`} title={skill.usage ? `Dipakai ${skill.usage}×` : undefined} onclick={() => add(skill.name)}>
          <strong>{skill.name}</strong>{#if skill.usage}<span class="skill-source">{skill.usage}×</span>{/if}{#if skill.source}<span class="skill-source">{skill.source}</span>{/if}{#if skill.description}<small>{skill.description}</small>{/if}
        </button>
      {/each}
      {#if showCustom}
        <button class="skill-result" type="button" aria-label={`Tambah skill ${customName}`} onclick={() => add(customName)}>
          <strong>+ Tambah “{customName}”</strong><small>Nama manual (file SKILL.md menyusul)</small>
        </button>
      {/if}
      {#if !results.length && !showCustom}
        <p class="skill-empty">Tidak ada skill yang cocok.</p>
      {/if}
    </div>
  {/if}
</div>
