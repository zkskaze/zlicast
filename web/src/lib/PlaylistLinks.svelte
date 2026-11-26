<script>
  import { onDestroy, tick } from 'svelte';
  import Icon from './Icon.svelte';

  export let slug;
  let address = '';
  let copied = false;
  let reset;
  let fallbackInput;
  $: path = '/listen/' + encodeURIComponent(slug) + '.m3u';
  onDestroy(() => clearTimeout(reset));

  async function copy() {
    const url = new URL(path, window.location.origin).href;
    try {
      await navigator.clipboard.writeText(url);
      address = '';
      copied = true;
      clearTimeout(reset);
      reset = setTimeout(() => copied = false, 1800);
    } catch {
      address = url;
      await tick();
      fallbackInput?.focus();
      fallbackInput?.select();
    }
  }
</script>

<div class="playlist-links">
  <a class="format" href={path} data-sveltekit-reload title="Отвори M3U линк" aria-label="M3U линк">M3U</a>
  <button class:copied type="button" title="Копирај M3U адресу" aria-label="Копирај M3U адресу" on:click={copy}>
    <Icon name={copied ? 'check' : 'copy'} size={15}/>
  </button>
  <a href={path+'?download=1'} download={slug+'.m3u'} data-sveltekit-reload
    title="Преузми M3U" aria-label="Преузми M3U"><Icon name="download" size={15}/></a>
  <span class="feedback" role="status">{copied ? 'M3U адреса је копирана.' : ''}</span>
</div>
{#if address}
  <input class="address" type="url" value={address} readonly bind:this={fallbackInput}
    aria-label="M3U адреса за копирање"/>
{/if}

<style>
  .playlist-links {
    position: relative;
    display: flex;
    align-items: center;
    border: 1px solid var(--edge);
    border-radius: 6px;
  }
  a, button {
    display: grid;
    align-items: center;
    justify-content: center;
    min-width: 32px;
    height: 34px;
    padding: 0;
    border: 0;
    border-radius: 0;
    background: transparent;
    color: var(--muted);
  }
  button, a:last-of-type {
    border-left: 1px solid var(--edge);
  }
  .format {
    padding: 0 9px;
    font: 700 11px Consolas, monospace;
    letter-spacing: 0.045em;
  }
  a:hover, button:hover {
    background: var(--dim);
    color: var(--text);
  }
  a:focus-visible, button:focus-visible {
    position: relative;
    z-index: 1;
    outline-offset: -2px;
  }
  .copied {
    color: var(--signal);
  }
  .feedback {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
  .address {
    grid-column: 1 / -1;
    min-width: 0;
    width: 100%;
    font: 12px Consolas, monospace;
  }
</style>
