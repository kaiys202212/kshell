import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    // 不清空输出目录：frontend/dist/.gitkeep 是 embed 占位文件，必须常驻
    emptyOutDir: false,
  },
})
