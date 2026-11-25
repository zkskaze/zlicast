<script>
  import { onMount } from 'svelte';
  import { api, clock, duration } from './api.js';
  import Icon from './Icon.svelte';
  export let data, notify;
  let slug = data.stations[0]?.slug || '',
    stats = null,
    system = null,
    history = [],
    timer;
  async function load() {
    if (!slug) return;
    const selected = slug;
    try {
      const [s, v, h] = await Promise.all([
        api('/api/stations/' + slug + '/stats'),
        api('/api/system'),
        api('/api/stations/' + slug + '/history')
      ]);
      if (selected === slug) {
        stats = s;
        system = v;
        history = h || [];
      }
    } catch (e) {
      notify(e.message);
    }
  }
  onMount(() => {
    load();
    timer = setInterval(load, 5000);
    return () => clearInterval(timer);
  });
  $: samples = stats?.samples || [];
  $: peak = Math.max(1, ...samples.map((p) => p.listeners));
  $: bins = Array.from({ length: 24 }, (_, i) => {
    const matches = samples.filter(
      (p) =>
        Number(
          new Intl.DateTimeFormat('en-GB', { hour: '2-digit', timeZone: data.timezone }).format(
            new Date(p.at)
          )
        ) === i
    );
    return { hour: i, count: matches.length ? Math.max(...matches.map((p) => p.listeners)) : null };
  });
  const fmt = (b) =>
    b >= 1073741824 ? (b / 1073741824).toFixed(1) + ' GiB' : (b / 1048576).toFixed(1) + ' MiB';
</script>
<div class="row">
  <h2>Слушаност</h2>
  <select aria-label="Станица" bind:value={slug} on:change={load}>
    {#each data.stations as s}
      <option value={s.slug}>{s.name}</option>{/each}
  </select>
</div>
{#if stats}
  <div class="metrics">
    {#each [['Слушају сада',stats.listeners],['Врхунац сесије',stats.peak],['Просечно слушање',duration(stats.averageSeconds)],['Доступност',samples.length?(100*samples.filter(x=>x.onAir).length/samples.length).toFixed(1)+'%':'—']] as m}
      <div>
        <small>{m[0]}</small>
        <strong>{m[1]}</strong>
      </div>{/each}
  </div>
  <section class="panel">
    <div class="row">
      <h2>Последња 24 сата</h2><span class="mono">{data.timezone}</span>
    </div>
    <div class="chart">
      {#each bins as v}
        <div title={v.hour+' h: '+(v.count??'без података')}><i style={'height:'+(v.count===null?0:Math.max(2,100*v.count/peak))+'%'}></i>
          <small>{v.hour%3===0?v.hour:''}</small>
        </div>{/each}
    </div>
    <p>Највећи број истовремених слушалаца по сату из минутних узорака.</p>
  </section>
  <section class="panel">
    <h2>Систем</h2>
    <div class="system">
      {#each [['CPU сервера',system.cpu.toFixed(1)+'%'],['RAM сервера',fmt(system.ram)],['Слободан диск',fmt(system.diskFree)],['Медијатека',fmt(system.mediaBytes)],['Саобраћај станице',fmt(stats.bytes)],['Грешке емитовања',stats.errors]] as v}
        <div>
          <small>{v[0]}</small><b>{v[1]}</b>
        </div>{/each}
    </div>
  </section>
  <section class="panel">
    <h2>Историја емитовања</h2>
    {#each history.slice(0,30) as h}
      <div class="item"><span class="mono">{clock(h.at)}</span><b class="grow">{h.title}</b>
        <small>{h.artist}</small>
      </div>{/each}
  </section>{/if}
