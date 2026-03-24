import { defineConfig } from 'vitest/config'

export default defineConfig({
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
  },
  resolve: {
    // Allow importing .ts files with .js extension (NodeNext ESM pattern)
    // When TypeScript code imports './foo.js', resolve to './foo.ts'
    alias: [
      {
        find: /^(\..*)\.js$/,
        replacement: '$1.ts',
      },
    ],
  },
})
