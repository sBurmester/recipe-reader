// web/src/auth.ts
//
// Holds the API bearer token the backend requires on writes when API_TOKEN is
// configured. A loopback deployment normally runs without one, in which case
// nothing here is ever used.
//
// The token lives in localStorage rather than in the bundle: it is the
// operator's credential, not the application's, and a bundled secret would be
// readable by anyone who can fetch the JavaScript — which is everyone the
// token is meant to keep out.

const STORAGE_KEY = "recipe-reader.api-token";

/** getApiToken returns the stored token, or "" when none is set. */
export function getApiToken(): string {
  // Storage throws in a private window or when site data is blocked, and the
  // app is fully usable without a token, so a failure here is not an error.
  try {
    return window.localStorage.getItem(STORAGE_KEY) ?? "";
  } catch {
    return "";
  }
}

/** setApiToken stores the token for subsequent writes; "" clears it. */
export function setApiToken(token: string): void {
  try {
    if (token) window.localStorage.setItem(STORAGE_KEY, token);
    else window.localStorage.removeItem(STORAGE_KEY);
  } catch {
    // Nothing to do: the caller already has the token for this page load.
  }
}
