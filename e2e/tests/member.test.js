import {randomUUID} from 'node:crypto';
import {api, ME, membership, memberOperation} from '../src/api.js';
import {authRequest} from '../src/auth.js';
import {CREATE_TRIP, UPDATE_TRIP, GET_TRIP} from './graphql.js';

const anonymous = api();
const member = api(process.env.DTM_E2E_ID_TOKEN);
const operations = ['subscribeTrip', 'unsubscribeTrip', 'touchSubscribedTrip'];
const create = async actor => (await actor.run(CREATE_TRIP, {input: {name: `會員流程 ${randomUUID()}`}})).createTrip;

async function expectCode(actor, operation, id, code) {
 const result = await actor.request(membership(operation), {id});
 expect(result.status).toBe(200);
 expect(result.body.errors).toEqual([expect.objectContaining({extensions: expect.objectContaining({code})})]);
}

test('anonymous and malformed credentials cannot change membership or trip data', async () => {
 const trip = await create(anonymous);
 const entries = async () => (await member.run(ME)).me.subscribedTrips.filter(t => t.id === trip.id);
 const baseline = await entries();
 expect(await anonymous.run(ME)).toEqual({me: null});
 for (const operation of operations) {
  await expectCode(anonymous, operation, trip.id, 'UNAUTHENTICATED');
 }
 for (const header of ['Bearer invalid', 'Basic invalid', 'Bearer', '']) {
  const result = await anonymous.request(UPDATE_TRIP, {tripId: trip.id, input: {name: 'must not execute'}}, header);
  expect(result).toEqual({status: 401, body: {error: 'UNAUTHENTICATED'}});
 }
 expect((await anonymous.run(GET_TRIP, {tripId: trip.id})).trip.name).toBe(trip.name);
 expect(await entries()).toEqual(baseline);
});

test('real login, automatic collection, shared trip collection, refresh and removal', async () => {
 const ids = [];
 try {
  const initial = (await member.run(ME)).me;
  expect(initial.id).toBeTruthy();
  const owned = await create(member);
  ids.push(owned.id);
  const shared = await create(anonymous);
  ids.push(shared.id);
  const ownEntries = user => user.subscribedTrips.filter(t => ids.includes(t.id));
  let entries = ownEntries((await member.run(ME)).me);
  expect(entries).toEqual([expect.objectContaining({id: owned.id, name: owned.name})]);
  expect(entries[0].lastViewedAt).toMatch(/^\d+$/);
  expect(Number(entries[0].lastViewedAt)).toBeGreaterThan(0);

  const collected = await memberOperation(member, 'subscribeTrip', shared.id);
  const before = ownEntries(collected);
  expect(before.map(t => t.id)).toEqual([...ids].sort());
  expect(ownEntries(await memberOperation(member, 'subscribeTrip', shared.id))).toEqual(before);
  // The API exposes milliseconds, so separate writes by more than one millisecond.
  await new Promise(resolve => setTimeout(resolve, 5));
  entries = ownEntries(await memberOperation(member, 'touchSubscribedTrip', shared.id));
  expect(Number(entries.find(t => t.id === shared.id).lastViewedAt)).toBeGreaterThan(Number(before.find(t => t.id === shared.id).lastViewedAt));
  expect(entries.find(t => t.id === owned.id)).toEqual(before.find(t => t.id === owned.id));
  expect(entries.map(t => t.id)).toEqual([...ids].sort());

  await anonymous.run(UPDATE_TRIP, {tripId: shared.id, input: {name: '匿名改名 🌻'}});
  expect(ownEntries((await member.run(ME)).me).find(t => t.id === shared.id).name).toBe('匿名改名 🌻');
  const again = await authRequest('password');
  const refreshed = await authRequest('refresh', {refreshToken: again.refreshToken});
  for (const token of [again.idToken, refreshed.idToken]) {
   const actor = api(token);
   expect((await actor.run(ME)).me.id).toBe(initial.id);
   expect(ownEntries(await memberOperation(actor, 'subscribeTrip', shared.id))).toHaveLength(2);
  }

  await memberOperation(member, 'unsubscribeTrip', shared.id);
  await memberOperation(member, 'unsubscribeTrip', shared.id);
  await expectCode(member, 'touchSubscribedTrip', shared.id, 'NOT_FOUND');
  await member.run(UPDATE_TRIP, {tripId: shared.id, input: {name: '取消收藏後仍可編輯'}});
  expect((await anonymous.run(GET_TRIP, {tripId: shared.id})).trip.name).toBe('取消收藏後仍可編輯');
  expect(ownEntries((await member.run(ME)).me).map(t => t.id)).toEqual([owned.id]);
  expect(ownEntries(await memberOperation(member, 'subscribeTrip', shared.id))).toHaveLength(2);

  const baseline = ownEntries((await member.run(ME)).me);
  for (const operation of operations) await expectCode(member, operation, 'bad-id', 'BAD_USER_INPUT');
  const missing = randomUUID();
  for (const operation of ['subscribeTrip', 'touchSubscribedTrip']) await expectCode(member, operation, missing, 'NOT_FOUND');
  await memberOperation(member, 'unsubscribeTrip', missing);
  expect(ownEntries((await member.run(ME)).me)).toEqual(baseline);
 } finally {
  for (const id of ids) await memberOperation(member, 'unsubscribeTrip', id);
 }
}, 60000);
