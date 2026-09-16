import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import { fileURLToPath, URL } from 'node:url';

export default defineConfig(({ mode }) => {
    const env = loadEnv(mode, process.cwd(), '');
    // 开发代理默认只连本机 Go 服务；指向真实环境必须显式配置 VITE_DEV_PROXY_TARGET，
    // 避免本地调试误写生产数据。
    const proxyTarget = env.VITE_DEV_PROXY_TARGET || 'http://localhost:8080';

    return {
        base: '/admin/',
        plugins: [react()],
        resolve: {
            alias: {
                'react-icons/fi': fileURLToPath(new URL('./src/compat/react-icons-fi-proxy.js', import.meta.url)),
            },
        },
        build: {
            // Open tabs can still request earlier hashed chunks during a preview update.
            // Production releases should use separate directories and retain the previous release.
            emptyOutDir: false,
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
