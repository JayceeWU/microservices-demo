declare module '@dancehub/telemetry' {
  export function initializeBrowserTelemetry(serviceName: string): void;
}

declare module '@dancehub/auth' {
  export type OidcTokens = {
    accessToken: string;
    idToken?: string;
    refreshToken?: string;
    expiresAt: number;
  };
  export function createOidcClient(options: {
    issuer: string;
    clientId: string;
    redirectUri?: string;
    onToken: (token: string) => void;
  }): {
    login(returnTo?: string): Promise<void>;
    initialize(): Promise<OidcTokens | null>;
    logout(): void;
  };
}

declare global {
  var DANCEHUB_API_URL: string;
  var DANCEHUB_OIDC_ISSUER: string;
  var DANCEHUB_AUTH_MODE: string;
}

export {};
