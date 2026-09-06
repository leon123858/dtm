import { gql } from '@apollo/client/core';
import { client } from '../src/apolloClient';
import { CREATE_TRIP, CREATE_ADDRESS, CREATE_RECORD, UPDATE_RECORD } from './graphql';

it('defaults to live tails and isolates a history alias through deletion and restoration', async () => {
 const createdTrip = await client.mutate({ mutation: CREATE_TRIP, variables: { input: { name: 'History modes' } } });
 const tripId = createdTrip.data.createTrip.id;
 const members = [];
 for (const name of ['payer', 'member']) {
  const response = await client.mutate({ mutation: CREATE_ADDRESS, variables: { tripId, input: { name } } });
  members.push(response.data.createAddress.id);
 }
 const original = { name: 'meal', amount: 20, time: '1234', prePayAddressId: members[0], shouldPayAddressIds: [members[1]], isDeleted: false };
 const created = await client.mutate({ mutation: CREATE_RECORD, variables: { tripId, input: original } });
 const recordId = created.data.createRecord.id;
 const query = gql`
  query HistoryModes($tripId: ID!) {
   latest: trip(tripId: $tripId) {
    records { id isActive isDeleted isValid shouldPayAddress { id } extendPayMsg }
    isValid moneyShare { input { amount address { id } } output { amount address { id } } }
   }
   history: trip(tripId: $tripId, haveHistory: true) {
    records { id isActive isDeleted }
    isValid moneyShare { input { amount address { id } } output { amount address { id } } }
   }
  }
 `;
 const read = async () => (await client.query({ query, variables: { tripId } })).data;
 const deleted = { ...original, isDeleted: true };
 const deletion = await client.mutate({ mutation: UPDATE_RECORD, variables: { recordId, input: { old: original, new: deleted } } });
 let state = await read();
 expect(state.latest.records).toEqual([]);
 expect(state.history.records).toHaveLength(2);
 expect(state.history.records.find(r => r.id === deletion.data.updateRecord.id)).toMatchObject({ isActive: true, isDeleted: true });
 expect(state.latest.moneyShare).toEqual(state.history.moneyShare);
 const restored = await client.mutate({ mutation: UPDATE_RECORD, variables: { recordId, input: { old: deleted, new: original } } });
 state = await read();
 expect(state.latest.records).toHaveLength(1);
 expect(state.latest.records[0]).toMatchObject({ id: restored.data.updateRecord.id, isActive: true, isDeleted: false, isValid: true });
 expect(state.latest.records[0].shouldPayAddress.map(a => a.id)).toEqual([members[1]]);
 expect(state.latest.records[0].extendPayMsg).toEqual([0]);
 expect(state.history.records).toHaveLength(3);
 expect(state.latest.moneyShare).toEqual(state.history.moneyShare);
 expect(state.latest.isValid).toBe(true);
 expect(state.history.isValid).toBe(true);
});
