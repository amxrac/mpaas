<script lang="ts">
  import type { Deployment } from '$lib/types';
  import { listDeployments, createDeployment, deleteDeployment, stopDeployment } from '$lib/api';
  import { getDisplayName } from '$lib/utils';
  import Header from '$lib/components/Header.svelte';
  import DeployForm from '$lib/components/DeployForm.svelte';
  import DeploymentList from '$lib/components/DeploymentList.svelte'
  import LogPanel from '$lib/components/LogPanel.svelte';
  import ConfirmDialog from '$lib/components/ConfirmDialog.svelte'
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
  let deleteTarget = $state<Deployment | null>(null);
  const pendingIds = new SvelteSet<string>();

  let selected = $derived(deployments.find((d) => d.id === selectedId) ?? null);


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
      <section class="w-3/5 border-r border-gray-700">
            <LogPanel
              deploymentId={selectedId}
              deployment={selected}
              stopping={selectedId !== null && pendingIds.has(selectedId)}
              onstop={handleStop}
            />
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
  <ConfirmDialog
      open={deleteTarget !== null}
      title="Delete deployment"
      message={deleteTarget ? `Delete ${getDisplayName(deleteTarget)}? This can't be undone.` : ''}
      onconfirm={confirmDelete}
      oncancel={cancelDelete}
    />
</div>
