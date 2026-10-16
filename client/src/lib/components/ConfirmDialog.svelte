<script lang="ts">
  interface Props {
    open: boolean;
    title: string;
    message: string;
    confirmLabel?: string;
    onconfirm: () => void;
    oncancel: () => void;
  }

  let { open, title, message, confirmLabel = 'Delete', onconfirm, oncancel }: Props = $props();

  let dialog: HTMLDialogElement | undefined = $state();

  $effect(() => {
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    else if (!open && dialog.open) dialog.close();
  });
</script>

<dialog
  bind:this={dialog}
  onclose={oncancel}
  class="m-auto w-full max-w-sm rounded-lg border border-gray-700 bg-gray-900 p-5 text-gray-200 backdrop:bg-black/60"
>
  <h2 class="text-base font-medium">{title}</h2>
  <p class="mt-2 text-sm text-gray-400">{message}</p>

  <div class="mt-5 flex justify-end gap-2">
    <button
      type="button"
      onclick={oncancel}
      class="rounded px-3 py-1.5 text-sm text-gray-300 hover:bg-gray-800"
    >
      Cancel
    </button>
    <button
      type="button"
      onclick={onconfirm}
      class="rounded bg-red-600 px-3 py-1.5 text-sm text-white hover:bg-red-700"
    >
      {confirmLabel}
    </button>
  </div>
</dialog>
