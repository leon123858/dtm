import { client } from '../src/apolloClient';
import { CREATE_TRIP, UPDATE_TRIP, CREATE_ADDRESS, UPDATE_ADDRESS, CREATE_RECORD, UPDATE_RECORD, GET_TRIP } from './graphql';

const mutate = (mutation, variables) => client.mutate({ mutation, variables });
describe('Names remain plain text', () => {
 let tripId, addressId, recordId, input;
 const read = async () => (await client.query({ query: GET_TRIP, variables: { tripId, haveHistory: true } })).data.trip;
 beforeAll(async () => {
  tripId = (await mutate(CREATE_TRIP, { input: { name: 'baseline' } })).data.createTrip.id;
  addressId = (await mutate(CREATE_ADDRESS, { tripId, input: { name: 'member' } })).data.createAddress.id;
  input = { name: 'meal', amount: 20, time: '1234', prePayAddressId: addressId, shouldPayAddressIds: [addressId] };
  recordId = (await mutate(CREATE_RECORD, { tripId, input })).data.createRecord.id;
 });
 it.each([
  ['', 'must not be empty or whitespace-only'],
  [' \u3000', 'must not be empty or whitespace-only'],
  ['中'.repeat(101), 'must not exceed 100 Unicode code points'],
  ['🍜'.repeat(101), 'must not exceed 100 Unicode code points'],
  ['👩🏽‍💻'.repeat(26), 'must not exceed 100 Unicode code points'],
  ...['\0', '\t', '\n', '\u0085', '\u2028', '\u2029'].map(c => [c, `contains invalid characters: U+${c.codePointAt(0).toString(16).toUpperCase().padStart(4, '0')} is not allowed`]),
 ])('rejects %j on all create/update paths', async (name, reason) => {
  const before = await read();
  for (const [field, mutation, variables] of [
   ['trip', CREATE_TRIP, { input: { name } }],
   ['trip', UPDATE_TRIP, { tripId, input: { name } }],
   ['address', CREATE_ADDRESS, { tripId, input: { name } }],
   ['address', UPDATE_ADDRESS, { tripId, addressId, input: { name } }],
   ['record', CREATE_RECORD, { tripId, input: { ...input, name } }],
   ['record', UPDATE_RECORD, { recordId, input: { old: input, new: { ...input, name } } }],
  ]) {
   await expect(mutate(mutation, variables)).rejects.toMatchObject({ graphQLErrors: expect.arrayContaining([expect.objectContaining({ message: expect.stringContaining(`${field} name ${reason}`) })]) });
   expect(await read()).toEqual(before);
  }
 });
 it.each([" <meal><script>'\"; DROP TABLE records; -- 👩🏽‍💻 e\u0301 ", '中'.repeat(100), '🍜'.repeat(100)])('preserves %j through creation, update and history', async name => {
  const createdTrip = (await mutate(CREATE_TRIP, { input: { name } })).data.createTrip;
  expect(createdTrip.name).toBe(name);
  const id = createdTrip.id;
  const address = (await mutate(CREATE_ADDRESS, { tripId: id, input: { name } })).data.createAddress;
  expect(address.name).toBe(name);
  const old = { ...input, name, prePayAddressId: address.id, shouldPayAddressIds: [address.id] };
  const record = (await mutate(CREATE_RECORD, { tripId: id, input: old })).data.createRecord;
  expect(record.name).toBe(name);
  const next = name === '中'.repeat(100) ? '🍜'.repeat(100) : name === '🍜'.repeat(100) ? '中'.repeat(100) : ' <updated> 👨‍👩‍👧‍👦 "quoted" ';
  expect((await mutate(UPDATE_TRIP, { tripId: id, input: { name: next } })).data.updateTrip.name).toBe(next);
  expect((await mutate(UPDATE_ADDRESS, { tripId: id, addressId: address.id, input: { name: next } })).data.updateAddress.name).toBe(next);
  expect((await mutate(UPDATE_RECORD, { recordId: record.id, input: { old, new: { ...old, name: next } } })).data.updateRecord.name).toBe(next);
  const trip = (await client.query({ query: GET_TRIP, variables: { tripId: id, haveHistory: true } })).data.trip;
  expect(trip.name).toBe(next);
  expect(trip.addresses[0].name).toBe(next);
  expect(trip.records.map(r => r.name).sort()).toEqual([name, next].sort());
 });
});
