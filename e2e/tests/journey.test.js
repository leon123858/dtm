import {api, ME, memberOperation} from '../src/api.js';
import {CREATE_TRIP, UPDATE_TRIP, CREATE_ADDRESS, UPDATE_ADDRESS, DELETE_ADDRESS, CREATE_RECORD, UPDATE_RECORD, GET_TRIP} from './graphql.js';

const anonymous = api();
const member = api(process.env.DTM_E2E_ID_TOKEN);
const TOTALS = `query($tripId:ID!){trip(tripId:$tripId){addresses{id statistics{totalExpense totalPrepaid activeRecordCount}}}}`;

test('member and anonymous collaborators complete a trip with history, settlement and failure isolation', async () => {
 const trip = (await member.run(CREATE_TRIP, {input: {name: '週末旅行 ✈️'}})).createTrip;
 const tripId = trip.id;
 try {
  expect((await member.run(ME)).me.subscribedTrips).toContainEqual(expect.objectContaining({id: tripId}));
  expect((await anonymous.run(GET_TRIP, {tripId})).trip).toMatchObject({records: [], addresses: [], isValid: true});
  const alice = (await member.run(CREATE_ADDRESS, {tripId, input: {name: 'Alice'}})).createAddress;
  const bob = (await anonymous.run(CREATE_ADDRESS, {tripId, input: {name: 'Bob'}})).createAddress;
  await anonymous.run(UPDATE_ADDRESS, {tripId, addressId: alice.id, input: {name: 'Alice 🌻'}});
  await anonymous.run(UPDATE_TRIP, {tripId, input: {name: '共同旅行'}});
  const old = {name: '午餐 🍱', amount: 120, time: '1700000000000', prePayAddressId: alice.id, shouldPayAddressIds: [alice.id, bob.id], category: 'NORMAL', extendPayMsg: []};
  const record = (await member.run(CREATE_RECORD, {tripId, input: old})).createRecord;
  const next = {...old, amount: 200};
  const updated = (await anonymous.run(UPDATE_RECORD, {recordId: record.id, input: {old, new: next}})).updateRecord;
  expect(updated).toMatchObject({parentRecordId: record.id, amount: 200});
  expect(updated.id).not.toBe(record.id);
  const live = (await member.run(GET_TRIP, {tripId})).trip;
  expect(live.name).toBe('共同旅行');
  expect(live.records.map(r => r.id)).toEqual([updated.id]);
  expect(live.moneyShare).toEqual([{input: [{address: {id: bob.id, name: 'Bob'}, amount: 100}], output: {address: {id: alice.id, name: 'Alice 🌻'}, amount: 100}}]);
  expect((await anonymous.run(TOTALS, {tripId})).trip.addresses).toEqual(expect.arrayContaining([
   {id: alice.id, statistics: {totalExpense: 100, totalPrepaid: 200, activeRecordCount: 1}},
   {id: bob.id, statistics: {totalExpense: 100, totalPrepaid: 0, activeRecordCount: 1}},
  ]));

  const foreignTrip = (await anonymous.run(CREATE_TRIP, {input: {name: '另一旅程'}})).createTrip;
  const foreign = (await anonymous.run(CREATE_ADDRESS, {tripId: foreignTrip.id, input: {name: '外部成員'}})).createAddress;
  const foreignBefore = await anonymous.run(GET_TRIP, {tripId: foreignTrip.id});
  const baseline = await member.run(GET_TRIP, {tripId, haveHistory: true});
  for (const [document, variables] of [
   [CREATE_RECORD, {tripId, input: {...old, prePayAddressId: foreign.id}}],
   [CREATE_RECORD, {tripId, input: {...old, shouldPayAddressIds: [foreign.id]}}],
   [UPDATE_ADDRESS, {tripId, addressId: foreign.id, input: {name: 'must not change'}}],
   [DELETE_ADDRESS, {tripId, addressId: foreign.id}],
   [DELETE_ADDRESS, {tripId, addressId: alice.id}],
  ]) {
   const result = await anonymous.request(document, variables);
   expect(result.status).toBe(200);
   expect(result.body.errors?.length).toBeGreaterThan(0);
   expect(await member.run(GET_TRIP, {tripId, haveHistory: true})).toEqual(baseline);
   expect(await anonymous.run(GET_TRIP, {tripId: foreignTrip.id})).toEqual(foreignBefore);
  }

  const deleted = (await member.run(UPDATE_RECORD, {recordId: updated.id, input: {old: next, new: {...next, isDeleted: true}}})).updateRecord;
  expect(deleted).toMatchObject({parentRecordId: updated.id, isDeleted: true});
  const history = (await anonymous.run(GET_TRIP, {tripId, haveHistory: true})).trip;
  expect(history.records).toHaveLength(3);
  expect(history.records.filter(r => r.isActive).map(r => r.id)).toEqual([deleted.id]);
  expect(history.moneyShare).toEqual([]);
  for (const address of (await member.run(TOTALS, {tripId})).trip.addresses) {
   expect(address.statistics).toEqual({totalExpense: 0, totalPrepaid: 0, activeRecordCount: 0});
  }
  const spare = (await anonymous.run(CREATE_ADDRESS, {tripId, input: {name: '未使用'}})).createAddress;
  expect((await member.run(DELETE_ADDRESS, {tripId, addressId: spare.id})).deleteAddress.id).toBe(spare.id);
  expect((await anonymous.run(GET_TRIP, {tripId})).trip.addresses.map(a => a.id)).not.toContain(spare.id);
 } finally {
  await memberOperation(member, 'unsubscribeTrip', tripId);
 }
});
