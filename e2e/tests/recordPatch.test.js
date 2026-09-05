import { client } from '../src/apolloClient';
import { CREATE_TRIP, CREATE_ADDRESS, CREATE_RECORD, UPDATE_RECORD, GET_TRIP } from './graphql';

describe('Record changelog patches', () => {
	it('applies changes to the latest tail and ignores equivalent stale input', async () => {
		const trip = await client.mutate({ mutation: CREATE_TRIP, variables: { input: { name: 'Patch regression' } } });
		const tripId = trip.data.createTrip.id;
		const addresses = [];
		for (const name of ['Alice', 'Bob']) {
			const response = await client.mutate({ mutation: CREATE_ADDRESS, variables: { tripId, input: { name } } });
			addresses.push(response.data.createAddress);
		}
		const [alice, bob] = addresses;
		const old = {
			name: 'meal', amount: 20, time: '1234',
			prePayAddressId: alice.id, shouldPayAddressIds: [bob.id],
		};
		const created = await client.mutate({ mutation: CREATE_RECORD, variables: { tripId, input: old } });
		const rootId = created.data.createRecord.id;
		const update = async (baseline, next) => {
			const result = await client.mutate({ mutation: UPDATE_RECORD, variables: { recordId: rootId, input: { old: baseline, new: next } } });
			return result.data.updateRecord;
		};
		const latest = { ...old, amount: 30, time: '4567', prePayAddressId: bob.id, shouldPayAddressIds: [alice.id] };
		const first = await update(old, latest);
		const equivalent = {
			...old, time: '+001234', prePayAddressId: alice.id.toUpperCase(),
			shouldPayAddressIds: [bob.id.toUpperCase()], extendPayMsg: [0], category: 'NORMAL',
		};
		expect((await update(old, equivalent)).id).toBe(first.id);
		const second = await update(old, { ...equivalent, name: 'dinner' });
		expect(second.parentRecordId).toBe(first.id);
		expect(second.name).toBe('dinner');
		expect(second.amount).toBe(30);
		expect(second.time).toBe('4567');
		expect(second.prePayAddress.id).toBe(bob.id);
		expect(second.shouldPayAddress.map(a => a.id)).toEqual([alice.id]);

		// A genuine list edit atomically replaces the tail's different list.
		const third = await update(old, { ...old, shouldPayAddressIds: [bob.id, alice.id], extendPayMsg: [1, 2] });
		expect(third.parentRecordId).toBe(second.id);
		expect(third.shouldPayAddress.map(a => a.id)).toEqual([bob.id, alice.id]);
		expect(third.extendPayMsg).toEqual([1, 2]);
		expect(third.name).toBe('dinner');
		expect(third.amount).toBe(30);
		const tripState = await client.query({ query: GET_TRIP, variables: { tripId } });
		expect(tripState.data.trip.records).toHaveLength(4);
		expect(tripState.data.trip.records.filter(r => r.isActive).map(r => r.id)).toEqual([third.id]);
	});
});
