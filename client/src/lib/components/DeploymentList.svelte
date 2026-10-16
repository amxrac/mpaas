<script lang="ts">
  import type { Deployment } from '$lib/types';
  import StatusPill from './StatusPill.svelte';
  import { getDisplayName } from '$lib/utils';

  interface Props {
    deployments: Deployment[];
    selectedId: string | null;
    pendingIds: ReadonlySet<string>;
    fetchError: string | null;
    actionError: string | null;
    onselect: (id: string) => void;
    ondelete: (dep: Deployment) => void;
  }

  let { deployments, selectedId, pendingIds, fetchError, actionError, onselect, ondelete }: Props = $props();
</script>

<aside class="flex h-full min-h-0 flex-col">
  <h2 class="border-b border-gray-700 px-3 py-2 text-xs text-gray-400">Deployments</h2>

  {#if fetchError}
    <p role="status" class="px-3 py-2 text-xs text-yellow-400">{fetchError}</p>
  {/if}

  {#if actionError}
    <p role="alert" class="px-3 py-2 text-xs text-red-400">{actionError}</p>
  {/if}

  {#if deployments.length === 0 && !fetchError }
    <p class="px-3 py-4 text-sm text-gray-500">No deployments yet.</p>
  {:else if deployments.length > 0}
    <ul class="flex-1 overflow-y-auto">
      {#each deployments as dep (dep.id)}
        <li
          class="flex items-center gap-1 border-b border-gray-800 pr-2 {dep.id === selectedId
            ? 'bg-gray-800'
            : 'hover:bg-gray-800/50'}"
        >
          <button
            type="button"
            onclick={() => onselect(dep.id)}
            aria-current={dep.id === selectedId ? 'true' : undefined}
            class="flex min-w-0 flex-1 items-center justify-between gap-2 px-3 py-2 text-left text-sm"
          >
            <span class="truncate">{getDisplayName(dep)}</span>
            <StatusPill status={dep.status} />
          </button>

          {#if dep.status === 'running' && dep.caddy_route}
            <a
              href={`http://${dep.caddy_route}`}
              target="_blank"
              aria-label={`Open ${getDisplayName(dep)}`}
              class="rounded p-1 text-gray-400 hover:bg-gray-700 hover:text-white"
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                <path d="M14 4h6v6" />
                <path d="M10 14 20 4" />
                <path d="M20 14v6H4V4h6" />
              </svg>
            </a>
          {:else}
            <span class="w-6" aria-hidden="true"></span>
          {/if}

          <button
            type="button"
            onclick={() => ondelete(dep)}
            disabled={pendingIds.has(dep.id)}
            aria-label={`Delete ${getDisplayName(dep)}`}
            class="rounded p-1 text-gray-400 hover:bg-gray-700 hover:text-red-400 disabled:opacity-40"
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M3 6h18" />
              <path d="M8 6V4h8v2" />
              <path d="M6 6l1 14h10l1-14" />
            </svg>
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</aside>
