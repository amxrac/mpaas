<script lang="ts">
  interface Props {
    ondeploy: (url: string, port: number | undefined) => Promise<void>;
  }

  let { ondeploy }: Props = $props();

  let url = $state('');

  let port = $state<number | undefined>(undefined);
  let submitting = $state(false);
  let error = $state<string | null>(null);

  async function handleSubmit(e: SubmitEvent) {
    e.preventDefault();
    if (submitting) return;

    submitting = true;
    error = null;
    try {
      await ondeploy(url.trim(), port);
      url = '';
      port = undefined;
    } catch {
      error = 'Failed to create deployment';
    } finally {
      submitting = false;
    }
  }
</script>

<form
  onsubmit={handleSubmit}
  class="grid grid-cols-1 items-end gap-3 border-b border-gray-700 px-4 py-3 sm:grid-cols-[minmax(0,1fr)_7rem_auto]"
>
  <div>
    <label for="deploy-url" class="mb-1 block text-xs text-gray-400">GitHub URL</label>
    <input
      id="deploy-url"
      type="url"
      required
      placeholder="https://github.com/user/repo"
      bind:value={url}
      disabled={submitting}
      class="w-full rounded border border-gray-700 bg-gray-800 px-3 py-2 text-sm text-white disabled:opacity-50"
    />
  </div>

  <div>
    <label for="deploy-port" class="mb-1 block text-xs text-gray-400">
      Port <span class="text-gray-500">(optional)</span>
    </label>
    <input
      id="deploy-port"
      type="number"
      min="1"
      max="65535"
      placeholder="8080"
      bind:value={port}
      disabled={submitting}
      aria-describedby="deploy-port-hint"
      class="w-full rounded border border-gray-700 bg-gray-800 px-3 py-2 text-sm text-white disabled:opacity-50 [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
      />
  </div>

  <button
    type="submit"
    disabled={submitting}
    class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
  >
    {submitting ? 'Deploying…' : 'Deploy'}
  </button>

  <p id="deploy-port-hint" class="col-span-full text-xs text-gray-500">
    Leave port blank to use 8080. Your app should listen on the PORT env var.
  </p>

  {#if error}
    <p role="alert" class="col-span-full text-sm text-red-400">{error}</p>
  {/if}
</form>
