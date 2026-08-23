import { client } from '../src/apolloClient';
import {
	GET_TRIP,
	CREATE_TRIP,
	CREATE_ADDRESS,
	DELETE_ADDRESS,
	CREATE_RECORD,
	UPDATE_RECORD,
	REMOVE_RECORD,
} from './graphql';

describe('GraphQL API End-to-End Tests', () => {
	let tripId;
	let recordId;
	let addressAlice;
	let addressBob;
	let newRecord;
	const testTripName = `Test Trip - ${Date.now()}`;

	beforeAll(async () => {
		const { data } = await client.mutate({
			mutation: CREATE_TRIP,
			variables: { input: { name: testTripName } },
		});
		expect(data.createTrip.id).toBeDefined();
		tripId = data.createTrip.id;
	});

	// --- Trip ---
	describe('Trip Management', () => {
		it('should fetch the created trip correctly', async () => {
			const { data, error } = await client.query({
				query: GET_TRIP,
				variables: { tripId },
			});

			expect(error).toBeUndefined();
			expect(data.trip.id).toBe(tripId);
			expect(data.trip.name).toBe(testTripName);
			expect(data.trip.isValid).toBe(true);
			expect(data.trip.records).toEqual([]);
			expect(data.trip.addresses).toEqual([]);
		});

		it('should throw an error for a non-existent trip', async () => {
			try {
				await client.query({
					query: GET_TRIP,
					variables: { tripId: '8bc14c40-214d-4c1d-bbd0-ebb4d5a4eee3' },
				});
				fail('Expected client.query to throw an error, but it did not.');
			} catch (error) {
				expect(error.graphQLErrors).toBeDefined();
				expect(error.graphQLErrors.length).toBeGreaterThan(0);

				const firstError = error.graphQLErrors[0];
				expect(firstError.message).toContain('not found');
			}
		});
	});

	// --- Address ---
	describe('Address Management', () => {
		it('should create new addresses for the trip', async () => {
			const resAlice = await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId, input: { name: 'Alice' } },
			});
			addressAlice = resAlice.data.createAddress;
			expect(addressAlice.name).toBe('Alice');

			const resBob = await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId, input: { name: 'Bob' } },
			});
			addressBob = resBob.data.createAddress;
			expect(addressBob.name).toBe('Bob');
			newRecord = {
				name: 'Lunch', amount: 150.75, time: '1672531199',
				prePayAddressId: addressAlice.id,
				shouldPayAddressIds: [addressAlice.id],
				category: 'NORMAL', extendPayMsg: [],
			};

			const { data } = await client.query({
				query: GET_TRIP,
				variables: { tripId: tripId },
			});
			expect(data.trip.addresses).toEqual(expect.arrayContaining([addressAlice, addressBob]));
		});

		it('should delete an address from the trip', async () => {
			const { data: deleteData } = await client.mutate({
				mutation: DELETE_ADDRESS,
				variables: { tripId, addressId: addressBob.id },
			});
			expect(deleteData.deleteAddress).toEqual(addressBob);

			const { data: queryData } = await client.query({
				query: GET_TRIP,
				variables: { tripId },
			});
			expect(queryData.trip.addresses).toContainEqual(addressAlice);
			expect(queryData.trip.addresses).not.toContainEqual(addressBob);
		});
	});

	describe('Record Management (Create -> Update -> Delete)', () => {
		it('should create a new record', async () => {

			const { data, error } = await client.mutate({
				mutation: CREATE_RECORD,
				variables: {
					tripId: tripId,
					input: newRecord,
				},
			});

			expect(error).toBeUndefined();
			expect(data.createRecord.id).toBeDefined();
			expect(data.createRecord.name).toBe(newRecord.name);
			expect(data.createRecord.category).toBe(newRecord.category);
			expect(data.createRecord.isValid).toBe(true);
			recordId = data.createRecord.id;

			const { data: tripData } = await client.query({
				query: GET_TRIP,
				variables: { tripId },
			});
			expect(tripData.trip.records).toHaveLength(1);
			expect(tripData.trip.records[0].name).toBe(newRecord.name);
			expect(tripData.trip.records[0].time).toBe(newRecord.time);
			expect(tripData.trip.records[0].category).toBe('NORMAL');
		});

		it('should update the created record', async () => {
			expect(recordId).toBeDefined();

			const updatedRecord = {
				name: 'Expensive Dinner',
				amount: 500,
				prePayAddressId: addressAlice.id,
				shouldPayAddressIds: [addressAlice.id],
				category: 'FIX',
				extendPayMsg: [500],
			};

			const { data, error } = await client.mutate({
				mutation: UPDATE_RECORD,
				variables: {
					recordId,
					input: {
                        old: newRecord,
                        new: updatedRecord,
                    },
				},
			});

			expect(error).toBeUndefined();
			expect(data.updateRecord.name).toBe(updatedRecord.name);
			expect(data.updateRecord.amount).toBe(updatedRecord.amount);
			expect(data.updateRecord.category).toBe(updatedRecord.category);
			expect(data.updateRecord.extendPayMsg).toEqual(
				updatedRecord.extendPayMsg
			);
		});

		it('should remove the record', async () => {
			expect(recordId).toBeDefined();

			const { data, error } = await client.mutate({
				mutation: REMOVE_RECORD,
				variables: { recordId },
			});

			expect(error).toBeUndefined();
			expect(data.removeRecord).toBe(recordId);

			const { data: tripData } = await client.query({
				query: GET_TRIP,
				variables: { tripId },
			});
			expect(tripData.trip.records).toHaveLength(0);
		});
	});
});
