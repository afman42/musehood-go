<script lang="ts">
  let { target, src, label, onChoose }: {
    target: string;
    src: string;
    label: string;
    onChoose: (target: string) => void;
  } = $props();
</script>

<button class="avatar-upload" type="button" data-upload={target} aria-label={label} onclick={() => onChoose(target)}>
  <img data-avatar-target={target} {src} alt="" width="48" height="48"><span>Ganti</span>
</button>
