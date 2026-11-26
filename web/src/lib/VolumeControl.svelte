<script>
  import Icon from './Icon.svelte';

  export let volume;
  export let setVolume;
</script>

<label class="volume-control">
  <Icon name="volume-2" size={16}/>
  <input type="range" min="0" max="100" step="1" value={volume}
    aria-label="Јачина звука" aria-valuetext={volume + '%'}
    on:input={(event) => setVolume(Number(event.currentTarget.value))}/>
  <span aria-hidden="true">{volume}%</span>
</label>

<style>
  .volume-control {
    display: flex;
    flex-direction: row;
    align-items: center;
    gap: 8px;
    margin: 0;
  }
  input[type='range'] {
    width: min(8rem, 30vw);
  }
  span {
    min-width: 3ch;
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
</style>
