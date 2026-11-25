<script>
  import { focusDialog } from './dialog.js';
  import Icon from './Icon.svelte';
  import { api, cats, dateISO, permitted } from './api.js';
  export let data, refresh, notify;
  let slug = data.stations[0]?.slug || '',
    base = dateISO(),
    editor = null,
    pl = null,
    copyTo = '',
    dragID = '';
  const repeats = { once: 'Једном', daily: 'Сваког дана', weekly: 'Недељно', monthly: 'Месечно' };
  const dt = (s) => new Date(s + 'T12:00:00');
  function plus(s, n) {
    const d = dt(s);
    d.setDate(d.getDate() + n);
    return dateISO(d);
  }
  function week(s) {
    let m = plus(s, -((dt(s).getDay() + 6) % 7));
    return Array.from({ length: 7 }, (_, i) => plus(m, i));
  }
  function onDay(e, d) {
    if (e.date > d) return false;
    return (
      e.repeat === 'daily' ||
      (e.repeat === 'weekly' && dt(e.date).getDay() === dt(d).getDay()) ||
      (e.repeat === 'monthly' && dt(e.date).getDate() === dt(d).getDate()) ||
      e.date === d
    );
  }
  $: events = (data.events || [])
    .filter((e) => e.station === slug)
    .sort((a, b) => a.time.localeCompare(b.time));
  $: tracks = data.media.filter((m) => !m.station || m.station === slug);
  $: lists = (data.playlists || []).filter((p) => !p.station || p.station === slug);
  $: can = permitted(data.role, 'editor', 'dj');
  function edit(e) {
    editor = e
      ? { ...e }
      : {
          id: '',
          station: slug,
          name: '',
          date: base,
          time: '09:00',
          repeat: 'once',
          target: 'track',
          value: '',
          interrupt: false,
          priority: 0,
          intro: '',
          outro: ''
        };
  }
  async function post(path, obj) {
    try {
      await api('/api/' + path, 'POST', obj);
      await refresh();
      return true;
    } catch (e) {
      notify(e.message);
      return false;
    }
  }
  async function save() {
    if (await post('events', editor)) {
      editor = null;
      notify('Термин је сачуван.');
    }
  }
  async function del(kind, id) {
    if (!confirm('Уклони изабрану ставку?')) return;
    try {
      await api('/api/' + kind + '/' + id, 'DELETE');
      await refresh();
      editor = null;
      pl = null;
    } catch (e) {
      notify(e.message);
    }
  }
  async function move(d) {
    const e = events.find((x) => x.id === dragID);
    if (e) {
      await post('events', { ...e, date: d });
      dragID = '';
    }
  }
  async function copy() {
    if (!copyTo || copyTo === base) return;
    for (const e of events.filter((x) => onDay(x, base))) {
      if (!(await post('events', { ...e, id: '', date: copyTo, repeat: 'once' }))) return;
    }
    notify('Термини су копирани.');
  }
  function addTrack(id) {
    if (id) pl = { ...pl, tracks: [...pl.tracks, id] };
  }
</script>
<section class="panel">
  <div class="row wrap">
    <div>
      <h2>Недељни програм</h2>
      <small>{data.timezone}</small>
    </div>
    <div class="controls">
      <select bind:value={slug} aria-label="Станица">
        {#each data.stations as s}
          <option value={s.slug}>{s.name}</option>{/each}
      </select>
      {#if can}
        <button class="primary" disabled={!slug} on:click={()=>edit()}>
          <Icon name="plus"/> Термин
        </button>{/if}
    </div>
  </div>
  <div class="row weeknav">
    <button on:click={()=>base=plus(base,-7)} aria-label="Претходна недеља">←</button>
    <input type="date" bind:value={base} aria-label="Избор датума"/>
    <button on:click={()=>base=plus(base,7)} aria-label="Следећа недеља">→</button>
  </div>
  <div class="week">
    {#each week(base) as d}
      <div class:today={d===dateISO()} class="day" role="group" aria-label={d} on:dragover|preventDefault={()=>{}} on:drop|preventDefault={()=>move(d)}><b>{dt(d).toLocaleDateString('sr-Cyrl-RS',{weekday:'short'})}</b><span>{d.slice(8)}.{d.slice(5,7)}.</span>
        {#each events.filter(e=>onDay(e,d)) as e}
          <button class="event" disabled={!can} draggable={can} on:dragstart={()=>dragID=e.id} on:click={()=>edit(e)}>
            <strong>{e.time}</strong>{e.name}
          </button>{/each}
      </div>{/each}
  </div>
  {#if can}
    <details>
      <summary>Копирај програм за изабрани дан</summary>
      <div class="filters">
        <input type="date" bind:value={copyTo} aria-label="Одредишни датум"/>
        <button on:click={copy} disabled={!copyTo}>Копирај термине</button>
      </div>
    </details>{/if}
</section>
<section class="panel">
  <div class="row">
    <h2>Плејлисте</h2>
    {#if permitted(data.role,'editor')}
      <button on:click={()=>pl={id:'',name:'',station:slug,tracks:[]}}>
        <Icon name="plus"/> Плејлиста
      </button>{/if}
  </div>
  {#each lists as p}
    <button class="listbtn" on:click={()=>pl={...p,tracks:[...p.tracks]}}>
      <Icon name="list-music"/><b>{p.name}</b>
      <small>{p.tracks.length} снимака</small>
      <Icon name="chevron-right"/>
    </button>{:else}
    <div class="empty">
      <Icon name="list-music"/> Још нема плејлиста за ову станицу.
    </div>{/each}
</section>
{#if editor}
  <div class="overlay">
    <div use:focusDialog={()=>{editor=null;pl=null}} class="dialog" role="dialog" aria-modal="true" aria-label="Термин">
      <div class="row">
        <h2>Термин емитовања</h2>
        <button class="icon" on:click={()=>editor=null} aria-label="Затвори">
          <Icon name="x"/>
        </button>
      </div>
      <form on:submit|preventDefault={save}>
        <label>Назив
          <input bind:value={editor.name} required/>
        </label>
        <div class="formgrid">
          <label>Датум
            <input type="date" bind:value={editor.date} required/>
          </label>
          <label>Време
            <input type="time" bind:value={editor.time} required/>
          </label>
          <label>Понављање
            <select bind:value={editor.repeat}>
              {#each Object.entries(repeats) as [v,n]}
                <option value={v}>{n}</option>{/each}
            </select>
          </label>
          <label>Програм
            <select bind:value={editor.target} on:change={()=>editor.value=''}>
              <option value="track">Снимак</option>
              <option value="playlist">Плејлиста</option>
              <option value="category">Категорија</option>
              <option value="live">Термин уживо</option>
            </select>
          </label>
        </div>
        {#if editor.target!=='live'}
          <label>Избор
            <select bind:value={editor.value} required>
              <option value="">Изабери</option>
              {#if editor.target==='track'}
                {#each tracks as m}
                  <option value={m.id}>{m.title}</option>{/each}
                {:else if editor.target==='playlist'}
                  {#each lists as p}
                    <option value={p.id}>{p.name}</option>{/each}{:else}
                  {#each Object.entries(cats).slice(0,6) as [v,n]}
                    <option value={v}>{n}</option>{/each}{/if}
            </select>
          </label>{:else}
          <p>Студио преузима програм чим се водитељ повеже. До тада свира ротација.</p>{/if}
        <details>
          <summary>Напредно</summary>
          <label>Приоритет
            <input type="number" bind:value={editor.priority}/>
          </label>
          <label class="check">
            <input type="checkbox" bind:checked={editor.interrupt}/> Прекини тренутну нумеру
          </label>
          <div class="formgrid">
            {#each ['intro','outro'] as k}
              <label>{k==='intro'?'Уводни џингл':'Одјавни џингл'}
                <select bind:value={editor[k]}>
                  <option value="">Без џингла</option>
                  {#each tracks.filter(m=>m.category==='jingles') as m}
                    <option value={m.id}>{m.title}</option>{/each}
                </select>
              </label>{/each}
          </div>
        </details>
        <div class="actions">
          {#if editor.id&&permitted(data.role,'editor')}
            <button type="button" class="danger" on:click={()=>del('events',editor.id)}>Уклони</button>{/if}
          <button class="primary">Сачувај термин</button>
        </div>
      </form>
    </div>
  </div>{/if}
{#if pl}
  <div class="overlay">
    <div use:focusDialog={()=>{editor=null;pl=null}} class="dialog" role="dialog" aria-modal="true" aria-label="Плејлиста">
      <div class="row">
        <h2>Плејлиста</h2>
        <button class="icon" aria-label="Затвори" on:click={()=>pl=null}>
          <Icon name="x"/>
        </button>
      </div>
      <label>Назив
        <input bind:value={pl.name}/>
      </label>
      <label>Додај нумеру
        <select on:change={e=>{addTrack(e.target.value);e.target.value=''}}>
          <option value="">Изабери снимак</option>
          {#each tracks as m}
            <option value={m.id}>{m.title}</option>{/each}
        </select>
      </label>
      {#each pl.tracks as id,i}
        <div class="item"><span class="mono">{i+1}</span><b class="grow">{tracks.find(m=>m.id===id)?.title||'Уклоњен снимак'}</b>
          <button class="icon" aria-label="Уклони из плејлисте" on:click={()=>pl={...pl,tracks:pl.tracks.filter((_,n)=>n!==i)}}>
            <Icon name="x"/>
          </button>
        </div>{/each}
      {#if permitted(data.role,'editor')}
        <div class="actions">
          {#if pl.id}
            <button class="danger" on:click={()=>del('playlists',pl.id)}>Уклони</button>{/if}
          <button class="primary" on:click={async()=>{if(await post('playlists',pl))pl=null}}>Сачувај</button>
        </div>{/if}
    </div>
  </div>{/if}
