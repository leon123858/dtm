import { client } from '../src/apolloClient';
import { CREATE_TRIP, CREATE_ADDRESS, CREATE_RECORD, UPDATE_RECORD, GET_TRIP } from './graphql';

describe('Rejected record writes are atomic', () => {
 let tripId, input, recordId, foreignId;
 const read = async () => (await client.query({ query: GET_TRIP, variables: { tripId, haveHistory: true } })).data.trip;
 beforeAll(async () => {
  tripId = (await client.mutate({ mutation: CREATE_TRIP, variables: { input: { name: 'Validation' } } })).data.createTrip.id;
  const member = (await client.mutate({ mutation: CREATE_ADDRESS, variables: { tripId, input: { name: 'member' } } })).data.createAddress.id;
  const foreignTrip = (await client.mutate({ mutation: CREATE_TRIP, variables: { input: { name: 'Other trip' } } })).data.createTrip.id;
  foreignId = (await client.mutate({ mutation: CREATE_ADDRESS, variables: { tripId: foreignTrip, input: { name: 'foreign' } } })).data.createAddress.id;
  input = { name: 'meal', amount: 20, time: '1234', prePayAddressId: member, shouldPayAddressIds: [member] };
  recordId = (await client.mutate({ mutation: CREATE_RECORD, variables: { tripId, input } })).data.createRecord.id;
 });
 it.each([
  ['zero amount', () => ({ amount: 0 }), /amount/],
  ['negative amount', () => ({ amount: -1 }), /amount/],
  ['empty recipients', () => ({ shouldPayAddressIds: [] }), /should-pay/],
  ['duplicate recipients', () => ({ shouldPayAddressIds: [input.prePayAddressId, input.prePayAddressId] }), /duplicate/],
  ['foreign payer', () => ({ prePayAddressId: foreignId }), /belong/],
  ['foreign recipient', () => ({ shouldPayAddressIds: [foreignId] }), /belong/],
  ['malformed timestamp', () => ({ time: 'yesterday' }), /time/],
  ['unsafe name', () => ({ name: '<meal>' }), /name/],
  ['extra split amounts', () => ({ extendPayMsg: [1, 2] }), /invalid record input/],
 ])('rejects %s on create and update without changing history', async (_, patch, message) => {
  const before = await read();
  const next = { ...input, ...patch() };
  for (const operation of [
   { mutation: CREATE_RECORD, variables: { tripId, input: next } },
   { mutation: UPDATE_RECORD, variables: { recordId, input: { old: input, new: next } } },
  ]) {
   await expect(client.mutate(operation)).rejects.toMatchObject({ graphQLErrors: expect.arrayContaining([expect.objectContaining({ message: expect.stringMatching(message) })]) });
   expect(await read()).toEqual(before);
  }
 });
 it.each([{}, { old: null, new: null }])('requires both update snapshots: %j', async edit => {
  const before = await read();
  await expect(client.mutate({ mutation: UPDATE_RECORD, variables: { recordId, input: edit } })).rejects.toMatchObject({ graphQLErrors: expect.arrayContaining([expect.objectContaining({ message: 'old and new record inputs are required' })]) });
  expect(await read()).toEqual(before);
 });
});
