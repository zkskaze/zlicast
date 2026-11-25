<script>
  import { onMount } from 'svelte';
  import '../style.css';
  import Icon from '$lib/Icon.svelte';
  import Art from '$lib/Art.svelte';
  import Library from '$lib/Library.svelte';
  import StationForm from '$lib/StationForm.svelte';
  import Program from '$lib/Program.svelte';
  import Studio from '$lib/Studio.svelte';
  import Admin from '$lib/Admin.svelte';
  import Stats from '$lib/Stats.svelte';
  import { api, cats, duration, permitted } from '$lib/api.js';
  let mode = 'loading',
    data = null,
    view = 'home',
    stationEdit = undefined,
    notice = '',
    loginError = '',
    username = 'admin',
    password = '',
    busy = false,
    show = false,
    publicSlug = '',
    pub = null,
    publicError = '',
    history = [],
    player = '',
    audio,
    toastTimer,
    navOpen = false;
  const nav = [
    ['home', 'layout-dashboard', 'Контролна соба'],
    ['media', 'library-big', 'Медијатека'],
    ['schedule', 'calendar-days', 'Распоред'],
    ['studio', 'mic', 'Студио уживо'],
    ['stats', 'activity', 'Статистика'],
    ['admin', 'settings', 'Администрација']
  ];
  const notify = (s) => {
    notice = s;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => (notice = ''), 7000);
  };
  const go = (v) => {
    view = v;
    navOpen = false;
    historyReplace(v);
  };
  function historyReplace(v) {
    window.history.replaceState(null, '', v === 'home' ? '/' : '/#' + v);
  }
  async function refresh() {
    try {
      data = await api('/api/dashboard');
      mode = 'admin';
    } catch (e) {
      if (e.status === 401) {
        if (player) endListen();
        mode = 'login';
      } else notify(e.message);
    }
  }
  async function publicData() {
    try {
      pub = await api('/api/stations/' + publicSlug + '/now-playing');
      history = (await api('/api/stations/' + publicSlug + '/history')) || [];
      publicError = '';
    } catch (e) {
      publicError = e.message;
    }
  }
  onMount(() => {
    publicSlug = location.pathname.match(/^\/listen\/([^/]+)$/)?.[1] || '';
    if (publicSlug) {
      mode = 'public';
      publicData();
    } else {
      const h = location.hash.slice(1);
      if (nav.some((n) => n[0] === h)) view = h;
      refresh();
    }
    const t = setInterval(() => {
      if (mode === 'admin' && !document.hidden) refresh();
      if (mode === 'public') publicData();
    }, 3500);
    return () => {
      clearInterval(t);
      clearTimeout(toastTimer);
      endListen();
    };
  });
  async function login() {
    busy = true;
    loginError = '';
    try {
      await api('/api/login', 'POST', { user: username, password });
      password = '';
      await refresh();
    } catch (e) {
      loginError = e.message;
    } finally {
      busy = false;
    }
  }
  async function logout() {
    try {
      await api('/api/logout', 'POST');
      endListen();
      data = null;
      mode = 'login';
      go('home');
    } catch (e) {
      notify(e.message);
    }
  }
  function endListen() {
    if (audio) {
      audio.pause();
      audio.removeAttribute('src');
      audio.load();
      audio = null;
    }
    player = '';
  }
  async function listen(s) {
    if (player === s.slug) {
      endListen();
      return;
    }
    endListen();
    const a = new Audio('/stream/' + s.slug);
    audio = a;
    player = s.slug;
    a.onerror = () => {
      if (audio === a) {
        endListen();
        notify('Стрим је прекинут. Покрени слушање поново.');
      }
    };
    try {
      await a.play();
    } catch (e) {
      if (audio === a) {
        endListen();
        notify('Прегледач није покренуо репродукцију.');
      }
    }
  }
  $: title = nav.find((n) => n[0] === view)?.[2] || 'Контролна соба';
  $: if (data && view === 'admin' && data.role !== 'admin') view = 'home';
  $: if (data && view === 'studio' && !permitted(data.role, 'editor', 'dj')) view = 'home';
  $: totalListeners = data?.stations.reduce((n, s) => n + s.status.listeners, 0) || 0;
  const brand = 'ZliCast';
</script>
<svelte:head><title>{mode==='public'&&pub?pub.name:mode==='admin'?title:'Радио студио'} · ZliCast</title></svelte:head>
{#if mode==='loading'}
  <main class="boot"><img src="/mark.svg" alt=""/> ZliCast</main>
  {:else if mode==='login'}
    <main class="login-page">
      <header class="brand"><img src="/mark.svg" alt=""/>{brand}</header>
      <section class="loginbox">
        <div class="login-ident">
          <Art large={true} name="ZliCast" slug="zks" color="#c5e6a0"/>
        </div>
        <div class="loginform"><span class="tiny">
            <Icon name="radio-tower" size={15}/> ZKS / РАДИО СТУДИО</span>
          <h1>Пријава у студио</h1>
          <form on:submit|preventDefault={login}>
            <label>Корисничко име
              <input autocomplete="username" bind:value={username} required/>
            </label>
            <label>Лозинка
              <div class="pass">
                <input type={show?'text':'password'} autocomplete="current-password" bind:value={password} required/>
                <button aria-label="Прикажи или сакриј лозинку" class="icon" type="button" on:click={()=>show=!show}>
                  <Icon name={show?'eye-off':'eye'}/>
                </button>
              </div>
            </label>
            {#if loginError}
              <p class="error" role="alert">{loginError}</p>{/if}
            <button class="primary wide" disabled={busy}>{busy?'Пријављивање…':'Уђи у студио'}
              <Icon name="arrow-right"/>
            </button>
          </form>
          <small>Подаци за локалну пријаву су у фајлу <code>.env</code>.</small>
        </div>
      </section>
      <footer>ZKS <span>·</span> CRNBG.ORG</footer>
    </main>
    {:else if mode==='public'}
      <main class="listenpage">
        <a class="brand" href="/"><img src="/mark.svg" alt=""/>ZliCast</a>
        {#if pub}
          <section class="listenbox">
            <Art slug={pub.slug} name={pub.name} color={pub.color} logo={pub.logo} large={true}/>
            <div class="listenbody">
              <div class="row"><span class="pill" class:live={pub.status.onAir}>{pub.status.onAir?'● У ЕТРУ':'Припрема'}</span><span class="mono">{pub.format.toUpperCase()} / {pub.bitrate} kbps</span></div>
              <h1>{pub.name}</h1>
              <h2>{pub.status.now.title||'Покретање програма'}</h2>
              <p>{pub.status.now.artist||cats[pub.status.now.category]}</p>
              <div class="controls">
                <button class="primary" on:click={()=>listen(pub)}>
                  <Icon name={player?'pause':'play'}/>{player?'Заустави':'Слушај уживо'}
                </button>
                <a class="btn" href={'/listen/'+pub.slug+'.m3u'}>M3U
                  <Icon name="download"/>
                </a>
              </div>
            </div>
          </section>
          {#if history.length>1}
            <section class="panel">
              <h2>Претходно у етру</h2>
              {#each history.slice(1,5) as h}
                <div class="item"><b class="grow">{h.title}</b>
                  <small>{h.artist}</small>
                </div>{/each}
            </section>{/if}{:else}
          <p>{publicError||'Повезивање са станицом…'}</p>{/if}
      </main>
      {:else if data}
        <div class="shell">
          <aside class:open={navOpen}>
            <div class="brand"><img src="/mark.svg" alt=""/>ZliCast</div>
            <nav>
              {#each nav as [id,icon,label]}
                {#if (id!=='admin'||data.role==='admin')&&(id!=='studio'||permitted(data.role,'dj','editor'))}
                  <button class:active={view===id} on:click={()=>go(id)}>
                    <Icon name={icon}/>{label}
                  </button>{/if}{/each}
            </nav>
            <div class="sidebarfoot">
              <div class="avatar">{data.user.slice(0,1).toUpperCase()}</div>
              <div class="grow"><b>{data.user}</b>
                <small>{data.role==='admin'?'Администратор':data.role}</small>
              </div>
              <button class="icon" aria-label="Одјави се" on:click={logout}>
                <Icon name="log-out"/>
              </button>
            </div>
          </aside>
          <main class="console">
            <header class="topbar">
              <div class="controls">
                <button class="mobile icon" aria-label="Навигација" on:click={()=>navOpen=!navOpen}>
                  <Icon name="menu"/>
                </button>
                <Icon name="radio-tower"/><span>ZKS</span><span class="slash">/</span><b>Радио студио</b>
              </div><span class="connected"><i></i> Сервер повезан</span>
            </header>
            <div class="content">
              <div class="pagehead">
                <div><span class="tiny">{view==='home'?'ZLICAST / MASTER CONTROL':'ZLICAST / STUDIO'}</span>
                  <h1>{title}</h1>
                </div>
                {#if view==='home'&&data.role==='admin'}
                  <button class="primary" on:click={()=>stationEdit=null}>
                    <Icon name="plus"/> Нова станица
                  </button>{/if}
              </div>
              {#if view==='home'}
                <div class="summary">
                  {#each [['radio-tower',data.stations.filter(s=>s.status.onAir).length,'Станице у етру'],['headphones',totalListeners,'Слушалаца сада'],['disc-3',data.media.length,'Снимака у библиотеци']] as m}
                    <div>
                      <Icon name={m[0]} size={19}/><b>{m[1]}</b><span>{m[2]}</span>
                    </div>{/each}
                </div>
                <div class="sectionhead">
                  <h2>Радио-станице</h2><span class="mono">FM / IP</span>
                </div>
                <div class="stations">
                  {#each data.stations as s}
                    <article class="station">
                      <Art slug={s.slug} name={s.name} color={s.color} logo={s.logo}/>
                      <div class="stationbody">
                        <div class="row">
                        <h2>{s.name}</h2><span class="pill" class:live={s.status.onAir}>{s.status.onAir?'● У ЕТРУ':'Припрема'}</span>
                        </div>
                        <div class="specs">{s.format.toUpperCase()} <span>/</span> {s.bitrate} kbps <span>/</span> {s.status.listeners}
                        <Icon name="headphones" size={12}/>
                        </div>
                        <div class="now">
                        <div class="audioicon">
                        <Icon name="audio-lines" size={22}/>
                        </div>
                        <div class="grow">
                        <small>Сада свира</small><b>{s.status.now.title||'Припрема програма'}</b>
                        </div>
                        </div>
                        <div class="stationactions">
                        <button on:click={()=>listen(s)} class:playing={player===s.slug}>
                        <Icon name={player===s.slug?'pause':'play'} size={15}/>{player===s.slug?'Заустави':'Слушај'}
                        </button>
                        <a class="icon" href={'/listen/'+s.slug} target="_blank" rel="noreferrer" aria-label={'Јавни плејер · '+s.name} title="Јавни плејер">
                        <Icon name="external-link"/>
                        </a>
                        {#if data.role==='admin'}
                        <button class="icon" on:click={()=>stationEdit=s} aria-label={'Подешавања · '+s.name} title="Подешавања">
                        <Icon name="sliders-horizontal"/>
                        </button>{/if}
                        </div>
                      </div>
                    </article>{:else}
                    <p>Додај прву станицу да покренеш емитовање.</p>{/each}
                </div>
                <div class="sectionhead">
                  <h2>Медијатека</h2>
                  <button class="textbtn" on:click={()=>go('media')}>Отвори библиотеку
                    <Icon name="arrow-up-right" size={15}/>
                  </button>
                </div>
                <section class="panel compact">
                  {#if data.media.length}
                    {#each data.media.slice(-4).reverse() as m}
                      <div class="item">
                        <Icon name="music-2"/><b class="grow">{m.title}</b>
                        <small>{cats[m.category]}</small><span class="mono">{duration(m.duration)}</span>
                      </div>{/each}{:else}
                    <div class="item">
                      <div class="thumb">
                        <Icon name="cloud-upload"/>
                      </div>
                      <div class="grow"><b>Додај прву нумеру</b>
                        <small>Док је библиотека празна, станице пуштају „Ноћни сигнал“.</small>
                      </div>
                      <button class="icon" aria-label="Отпреми музику" on:click={()=>go('media')}>
                        <Icon name="arrow-right"/>
                      </button>
                    </div>{/if}
                </section>
                {:else if view==='media'}
                  <Library {data} {refresh} {notify}/>
                  {:else if view==='schedule'}
                    <Program {data} {refresh} {notify}/>
                    {:else if view==='studio'}
                      <Studio {data} {refresh} {notify}/>
                      {:else if view==='stats'}
                        <Stats {data} {notify}/>
                        {:else if view==='admin'}
                        <Admin {notify}/>{/if}
              <footer>ZKS <span>·</span> CRNBG.ORG</footer>
            </div>
          </main>
        </div>{/if}
{#if stationEdit!==undefined}
  <StationForm station={stationEdit} close={()=>stationEdit=undefined} done={refresh} {notify}/>{/if}
{#if notice}
  <div class="toast controls" role="status">{notice}
    <button class="icon" aria-label="Затвори" on:click={()=>notice=''}>
      <Icon name="x" size={14}/>
    </button>
  </div>{/if}
