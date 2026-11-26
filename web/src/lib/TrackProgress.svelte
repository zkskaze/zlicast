<script>
  import { onMount } from 'svelte';
  import { duration } from './api.js';

  export let status;
  let received = 0;
  let tick = 0;

  function sync(next) {
    received = tick = performance.now();
    return Number(next?.elapsed) || 0;
  }

  $: elapsedAtSync = sync(status);
  $: total = Number(status?.now?.duration) || 0;
  $: advancing = status?.onAir && Number.isFinite(status.elapsed);
  $: elapsed = Math.min(total, Math.max(0, elapsedAtSync + (advancing ? (tick - received) / 1000 : 0)));
  $: lengthLabel = duration(Math.ceil(total));
  $: remainingLabel = duration(Math.ceil(total - elapsed));

  onMount(() => {
    const timer = setInterval(() => tick = performance.now(), 250);
    return () => clearInterval(timer);
  });
</script>

{#if total > 0 && !status.live && status.now.category !== 'fallback'}
  <div class="track-progress">
    <progress value={elapsed} max={total} aria-label="Напредак нумере"
      aria-valuetext={`${duration(elapsed)} од ${lengthLabel}, још ${remainingLabel}`}></progress>
    <div class="times">
      <span>{duration(elapsed)} / {lengthLabel}</span>
      <span>Још {remainingLabel}</span>
    </div>
  </div>
{/if}

<style>
  .track-progress { margin-top: 12px; }
  progress {
    display: block;
    appearance: none;
    width: 100%;
    height: 5px;
    border: 0;
    border-radius: 4px;
    overflow: hidden;
    background: var(--edge);
    accent-color: var(--accent);
  }
  progress::-webkit-progress-bar { background: var(--edge); }
  progress::-webkit-progress-value { background: var(--accent); border-radius: 4px; }
  progress::-moz-progress-bar { background: var(--accent); border-radius: 4px; }
  .times {
    display: flex;
    justify-content: space-between;
    gap: 8px;
    margin-top: 7px;
    color: var(--muted);
    font-size: 12px;
    font-variant-numeric: tabular-nums;
  }
</style>
