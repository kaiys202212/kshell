/// <reference types="vitest/config" />
import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    // 不清空输出目录：frontend/dist/.gitkeep 是 embed 占位文件，必须常驻
    emptyOutDir: false,
  },
  test: {
    // 组件测试跑在 jsdom 里；测试文件自身从 vitest 导入 API（未开 globals）
    environment: 'jsdom',
    // 统一注册 i18n 内置资源，组件测试可直接用 t()/tt()
    setupFiles: ['./src/test/setup.ts'],
  },
})
