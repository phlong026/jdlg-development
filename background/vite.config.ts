import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import { fileURLToPath, URL } from 'node:url';

export default defineConfig(({ mode }) => {
    const env = loadEnv(mode, process.cwd(), '');
    const proxyTarget = env.VITE_DEV_PROXY_TARGET || 'https://api.jiadilingguang.com';

    return {
        base: '/admin/',
        plugins: [react()],
        resolve: {
            alias: {
                'react-icons/fi': fileURLToPath(new URL('./src/compat/react-icons-fi-proxy.js', import.meta.url)),
            },
        },
        build: {
            rollupOptions: {
                output: {
                    manualChunks(id) {
                        if (!id.includes('node_modules')) {
                            return undefined;
                        }
                        if (id.includes('react-router')) {
                            return 'router-vendor';
                        }
                        if (id.includes('react-dom') || id.includes('react')) {
                            return 'react-vendor';
                        }
                        if (id.includes('react-icons')) {
                            return 'icon-vendor';
                        }
                        return 'vendor';
                    },
                },
            },
        },
        server: {
            proxy: {
                '/api': {
                    target: proxyTarget,
                    changeOrigin: true,
                    secure: false,
                },
            },
        },
    };
});
