<script>
  import { api, waitForMediaImport } from './api.js';

  export let station = '';
  export let category = 'music';
  export let tags = '';
  export let expires = '';
  export let disabled = false;
  export let refresh;

  let url = '';
  let title = '';
  let busy = false;
  let message = '';
  let failed = false;

  async function submit() {
    if (busy || disabled || !url.trim()) return;
    busy = true;
    failed = false;
    message = 'Преузимам и обрађујем звук…';
    try {
      const job = await api('/api/media/youtube', 'POST', { url: url.trim(), title: title.trim(), station, category, tags, expires });
      const result = await waitForMediaImport(job);
      url = '';
      title = '';
      await refresh();
      message = result.duplicate ? '„' + result.media.title + '“ је већ у медијатеци.' : 'Додато · „' + result.media.title + '“';
    } catch (error) {
      failed = true;
      message = error.message;
    } finally {
      busy = false;
    }
  }
</script>

<form on:submit|preventDefault={submit}>
  <label>
    YouTube линк
    <input type="url" bind:value={url} placeholder="https://youtu.be/…" maxlength="4096"
      required disabled={busy || disabled} />
  </label>
  <label>
    Наслов песме (необавезно)
    <input type="text" bind:value={title} placeholder="Наслов са YouTube-а" maxlength="200" disabled={busy || disabled} />
  </label>
  <button type="submit" disabled={busy || disabled || !url.trim()}>Увези са YouTube-а</button>
</form>
<div role="status" class:failed>{message}</div>

<style>
  form {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: 8px;
    margin-top: 12px;
  }
  label {
    flex: 1 1 16rem;
    min-width: 0;
    margin: 0;
  }
  [role='status']:not(:empty) {
    margin-top: 8px;
  }
  .failed {
    color: #ffada2;
  }
</style>
