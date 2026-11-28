<script lang="ts">
  import type { Notice } from './types';

  let { notice, target }: { notice: Notice | undefined; target: string | null } = $props();
</script>

<div class={['toast', { error: notice?.error }]} role="status" hidden={!notice || notice.target !== target}>{notice?.target === target ? notice.text : ''}</div>
