<script>
  import { tick } from 'svelte';
  import Icon from './Icon.svelte';

  export let slug;
  export let notify;

  let address = '';
  let fallbackInput;
  $: path = '/listen/' + encodeURIComponent(slug) + '.m3u';

  async function copy() {
    const url = new URL(path, window.location.origin).href;
    try {
      await navigator.clipboard.writeText(url);
      notify('M3U адреса је копирана.');
    } catch {
      address = url;
      await tick();
      fallbackInput.focus();
      fallbackInput.select();
    }
  }
</script>

<div class="controls">
  <a class="btn" href={path} data-sveltekit-reload><Icon name="link" size={16}/> M3U линк</a>
  <button class="icon" type="button" title="Копирај M3U адресу" aria-label="Копирај M3U адресу" on:click={copy}>
    <Icon name="copy" size={16}/>
  </button>
  <a class="icon" href={path+'?download=1'} download={slug+'.m3u'} data-sveltekit-reload
    title="Преузми M3U" aria-label="Преузми M3U"><Icon name="download" size={16}/></a>
  {#if address}
    <input type="url" value={address} readonly bind:this={fallbackInput} aria-label="M3U адреса за копирање"/>
  {/if}
</div>
