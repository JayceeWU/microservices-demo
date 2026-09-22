import { getAuthContext } from '@dancehub/api-client';

// Preserve the same attempt across reloads and lost responses. A confirmed
// order completes the attempt; the next deliberate purchase gets a new key.
export function checkoutKey(action: string, input: unknown): string {
  const owner = getAuthContext().userId;
  if (!owner) throw new Error('Sign in again before continuing checkout.');
  const storageKey = `dancehub.checkout.${owner}.${action}`;
  const fingerprint = JSON.stringify(input);
  const previous = sessionStorage.getItem(storageKey);
  if (previous) {
    try {
      const saved = JSON.parse(previous);
      if (saved.fingerprint === fingerprint && typeof saved.key === 'string') return saved.key;
    } catch {
      /* A corrupt entry starts a new attempt. */
    }
  }
  const key = crypto.randomUUID();
  sessionStorage.setItem(storageKey, JSON.stringify({ fingerprint, key }));
  return key;
}

export function finishCheckout(action: string) {
  sessionStorage.removeItem(`dancehub.checkout.${getAuthContext().userId}.${action}`);
}
