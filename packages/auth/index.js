const encoder = new TextEncoder();
const encode = (value) =>
  btoa(String.fromCharCode(...new Uint8Array(value)))
    .replaceAll('+', '-')
    .replaceAll('/', '_')
    .replaceAll('=', '');
const decode = (value) => {
  const normalized = value.replaceAll('-', '+').replaceAll('_', '/');
  const padded = normalized + '='.repeat((4 - (normalized.length % 4)) % 4);
  return JSON.parse(
    new TextDecoder().decode(Uint8Array.from(atob(padded), (c) => c.charCodeAt(0))),
  );
};

export function createOidcClient({
  issuer,
  clientId,
  redirectUri = location.origin + '/auth/callback',
  onToken,
}) {
  if (!issuer || !clientId || !onToken) throw new Error('OIDC configuration is incomplete');
  const endpoint = (path) => `${issuer.replace(/\/$/, '')}/protocol/openid-connect/${path}`;
  const tokenKey = `dancehub.oidc.${clientId}`;
  let refreshTimer;
  let generation = 0;
  const read = () => JSON.parse(sessionStorage.getItem(tokenKey) || 'null');
  const save = (tokens) => {
    const value = {
      accessToken: tokens.access_token,
      idToken: tokens.id_token,
      refreshToken: tokens.refresh_token,
      expiresAt: Date.now() + Number(tokens.expires_in) * 1000,
    };
    sessionStorage.setItem(tokenKey, JSON.stringify(value));
    onToken(value.accessToken);
    schedule(value);
    return value;
  };
  const schedule = (value) => {
    clearTimeout(refreshTimer);
    const scheduledGeneration = generation;
    refreshTimer = setTimeout(
      () =>
        client.refresh().catch(() => {
          if (scheduledGeneration === generation) return client.login();
        }),
      Math.max(5_000, value.expiresAt - Date.now() - 30_000),
    );
  };
  const client = {
    async login(returnTo = location.pathname + location.search) {
      const verifier = encode(crypto.getRandomValues(new Uint8Array(48)));
      const challenge = encode(await crypto.subtle.digest('SHA-256', encoder.encode(verifier)));
      const state = crypto.randomUUID(),
        nonce = crypto.randomUUID();
      sessionStorage.setItem('dancehub.pkce', JSON.stringify({ verifier, state, nonce, returnTo }));
      const query = new URLSearchParams({
        client_id: clientId,
        redirect_uri: redirectUri,
        response_type: 'code',
        scope: 'openid profile email',
        code_challenge: challenge,
        code_challenge_method: 'S256',
        state,
        nonce,
      });
      location.assign(`${endpoint('auth')}?${query}`);
    },
    async completeLogin() {
      const startedGeneration = generation;
      const query = new URLSearchParams(location.search);
      if (query.has('error'))
        throw new Error(
          `OIDC login failed: ${query.get('error_description') || query.get('error')}`,
        );
      if (!query.has('code')) return null;
      const saved = JSON.parse(sessionStorage.getItem('dancehub.pkce') || '{}');
      if (!saved.verifier || saved.state !== query.get('state'))
        throw new Error('OIDC state validation failed');
      const body = new URLSearchParams({
        grant_type: 'authorization_code',
        client_id: clientId,
        redirect_uri: redirectUri,
        code: query.get('code'),
        code_verifier: saved.verifier,
      });
      const response = await fetch(endpoint('token'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body,
      });
      if (!response.ok) throw new Error('OIDC token exchange failed');
      const tokens = await response.json();
      if (startedGeneration !== generation) return null;
      if (!tokens.id_token || decode(tokens.id_token.split('.')[1]).nonce !== saved.nonce)
        throw new Error('OIDC nonce validation failed');
      sessionStorage.removeItem('dancehub.pkce');
      const value = save(tokens);
      history.replaceState({}, '', saved.returnTo || '/');
      return value;
    },
    async refresh() {
      const startedGeneration = generation;
      const current = read();
      if (!current?.refreshToken) throw new Error('OIDC refresh token is unavailable');
      const response = await fetch(endpoint('token'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams({
          grant_type: 'refresh_token',
          client_id: clientId,
          refresh_token: current.refreshToken,
        }),
      });
      if (startedGeneration !== generation) return null;
      if (!response.ok) {
        sessionStorage.removeItem(tokenKey);
        onToken('');
        throw new Error('OIDC refresh failed');
      }
      const tokens = await response.json();
      if (startedGeneration !== generation) return null;
      return save(tokens);
    },
    async initialize() {
      if (location.pathname === '/auth/callback' && location.search.includes('code='))
        return client.completeLogin();
      const current = read();
      if (!current) {
        onToken('');
        return null;
      }
      if (current.expiresAt <= Date.now() + 30_000) return client.refresh();
      onToken(current.accessToken);
      schedule(current);
      return current;
    },
    logout() {
      generation++;
      const current = read();
      clearTimeout(refreshTimer);
      sessionStorage.removeItem(tokenKey);
      sessionStorage.removeItem('dancehub.auth');
      onToken('');
      const query = new URLSearchParams({
        client_id: clientId,
        post_logout_redirect_uri: location.origin,
      });
      if (current?.idToken) query.set('id_token_hint', current.idToken);
      location.assign(`${endpoint('logout')}?${query}`);
    },
  };
  return client;
}
