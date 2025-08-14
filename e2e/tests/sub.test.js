import { client } from '../src/apolloClient';
import {
	GET_TRIP,
	CREATE_TRIP,
	CREATE_ADDRESS,
	DELETE_ADDRESS,
	CREATE_RECORD,
	UPDATE_RECORD,
	REMOVE_RECORD,
	SUB_RECORD_CREATE,
	SUB_RECORD_UPDATE,
	SUB_RECORD_DELETE,
	SUB_ADDRESS_CREATE,
	SUB_ADDRESS_DELETE,
} from './graphql';

/**
 * let thread sleep for a while
 * @param {number} ms
 * @returns {Promise<void>} Promise。
 */
function sleep(ms) {
	return new Promise((resolve) => setTimeout(resolve, ms));
}

// Helper function to wait for a single emission from an Apollo Observable
const waitForSubscription = (observable, timeout = 7000) => {
	return new Promise((resolve, reject) => {
		let received = false;
		const timer = setTimeout(() => {
			if (!received) {
				received = true;
				// It's important that 'observer' is defined before this timer function is called.
				// Ensure 'observer' is in a scope accessible here or passed to this timer function.
				// If 'observer' is the subscription object returned by observable.subscribe(),
				// then it should be defined by the time setTimeout is called.
				if (typeof observer !== 'undefined' && observer.unsubscribe) {
					observer.unsubscribe();
				}
				reject(new Error(`Subscription timed out after ${timeout}ms`));
			}
		}, timeout);

		const observer = observable.subscribe({
			next: (data) => {
				if (!received) {
					received = true;
					clearTimeout(timer);
					observer.unsubscribe();
					resolve(data);
				}
			},
			error: (err) => {
				if (!received) {
					received = true;
					clearTimeout(timer);
					// observer.unsubscribe(); // Unsubscribe might not be defined on error or might have already happened
					reject(err);
				}
			},
		});
	});
};

// Helper function to wait for a multi emission from an Apollo Observable
const waitForMultiSubscription = (observable, count, timeout = 7000) => {
	return new Promise((resolve, reject) => {
		let receivedCount = 0;
		const results = [];
		const timer = setTimeout(() => {
			if (receivedCount < count) {
				reject(new Error(`Subscription timed out after ${timeout}ms`));
			}
		}, timeout);

		const observer = observable.subscribe({
			next: (data) => {
				results.push(data);
				receivedCount++;
				if (receivedCount === count) {
					clearTimeout(timer);
					observer.unsubscribe();
					resolve(results);
				}
			},
			error: (err) => {
				clearTimeout(timer);
				reject(err);
			},
		});
	});
};

describe('GraphQL API End-to-End Tests', () => {
	let tripId;
	const testTripName = `Test Trip - ${Date.now()}`;
	let nenExistTripId = '0b4d17a1-7db3-4686-aae2-2120f7919d50';

	beforeAll(async () => {
		const { data } = await client.mutate({
			mutation: CREATE_TRIP,
			variables: { input: { name: testTripName } },
		});
		expect(data.createTrip.id).toBeDefined();
		tripId = data.createTrip.id;
	});

	// --- Subscription ---
	describe('Subscription Tests', () => {
		const commonAddressForSubRecords = `SubRecAddr-${Date.now()}`;
		let recordIdForSubTests;

		beforeAll(async () => {
			const { data, error } = await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId, address: commonAddressForSubRecords },
			});
			expect(error).toBeUndefined();
			expect(data.createAddress).toBe(commonAddressForSubRecords);
		});

		afterAll(async () => {
			try {
				await client.mutate({
					mutation: DELETE_ADDRESS,
					variables: { tripId, address: commonAddressForSubRecords },
				});
			} catch (e) {
				console.warn(
					`Could not clean up commonAddressForSubRecords: ${commonAddressForSubRecords}. Error: ${e.message}`
				);
			}

			if (recordIdForSubTests) {
				try {
					await client.mutate({
						mutation: REMOVE_RECORD,
						variables: { recordId: recordIdForSubTests },
					});
				} catch (e) {
					console.warn(
						`Could not clean up recordIdForSubTests: ${recordIdForSubTests}. Error: ${e.message}`
					);
				}
			}
		});

		it('should receive a notification when a new address is created (subAddressCreate)', async () => {
			const newAddressName = `SubAddr-${Date.now()}`;

			const subObservable = client.subscribe({
				query: SUB_ADDRESS_CREATE,
				variables: { tripId },
			});
			const subscriptionPromise = waitForSubscription(subObservable);

			// sleep to wait subscript trigger
			await sleep(1000);

			// mutation
			const { data: mutationData, error: mutationError } = await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId, address: newAddressName },
			});

			expect(mutationError).toBeUndefined();
			expect(mutationData.createAddress).toBe(newAddressName);

			// wait and verify subscription
			const { data: subData, errors: subErrors } = await subscriptionPromise;
			expect(subErrors).toBeUndefined();
			expect(subData.subAddressCreate).toBe(newAddressName);

			// clear
			await client.mutate({
				mutation: DELETE_ADDRESS,
				variables: { tripId, address: newAddressName },
			});
		});

		it('should receive multiple notifications when multiple addresses are created (subAddressCreate)', async () => {
			const newAddressNames = [
				`SubAddr1-${Date.now()}`,
				`SubAddr2-${Date.now()}`,
			];
			const subObservable = client.subscribe({
				query: SUB_ADDRESS_CREATE,
				variables: { tripId },
			});

			const subscriptionPromise = waitForMultiSubscription(subObservable, 2);
			// sleep to wait subscript trigger
			await sleep(1000);

			// trigger many mutation
			const mutationPromises = newAddressNames.map((address) =>
				client.mutate({
					mutation: CREATE_ADDRESS,
					variables: { tripId, address },
				})
			);
			const mutationResults = await Promise.all(mutationPromises);
			mutationResults.forEach((result, index) => {
				expect(result.error).toBeUndefined();
				expect(result.data.createAddress).toBe(newAddressNames[index]);
			});
			// subscription
			const subscriptionResults = await subscriptionPromise;
			expect(subscriptionResults.length).toBe(2);
			// just check include
			expect(subscriptionResults.map((r) => r.data.subAddressCreate)).toEqual(
				expect.arrayContaining(newAddressNames)
			);

			await Promise.all(
				newAddressNames.map((address) =>
					client.mutate({
						mutation: DELETE_ADDRESS,
						variables: { tripId, address },
					})
				)
			);
		});

		it('should not receive a notification when a new address is created (because not tripId)', async () => {
			const newAddressName = `SubAddr-${Date.now()}`;

			const subObservable = client.subscribe({
				query: SUB_ADDRESS_CREATE,
				variables: { tripId: nenExistTripId },
			});
			const subscriptionPromise = waitForSubscription(subObservable, 1500);

			// sleep to wait subscript trigger
			await sleep(1000);

			// mutation
			const { data: mutationData, error: mutationError } = await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId, address: newAddressName },
			});

			expect(mutationError).toBeUndefined();
			expect(mutationData.createAddress).toBe(newAddressName);

			// subscription
			try {
				await subscriptionPromise;
				throw 'should not receive here as tripID not map';
			} catch (err) {
				expect(err).not.toBe('should not receive here as tripID not map');
			}

			await client.mutate({
				mutation: DELETE_ADDRESS,
				variables: { tripId, address: newAddressName },
			});
		});

		it('should receive a notification when a new record is created (subRecordCreate)', async () => {
			const newRecordPayload = {
				name: 'Sub Test Record Create',
				amount: 77.88,
				time: '1672531399',
				prePayAddress: commonAddressForSubRecords,
				shouldPayAddress: [commonAddressForSubRecords],
				category: 'NORMAL',
				extendPayMsg: [],
			};

			const subObservable = client.subscribe({
				query: SUB_RECORD_CREATE,
				variables: { tripId },
			});
			const subscriptionPromise = waitForSubscription(subObservable);

			// sleep to wait subscript trigger
			await sleep(1000);

			const { data: mutationData, error: mutationError } = await client.mutate({
				mutation: CREATE_RECORD,
				variables: { tripId, input: newRecordPayload },
			});

			expect(mutationError).toBeUndefined();
			expect(mutationData.createRecord.id).toBeDefined();
			recordIdForSubTests = mutationData.createRecord.id;

			const { data: subData, errors: subErrors } = await subscriptionPromise;
			expect(subErrors).toBeUndefined();
			expect(subData.subRecordCreate).toBeDefined();
			expect(subData.subRecordCreate.id).toBe(recordIdForSubTests);
			expect(subData.subRecordCreate.name).toBe(newRecordPayload.name);
			expect(subData.subRecordCreate.amount).toBe(newRecordPayload.amount);
			expect(subData.subRecordCreate.time).toBe(newRecordPayload.time);
			expect(subData.subRecordCreate.prePayAddress).toBe(
				newRecordPayload.prePayAddress
			);
			expect(subData.subRecordCreate.shouldPayAddress).toEqual(
				newRecordPayload.shouldPayAddress
			);
			expect(subData.subRecordCreate.category).toBe('NORMAL');
			expect(subData.subRecordCreate.isValid).toBe(true);
		});

		it('should receive a notification when a record is updated (subRecordUpdate)', async () => {
			expect(recordIdForSubTests).toBeDefined();

			const updatedRecordPayload = {
				name: 'Sub Test Record Updated',
				amount: 99.55,
				time: '1672531499',
				prePayAddress: commonAddressForSubRecords,
				shouldPayAddress: [commonAddressForSubRecords],
				category: 'FIX',
				extendPayMsg: [99.55],
			};

			const subObservable = client.subscribe({
				query: SUB_RECORD_UPDATE,
				variables: { tripId },
			});
			const subscriptionPromise = waitForSubscription(subObservable);

			// sleep to wait subscript trigger
			await sleep(1000);

			const { data: mutationData, error: mutationError } = await client.mutate({
				mutation: UPDATE_RECORD,
				variables: {
					recordId: recordIdForSubTests,
					input: updatedRecordPayload,
				},
			});

			expect(mutationError).toBeUndefined();
			expect(mutationData.updateRecord.name).toBe(updatedRecordPayload.name);

			const { data: subData, errors: subErrors } = await subscriptionPromise;
			expect(subErrors).toBeUndefined();
			expect(subData.subRecordUpdate).toBeDefined();
			expect(subData.subRecordUpdate.id).toBe(recordIdForSubTests);
			expect(subData.subRecordUpdate.name).toBe(updatedRecordPayload.name);
			expect(subData.subRecordUpdate.amount).toBe(updatedRecordPayload.amount);
			expect(subData.subRecordUpdate.time).toBe(updatedRecordPayload.time);
			expect(subData.subRecordUpdate.category).toBe('FIX');
		});

		it('should receive a notification when a record is deleted (subRecordDelete)', async () => {
			expect(recordIdForSubTests).toBeDefined();

			const subObservable = client.subscribe({
				query: SUB_RECORD_DELETE,
				variables: { tripId },
			});
			const subscriptionPromise = waitForSubscription(subObservable);

			// sleep to wait subscript trigger
			await sleep(1000);

			const { data: mutationData, error: mutationError } = await client.mutate({
				mutation: REMOVE_RECORD,
				variables: { recordId: recordIdForSubTests },
			});
			expect(mutationError).toBeUndefined();
			expect(mutationData.removeRecord).toBe(recordIdForSubTests);

			const { data: subData, errors: subErrors } = await subscriptionPromise;
			expect(subErrors).toBeUndefined();
			expect(subData.subRecordDelete).toBeDefined();
			expect(subData.subRecordDelete).toBe(recordIdForSubTests);

			recordIdForSubTests = null;
		});

		it('should receive a notification when an address is deleted (subAddressDelete)', async () => {
			const addressToDelete = `SubAddrDelete-${Date.now()}`;

			await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId, address: addressToDelete },
			});

			const subObservable = client.subscribe({
				query: SUB_ADDRESS_DELETE,
				variables: { tripId },
			});
			const subscriptionPromise = waitForSubscription(subObservable);

			// sleep to wait subscript trigger
			await sleep(1000);

			const { data: mutationData, error: mutationError } = await client.mutate({
				mutation: DELETE_ADDRESS,
				variables: { tripId, address: addressToDelete },
			});

			expect(mutationError).toBeUndefined();
			expect(mutationData.deleteAddress).toBe(addressToDelete);

			const { data: subData, errors: subErrors } = await subscriptionPromise;
			expect(subErrors).toBeUndefined();
			expect(subData.subAddressDelete).toBe(addressToDelete);
		});
	});
});
