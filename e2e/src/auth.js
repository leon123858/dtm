import fetch from 'cross-fetch';

export const endpoint = process.env.GRAPHQL_ENDPOINT_HTTP || 'http://127.0.0.1:8080/query';
const origin = new URL(endpoint).origin;

export async function authRequest(path, body) {
 const response = await fetch(`${origin}/auth/${path}`, {
  method: 'POST',
  headers: {'Content-Type': 'application/json'},
  ...(body === undefined ? {} : {body: JSON.stringify(body)}),
  signal: AbortSignal.timeout(30000),
 });
 // Never include response contents: successful responses contain credentials.
 if (!response.ok) throw new Error(`Auth ${path} failed (HTTP ${response.status})`);
 const tokens = await response.json();
 if (!tokens.idToken || !tokens.refreshToken || !(tokens.expiresIn > 0)) {
  throw new Error(`Auth ${path} returned incomplete tokens`);
 }
 return tokens;
}

export default async function setupAuth() {
 let tokens;
 try {
  tokens = await authRequest('password');
 } catch {
  throw new Error('E2E login failed. Start the server with E2E_AUTH_ENABLED=true and valid E2E_AUTH_EMAIL / E2E_AUTH_PASSWORD; verify identity-provider connectivity.');
 }
 // Passed to Jest workers in memory; never written to disk or logs.
 process.env.DTM_E2E_ID_TOKEN = tokens.idToken;
}
