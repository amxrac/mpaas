<script lang="ts">
  import type { Deployment, LogEntry, DeploymentStatus } from '$lib/types';
  import { listDeployments, createDeployment, deleteDeployment, stopDeployment, streamDeploymentLogs } from '$lib/api';
  import { SvelteSet } from 'svelte/reactivity';

  type ApiResponse<T> = {
    success: boolean;
    message: string;
    data?: T;
  };

  let url = $state('');
  let port = $state<number | undefined>(undefined);
  let deployments = $state<Deployment[]>([]);
  let selectedId = $state<string | null>(null);
  let isSubmitting = $state(false);
  let error = $state<string | null>(null);
  let fetchError = $state<string | null>(null);
  let actionError = $state<string | null>(null);
  let logs = $state<LogEntry[]>([]);
  let eventSource: EventSource | null = null

  let deleteTarget = $state<Deployment | null>(null);


  const pendingIds = new SvelteSet<string>();

  function getStatusClass(status: DeploymentStatus): string {
    switch (status) {
      case 'running': return 'bg-green-500';
      case 'building':
      case 'deploying': return 'bg-yellow-500';
      case 'failed': return 'bg-red-500';
      case 'pending':
      case 'stopped':
      default: return 'bg-gray-400';
    }
  }

  function isValidPort(p: number | undefined): boolean {
    return p === undefined || (Number.isInteger(p) && p >= 1 && p <= 65535);
  }

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

  async function handleSubmit(e: Event) {
    e.preventDefault();
    if (isSubmitting) return;
    if (!url.trim()) return;

    if (!isValidPort(port)) {
      error = 'Port must be a whole number from 1 to 65535';
      return;
    }

    isSubmitting = true;
    error = null;

    try {
      const res: ApiResponse<Deployment> = await createDeployment(url.trim(), port);
      await fetchDeployments();
      selectedId = res.data?.id ?? null;
      url = "";
      port = undefined;
    } catch (err) {
      error = "Failed to create deployment";
      console.error(error, err);
    } finally {
      isSubmitting = false;
    }
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

  function getDisplayName(dep: Deployment): string {
    return dep.github_url.split('/').slice(-2).join('/');
  }
</script>
