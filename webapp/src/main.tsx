import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';
import { initVpnClient } from './vpn-client';

async function bootstrap() {
  await initVpnClient();
  ReactDOM.createRoot(document.getElementById('root')!).render(
    <React.StrictMode>
      <App />
    </React.StrictMode>,
  );
}

bootstrap();
