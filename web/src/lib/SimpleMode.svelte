<script>
  import Icon from './Icon.svelte';
  import PlaybackControls from './PlaybackControls.svelte';
  import StationDetails from './StationDetails.svelte';
  import TrackProgress from './TrackProgress.svelte';
  import YouTubeImport from './YouTubeImport.svelte';
  import { api, importMedia, permitted } from './api.js';

  export let station;
  export let media;
  export let role;
  export let player;
  export let listen;
  export let volume;
  export let setVolume;
  export let refresh;
  export let notify;
  export let editStation;

  let every = station.rules.jingleSongs || 0;
  let jingle = station.rules.jingleId || '';
  let news = '';
  let songQuery = '';
  let searchLimit = 12;
  let mixing = false;
  let skippedStart = '';
  let busy = '';
  let songsInput;
  let jingleInput;
  let newsInput;
  let dragging = false;
  let uploadErrors = [];

  $: available = media.filter(m => !m.station || m.station === station.slug);
  $: songs = available.filter(m => m.category === 'music');
  $: terms = songQuery.trim().toLowerCase().split(/\s+/).filter(Boolean);
  $: matchingSongs = terms.length ? songs.filter(m => {
    const titleAndArtist = (m.title + ' ' + (m.artist || '')).toLowerCase();
    return terms.every(term => titleAndArtist.includes(term));
  }) : [];
  $: jingles = available.filter(m => m.category === 'jingles');
  $: bulletins = available.filter(m => ['news', 'emergency', 'shows'].includes(m.category));
  $: canUpload = permitted(role, 'dj', 'editor');
  $: upcoming = station.upcoming || [];

  async function upload(file, category, onProcessing) {
    const form = new FormData();
    form.append('file', file);
    form.append('category', category);
    form.append('station', station.slug);
    return (await importMedia(form, onProcessing)).media;
  }

  async function withAction(label, action) {
    if (busy) return;
    busy = label;
    try {
      await action();
    } catch (error) {
      notify(error.message);
    } finally {
      busy = '';
      await refresh();
    }
  }

  function addSongs(files) {
    if (!files?.length) return;
    withAction('Додајем песме…', async () => {
      uploadErrors = [];
      for (const [i, file] of Array.from(files).entries()) {
        const position = `${i + 1}/${files.length} · ${file.name}`;
        busy = 'Шаљем ' + position;
        try {
          await upload(file, 'music', () => busy = 'Обрађујем ' + position);
        } catch (error) {
          uploadErrors = [...uploadErrors, `${file.name}: ${error.message}`];
        }
      }
      notify(uploadErrors.length ? 'Неуспела отпремања: ' + uploadErrors.length : 'Песме су у ротацији.');
    });
  }

  function pickFile(event, action) {
    const file = event.currentTarget.files?.[0];
    event.currentTarget.value = '';
    if (file) action(file);
  }

  function saveJingle(file) {
    withAction('Чувам џингл…', async () => {
      const id = file ? (await upload(file, 'jingles', () => busy = 'Обрађујем џингл…')).id : jingle;
      await api('/api/stations/' + station.slug + '/jingle', 'PATCH', { id, every });
      jingle = id;
      notify(every ? `Џингл иде после ${every} песама.` : 'Аутоматски џингл по броју песама је искључен.');
    });
  }

  function playNews(file) {
    withAction('Шаљем снимак у етар…', async () => {
      if (file) {
        const form = new FormData();
        form.append('file', file);
        await api('/api/stations/' + station.slug + '/bulletin', 'POST', form);
      } else {
        await playNow(news);
      }
      notify('Снимак је послат у етар.');
    });
  }

  function playNow(id) {
    return api('/api/stations/' + station.slug + '/play', 'POST', { id, interrupt: true });
  }

  function playSong(song) {
    withAction('Шаљем песму у етар…', async () => {
      await playNow(song);
      notify('Песма је послата у етар.');
    });
  }

  async function shuffle() {
    mixing = true;
    try {
      await api('/api/stations/' + station.slug + '/shuffle', 'POST');
      await refresh();
      notify('Преостале песме су поново измешане.');
    } catch (error) {
      notify(error.message);
    } finally {
      mixing = false;
    }
  }

  async function skip() {
    const started = station.status.started;
    skippedStart = started;
    try {
      await api('/api/stations/' + station.slug + '/skip', 'POST', { started });
      await refresh();
    } catch (error) {
      skippedStart = '';
      notify(error.message);
    }
  }
</script>

<section class="panel">
  <div class="row">
    <div>
      <h2>{station.name}</h2>
    </div>
    {#if role === 'admin'}
      <button type="button" class="icon" aria-label="Подешавања станице" title="Подешавања станице" on:click={() => editStation(station)}>
        <Icon name="sliders-horizontal" size={18}/>
      </button>
    {/if}
  </div>
  <StationDetails {station}/>
  <div class="simple-receiver">
    <PlaybackControls {station} playing={player === station.slug} {listen} {volume} {setVolume} expanded/>
  </div>
  <div class="now">
    <div class="audioicon"><Icon name="audio-lines" size={18}/></div>
    <div class="grow" aria-live="polite">
      <small>Тренутно свира</small>
      <b>{station.status.now.title || 'Припрема програма…'}</b>
      {#if station.status.now.artist}<small>{station.status.now.artist}</small>{/if}
    </div>
    {#if canUpload && station.status.now.category === 'music'}
      <button class="skip" type="button" disabled={skippedStart === station.status.started} on:click={skip}>
        <Icon name="skip-forward" size={16}/> Прескочи песму
      </button>
    {/if}
  </div>
  <TrackProgress status={station.status}/>
</section>

<section class="panel">
  <div class="row">
    <h2>Следеће у ротацији</h2>
    {#if role === 'admin'}
      <button type="button" disabled={mixing || upcoming.length < 2} on:click={shuffle}>
        {mixing ? 'Мешам…' : 'Поново измешај'}
      </button>
    {/if}
  </div>
  {#if upcoming.length}
    <ol class="upcoming">
      {#each upcoming as track (track.id)}
        <li>
          <span class="title">{track.title}</span>
          {#if track.artist}<small>{track.artist}</small>{/if}
        </li>
      {/each}
    </ol>
    <small>Заказане емисије, вести и џинглови могу да се убаце између ових ставки.</small>
  {:else}
    <p>Ротација је празна.</p>
  {/if}
</section>

{#if canUpload}
<section class="panel">
  <h2>Додај песме</h2>
    <button
      class="drop"
      class:drag={dragging}
      disabled={!!busy}
      on:click={() => songsInput.click()}
      on:dragover|preventDefault={() => dragging = true}
      on:dragleave={() => dragging = false}
      on:drop|preventDefault={(event) => { dragging = false; addSongs(event.dataTransfer.files); }}
    >
      <Icon name="cloud-upload" size={30}/>
      <strong>{busy || 'Превуци песме овде'}</strong>
      <span>{songs.length} песама у ротацији · кликни за избор фајлова</span>
    </button>
    <input class="hidden" type="file" multiple accept="audio/*,.mp3,.flac,.wav,.ogg,.m4a,.aac" bind:this={songsInput}
      on:change={(event) => { addSongs(event.currentTarget.files); event.currentTarget.value = ''; }}/>
    <YouTubeImport station={station.slug} disabled={!!busy} {refresh}/>
    {#if uploadErrors.length}
      <details open>
        <summary>Неуспела отпремања ({uploadErrors.length})</summary>
        <ul>{#each uploadErrors as error}<li>{error}</li>{/each}</ul>
      </details>
    {/if}
</section>
{/if}

{#if role === 'admin'}
  <section class="panel">
    <h2>Џингл између песама</h2>
    <form on:submit|preventDefault={() => saveJingle()}>
      <div class="formgrid">
        <label>После колико песама
          <input type="number" min="0" max="1000" required bind:value={every} aria-describedby="jingle-help"/>
          <small id="jingle-help">Унеси 0 да искључиш ово правило.</small>
        </label>
        <label>Џингл
          <select bind:value={jingle}>
            <option value="">Било који доступан џингл</option>
            {#each jingles as item (item.id)}
              <option value={item.id}>{item.title}</option>
            {/each}
          </select>
        </label>
      </div>
      <div class="controls">
        <button type="button" disabled={!!busy || !Number.isInteger(every)} on:click={() => jingleInput.click()}>
          <Icon name="upload" size={16}/> Додај и користи MP3
        </button>
        <button disabled={!!busy}>Сачувај правило</button>
      </div>
      <input class="hidden" type="file" accept=".mp3,audio/mpeg" bind:this={jingleInput}
        on:change={(event) => pickFile(event, saveJingle)}/>
    </form>
  </section>
{/if}


{#if canUpload}
  <section class="panel">
    <h2>Вести и обавештења</h2>
    <p>Отпремљени снимак пушта се једном и не улази у медијатеку. Кад се заврши, радио наставља с музиком.</p>
    {#if bulletins.length}
      <label>Сачувани снимак
        <select bind:value={news}>
          <option value="">Изабери снимак</option>
          {#each bulletins as item (item.id)}
            <option value={item.id}>{item.title}</option>
          {/each}
        </select>
      </label>
    {/if}
    <div class="controls">
      <button type="button" class="primary" disabled={!!busy} on:click={() => newsInput.click()}>
        <Icon name="megaphone" size={16}/> Отпреми и пусти једном
      </button>
      {#if bulletins.length}
        <button type="button" disabled={!!busy || !news} on:click={() => playNews()}>Пусти изабрани снимак</button>
      {/if}
    </div>
    <input class="hidden" type="file" accept="audio/*,.mp3,.wav,.m4a,.aac" bind:this={newsInput}
      on:change={(event) => pickFile(event, playNews)}/>
  </section>
  <section class="panel">
    <h2>Пусти песму одмах</h2>
    <label>
      Претражи песме
      <input type="search" placeholder="Назив песме или извођач" bind:value={songQuery}
        on:input={() => searchLimit = 12}/>
    </label>
    {#if terms.length}
      {#if matchingSongs.length}
        <ul class="song-results">
          {#each matchingSongs.slice(0, searchLimit) as track (track.id)}
            <li>
              <div>
                <span class="title">{track.title}</span>
                {#if track.artist}<small>{track.artist}</small>{/if}
              </div>
              <button type="button" disabled={!!busy} aria-label={'Пусти ' + track.title}
                on:click={() => playSong(track.id)}><Icon name="play" size={15}/> Пусти</button>
            </li>
          {/each}
        </ul>
        {#if matchingSongs.length > searchLimit}
          <button type="button" on:click={() => searchLimit += 12}>Прикажи још</button>
        {/if}
      {:else}
        <p role="status">Нема песама за ову претрагу.</p>
      {/if}
    {/if}
  </section>
{/if}

<style>
  .skip { flex-shrink: 0; }
  .upcoming { padding-left: 26px; margin: 0 0 12px; }
  .upcoming li { padding: 8px 0 8px 5px; border-bottom: 1px solid var(--edge); }
  .upcoming li::marker { color: var(--muted); font-family: Consolas, monospace; }
  .title { font-weight: 650; overflow-wrap: anywhere; }
  .song-results { list-style: none; padding: 0; margin: 0 0 12px; }
  .song-results li { display: flex; justify-content: space-between; align-items: center; gap: 10px; padding: 8px 0; border-bottom: 1px solid var(--edge); }
  .song-results button { flex-shrink: 0; }
  @media (max-width: 420px) {
    .now { flex-wrap: wrap; }
    .now .skip { margin-left: auto; }
  }
</style>
