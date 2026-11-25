<script>
  import { focusDialog } from './dialog.js';
  import Icon from './Icon.svelte';
  import { api } from './api.js';
  export let station = null,
    close,
    done,
    notify;
  let draft = station
    ? structuredClone(station)
    : {
        name: '',
        slug: '',
        bitrate: 128,
        format: 'mp3',
        crossfade: 3,
        color: '#c6f479',
        logo: '',
        rules: {
          jingleSongs: 3,
          jingleMinutes: 0,
          adMinutes: 0,
          idHourly: false,
          repeatSong: 180,
          repeatArtist: 45,
          category: 'music'
        }
      };
  let busy = false,
    err = '';
  const letters = Object.fromEntries(
    [...'абвгдђежзијклљмнњопрстћуфхцчџш'].map((c,i) =>
      [c,'a b v g d dj e z z i j k l lj m n nj o p r s t c u f h c c dz s'.split(' ')[i]])
  );
  function suggest(event) {
    if (station) return;
    draft.slug = [...event.currentTarget.value.toLowerCase()].map(c=>letters[c]??c).join('')
      .normalize('NFD').replace(/[\u0300-\u036f]/g,'').replace(/[^a-z0-9]+/g,'-').replace(/^-|-$/g,'');
  }
  async function save() {
    busy = true;
    err = '';
    try {
      await api('/api/stations', 'POST', draft);
      await done();
      notify('Станица је сачувана.');
      close();
    } catch (e) {
      err = e.message;
    } finally {
      busy = false;
    }
  }
  async function remove() {
    if (!confirm('Уклони станицу ' + draft.name + '? Њене нумере ће прећи у заједничку библиотеку.'))
      return;
    try {
      await api('/api/stations/' + draft.slug, 'DELETE');
      await done();
      close();
    } catch (e) {
      err = e.message;
    }
  }
  async function logo(e) {
    const f = e.target.files[0];
    if (!f) return;
    if (f.size > 256 * 1024) {
      err = 'Лого може имати највише 256 KiB.';
      return;
    }
    const rd = new FileReader();
    rd.onload = () => (draft.logo = rd.result);
    rd.readAsDataURL(f);
  }
</script>
<div class="overlay">
  <div class="dialog" use:focusDialog={close} role="dialog" aria-modal="true" aria-label="Подешавања станице">
    <div class="row">
      <h2>{station?'Подешавања станице':'Нова станица'}</h2>
      <button class="icon" on:click={close} aria-label="Затвори">
        <Icon name="x"/>
      </button>
    </div>
    <form on:submit|preventDefault={save}>
      <label>Назив
        <input bind:value={draft.name} on:input={suggest} required maxlength="90" placeholder="Моја станица"/>
      </label>
      <label>Адреса стрима
        <div class="prefix"><span>/stream/</span>
          <input bind:value={draft.slug} required disabled={!!station} pattern="[a-z0-9][a-z0-9\-]*" placeholder="moj-radio"/>
        </div>
      </label>
      <details>
        <summary>Напредна подешавања</summary>
        <div class="formgrid">
          <label>Формат
            <select bind:value={draft.format}>
              <option value="mp3">MP3</option>
              <option value="aac">AAC</option>
            </select>
          </label>
          <label>Битрејт
            <select bind:value={draft.bitrate}>
              {#each [64,96,128,160,192,256,320] as b}
                <option value={b}>{b} kbps</option>{/each}
            </select>
          </label>
          <label>Crossfade (s)
            <input type="number" min="0" max="15" bind:value={draft.crossfade}/>
          </label>
          <label>Боја
            <input type="color" bind:value={draft.color}/>
          </label>
          <label>Џингл после песама
            <input type="number" min="0" bind:value={draft.rules.jingleSongs}/>
          </label>
          <label>Џингл сваких (мин)
            <input type="number" min="0" bind:value={draft.rules.jingleMinutes}/>
          </label>
          <label>Рекламе сваких (мин)
            <input type="number" min="0" bind:value={draft.rules.adMinutes}/>
          </label>
          <label>Размак песме (мин)
            <input type="number" min="0" bind:value={draft.rules.repeatSong}/>
          </label>
          <label>Размак извођача (мин)
            <input type="number" min="0" bind:value={draft.rules.repeatArtist}/>
          </label>
          <label>Категорија ротације
            <select bind:value={draft.rules.category}>
              <option value="music">Музика</option>
              <option value="shows">Емисије</option>
              <option value="news">Вести</option>
            </select>
          </label>
        </div>
        <label class="check">
          <input type="checkbox" bind:checked={draft.rules.idHourly}/> Идентификација на пун сат
        </label>
        <label class="check">
          <input type="checkbox" bind:checked={draft.hls}/> HLS стрим за спољне плејере
        </label>
        {#if draft.hls}
          <small>/hls/{draft.slug}/index.m3u8</small>{/if}
        <label>Лого (до 256 KiB)
          <input type="file" accept="image/png,image/jpeg,image/webp" on:change={logo}/>
        </label>
      </details>
      {#if err}
        <p class="error" role="alert">{err}</p>{/if}
      <div class="actions">
        {#if station}
          <button class="danger" type="button" on:click={remove}>
            <Icon name="trash-2"/> Уклони
          </button>{/if}
        <button class="primary" disabled={busy}>{station?'Сачувај':'Направи станицу'}</button>
      </div>
    </form>
  </div>
</div>
