import react from '@vitejs/plugin-react-swc';
import { defineConfig } from 'vite';

export default defineConfig({
    plugins: [react()],
    server: {
        proxy: {
            '/api': {
                target: 'http://localhost:8080', // Ihr Go-Webservice
                changeOrigin: true,
                // rewrite: (path) => path.replace(/^\/api/, ''), // Entfernen Sie '/api' aus dem Pfad
            },
        },
    },
});
