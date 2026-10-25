import fetch from 'cross-fetch';

const origin = new URL(process.env.GRAPHQL_ENDPOINT_HTTP || 'http://127.0.0.1:8080/query').origin;

describe('Authentication REST boundaries', () => {
 beforeAll(async () => {
  const response = await fetch(`${origin}/auth/exchange`, {
  method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({code: 'invalid-login-code'}),
  });
  expect(response.status).toBe(400);
  expect(await response.json()).toEqual({error: 'invalid_login_code'});
  expect(response.headers.get('cache-control')).toBe('no-store');
 });
 test('unknown providers do not initiate login', async () => {
  const response = await fetch(`${origin}/auth/github`, {redirect: 'manual'});
  expect(response.status).toBe(404);
 });
 test('invalid callback never returns tokens', async () => {
  const response = await fetch(`${origin}/auth/callback?state=invalid&code=invalid`, {redirect: 'manual'});
  expect(response.status).toBe(302);
  const target = new URL(response.headers.get('location'));
  expect(target.pathname).toBe('/auth/complete');
  expect(target.searchParams.get('error')).toBe('invalid_state');
  expect(target.searchParams.has('code')).toBe(false);
 });
 test('empty refresh input is rejected without contacting Google', async () => {
  const response = await fetch(`${origin}/auth/refresh`, {
  method: 'POST', headers: {'Content-Type': 'application/json'}, body: '{}',
  });
  expect(response.status).toBe(400);
  expect(response.headers.get('cache-control')).toBe('no-store');
 });
});

 describe('OAuth returnTo validation', () => {
  test.each([
   'returnTo=https%3A%2F%2Fevil.example',
   'returnTo=%2F%2Fevil.example',
   'returnTo=%2Fauth%2Fcomplete',
   'returnTo=%2Ftrip%2Fabc123',
   'returnTo=%2F%252fhistory',
   'returnTo=%2Fhistory%250a',
   'returnTo=%2F&%72eturnTo=%2Fhistory',
  ])('rejects %s before OAuth starts', async (query) => {
   const response = await fetch(`${origin}/auth/google?${query}`, {redirect: 'manual'});
   expect(response.status).toBe(400);
   expect(await response.json()).toEqual({error: 'invalid_request'});
   expect(response.headers.get('set-cookie')).toBe(null);
   expect(response.headers.get('location')).toBe(null);
  });
  test('exchange does not accept returnTo in its body', async () => {
   const response = await fetch(`${origin}/auth/exchange`, {
    method: 'POST', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({code: 'unused', returnTo: '/history'}),
   });
   expect(response.status).toBe(400);
   expect(await response.json()).toEqual({error: 'invalid_request'});
  });
 });
