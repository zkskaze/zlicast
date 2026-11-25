<script>
  import Icon from './Icon.svelte';
  import { api, cats, duration, permitted } from './api.js';
  export let data, refresh, notify;
  let query = '',
    filter = '',
    cat = 'music',
    scope = '',
    tags = '',
    expires = '',
    busy = false,
    picker,
    drag = false,
    progress = '',
    selected = '',
    preview = '';
  async function upload(files) {
    if (busy || !files?.length) return;
    busy = true;
    let n = 0,
      dups = 0;
    const errors = [];
    for (const f of Array.from(files)) {
      progress = n + 1 + '/' + files.length + ' · ' + f.name;
      try {
        const body = new FormData();
        Object.entries({ file: f, category: cat, station: scope, tags, expires }).forEach(([k, v]) =>
          body.append(k, v)
        );
        const result = await api('/api/media', 'POST', body);
        if (result.duplicate) dups++;
      } catch (e) {
        errors.push(f.name + ' · ' + e.message);
      }
      n++;
    }
    busy = false;
    progress = '';
    if (picker) picker.value = '';
    await refresh();
    notify(errors.length ? errors.join(' / ') : 'Обрађено ' + n + ' · дупликата ' + dups);
  }
  async function remove(m) {
    if (!confirm('Уклони „' + m.title + '“ из библиотеке?')) return;
    try {
      await api('/api/media/' + m.id, 'DELETE');
      await refresh();
    } catch (e) {
      notify(e.message);
    }
  }
  async function play(m) {
    const dest = selected || m.station || data.stations[0]?.slug;
    if (!dest) return;
    try {
      await api('/api/stations/' + dest + '/play', 'POST', { id: m.id, interrupt: true });
      notify('Снимак је послат у етар.');
    } catch (e) {
      notify(e.message);
    }
  }
  $: list = data.media.filter(
    (m) =>
      (!filter || m.category === filter) &&
      (m.title + ' ' + m.artist + ' ' + (m.tags || []).join(' '))
        .toLowerCase()
        .includes(query.toLowerCase())
  );
</script>
{#if permitted(data.role,'editor','dj')}
  <section class="panel">
    <div class="row">
      <h2>Додај снимке</h2>
      <Icon name="upload"/>
    </div>
    <button class:drag class="drop" disabled={busy} on:dragover|preventDefault={()=>drag=true} on:dragleave={()=>drag=false} on:drop|preventDefault={(e)=>{drag=false;upload(e.dataTransfer.files)}} on:click={()=>picker.click()}>
      <Icon name="cloud-upload" size={31}/>
      <strong>{busy?progress:'Превуци музику овде'}</strong><span>или изабери фајлове · MP3, FLAC, WAV, OGG, M4A, AAC</span>
    </button>
    <input class="hidden" type="file" multiple bind:this={picker} accept="audio/*,.m4a,.aac,.flac,.webm" on:change={(e)=>upload(e.target.files)}/>
    <details>
      <summary>Опције отпремања</summary>
      <div class="formgrid">
        <label>Категорија
          <select bind:value={cat}>
            {#each Object.entries(cats).slice(0,6) as [v,n]}
              <option value={v}>{n}</option>{/each}
          </select>
        </label>
        <label>Библиотека
          <select bind:value={scope}>
            <option value="">Заједничка</option>
            {#each data.stations as s}
              <option value={s.slug}>{s.name}</option>{/each}
          </select>
        </label>
        <label>Тагови
          <input bind:value={tags} placeholder="амбијент, вече"/>
        </label>
        <label>Важи до
          <input type="date" bind:value={expires}/>
        </label>
      </div>
    </details>
  </section>{/if}
<section class="panel">
  <div class="row">
    <h2>Снимци
      <small>{data.media.length}</small>
    </h2>
  </div>
  <div class="filters">
    <input type="search" bind:value={query} placeholder="Претрага по називу, извођачу, тагу" aria-label="Претрага медијатеке"/>
    <select bind:value={filter} aria-label="Филтер категорије">
      <option value="">Све категорије</option>
      {#each Object.entries(cats).slice(0,6) as [v,n]}
        <option value={v}>{n}</option>{/each}
    </select>
    <select bind:value={selected} aria-label="Станица за емитовање">
      <option value="">Аутоматски избор станице</option>
      {#each data.stations as s}
        <option value={s.slug}>{s.name}</option>{/each}
    </select>
  </div>
  {#each list as m}
    <div class="item">
      <div class="thumb">
        {#if m.cover}<img src={m.cover} alt=""/>{:else}
          <Icon name="music-2"/>{/if}
      </div>
      <div class="grow"><b>{m.title}</b>
        <small>{m.artist||cats[m.category]} · {m.station||'Заједничка'}</small>
      </div><span class="mono">{duration(m.duration)}</span>
      <button class="icon" aria-label={'Преслушај '+m.title} on:click={()=>preview=preview===m.id?'':m.id}>
        <Icon name="headphones"/>
      </button>
      {#if permitted(data.role,'dj','editor')}
        <button class="icon" title="Пусти одмах" aria-label="Пусти одмах" on:click={()=>play(m)}>
          <Icon name="radio"/>
        </button>{/if}
      {#if permitted(data.role,'editor')}
        <button class="icon" aria-label="Уклони снимак" on:click={()=>remove(m)}>
          <Icon name="trash-2"/>
        </button>{/if}
    </div>
    {#if preview===m.id}
      <audio controls autoplay src={'/api/media/'+m.id+'/audio'}></audio>{/if}
{:else}
    <div class="empty">
      <Icon name="disc-3"/><span>{data.media.length?'Нема резултата за ову претрагу.':'Твоја библиотека је празна. Додај прву нумеру изнад.'}</span>
    </div>{/each}
</section>
