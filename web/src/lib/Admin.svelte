<script>
  import { onMount } from 'svelte';
  import { api, clock } from './api.js';
  import Icon from './Icon.svelte';
  export let notify;
  export let stations = [];
  let shuffleStation = '';
  let shuffling = false;
  $: if (!stations.some(station => station.slug === shuffleStation)) {
    shuffleStation = stations[0]?.slug || '';
  }

  async function shuffle() {
    shuffling = true;
    try {
      await api('/api/stations/' + encodeURIComponent(shuffleStation) + '/shuffle', 'POST');
      notify('Преостале песме су поново измешане.');
    } catch (e) {
      notify(e.message);
    } finally {
      shuffling = false;
    }
  }
  let info = null,
    kind = 'users',
    name = '',
    password = '',
    role = 'viewer',
    service = 'gotify',
    url = '',
    secret = '',
    token = '',
    restoreFile,
    busy = false;
  async function load() {
    try {
      info = await api('/api/admin');
    } catch (e) {
      notify(e.message);
    }
  }
  onMount(load);
  async function add() {
    try {
      const r = await api('/api/admin/' + kind, 'POST', {
        name,
        password,
        role,
        kind: service,
        url,
        secret
      });
      token = r.token;
      name = password = url = secret = '';
      await load();
      notify('Сачувано.');
    } catch (e) {
      notify(e.message);
    }
  }
  async function remove(k, id) {
    if (!confirm('Уклони ову ставку?')) return;
    try {
      await api('/api/admin/' + k + '/' + encodeURIComponent(id), 'DELETE');
      await load();
    } catch (e) {
      notify(e.message);
    }
  }
  async function restore() {
    if (
      !restoreFile ||
      !confirm(
        'Врати изабрану копију при следећем покретању? Она ће заменити тренутни програм и налоге.'
      )
    )
      return;
    busy = true;
    try {
      await api('/api/admin/restore', 'POST', restoreFile);
      notify('Копија је спремна. Примениће се при следећем покретању.');
    } catch (e) {
      notify(e.message);
    } finally {
      busy = false;
    }
  }
</script>
{#if stations.length}
  <section class="panel">
    <h2>Редослед песама</h2>
    <form on:submit|preventDefault={shuffle}>
      <div class="controls">
        <label>Станица
          <select bind:value={shuffleStation} disabled={shuffling}>
            {#each stations as station (station.slug)}
              <option value={station.slug}>{station.name}</option>
            {/each}
          </select>
        </label>
        <button disabled={shuffling || !shuffleStation}>Измешај преостале песме</button>
      </div>
      <small>Нови редослед важи од следеће песме.</small>
    </form>
  </section>
{/if}
<section class="panel">
  <div class="row">
    <h2>Приступ и интеграције</h2>
    <Icon name="shield"/>
  </div>
  <form on:submit|preventDefault={add}>
    <label>Додај
      <select bind:value={kind}>
        <option value="users">Кориснички налог</option>
        <option value="tokens">API токен</option>
        <option value="hooks">Webhook</option>
      </select>
    </label>
    {#if kind==='hooks'}
      <div class="formgrid">
        <label>Сервис
          <select bind:value={service}>
            <option value="gotify">Gotify</option>
            <option value="discord">Discord</option>
          </select>
        </label>
        <label>Адреса webhook-а
          <input type="url" bind:value={url} required/>
        </label>
      </div>
      {#if service==='gotify'}
        <label>Кључ апликације
          <input type="password" bind:value={secret}/>
        </label>{/if}{:else}
      <div class="formgrid">
        <label>Име
          <input bind:value={name} required/>
        </label>
        <label>Улога
          <select bind:value={role}>
            {#each ['viewer','editor','dj','admin'] as r}
              <option value={r}>{r}</option>{/each}
          </select>
        </label>
      </div>
      {#if kind==='users'}
        <label>Лозинка
          <input type="password" bind:value={password} minlength="8" required autocomplete="new-password"/>
        </label>{/if}{/if}
    <button class="primary">
      <Icon name="plus"/> Додај
    </button>
  </form>
  {#if token}
    <label>Копирај нови токен
      <input value={token} readonly/>
    </label>{/if}
  {#if info}
    {#each [['users',info.accounts],['tokens',info.tokens],['hooks',info.hooks]] as group}
      {#each group[1] as item}
        <div class="item"><span class="grow">{item.name||item.kind}
            <small>{item.role||'Webhook'}</small></span>
          <button class="icon" aria-label="Уклони" on:click={()=>remove(group[0],item.id||item.name)}>
            <Icon name="trash-2"/>
          </button>
        </div>{/each}{/each}{/if}
</section>
<section class="panel">
  <h2>Резервне копије</h2>
  <p>Копија садржи снимке, програм, налоге и интеграције. Главни администратор се чита из локалног .env фајла.</p>
  <a class="btn" href="/api/admin/backup" download>
    <Icon name="download"/> Преузми ZIP
  </a>
  <details>
    <summary>Врати резервну копију</summary>
    <div class="filters">
      <input type="file" aria-label="Резервна копија" accept=".zip" on:change={e=>restoreFile=e.target.files[0]}/>
      <button disabled={busy||!restoreFile} on:click={restore}>Припреми враћање</button>
    </div>
  </details>
</section>
<section class="panel">
  <h2>Дневник измена</h2>
  {#each (info?.audit||[]).slice(-30).reverse() as e}
    <div class="item"><span class="mono">{clock(e.at)}</span><b class="grow">{e.action}</b>
      <small>{e.user}</small>
    </div>{:else}
    <p>Овде се бележе административне измене.</p>{/each}
</section>
