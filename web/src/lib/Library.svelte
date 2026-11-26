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
    stationFilter = '',
    preview = '',
    page = 1,
    pageSize = 25,
    marked = new Set(),
    deleting = false;
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
  function toggle(target, checked) {
    const change = new Set(marked);
    for (const id of target) {
      if (checked) change.add(id);
      else change.delete(id);
    }
    marked = change;
  }
  async function removeMarked() {
    const ids = [...marked];
    if (!ids.length || !confirm('Уклони изабране снимке (' + ids.length + ')?')) return;
    deleting = true;
    try {
      const result = await api('/api/media/bulk-delete', 'POST', { ids });
      marked = new Set();
      if (ids.includes(preview)) preview = '';
      await refresh();
      notify('Уклоњено снимака: ' + result.deleted);
    } catch (e) {
      notify(e.message);
    } finally {
      deleting = false;
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
      (!stationFilter || !m.station || m.station === stationFilter) &&
      (m.title + ' ' + m.artist + ' ' + (m.tags || []).join(' '))
        .toLowerCase()
        .includes(query.trim().toLowerCase())
  );
  $: canDelete = permitted(data.role, 'editor');
  $: pageCount = Math.max(1, Math.ceil(list.length / pageSize));
  $: currentPage = Math.min(page, pageCount);
  $: start = (currentPage - 1) * pageSize;
  $: shown = list.slice(start, start + pageSize);
  $: existing = new Set(data.media.map(item => item.id));
  $: if ([...marked].some(id => !existing.has(id))) {
    marked = new Set([...marked].filter(id => existing.has(id)));
  }
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
    <input type="search" bind:value={query} on:input={() => page = 1} placeholder="Претрага по називу, извођачу, тагу" aria-label="Претрага медијатеке"/>
    <select bind:value={filter} on:change={() => page = 1} aria-label="Филтер категорије">
      <option value="">Све категорије</option>
      {#each Object.entries(cats).slice(0,6) as [v,n]}
        <option value={v}>{n}</option>{/each}
    </select>
    <select bind:value={stationFilter} on:change={() => page = 1} aria-label="Филтер станице">
      <option value="">Све станице</option>
      {#each data.stations as s (s.slug)}
        <option value={s.slug}>Доступно за {s.name}</option>
      {/each}
    </select>
    <select bind:value={selected} aria-label="Станица за емитовање">
      <option value="">Аутоматски избор станице</option>
      {#each data.stations as s}
        <option value={s.slug}>{s.name}</option>{/each}
    </select>
  </div>
  {#if canDelete && (shown.length || marked.size)}
    <div class="selection-bar">
      <label class="selection">
        <input type="checkbox" aria-label="Изабери све на овој страници"
          checked={shown.length > 0 && shown.every(m => marked.has(m.id))} disabled={!shown.length || deleting}
          on:change={(event) => toggle(shown.map(m => m.id), event.currentTarget.checked)}/>
        Ова страница
      </label>
      {#if marked.size}
        <div class="controls">
          <span class="mono">Изабрано {marked.size}</span>
          <button type="button" disabled={deleting} on:click={() => marked = new Set()}>Поништи избор</button>
          <button type="button" class="danger" disabled={deleting} on:click={removeMarked}><Icon name="trash-2" size={15}/> Уклони изабрано</button>
        </div>
      {/if}
    </div>
  {/if}
  {#each shown as m (m.id)}
    <div class="item">
      {#if canDelete}
        <input type="checkbox" aria-label={'Изабери ' + m.title} checked={marked.has(m.id)} disabled={deleting}
          on:change={(event) => toggle([m.id], event.currentTarget.checked)}/>
      {/if}
      <div class="thumb">
        {#if m.cover}<img src={m.cover} alt="" loading="lazy"/>{:else}
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
  {#if list.length}
    <nav class="pagination" aria-label="Странице медијатеке">
      <span class="mono">{start + 1}–{Math.min(start + pageSize, list.length)} од {list.length}</span>
      <div class="controls">
        <select bind:value={pageSize} on:change={() => page = 1} aria-label="Снимака по страници">
          <option value={25}>25 по страници</option>
          <option value={50}>50 по страници</option>
          <option value={100}>100 по страници</option>
        </select>
        <button type="button" disabled={currentPage <= 1} on:click={() => page = currentPage - 1}>Претходна</button>
        <span class="mono">{currentPage} / {pageCount}</span>
        <button type="button" disabled={currentPage >= pageCount} on:click={() => page = currentPage + 1}>Следећа</button>
      </div>
    </nav>
  {/if}
</section>

<style>
  .selection-bar, .pagination {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px 16px;
    padding: 10px 0;
  }
  .selection-bar { border-bottom: 1px solid var(--edge); }
  .pagination { border-top: 1px solid var(--edge); margin-top: 8px; }
  .selection {
    display: flex;
    flex-direction: row;
    align-items: center;
    gap: 8px;
    margin: 0;
  }
  input[type='checkbox'] { flex: none; }
</style>
