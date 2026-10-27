<script lang="ts">
  import { tick } from 'svelte';
  import type { Deployment, LogEntry } from '$lib/types';
  import { streamDeploymentLogs } from '$lib/api';
  import { getDisplayName } from '$lib/utils';
  import StatusPill from './StatusPill.svelte';
  import { SvelteSet } from 'svelte/reactivity';

  interface Props {
    deploymentId: string | null;
    deployment: Deployment | null;
    stopping: boolean;
    onstop: (id: string) => void;
  }

  let { deploymentId, deployment, stopping, onstop }: Props = $props();

  const FINISHED = new Set(['running', 'failed', 'stopped']);

  let logs = $state<LogEntry[]>([]);
  let connection = $state<'connecting' | 'open' | 'closed'>('closed');

  let container: HTMLElement | undefined = $state();

  const seen = new SvelteSet<string>();

  const STICKY_THRESHOLD = 40;

  $effect(() => {
    const id = deploymentId;

    logs = [];
    seen.clear();
    connection = 'closed';
    if (!id) return;

    connection = 'connecting';
    const es = streamDeploymentLogs(id);

    es.onopen = () => {
      connection = 'open';
    };

    const handleLog = async (event: MessageEvent<string>) => {
      let entry: LogEntry;
      try {
        entry = JSON.parse(event.data);
      } catch (err) {
        console.error('Malformed log event', err);
        return;
      }

      if (seen.has(entry.id)) return;
      seen.add(entry.id);

      const atBottom =
        !container ||
        container.scrollHeight - container.scrollTop - container.clientHeight < STICKY_THRESHOLD;

      logs.push(entry);

      if (atBottom) {
        await tick();
        container?.scrollTo({ top: container.scrollHeight });
      }
    };

    es.addEventListener('log', handleLog);

    es.onerror = () => {
      if (!deployment || FINISHED.has(deployment.status)) {
        es.close();
        connection = 'closed';
      } else {
        connection = 'connecting';
      }
    };

    return () => {
      es.removeEventListener('log', handleLog);
      es.close();
    };
  });

  function handleStop() {
    if (deployment) onstop(deployment.id);
  }

  function formatTime(iso: string): string {
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? '' : d.toLocaleTimeString();
  }

  const FINISHED_LABELS: Record<string, string> = {
    running: 'Build complete',
    failed: 'Build failed',
    stopped: 'Build stopped',
  };

  let connectionLabel = $derived(
    deployment && FINISHED_LABELS[deployment.status]
      ? FINISHED_LABELS[deployment.status]
      : connection === 'open'
        ? 'Streaming'
        : connection === 'connecting'
          ? 'Connecting…'
          : 'Stream ended',
  );

  let meta = $derived(
    deployment
      ? [
          deployment.container_port > 0 ? `Port: ${deployment.container_port}` : '',
          deployment.root_dir ? `Root dir: ${deployment.root_dir}` : '',
        ]
          .filter(Boolean)
          .join(' · ')
      : '',
  );
</script>

<div class="flex h-full min-h-0 flex-col">
  {#if !deployment}
    <p class="p-4 text-sm text-gray-500">Select a deployment to see its logs.</p>
  {:else}
    <div class="flex items-center justify-between gap-2 border-b border-gray-700 px-3 py-2">
        <div class="min-w-0">
          <p class="truncate text-sm font-medium">{getDisplayName(deployment)}</p>
          {#if deployment.status === 'running' && deployment.caddy_route}
            <a
              href={`http://${deployment.caddy_route}`}
              target="_blank"
              class="text-xs text-blue-400 hover:underline"
            >
              {deployment.caddy_route}
            </a>
          {/if}
          {#if meta}
            <p class="truncate font-mono text-[11px] text-gray-500">{meta}</p>
          {/if}
        </div>

      <div class="flex items-center gap-2">
        <StatusPill status={deployment.status} />
        {#if deployment.status === 'running'}
          <button
            type="button"
            onclick={handleStop}
            disabled={stopping}
            class="rounded border border-gray-600 px-2 py-0.5 text-xs hover:bg-gray-800 disabled:opacity-50"
          >
            {stopping ? 'Stopping…' : 'Stop'}
          </button>
        {/if}
      </div>
    </div>

    <div
      bind:this={container}
      role="log"
      class="flex-1 overflow-y-auto bg-gray-950 px-3 py-2 font-mono text-xs leading-relaxed"
    >
      {#if logs.length === 0}
        <p class="text-gray-500">No logs yet.</p>
      {:else}
        {#each logs as log (log.id)}
          <p class="whitespace-pre-wrap wrap-break-word">
            <span class="mr-2 text-gray-500">{formatTime(log.created_at)}</span>{log.message}
          </p>
        {/each}
      {/if}
    </div>

    <p class="border-t border-gray-700 px-3 py-1 text-[11px] text-gray-500">{connectionLabel}</p>
  {/if}
</div>
