const sampleRate = 48000;
const framesPerBuffer = 7680;
const bytesPerFrame = 4; // Two signed little-endian 16-bit channels.

export function openRadioPlayer(station, failed, initialVolume = 1) {
  const controller = new AbortController();
  const { signal } = controller;
  let context;
  let gain;
  let volume = initialVolume;
  let startupTimer;

  function close() {
    if (signal.aborted) return;
    controller.abort();
    clearTimeout(startupTimer);
    context?.close().catch(() => {});
  }

  function report(message) {
    if (signal.aborted) return;
    close();
    failed(message);
  }

  async function play() {
    context = new AudioContext();
    gain = context.createGain();
    gain.gain.value = volume;
    gain.connect(context.destination);
    // Resume synchronously from the user's click before waiting for the network.
    await context.resume();
    signal.throwIfAborted();

    let response;
    try {
      response = await fetch('/stream/' + encodeURIComponent(station.slug) + '/pcm', { signal });
    } catch (error) {
      if (signal.aborted) throw error;
      throw new Error('Нема везе са ZliCast сервером.');
    }
    if (!response.ok || !response.body) {
      throw new Error(response.status === 503
        ? 'Станица тренутно не шаље звук.'
        : `ZliCast није отворио стрим (HTTP ${response.status}).`);
    }

    const reader = response.body.getReader();
    const samples = new Uint8Array(framesPerBuffer * bytesPerFrame);
    const view = new DataView(samples.buffer);
    let filled = 0;
    let nextAt = 0;

    function schedule() {
      const now = context.currentTime;
      if (nextAt > now + 0.85) return; // Recover after a burst without drifting behind live.
      if (nextAt <= now + 0.025) nextAt = now + 0.16;

      const buffer = context.createBuffer(2, framesPerBuffer, sampleRate);
      const left = buffer.getChannelData(0);
      const right = buffer.getChannelData(1);
      for (let i = 0; i < framesPerBuffer; i++) {
        left[i] = view.getInt16(i * bytesPerFrame, true) / 32768;
        right[i] = view.getInt16(i * bytesPerFrame + 2, true) / 32768;
      }
      const source = context.createBufferSource();
      source.buffer = buffer;
      source.connect(gain);
      source.onended = () => source.disconnect();
      source.start(nextAt);
      nextAt += buffer.duration;
      clearTimeout(startupTimer);
    }

    try {
      while (!signal.aborted) {
        const { value, done } = await reader.read();
        if (done) throw new Error('ZliCast је затворио аудио-стрим.');
        for (let offset = 0; offset < value.length;) {
          const size = Math.min(samples.length - filled, value.length - offset);
          samples.set(value.subarray(offset, offset + size), filled);
          filled += size;
          offset += size;
          if (filled === samples.length) {
            schedule();
            filled = 0;
          }
        }
      }
    } finally {
      await reader.cancel().catch(() => {});
      reader.releaseLock();
    }
  }

  return {
    close,
    setVolume(level) {
      volume = level;
      if (gain && !signal.aborted) {
        gain.gain.setTargetAtTime(volume, context.currentTime, 0.01);
      }
    },
    start() {
      if (!globalThis.AudioContext) {
        report('Овај прегледач не подржава Web Audio. Станицу можеш отворити преко M3U плејлисте.');
        return;
      }
      startupTimer = setTimeout(() => report(context?.state === 'suspended'
        ? 'Прегледач је блокирао звук за ову страницу.'
        : 'Станица није послала звук на време.'), 10000);
      play().catch(error => report(error.name === 'NotAllowedError'
        ? 'Прегледач је блокирао звук за ову страницу.'
        : error.message));
    }
  };
}
