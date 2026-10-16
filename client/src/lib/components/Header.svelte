<script lang="ts">
  import githubLogo from '$lib/assets/GitHub_Invertocat_White.svg';

  interface Props {
    count: number;
  }

  let { count }: Props = $props();

  const REPO_URL = 'https://github.com/amxrac/mpaas';

  let label = $derived(count === 1 ? '1 deployment' : `${count} deployments`);
</script>

<header class="flex items-center justify-between border-b border-gray-700 px-4 py-3">
  <h1 class="text-base font-medium">mpaas</h1>

  <div class="flex items-center gap-3">

    <span class="text-xs text-gray-400">{label}</span>

    <a
      href={REPO_URL}
      target="_blank"
      aria-label="View source on GitHub"
      class="rounded p-1 hover:bg-gray-800 focus-visible:outline-2 focus-visible:outline-blue-500"
    >
      <img src={githubLogo} alt="" width="20" height="20" class="block" />
    </a>
  </div>
</header>
