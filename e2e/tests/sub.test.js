import {createClient} from 'graphql-ws';
import WebSocket from 'ws';
import {print} from 'graphql';
import {api, memberOperation} from '../src/api.js';
import {CREATE_TRIP, CREATE_ADDRESS, UPDATE_ADDRESS, DELETE_ADDRESS, CREATE_RECORD, UPDATE_RECORD, SUB_ADDRESS_CREATE, SUB_ADDRESS_UPDATE, SUB_ADDRESS_DELETE, SUB_RECORD_CREATE, SUB_RECORD_UPDATE} from './graphql.js';

// Retain every event so duplicates and cross-trip leaks cannot hide behind unsubscribe-on-first.
function watch(client, document, tripId, acceptProbe = false) {
 const events = [];
 let failure;
 let probeSeen = false;
 const stop = client.subscribe({query: print(document), variables: {tripId}}, {
  next: result => {
   if (result.errors) failure = new Error(JSON.stringify(result.errors));
   else {
    const event = Object.values(result.data)[0];
    if (acceptProbe && /^通知探測 \d+$/.test(event.name)) probeSeen = true;
    else events.push(event);
   }
  },
  error: () => { failure = new Error('Subscription transport failed'); },
  complete: () => {},
 });
 return {
  events, stop,
  get ready() { return probeSeen; },
  async count(count) {
   const deadline = Date.now() + 5000;
   while (events.length < count && !failure && Date.now() < deadline) {
    await new Promise(resolve => setTimeout(resolve, 20));
   }
   if (failure) throw failure;
   expect(events).toHaveLength(count);
   return events[count - 1];
  },
  check() { if (failure) throw failure; },
 };
}

test('anonymous streams receive member changes and stay isolated between trips', async () => {
 const member = api(process.env.DTM_E2E_ID_TOKEN);
 const anonymous = api();
 const trip = (await member.run(CREATE_TRIP, {input: {name: '通知流程'}})).createTrip;
 const sockets = createClient({
  url: process.env.GRAPHQL_ENDPOINT_WS || 'ws://127.0.0.1:8080/subscription',
  webSocketImpl: WebSocket, retryAttempts: 0,
 });
 const streams = [];
 try {
  const other = (await anonymous.run(CREATE_TRIP, {input: {name: '隔離旅程'}})).createTrip;
  const tripId = trip.id;
  const [created, renamed, removed, recorded, edited, isolated] = [
   [SUB_ADDRESS_CREATE, tripId], [SUB_ADDRESS_UPDATE, tripId], [SUB_ADDRESS_DELETE, tripId],
   [SUB_RECORD_CREATE, tripId], [SUB_RECORD_UPDATE, tripId], [SUB_ADDRESS_CREATE, other.id],
  ].map(([document, id]) => { const stream = watch(sockets, document, id, document === SUB_ADDRESS_CREATE && id === tripId); streams.push(stream); return stream; });
  // graphql-transport-ws acknowledges the connection, not individual subscriptions.
  // Establish readiness with actual probe events instead of an unconditional sleep.
  let ready = false;
  for (let attempt = 0; attempt < 20 && !ready; attempt++) {
   await member.run(CREATE_ADDRESS, {tripId, input: {name: `通知探測 ${attempt}`}});
   await new Promise(resolve => setTimeout(resolve, 50));
   streams.forEach(stream => stream.check());
   ready = created.ready;
  }
  expect(ready).toBe(true);
  // All subscribe messages were sent on the same socket before the probe response.
  const alice = (await member.run(CREATE_ADDRESS, {tripId, input: {name: 'Alice'}})).createAddress;
  expect(await created.count(1)).toEqual(alice);
  const bob = (await member.run(CREATE_ADDRESS, {tripId, input: {name: 'Bob'}})).createAddress;
  expect(await created.count(2)).toEqual(bob);
  const renamedAlice = (await member.run(UPDATE_ADDRESS, {tripId, addressId: alice.id, input: {name: 'Alice 🌻'}})).updateAddress;
  expect(await renamed.count(1)).toEqual(renamedAlice);
  const old = {name: '午餐', amount: 100, time: '1700000000000', prePayAddressId: alice.id, shouldPayAddressIds: [alice.id], category: 'NORMAL', extendPayMsg: []};
  const record = (await member.run(CREATE_RECORD, {tripId, input: old})).createRecord;
  expect(await recorded.count(1)).toEqual(record);
  const next = {...old, amount: 200};
  const updated = (await member.run(UPDATE_RECORD, {recordId: record.id, input: {old, new: next}})).updateRecord;
  expect(await edited.count(1)).toEqual(updated);
  const deleted = (await member.run(UPDATE_RECORD, {recordId: updated.id, input: {old: next, new: {...next, isDeleted: true}}})).updateRecord;
  expect(await edited.count(2)).toEqual(deleted);
  expect(deleted.isDeleted).toBe(true);
  const deletedBob = (await member.run(DELETE_ADDRESS, {tripId, addressId: bob.id})).deleteAddress;
  expect(await removed.count(1)).toEqual(deletedBob);
  const marker = (await member.run(CREATE_ADDRESS, {tripId: other.id, input: {name: '另一旅程事件'}})).createAddress;
  expect(await isolated.count(1)).toEqual(marker);
  await new Promise(resolve => setTimeout(resolve, 200));
  expect(isolated.events).toEqual([marker]);
  expect(created.events).toEqual([alice, bob]);
  streams.forEach(stream => stream.check());
 } finally {
  streams.forEach(stream => stream.stop());
  await sockets.dispose();
  await memberOperation(member, 'unsubscribeTrip', trip.id);
 }
}, 20000);
