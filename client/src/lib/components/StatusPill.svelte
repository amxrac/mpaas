<script lang="ts">
  import type { DeploymentStatus } from '$lib/types';

  interface Props {
    status: DeploymentStatus;
  }

  let { status }: Props = $props();

  const STATUS_CLASSES: Record<DeploymentStatus, string> = {
    running: 'bg-green-500/15 text-green-400',
    building: 'bg-yellow-500/15 text-yellow-400',
    deploying: 'bg-yellow-500/15 text-yellow-400',
    failed: 'bg-red-500/15 text-red-400',
    pending: 'bg-gray-500/15 text-gray-400',
    stopped: 'bg-gray-500/15 text-gray-400',
  };

  let classes = $derived(STATUS_CLASSES[status] ?? 'bg-gray-500/15 text-gray-400');
</script>

<span class="whitespace-nowrap rounded px-2 py-0.5 text-xs {classes}">{status}</span>
