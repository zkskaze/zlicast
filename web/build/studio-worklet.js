class RadioOutput extends AudioWorkletProcessor {
  constructor() {
    super();
    this.buffer = new Int16Array(1920);
    this.offset = 0;
  }
  process(inputs) {
    const ch = inputs[0];
    if (!ch || !ch.length) return true;
    for (let i = 0; i < ch[0].length; i++) {
      for (let side = 0; side < 2; side++) {
        const value = (ch[side] || ch[0])[i];
        this.buffer[this.offset++] = Math.round(Math.max(-1, Math.min(1, value)) * 32767);
      }
      if (this.offset === this.buffer.length) {
        this.port.postMessage(this.buffer.buffer, [this.buffer.buffer]);
        this.buffer = new Int16Array(1920);
        this.offset = 0;
      }
    }
    return true;
  }
}
registerProcessor('zlicast-output', RadioOutput);
