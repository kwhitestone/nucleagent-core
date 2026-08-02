/**
 * Token persistence helpers.
 *
 * The JWT (access token) is issued by the nucleagent-auth backend and shared
 * across micro-app sub-apps. auth-web writes it to localStorage under
 * `nucleagent_access_token`; we read from the same key so a user signed in via
 * the auth sub-app is automatically authenticated here.
 */

const ACCESS_TOKEN_KEY = "nucleagent_access_token";

export function getAccessToken(): string {
  return localStorage.getItem(ACCESS_TOKEN_KEY) ?? "";
}

export function clearAccessToken(): void {
  localStorage.removeItem(ACCESS_TOKEN_KEY);
}
