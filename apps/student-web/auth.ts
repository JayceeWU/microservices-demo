import NextAuth from 'next-auth';
import Keycloak from 'next-auth/providers/keycloak';
import type { JWT } from 'next-auth/jwt';

const required = (value: string | undefined, name: string) => {
  if (!value) throw new Error(`${name} is required`);
  return value;
};
type TokenResponse = { access_token: string; expires_in: number; refresh_token?: string };

export const { handlers, auth, signIn, signOut } = NextAuth(() => {
  const publicIssuer = required(process.env.KEYCLOAK_PUBLIC_URL, 'KEYCLOAK_PUBLIC_URL');
  const internalIssuer = required(process.env.KEYCLOAK_INTERNAL_URL, 'KEYCLOAK_INTERNAL_URL');
  const clientId = required(process.env.AUTH_KEYCLOAK_ID, 'AUTH_KEYCLOAK_ID');
  const clientSecret = required(process.env.AUTH_KEYCLOAK_SECRET, 'AUTH_KEYCLOAK_SECRET');

  async function refresh(token: JWT): Promise<JWT> {
    if (!token.refreshToken) return { ...token, error: 'RefreshTokenMissing' };
    const response = await fetch(`${internalIssuer}/protocol/openid-connect/token`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({
        client_id: clientId,
        client_secret: clientSecret,
        grant_type: 'refresh_token',
        refresh_token: token.refreshToken,
      }),
    });
    if (!response.ok) return { ...token, error: 'RefreshTokenError' };
    const value = (await response.json()) as TokenResponse;
    return {
      ...token,
      accessToken: value.access_token,
      accessTokenExpires: Date.now() + value.expires_in * 1000,
      refreshToken: value.refresh_token || token.refreshToken,
      error: undefined,
    };
  }

  return {
    trustHost: true,
    session: { strategy: 'jwt' },
    providers: [
      Keycloak({
        clientId,
        clientSecret,
        issuer: publicIssuer,
        wellKnown: `${internalIssuer}/.well-known/openid-configuration`,
        authorization: {
          url: `${publicIssuer}/protocol/openid-connect/auth`,
          params: { scope: 'openid profile email' },
        },
        token: `${internalIssuer}/protocol/openid-connect/token`,
        userinfo: `${internalIssuer}/protocol/openid-connect/userinfo`,
      }),
    ],
    callbacks: {
      async jwt({ token, account }) {
        if (account)
          return {
            ...token,
            accessToken: account.access_token,
            accessTokenExpires: (account.expires_at || 0) * 1000,
            refreshToken: account.refresh_token,
          };
        if (Date.now() < Number(token.accessTokenExpires || 0) - 30_000) return token;
        return refresh(token);
      },
      async session({ session, token }) {
        session.subject = token.sub;
        session.accessToken = token.accessToken;
        session.authError = token.error;
        return session;
      },
    },
  };
});
