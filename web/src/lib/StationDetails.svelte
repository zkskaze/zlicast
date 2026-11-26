<script>
  import Icon from './Icon.svelte';

  export let station;

  function webAddress(value) {
    if (!value) return null;
    try {
      const url = new URL(value);
      return url.protocol === 'http:' || url.protocol === 'https:' ? url : null;
    } catch {
      return null;
    }
  }

  $: website = webAddress(station.website);
</script>

{#if station.description || station.owner || website}
  <div class="station-details">
    {#if station.description}<p>{station.description}</p>{/if}
    {#if station.owner || website}
      <div class="details-links">
        {#if station.owner}<span><small>Власник</small> {station.owner}</span>{/if}
        {#if website}
          <a href={website.href} target="_blank" rel="noopener noreferrer">
            {website.host} <Icon name="external-link" size={13}/>
          </a>
        {/if}
      </div>
    {/if}
  </div>
{/if}

<style>
  .station-details { margin: 10px 0 18px; }
  .station-details p {
    margin: 0;
    max-width: 64ch;
    white-space: pre-line;
    overflow-wrap: anywhere;
  }
  .details-links {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 18px;
    margin-top: 7px;
    font-size: 13px;
    overflow-wrap: anywhere;
  }
  small { display: inline; margin-right: 4px; }
  a {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    max-width: 100%;
    text-decoration: underline;
    text-underline-offset: 3px;
  }
</style>
