<script>
  import Icon from './Icon.svelte';
  import PlaylistLinks from './PlaylistLinks.svelte';

  export let station;
  export let playing;
  export let listen;
  export let volume;
  export let setVolume;
  export let publicPage = false;

  $: playLabel = playing
    ? 'Заустави слушање'
    : publicPage ? 'Слушај уживо' : 'Слушај';
</script>

<div class="receiver" style:--station-ink={station.color || 'var(--accent)'}>
  <div class="rail">
    <button class="play" class:playing type="button" title={playLabel} aria-label={playLabel}
      on:click={() => listen(station)}>
      <Icon name={playing ? 'pause' : 'play'} size={18}/>
    </button>

    <label class="level">
      <Icon name="volume-2" size={15}/>
      <input type="range" min="0" max="100" step="1" value={volume} style:--level={volume + '%'}
        aria-label="Јачина звука" aria-valuetext={volume + '%'}
        on:input={(event) => setVolume(Number(event.currentTarget.value))}/>
      <span aria-hidden="true">{volume}<small>%</small></span>
    </label>

    <PlaylistLinks slug={station.slug}/>
  </div>
</div>

<style>
  .receiver {
    --signal: color-mix(in oklab, #eff5db 62%, var(--station-ink));
    container-type: inline-size;
  }
  .rail {
    position: relative;
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: 12px;
    padding: 8px;
    border: 1px solid var(--edge);
    border-radius: 11px;
    background: var(--well);
  }
  .play {
    width: 42px;
    height: 42px;
    padding: 0;
    color: #161e14;
    background: var(--signal);
    border: 1px solid var(--signal);
    border-radius: 7px;
  }
  .play:hover {
    background: #f2f7e6;
  }
  .play.playing {
    color: var(--signal);
    background: transparent;
  }
  .play.playing:hover {
    background: color-mix(in oklab, var(--signal) 12%, var(--well));
  }
  .level {
    display: grid;
    grid-template-columns: auto minmax(26px, 1fr) 4ch;
    align-items: center;
    gap: 8px;
    min-width: 0;
    margin: 0;
    color: var(--muted);
    font: 12px Consolas, monospace;
  }
  input[type='range'] {
    appearance: none;
    min-width: 0;
    width: 100%;
    height: 32px;
    border: 0;
    background: transparent;
    cursor: pointer;
  }
  input::-webkit-slider-runnable-track {
    height: 4px;
    border-radius: 3px;
    background: linear-gradient(90deg, var(--signal) var(--level), var(--edge) var(--level));
  }
  input::-moz-range-track {
    height: 4px;
    border-radius: 3px;
    background: linear-gradient(90deg, var(--signal) var(--level), var(--edge) var(--level));
  }
  input::-webkit-slider-thumb {
    appearance: none;
    width: 11px;
    height: 15px;
    margin-top: -5.5px;
    border: 0;
    border-radius: 3px;
    background: var(--signal);
  }
  input::-moz-range-thumb {
    width: 11px;
    height: 15px;
    border: 0;
    border-radius: 3px;
    background: var(--signal);
  }
  .level span {
    text-align: right;
    color: var(--text);
    font-variant-numeric: tabular-nums;
  }
  .level small {
    display: inline;
    color: var(--muted);
    font: inherit;
  }
  @container (max-width: 318px) {
    .rail {
      grid-template-columns: auto minmax(0, 1fr);
      gap: 6px 12px;
    }
    .rail :global(.playlist-links) {
      grid-column: 2;
      justify-self: end;
    }
  }
</style>
