import '@dancehub/ui/tokens.css';
import '@dancehub/ui/controls.css';
import './styles.css';
import { Providers } from './providers';

export const metadata = {
  title: 'BayAreaDanceHub',
  description: 'Book Bay Area dance classes and studios',
};
export const dynamic = 'force-dynamic';

export default function Layout({ children }: { children: React.ReactNode }) {
  const apiUrl = process.env.DANCEHUB_API_URL;
  const oidcIssuer = process.env.DANCEHUB_OIDC_ISSUER;
  const authMode = process.env.DANCEHUB_AUTH_MODE;
  const environment = process.env.DANCEHUB_ENVIRONMENT;
  if (!apiUrl || !oidcIssuer || !authMode || !environment)
    throw new Error('Student Web runtime configuration is incomplete');
  const runtimeConfig = JSON.stringify({
    DANCEHUB_API_URL: apiUrl,
    DANCEHUB_OIDC_ISSUER: oidcIssuer,
    DANCEHUB_AUTH_MODE: authMode,
    DANCEHUB_ENVIRONMENT: environment,
  }).replaceAll('<', '\\u003c');
  return (
    <html lang="en">
      <head>
        <script
          dangerouslySetInnerHTML={{
            __html: `Object.assign(globalThis, ${runtimeConfig});`,
          }}
        />
      </head>
      <body>
        <Providers apiUrl={apiUrl}>{children}</Providers>
      </body>
    </html>
  );
}
