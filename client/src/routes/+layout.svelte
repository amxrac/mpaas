<script lang="ts">
	import favicon from '$lib/assets/logo.svg';
	import '../app.css';

	let { children } = $props();
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
	<title>mpaas</title>
</svelte:head>

{@render children()}
