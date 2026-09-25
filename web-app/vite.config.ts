import react from '@vitejs/plugin-react-swc';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';

const root = path.dirname(fileURLToPath(import.meta.url));

// A UI extension is compiled into the app when PGPT_UI_EXTENSION names its
// entry module (see src/extension-api.ts); otherwise an empty stub is used.
const extensionEntry = process.env.PGPT_UI_EXTENSION
    ? path.resolve(process.env.PGPT_UI_EXTENSION)
    : path.resolve(root, 'src/extensions/none.ts');

export default defineConfig({
    plugins: [react()],
    resolve: {
        alias: {
            '@pgpt/ui-extension': extensionEntry,
            '@paperless-gpt/app': path.resolve(root, 'src/extension-api.ts'),
        },
        // Extension sources live outside this package; resolve shared
        // libraries from here so there is exactly one React in the bundle.
        dedupe: ['react', 'react-dom', 'react-router-dom', '@heroicons/react', 'axios', 'classnames'],
    },
    server: {
        fs: { allow: [root, path.dirname(extensionEntry)] },
        proxy: {
            '/api': {
                target: 'http://localhost:8080', // Ihr Go-Webservice
                changeOrigin: true,
                // rewrite: (path) => path.replace(/^\/api/, ''), // Entfernen Sie '/api' aus dem Pfad
            },
            '/extensions': {
                target: 'http://localhost:8080',
                changeOrigin: true,
            },
        },
    },
});
