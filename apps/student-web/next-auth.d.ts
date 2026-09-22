import 'next-auth';
import 'next-auth/jwt';

declare module 'next-auth' {
  interface Session {
    subject?: string;
    accessToken?: string;
    authError?: 'RefreshTokenMissing' | 'RefreshTokenError';
  }
}

declare module 'next-auth/jwt' {
  interface JWT {
    accessToken?: string;
    accessTokenExpires?: number;
    refreshToken?: string;
    error?: 'RefreshTokenMissing' | 'RefreshTokenError';
  }
}
