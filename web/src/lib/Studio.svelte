<script>
  import { onDestroy } from 'svelte';
  import Icon from './Icon.svelte';
  import { api, cats, duration, permitted } from './api.js';
  export let data, refresh, notify;
  let slug = data.stations[0]?.slug || '',
    title = 'Студио уживо',
    mic = 80,
    music = 35,
    pad = 85,
    master = 85,
    level = 0,
    running = false,
    live = false,
    rec = false,
    busy = false,
    category = 'news',
    recorded = null,
    url = '',
    background = '',
    seconds = 0;
  let ctx,
    stream,
    mg,
    bg,
    sg,
    out,
    dest,
    worklet,
    analyzer,
    ws,
    record,
    bed,
    raf,
    timer,
    quiet,
    readyPromise;
  let sources = new Set(),
    story = '',
    expiry = '';
  async function speech() {
    busy = true;
    try {
      await api('/api/news/speech', 'POST', { title, text: story, station: slug, expires: expiry });
      await refresh();
      notify('Вести су сачуване у медијатеци.');
    } catch (e) {
      notify(e.message);
    } finally {
      busy = false;
    }
  }
  $: if (mg) mg.gain.value = mic / 100;
  $: if (bg) bg.gain.value = music / 100;
  $: if (sg) sg.gain.value = pad / 100;
  $: if (out) out.gain.value = master / 100;
  $: media = data.media.filter((m) => !m.station || m.station === slug);
  async function prepare() {
    if (running) return;
    if (readyPromise) return readyPromise;
    readyPromise = (async () => {
      try {
        if (!navigator.mediaDevices?.getUserMedia)
          throw Error('Микрофон захтева localhost или HTTPS.');
        ctx = new AudioContext({ sampleRate: 48000 });
        await ctx.resume();
        stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true } });
        dest = ctx.createMediaStreamDestination();
        mg = ctx.createGain();
        bg = ctx.createGain();
        sg = ctx.createGain();
        out = ctx.createGain();
        mg.gain.value = mic / 100;
        bg.gain.value = music / 100;
        sg.gain.value = pad / 100;
        out.gain.value = master / 100;
        ctx.createMediaStreamSource(stream).connect(mg);
        [mg, bg, sg].forEach((g) => g.connect(out));
        out.connect(dest);
        analyzer = ctx.createAnalyser();
        out.connect(analyzer);
        await ctx.audioWorklet.addModule('/studio-worklet.js');
        worklet = new AudioWorkletNode(ctx, 'zlicast-output');
        out.connect(worklet);
        quiet = ctx.createGain();
        quiet.gain.value = 0;
        worklet.connect(quiet).connect(ctx.destination);
        worklet.port.onmessage = (e) => {
          if (ws?.readyState === 1 && ws.bufferedAmount < 384000) ws.send(e.data);
        };
        running = true;
        const buf = new Uint8Array(analyzer.fftSize);
        function meter() {
          if (!running) return;
          analyzer.getByteTimeDomainData(buf);
          let peak = 0;
          for (const v of buf) peak = Math.max(peak, Math.abs(v - 128));
          level = peak / 128;
          raf = requestAnimationFrame(meter);
        }
        meter();
      } catch (e) {
        stop();
        throw e;
      } finally {
        readyPromise = null;
      }
    })();
    return readyPromise;
  }
  function end() {
    const socket = ws;
    ws = null;
    live = false;
    if (socket) socket.close();
    clearInterval(timer);
    seconds = 0;
  }
  function stop() {
    end();
    if (record?.state === 'recording') record.stop();
    running = false;
    cancelAnimationFrame(raf);
    sources.forEach((s) => {
      try {
        s.stop();
      } catch {}
    });
    sources.clear();
    stream?.getTracks().forEach((t) => t.stop());
    ctx?.close().catch(() => {});
    ctx = null;
    mg = bg = sg = out = null;
    bed = null;
    level = 0;
  }
  async function goLive() {
    busy = true;
    try {
      await prepare();
      const socket = new WebSocket(
        (location.protocol === 'https:' ? 'wss://' : 'ws://') +
          location.host +
          '/api/stations/' +
          slug +
          '/live?title=' +
          encodeURIComponent(title)
      );
      ws = socket;
      await new Promise((ok, no) => {
        socket.onopen = ok;
        socket.onerror = () => no(Error('Студио је заузет или веза није успела.'));
      });
      live = true;
      seconds = 0;
      timer = setInterval(() => seconds++, 1000);
      socket.onclose = () => {
        if (ws === socket) {
          end();
          notify('Веза са студијом је завршена.');
        }
      };
    } catch (e) {
      end();
      notify(e.message);
    } finally {
      busy = false;
    }
  }
  async function play(id, isBed = false) {
    try {
      await prepare();
      const res = await fetch('/api/media/' + id + '/audio');
      if (!res.ok) throw Error('Снимак није доступан.');
      const b = await ctx.decodeAudioData(await res.arrayBuffer());
      const source = ctx.createBufferSource();
      source.buffer = b;
      source.loop = isBed;
      source.connect(isBed ? bg : sg);
      if (isBed) {
        bed?.stop();
        bed = source;
      }
      sources.add(source);
      source.onended = () => sources.delete(source);
      source.start();
    } catch (e) {
      notify(e.message);
    }
  }
  async function recordNow() {
    if (rec) {
      record.stop();
      return;
    }
    try {
      await prepare();
      record = new MediaRecorder(dest.stream);
      const chunks = [];
      record.ondataavailable = (e) => {
        if (e.data.size) chunks.push(e.data);
      };
      record.onstop = () => {
        if (url) URL.revokeObjectURL(url);
        recorded = new Blob(chunks, { type: record.mimeType });
        url = URL.createObjectURL(recorded);
        rec = false;
      };
      record.start(500);
      rec = true;
    } catch (e) {
      notify(e.message);
    }
  }
  async function archive() {
    if (busy) return;
    busy = true;
    try {
      const f = new FormData();
      f.append('file', recorded, (title || 'Емисија') + '.webm');
      f.append('station', slug);
      f.append('category', category);
      await api('/api/media', 'POST', f);
      await refresh();
      notify('Снимак је у медијатеци.');
      recorded = null;
      URL.revokeObjectURL(url);
      url = '';
    } catch (e) {
      notify(e.message);
    } finally {
      busy = false;
    }
  }
  onDestroy(() => {
    stop();
    if (url) URL.revokeObjectURL(url);
  });
</script>
<section class="panel">
  <div class="row">
    <h2>Миксета</h2><span class="pill" class:live>{live?'● У ЕТРУ · '+duration(seconds):'Ван етра'}</span>
  </div>
  <div class="formgrid">
    <label>Станица
      <select bind:value={slug} disabled={running}>
        {#each data.stations as s}
          <option value={s.slug}>{s.name}</option>{/each}
      </select>
    </label>
    <label>Назив емисије
      <input bind:value={title} disabled={live}/>
    </label>
  </div>
  <div class="faders">
    {#each [['Микрофон',mic,v=>mic=v],['Подлога',music,v=>music=v],['Џинглови',pad,v=>pad=v],['Излаз',master,v=>master=v]] as channel}
      <label><span>{channel[0]} <b>{channel[1]}%</b></span>
        <input type="range" min="0" max="100" value={channel[1]} on:input={e=>channel[2](+e.target.value)}/>
      </label>{/each}
  </div>
  <div class="meter"><i style={'width:'+Math.min(100,level*130)+'%'}></i></div>
  <div class="controls">
    {#if permitted(data.role,'dj')}
      <button class="primary" disabled={busy||!slug} on:click={()=>live?end():goLive()}>
        <Icon name="radio"/>{live?'Заврши пренос':'Укључи се уживо'}
      </button>{/if}
    <button disabled={busy||!slug} on:click={recordNow}>
      <Icon name={rec?'square':'mic'}/>{rec?'Заврши снимање':'Сними емисију'}
    </button>
    {#if running}
      <button on:click={stop}>Искључи микрофон</button>{/if}
  </div>
  {#if url}
    <div class="item wrap">
      <audio controls src={url}></audio>
      <select bind:value={category} aria-label="Категорија снимка">
        <option value="news">Вести</option>
        <option value="shows">Емисије</option>
      </select>
      <button disabled={busy} on:click={archive}>Сачувај у медијатеци</button>
    </div>{/if}
</section>
<section class="panel">
  <h2>Звучна табла</h2>
  <div class="controls">
    {#each media.filter(m=>['jingles','emergency','news'].includes(m.category)) as m}
      <button on:click={()=>play(m.id)}>
        <Icon name="play"/> {m.title}
      </button>{:else}
      <p>Додај џингл или вести у медијатеку да их пустиш преко микрофонског програма.</p>{/each}
  </div>
  <label>Музичка подлога
    <select bind:value={background}>
      <option value="">Изабери нумеру</option>
      {#each media.filter(m=>m.category==='music') as m}
        <option value={m.id}>{m.title}</option>{/each}
    </select>
  </label>
  <div class="controls">
    <button disabled={!background} on:click={()=>play(background,true)}>
      <Icon name="play"/> Пусти подлогу
    </button>
    <button disabled={!bed} on:click={()=>{bed.stop();bed=null}}>Заустави</button>
  </div>
</section>
<details class="panel">
  <summary>Спољни студио · BUTT</summary>
  <p>Изабери Icecast, адресу овог сервера и mount <code>/source/{slug}</code>. Корисник је <code>source</code>, а лозинка је у <code>SOURCE_PASSWORD</code> у фајлу <code>.env</code>. Подржани су MP3, AAC и OGG.</p>
</details>
<details class="panel">
  <summary>Претвори текст вести у говор</summary>
  <label>Текст вести
    <textarea rows="5" maxlength="6000" bind:value={story}></textarea>
  </label>
  <label>Датум истека
    <input type="date" bind:value={expiry}/>
  </label>
  <button disabled={busy||!story.trim()} on:click={speech}>Сачувај звучне вести</button>
</details>
