<script lang="ts">
  import type { Deployment, LogEntry } from '$lib/types';
  import { listDeployments, createDeployment, deleteDeployment, stopDeployment, streamDeploymentLogs } from '$lib/api';
  import { getDisplayName } from '$lib/utils';
  import Header from '$lib/components/Header.svelte';
  import DeployForm from '$lib/components/DeployForm.svelte';
  import DeploymentList from '$lib/components/DeploymentList.svelte'
  import { SvelteSet } from 'svelte/reactivity';

  type ApiResponse<T> = {
    success: boolean;
    message: string;
    data?: T;
  };

  let deployments = $state<Deployment[]>([]);
  let selectedId = $state<string | null>(null);
  let fetchError = $state<string | null>(null);
  let actionError = $state<string | null>(null);
  let logs = $state<LogEntry[]>([]);
  let eventSource: EventSource | null = null

  let deleteTarget = $state<Deployment | null>(null);


  const pendingIds = new SvelteSet<string>();

  async function fetchDeployments() {
    try {
      const res: ApiResponse<Deployment[]> = await listDeployments();
      deployments = res.data ?? [];
      fetchError = null;
    } catch (err) {
      fetchError = 'Could not load deployments';
      console.error('Failed to fetch deployments', err);
    }
  }

  $effect(() => {
    fetchDeployments();
    let intervalId = setInterval(fetchDeployments, 5000);
    return () => clearInterval(intervalId);
  });

  function connectSSE(id: string) {
    if (eventSource) {
      eventSource.close();
      eventSource = null;
    }
    logs = [];

    const es = streamDeploymentLogs(id);
    es.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data) as LogEntry;
        logs = [...logs, data];
        const container = document.querySelector('.log-container');
        if (container) {
          requestAnimationFrame(() => {
            container.scrollTop = container.scrollHeight;
          });
        }
      } catch {
        //
      }
    };
    es.onerror = () => {
      console.error('SSE error for', id);
    };
    eventSource = es;
  }

  $effect(() => {
    if (selectedId) {
      connectSSE(selectedId);
    } else {
      if (eventSource) {
        eventSource.close();
        eventSource = null;
      }
      logs = [];
    }
    return () => {
      if (eventSource) {
        eventSource.close();
        eventSource = null;
      }
    };
  });

  async function handleDeploy(url: string, port: number | undefined) {
    const res: ApiResponse<Deployment> = await createDeployment(url, port);
    await fetchDeployments();
    selectedId = res.data?.id ?? null;
  }

  async function handleStop(id: string) {
    if (pendingIds.has(id)) return;
    pendingIds.add(id);
    actionError = null;
    try {
      await stopDeployment(id);
      await fetchDeployments();
    } catch (err) {
      actionError = 'Failed to stop deployment';
      console.error(actionError, err);
    } finally {
      pendingIds.delete(id);
    }
  }

  function requestDelete(dep: Deployment) {
    if (pendingIds.has(dep.id)) return;
    deleteTarget = dep;
  }

  function confirmDelete() {
    const dep = deleteTarget;
    deleteTarget = null;
    if (dep) handleDelete(dep);
  }

  function cancelDelete() {
    deleteTarget = null;
  }

  async function handleDelete(dep: Deployment) {
    if (pendingIds.has(dep.id)) return;
    pendingIds.add(dep.id);
    actionError = null;
    try {
      await deleteDeployment(dep.id);
      deployments = deployments.filter(d => d.id !== dep.id);
      if (selectedId === dep.id) selectedId = null;
    } catch (err) {
      actionError = 'Failed to delete deployment';
      console.error(actionError, err);
    } finally {
      pendingIds.delete(dep.id);
    }
  }
</script>



<div class="flex h-screen flex-col bg-gray-900 text-gray-200">
  <Header count={deployments.length} />
  <DeployForm ondeploy={handleDeploy} />
  <main class="flex min-h-0 flex-1">
    <!-- <LogPanel /> -->
    <section class="w-3/5 border-r border-gray-700 p-4 text-sm text-gray-500">
      Select a deployment to see its logs.
    </section>

    <div class="w-2/5">
      <DeploymentList
        {deployments}
        {selectedId}
        {pendingIds}
        {fetchError}
        {actionError}
        onselect={(id) => (selectedId = id)}
        ondelete={requestDelete}
      />
    </div>
  </main>
</div>
