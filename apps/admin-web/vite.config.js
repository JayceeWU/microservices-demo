import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  build: {
    chunkSizeWarningLimit: 350,
    rollupOptions: {
      output: {
        manualChunks: {
          'vendor-react': ['react', 'react-dom', 'react-router-dom'],
          'vendor-mui': [
            '@emotion/react',
            '@emotion/styled',
            '@mui/icons-material',
            '@mui/material',
          ],
          'vendor-query': ['@tanstack/react-query'],
          'vendor-dancehub': ['@dancehub/api-client', '@dancehub/auth', '@dancehub/telemetry'],
        },
      },
    },
  },
});
