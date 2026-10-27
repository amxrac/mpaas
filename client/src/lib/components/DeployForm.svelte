<script lang="ts">
  import type { CreateDeploymentInput } from '$lib/types';
  import { toEnvObject, validateEnvRows, type EnvRow } from '$lib/env';
  import EnvEditor from './EnvEditor.svelte';

  interface Props {
    ondeploy: (input: CreateDeploymentInput) => Promise<void>;
  }

  let { ondeploy }: Props = $props();

  let url = $state('');
  let port = $state<number | undefined>(undefined);
  let rootDir = $state('');
  let envRows = $state<EnvRow[]>([]);
  let submitting = $state(false);
  let error = $state<string | null>(null);

  let envCheck = $derived(validateEnvRows(envRows));
  let envCount = $derived(envRows.filter((r) => r.key.trim() !== '').length);

  async function handleSubmit(e: SubmitEvent) {
    e.preventDefault();
    if (submitting || envCheck.message) return;

    submitting = true;
    error = null;
    try {
      await ondeploy({
        github_url: url.trim(),
        port: port ?? undefined,
        root_dir: rootDir.trim() || undefined,
        env: toEnvObject(envRows),
      });
      url = '';
      port = undefined;
      rootDir = '';
      envRows = [];
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to create deployment';
    } finally {
      submitting = false;
    }
  }

  const inputClass =
    'w-full rounded border border-gray-700 bg-gray-800 px-3 py-2 text-sm text-white disabled:opacity-50';
</script>

<form
  onsubmit={handleSubmit}
  class="grid grid-cols-1 items-end gap-3 border-b border-gray-700 px-4 py-3 sm:grid-cols-[minmax(0,1fr)_10rem_7rem_auto]"
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
      class={inputClass}
    />
  </div>

  <div>
    <label for="deploy-root" class="mb-1 block text-xs text-gray-400">
      Root dir <span class="text-gray-500">(optional)</span>
    </label>
    <input
      id="deploy-root"
      type="text"
      placeholder="apps/web"
      autocomplete="off"
      spellcheck="false"
      bind:value={rootDir}
      disabled={submitting}
      aria-describedby="deploy-hint"
      class="{inputClass} font-mono"
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
      aria-describedby="deploy-hint"
      class="{inputClass} [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
    />
  </div>

  <button
    type="submit"
    disabled={submitting || envCheck.message !== null}
    class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
  >
    {submitting ? 'Deploying…' : 'Deploy'}
  </button>

  <p id="deploy-hint" class="col-span-full text-xs text-gray-500">
    Leave root dir blank to build from the repo root. Leave port blank to use 8080. Your app
    should listen on the PORT env var.
  </p>

  <details class="col-span-full rounded border border-gray-700 px-3 py-2">
    <summary class="cursor-pointer text-xs text-gray-400">
      Environment variables <span class="text-gray-500">({envCount})</span>
    </summary>
    <div class="mt-2">
      <p class="mb-2 text-xs text-gray-500">Available during build and at runtime.</p>
      <EnvEditor bind:rows={envRows} invalidIds={envCheck.invalidIds} disabled={submitting} />
    </div>
  </details>

  {#if envCheck.message}
    <p class="col-span-full text-xs text-red-400">{envCheck.message}</p>
  {/if}

  {#if error}
    <p role="alert" class="col-span-full text-sm text-red-400">{error}</p>
  {/if}
</form>
