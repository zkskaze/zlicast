export async function api(path, method = 'GET', value) {
  const opt = { method, credentials: 'same-origin' };
  if (value instanceof FormData || value instanceof Blob) {
    opt.body = value;
  } else if (value !== undefined) {
    opt.headers = { 'Content-Type': 'application/json' };
    opt.body = JSON.stringify(value);
  }
  let r;
  try {
    r = await fetch(path, opt);
  } catch {
    throw new Error('Веза са ZliCast сервером је прекинута.');
  }
  const v = await r.json().catch(() => ({}));
  if (!r.ok) {
    const e = new Error(v.error || `Захтев није успео (HTTP ${r.status}).`);
    e.status = r.status;
    throw e;
  }
  return v;
}

export async function importMedia(form, onProcessing) {
  return waitForMediaImport(await api('/api/media?async=1', 'POST', form), onProcessing);
}

export async function waitForMediaImport({ job }, onProcessing) {
  if (!job) throw new Error('ZliCast није потврдио захтев за увоз.');
  onProcessing?.();
  for (;;) {
    await new Promise(resolve => setTimeout(resolve, 1200));
    const status = await api('/api/media/imports/' + encodeURIComponent(job));
    if (status.status === 'complete') return status;
    if (status.status === 'failed') throw new Error(status.error);
  }
}
export const cats = {
  music: 'Музика',
  jingles: 'Џинглови',
  news: 'Вести',
  ads: 'Рекламе',
  shows: 'Емисије',
  emergency: 'Хитна обавештења',
  fallback: 'Резервни програм',
  live: 'Уживо'
};
export const clock = (s) =>
  new Date(s).toLocaleTimeString('sr-RS', { hour: '2-digit', minute: '2-digit' });
export const duration = (s) =>
  Math.floor((s || 0) / 60) + ':' + String(Math.floor((s || 0) % 60)).padStart(2, '0');
export const dateISO = (d = new Date()) =>
  new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 10);
export const permitted = (role, ...allowed) => role === 'admin' || allowed.includes(role);
