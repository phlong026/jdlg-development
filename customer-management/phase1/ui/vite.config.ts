import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
export default defineConfig({plugins:[vue()],server:{port:5178,proxy:{'/api':'http://localhost:8087','/iycallback':'http://localhost:8087'}}})

