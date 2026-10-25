import fetch from 'cross-fetch';
import {print} from 'graphql';
import {endpoint} from './auth.js';

// undefined means anonymous; invalid credentials must never fall back to anonymous.
export function api(token) {
 const authorization = token === undefined ? undefined : `Bearer ${token}`;
 async function request(document, variables = {}, header = authorization) {
  const response = await fetch(endpoint, {
   method: 'POST',
   headers: {'Content-Type': 'application/json', ...(header === undefined ? {} : {Authorization: header})},
   body: JSON.stringify({query: typeof document === 'string' ? document : print(document), variables}),
   signal: AbortSignal.timeout(15000),
  });
  return {status: response.status, body: await response.json()};
 }
 return {
  request,
  async run(document, variables) {
   const {status, body} = await request(document, variables);
   expect(status).toBe(200);
   expect(body.errors).toBeUndefined();
   return body.data;
  },
 };
}

export const ME = '{me{id subscribedTrips{id name lastViewedAt}}}';
export const membership = operation => `mutation($id:ID!){${operation}(tripId:$id){id subscribedTrips{id name lastViewedAt}}}`;
export async function memberOperation(actor, operation, id) {
 return (await actor.run(membership(operation), {id}))[operation];
}
