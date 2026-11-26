function waitForEvent(target, name, signal) {
  return new Promise((resolve, reject) => {
    signal.throwIfAborted();
    const stop = () => {
      target.removeEventListener(name, done);
      target.removeEventListener('error', failed);
      signal.removeEventListener('abort', aborted);
    };
    const done = () => {
      stop();
      resolve();
    };
    const failed = () => {
      stop();
      reject(new Error('Прегледач није могао да декодира MP3 стрим.'));
    };
    const aborted = () => {
      stop();
      reject(signal.reason);
    };
    target.addEventListener(name, done, { once: true });
    target.addEventListener('error', failed, { once: true });
    signal.addEventListener('abort', aborted, { once: true });
  });
}

async function appendStream(audio, source, ready, url, signal) {
  await ready;
  const response = await fetch(url, { signal, cache: 'no-store' });
  if (!response.ok || !response.body) {
    throw new Error(response.status === 503
      ? 'Станица тренутно нема аудио. Провери да ли је у етру.'
      : `Аудио-стрим није доступан (HTTP ${response.status}).`);
  }

  const buffer = source.addSourceBuffer('audio/mpeg');
  const reader = response.body.getReader();
  async function update(action) {
    signal.throwIfAborted();
    action();
    await waitForEvent(buffer, 'updateend', signal);
  }

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) throw new Error('Сервер је затворио аудио-стрим.');
      await update(() => buffer.appendBuffer(value));
      if (buffer.buffered.length && audio.currentTime - buffer.buffered.start(0) > 30) {
        await update(() => buffer.remove(0, audio.currentTime - 5));
      }
    }
  } finally {
    await reader.cancel().catch(() => {});
    reader.releaseLock();
  }
}

export function openRadioPlayer(station, failed) {
  const audio = new Audio();
  const controller = new AbortController();
  const { signal } = controller;
  const url = '/stream/' + encodeURIComponent(station.slug);
  let objectURL;

  function close() {
    if (signal.aborted) return;
    controller.abort();
    audio.onerror = null;
    audio.pause();
    audio.removeAttribute('src');
    audio.load();
    if (objectURL) URL.revokeObjectURL(objectURL);
  }
  function report(message) {
    if (signal.aborted) return;
    close();
    failed(message);
  }

  return {
    close,
    start() {
      audio.onerror = () => {
        const detail = audio.error?.code;
        report(detail === 4
          ? 'Прегледач није могао да отвори формат аудио-стрима.'
          : `Репродукција је прекинута (грешка ${detail || '?'}).`);
      };
      if (station.format === 'mp3' && globalThis.MediaSource?.isTypeSupported('audio/mpeg')) {
        const source = new MediaSource();
        const ready = waitForEvent(source, 'sourceopen', signal);
        objectURL = URL.createObjectURL(source);
        audio.src = objectURL;
        appendStream(audio, source, ready, url, signal).catch(error => report(error.message));
      } else {
        audio.src = url;
      }
      audio.play().catch(error => report(error.name === 'NotAllowedError'
        ? 'Прегледач је блокирао звук. Провери дозволу за звук на овој страници.'
        : `Репродукција није покренута (${error.name}).`));
    }
  };
}
