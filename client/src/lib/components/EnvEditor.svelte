<script lang="ts">
  import { newEnvRow, type EnvRow } from '$lib/env';

  interface Props {
    rows: EnvRow[];
    invalidIds?: Set<string>;
    disabled?: boolean;
  }

  let { rows = $bindable(), invalidIds = new Set<string>(), disabled = false }: Props = $props();

  function addRow() {
    rows.push(newEnvRow());
  }

  function removeRow(id: string) {
    rows = rows.filter((r) => r.id !== id);
  }

  const inputBase =
    'w-full rounded border bg-gray-800 px-3 py-2 font-mono text-sm text-white disabled:opacity-50';
</script>

<div class="space-y-2">
  {#each rows as row (row.id)}
    {@const invalid = invalidIds.has(row.id)}
    <div class="grid grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)_auto] gap-2">
      <input
        aria-label="Variable name"
        placeholder="KEY"
        autocomplete="off"
        spellcheck="false"
        bind:value={row.key}
        {disabled}
        aria-invalid={invalid}
        class="{inputBase} {invalid ? 'border-red-500' : 'border-gray-700'}"
      />
      <input
        aria-label="Variable value"
        placeholder="value"
        autocomplete="off"
        spellcheck="false"
        bind:value={row.value}
        {disabled}
        class="{inputBase} border-gray-700"
      />
      <button
        type="button"
        onclick={() => removeRow(row.id)}
        {disabled}
        aria-label="Remove variable"
        class="rounded border border-gray-700 px-3 text-sm text-gray-400 hover:bg-gray-800 disabled:opacity-50"
      >
        ×
      </button>
    </div>
  {/each}

  <button
    type="button"
    onclick={addRow}
    {disabled}
    class="text-xs text-blue-400 hover:underline disabled:opacity-50"
  >
    + Add variable
  </button>
</div>
