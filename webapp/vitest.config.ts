import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig({
  plugins: [react()],
  test: {
    globals: true,
    environment: 'jsdom',
    environmentOptions: {
      jsdom: {
        // 启用更好的 CSS 支持
        resources: 'usable',
      },
    },
    setupFiles: ['./src/test/setup-dom.ts', './src/test/setup.ts'],
    include: ['src/**/*.{test,spec}.{js,mjs,cjs,ts,mts,cts,jsx,tsx}'],
    exclude: [
      '**/node_modules/**',
      '**/dist/**',
      '**/e2e/**',  // 排除 E2E 测试
    ],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json', 'html', 'lcov'],
      reportsDirectory: './coverage',
      include: [
        'src/services/**/*.ts',
        'src/hooks/**/*.ts',
        'src/components/**/*.tsx',
        'src/pages/**/*.tsx',
        'src/utils/**/*.ts',
      ],
      exclude: [
        'src/**/*.test.ts',
        'src/**/*.test.tsx',
        'src/test/**/*',
        'src/**/*.d.ts',
        'src/main.tsx',
        'src/App.tsx',
      ],
      thresholds: {
        // 覆盖率阈值（可根据项目调整）
        statements: 50,
        branches: 50,
        functions: 50,
        lines: 50,
      },
    },
    // 测试超时
    testTimeout: 10000,
    hookTimeout: 10000,
    // 并发
    pool: 'threads',
    poolOptions: {
      threads: {
        singleThread: false,
      },
    },
    // 报告 - 默认只使用 default reporter
    // 如需 html 报告，安装 @vitest/ui 后取消注释
    reporters: ['default'],
    // outputFile: {
    //   html: './test-results/index.html',
    // },
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
});
