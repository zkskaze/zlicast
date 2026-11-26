<script>
  import Icon from './Icon.svelte';
  import PlaybackControls from './PlaybackControls.svelte';
  import { api, permitted } from './api.js';

  export let station;
  export let media;
  export let role;
  export let player;
  export let listen;
  export let volume;
  export let setVolume;
  export let refresh;
  export let notify;

  let every = station.rules.jingleSongs || 0;
  let jingle = station.rules.jingleId || '';
  let news = '';
  let song = '';
  let busy = '';
  let songsInput;
  let jingleInput;
  let newsInput;
  let dragging = false;

  $: available = media.filter(m => !m.station || m.station === station.slug);
  $: songs = available.filter(m => m.category === 'music');
  $: jingles = available.filter(m => m.category === 'jingles');
  $: bulletins = available.filter(m => ['news', 'emergency', 'shows'].includes(m.category));
  $: canUpload = permitted(role, 'dj', 'editor');

  async function upload(file, category) {
    const form = new FormData();
    form.append('file', file);
    form.append('category', category);
    form.append('station', station.slug);
    return (await api('/api/media', 'POST', form)).media;
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
      const errors = [];
      for (const [i, file] of Array.from(files).entries()) {
        busy = `Додајем песме (${i + 1}/${files.length})…`;
        try {
          await upload(file, 'music');
        } catch (error) {
          errors.push(`${file.name}: ${error.message}`);
        }
      }
      notify(errors.length ? errors.join(' · ') : 'Песме су у ротацији.');
    });
  }

  function pickFile(event, action) {
    const file = event.currentTarget.files?.[0];
    event.currentTarget.value = '';
    if (file) action(file);
  }

  function saveJingle(file) {
    withAction('Чувам џингл…', async () => {
      const id = file ? (await upload(file, 'jingles')).id : jingle;
      await api('/api/stations/' + station.slug + '/jingle', 'PATCH', { id, every });
      jingle = id;
      notify(every ? `Џингл иде после ${every} песама.` : 'Аутоматски џингл по броју песама је искључен.');
    });
  }

  function playNews(file) {
    withAction('Шаљем снимак у етар…', async () => {
      const id = file ? (await upload(file, 'news')).id : news;
      await playNow(id);
      news = id;
      notify('Снимак је послат у етар.');
    });
  }

  function playNow(id) {
    return api('/api/stations/' + station.slug + '/play', 'POST', { id, interrupt: true });
  }

  function playSong() {
    withAction('Шаљем песму у етар…', async () => {
      await playNow(song);
      notify('Песма је послата у етар.');
    });
  }
</script>

<section class="panel">
  <div class="row">
    <div>
      <h2>{station.name}</h2>
      <small>{station.status.now.title || 'Припрема програма…'}</small>
    </div>
  </div>
  <div class="simple-receiver">
    <PlaybackControls {station} playing={player === station.slug} {listen} {volume} {setVolume}/>
  </div>
  {#if canUpload}
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
    {#if songs.length}
      <form on:submit|preventDefault={playSong}>
        <label>Песма у ротацији
          <select bind:value={song}>
            <option value="">Изабери песму</option>
            {#each songs as item (item.id)}
              <option value={item.id}>{item.title}</option>
            {/each}
          </select>
        </label>
        <button disabled={!!busy || !song}><Icon name="play" size={16}/> Пусти песму одмах</button>
      </form>
    {/if}
  {/if}
</section>

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
    <p>Снимак прекида тренутну песму. Кад се заврши, радио наставља с музиком.</p>
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
        <Icon name="megaphone" size={16}/> Додај и пусти одмах
      </button>
      {#if bulletins.length}
        <button type="button" disabled={!!busy || !news} on:click={() => playNews()}>Пусти изабрани снимак</button>
      {/if}
    </div>
    <input class="hidden" type="file" accept="audio/*,.mp3,.wav,.m4a,.aac" bind:this={newsInput}
      on:change={(event) => pickFile(event, playNews)}/>
  </section>
{/if}
