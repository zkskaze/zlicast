export default {
  kit: {
    adapter: {
      name: 'zlicast-embedded',
      async adapt(builder) {
        builder.rimraf('build');
        builder.writeClient('build');
        await builder.generateFallback('build/index.html');
      }
    },
    paths: { relative: false }
  }
};
