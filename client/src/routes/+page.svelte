<script lang="ts">
  import type { Deployment, LogEntry, DeploymentStatus } from '$lib/types';
  import { listDeployments, createDeployment, deleteDeployment, streamDeploymentLogs } from '$lib/api';

  let url = $state('');
  let deployments = $state<Deployment[]>([]);
  let selectedId = $state<string | null>(null);
  let isSubmitting = $state(false);
  let error = $state<string | null>(null);
  let logs = $state<LogEntry[]>([]);
  let modalOpen = $state(false);
  let eventSource: EventSource | null = null

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

  async function fetchDeployments() {
    try {
      const data = await listDeployments();
      deployments = data;
    } catch {
      console.error('Failed to fetch deployments');
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
    if (!url.trim()) return;
    isSubmitting = true;
    error = null;

    try {
      const response = await createDeployment(url.trim());
      await fetchDeployments();
      selectedId = response.id;
      url = "";
    } catch (err) {
      error = "Failed to create deployment";
      console.error(error, err);
    } finally {
      isSubmitting = false;
    }
  }

  async function handleDelete(id: string) {
    try {
      await deleteDeployment(id);
      deployments = deployments.filter(d => d.id !== id);
      if (selectedId === id) selectedId = null;
    } catch (err) {
      console.error("Failed to delete deployment", err);
    }
  }

  function getDisplayName(dep: Deployment): string {
    return dep.github_url.split('/').slice(-2).join('/');
  }
</script>
