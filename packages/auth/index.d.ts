export type OidcTokens = {
  accessToken: string;
  idToken?: string;
  refreshToken?: string;
  expiresAt: number;
};

export type OidcClient = {
  login(returnTo?: string): Promise<void>;
  initialize(): Promise<OidcTokens | null>;
  refresh(): Promise<OidcTokens | null>;
  logout(): void;
};

export function createOidcClient(options: {
  issuer: string;
  clientId: string;
  redirectUri?: string;
  onToken: (token: string) => void;
}): OidcClient;
