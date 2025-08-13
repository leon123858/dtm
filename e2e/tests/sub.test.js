import { client } from '../src/apolloClient'; // 假設您的 Apollo Client 實例從此處匯出
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
 * 使執行緒暫停指定的毫秒數。
 * @param {number} ms - 要暫停的毫秒數。
 * @returns {Promise<void>} 一個在時間到期後解析的 Promise。
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
	let tripId; // 用於儲存測試流程中建立的 Trip ID
	let recordId; // 用於儲存測試流程中建立的 Record ID (用於非 subscription 測試)
	const testTripName = `Test Trip - ${Date.now()}`;
	let nenExistTripId = '0b4d17a1-7db3-4686-aae2-2120f7919d50';

	// 在所有測試開始前，先建立一個共用的旅程
	beforeAll(async () => {
		const { data } = await client.mutate({
			mutation: CREATE_TRIP,
			variables: { input: { name: testTripName } },
		});
		expect(data.createTrip.id).toBeDefined();
		tripId = data.createTrip.id;
	});

	// --- 測試 Subscription ---
	describe('Subscription Tests', () => {
		const commonAddressForSubRecords = `SubRecAddr-${Date.now()}`;
		let recordIdForSubTests; // 用於 subscription 測試中 record 的生命週期

		// 在此 describe 區塊的所有測試開始前，建立一個共用地址
		beforeAll(async () => {
			const { data, error } = await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId, address: commonAddressForSubRecords },
			});
			expect(error).toBeUndefined();
			expect(data.createAddress).toBe(commonAddressForSubRecords);
		});

		// 在此 describe 區塊的所有測試結束後，清理共用地址
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
			// 清理可能未被刪除的 record (如果測試中途失敗)
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

			// 觸發 mutation
			const { data: mutationData, error: mutationError } = await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId, address: newAddressName },
			});

			expect(mutationError).toBeUndefined();
			expect(mutationData.createAddress).toBe(newAddressName);

			// 等待並驗證 subscription 結果
			const { data: subData, errors: subErrors } = await subscriptionPromise;
			expect(subErrors).toBeUndefined();
			expect(subData.subAddressCreate).toBe(newAddressName);

			// 清理
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

			// 觸發多個 mutation
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
			// 等待並驗證 subscription 結果
			const subscriptionResults = await subscriptionPromise;
			expect(subscriptionResults.length).toBe(2);
			// just check include
			expect(subscriptionResults.map((r) => r.data.subAddressCreate)).toEqual(
				expect.arrayContaining(newAddressNames)
			);
			// 清理
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

			// 觸發 mutation
			const { data: mutationData, error: mutationError } = await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId, address: newAddressName },
			});

			expect(mutationError).toBeUndefined();
			expect(mutationData.createAddress).toBe(newAddressName);

			// 等待並驗證 subscription 結果
			try {
				await subscriptionPromise;
				throw 'should not receive here as tripID not map';
			} catch (err) {
				expect(err).not.toBe('should not receive here as tripID not map');
			}

			// 清理
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
			recordIdForSubTests = mutationData.createRecord.id; // 保存 ID 供後續測試

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
			expect(recordIdForSubTests).toBeDefined(); // 確保 record 已被建立

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
			expect(recordIdForSubTests).toBeDefined(); // 確保 record 存在

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
			// 根據 schema，subRecordDelete 只回傳 ID
			expect(subData.subRecordDelete).toBe(recordIdForSubTests);

			recordIdForSubTests = null; // 標記為已刪除
		});

		it('should receive a notification when an address is deleted (subAddressDelete)', async () => {
			const addressToDelete = `SubAddrDelete-${Date.now()}`;
			// 先建立一個要刪除的地址
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

			// 觸發刪除
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
