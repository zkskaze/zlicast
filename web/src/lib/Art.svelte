<script>
  export let slug = 'zlifm',
    name = 'ZliFM',
    color = '#c6f479',
    logo = '',
    large = false;
  $: kaze = slug === 'kaze';
  const range = Array.from({ length: 46 }, (_, i) => i);
  function wave(i, kaze) {
    let path = '';
    for (let x = -20; x <= 620; x += 3) {
      const env = Math.exp(-Math.pow((x - (kaze ? 410 : 382)) / 133, 2));
      const peak = kaze
        ? 37 * Math.sin(x / 51 + i * 0.068) + 30 * Math.sin(x / 90 - i * 0.032)
        : -68 * Math.exp(-Math.pow((x - 365 - i * 0.75) / 47, 2)) + 35 * Math.sin(x / 65 - i * 0.054);
      let y = 73 + i * 2.32 + env * peak;
      path += (x === -20 ? 'M' : 'L') + x.toFixed(1) + ' ' + y.toFixed(1) + ' ';
    }
    return path;
  }
</script>
<div class:large class="ident" style={'--ink:'+color}>
  <svg class="engrave" viewBox="0 0 600 200" preserveAspectRatio="none" aria-hidden="true">
    {#each range as i}<path d={wave(i,kaze)} fill="none" stroke="currentColor" stroke-width={i%5===0?1.25:.8}/>{/each}
  </svg>
  <div class="ident-top"><span>ZKS BROADCAST</span><span>{slug.toUpperCase()} / FM</span></div>
  {#if logo}<img src={logo} alt={name} class="station-logo"/>{:else}
    <strong class:kaze>{kaze?'風':name}</strong>{/if}
  <div class="ident-bottom"><span>{kaze?'RADIO KAZE':'ZLICAST RADIO NETWORK'}</span><span class="cross">＋</span></div>
</div>
<style>
  .ident {
    position: relative;
    isolation: isolate;
    height: 148px;
    overflow: hidden;
    background: var(--ink);
    color: #1b251c;
    border-radius: 8px;
    padding: 13px 16px;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
  }
  .engrave {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    z-index: -1;
    opacity: 0.72;
  }
  .ident-top,
  .ident-bottom {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font:
      600 9px/1.2 Consolas,
      monospace;
    letter-spacing: 0.12em;
  }
  .ident strong {
    font:
      800 clamp(37px, 4vw, 64px)/0.95 Manrope,
      sans-serif;
    letter-spacing: -0.09em;
    margin-top: 2px;
    max-width: 100%;
    white-space: nowrap;
    text-overflow: ellipsis;
    overflow: hidden;
  }
  .ident strong.kaze {
    font:
      700 63px/0.95 'Yu Mincho',
      'MS Mincho',
      serif;
  }
  .cross {
    font-size: 17px;
  }
  .station-logo {
    object-fit: contain;
    height: 58%;
    width: 55%;
    object-position: left;
  }
  .large {
    height: 100%;
    min-height: 240px;
    border-radius: 0;
    padding: 22px;
  }
  .large strong {
    font-size: clamp(58px, 6vw, 100px);
  }
  .large .ident-top,
  .large .ident-bottom {
    font-size: 10px;
  }
</style>
