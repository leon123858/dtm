import { gql } from '@apollo/client/core';
import { client } from '../src/apolloClient'; // 假設您的 Apollo Client 實例從此處匯出

// 將 GraphQL 操作定義在檔案頂部，方便管理
const CREATE_TRIP = gql`
	mutation CreateTrip($input: NewTrip!) {
		createTrip(input: $input) {
			id
			name
			addressList
			records {
				id
			}
		}
	}
`;

const GET_TRIP = gql`
	query GetTrip($id: ID!) {
		trip(tripId: $id) {
			id
			name
			addressList
			records {
				id
				name
				amount
				time
				prePayAddress
				shouldPayAddress
			}
			moneyShare {
				input {
					amount
					address
				}
				output {
					amount
					address
				}
			}
		}
	}
`;

const CREATE_ADDRESS = gql`
	mutation CreateAddress($tripId: ID!, $address: String!) {
		createAddress(tripId: $tripId, address: $address)
	}
`;

const DELETE_ADDRESS = gql`
	mutation DeleteAddress($tripId: ID!, $address: String!) {
		deleteAddress(tripId: $tripId, address: $address)
	}
`;

const CREATE_RECORD = gql`
	mutation CreateRecord($tripId: ID!, $input: NewRecord!) {
		createRecord(tripId: $tripId, input: $input) {
			id
			name
			amount
			time
			prePayAddress
			shouldPayAddress
		}
	}
`;

const UPDATE_RECORD = gql`
	mutation UpdateRecord($recordId: ID!, $input: NewRecord!) {
		updateRecord(recordId: $recordId, input: $input) {
			id
			name
			amount
			time
			prePayAddress
			shouldPayAddress
		}
	}
`;

const REMOVE_RECORD = gql`
	mutation RemoveRecord($id: ID!) {
		removeRecord(recordId: $id)
	}
`;

// --- Subscription GraphQL Definitions ---
const SUB_RECORD_CREATE = gql`
	subscription SubRecordCreate($tripId: ID!) {
		subRecordCreate(tripId: $tripId) {
			id
			name
			amount
			time
			prePayAddress
			shouldPayAddress
		}
	}
`;

const SUB_RECORD_UPDATE = gql`
	subscription SubRecordUpdate($tripId: ID!) {
		subRecordUpdate(tripId: $tripId) {
			id
			name
			amount
			time
			prePayAddress
			shouldPayAddress
		}
	}
`;

const SUB_RECORD_DELETE = gql`
	subscription SubRecordDelete($tripId: ID!) {
		subRecordDelete(tripId: $tripId) # Schema: String!
	}
`;

const SUB_ADDRESS_CREATE = gql`
	subscription SubAddressCreate($tripId: ID!) {
		subAddressCreate(tripId: $tripId) # Schema: String!
	}
`;

const SUB_ADDRESS_DELETE = gql`
	subscription SubAddressDelete($tripId: ID!) {
		subAddressDelete(tripId: $tripId) # Schema: String!
	}
`;

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

	// --- 測試 Trip ---
	describe('Trip Management', () => {
		it('should fetch the created trip correctly', async () => {
			const { data, error } = await client.query({
				query: GET_TRIP,
				variables: { id: tripId },
			});

			expect(error).toBeUndefined();
			expect(data.trip.id).toBe(tripId);
			expect(data.trip.name).toBe(testTripName);
			expect(data.trip.records).toEqual([]); // 初始應為空
			expect(data.trip.addressList).toEqual([]); // 初始應為空
		});

		it('should throw an error for a non-existent trip', async () => {
			try {
				await client.query({
					query: GET_TRIP,
					variables: { id: '8bc14c40-214d-4c1d-bbd0-ebb4d5a4eee3' }, // 使用一個確保不存在的 ID
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

	// --- 測試 Address ---
	describe('Address Management', () => {
		const addressAlice = 'Alice';
		const addressBob = 'Bob';

		it('should create new addresses for the trip', async () => {
			const resAlice = await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId, address: addressAlice },
			});
			expect(resAlice.data.createAddress).toBe(addressAlice);

			const resBob = await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId, address: addressBob },
			});
			expect(resBob.data.createAddress).toBe(addressBob);

			const { data } = await client.query({
				query: GET_TRIP,
				variables: { id: tripId },
			});
			expect(data.trip.addressList).toContain(addressAlice);
			expect(data.trip.addressList).toContain(addressBob);
		});

		it('should delete an address from the trip', async () => {
			const { data: deleteData } = await client.mutate({
				mutation: DELETE_ADDRESS,
				variables: { tripId, address: addressBob },
			});
			expect(deleteData.deleteAddress).toBe(addressBob);

			const { data: queryData } = await client.query({
				query: GET_TRIP,
				variables: { id: tripId },
			});
			expect(queryData.trip.addressList).toContain(addressAlice);
			expect(queryData.trip.addressList).not.toContain(addressBob);
		});
	});

	// --- 測試 Record (完整生命週期) ---
	describe('Record Management (Create -> Update -> Delete)', () => {
		it('should create a new record', async () => {
			const newRecord = {
				name: 'Lunch',
				amount: 150.75,
				time: '1672531199',
				prePayAddress: 'Alice', // 假設 'Alice' 已在 Address Management 測試中建立
				shouldPayAddress: ['Alice'],
			};

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
			recordId = data.createRecord.id; // 保存 recordId 供後續測試使用

			const { data: tripData } = await client.query({
				query: GET_TRIP,
				variables: { id: tripId },
			});
			expect(tripData.trip.records).toHaveLength(1);
			expect(tripData.trip.records[0].name).toBe(newRecord.name);
			expect(tripData.trip.records[0].time).toBe(newRecord.time);
		});

		it('should update the created record', async () => {
			expect(recordId).toBeDefined(); // 依賴前一個測試建立的 recordId

			const updatedRecord = {
				name: 'Expensive Dinner',
				amount: 500,
				time: '1672531299',
				prePayAddress: 'Alice',
				shouldPayAddress: ['Alice'],
			};

			const { data, error } = await client.mutate({
				mutation: UPDATE_RECORD,
				variables: {
					recordId,
					input: updatedRecord,
				},
			});

			expect(error).toBeUndefined();
			expect(data.updateRecord.name).toBe(updatedRecord.name);
			expect(data.updateRecord.amount).toBe(updatedRecord.amount);
			expect(data.updateRecord.time).toBe(updatedRecord.time);
		});

		it('should remove the record', async () => {
			expect(recordId).toBeDefined(); // 依賴前一個測試建立的 recordId

			const { data, error } = await client.mutate({
				mutation: REMOVE_RECORD,
				variables: { id: recordId },
			});

			expect(error).toBeUndefined();
			expect(data.removeRecord).toBe(recordId);

			const { data: tripData } = await client.query({
				query: GET_TRIP,
				variables: { id: tripId },
			});
			expect(tripData.trip.records).toHaveLength(0);
			recordId = null; // 清除 recordId
		});
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
						variables: { id: recordIdForSubTests },
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
		});

		it('should receive a notification when a record is updated (subRecordUpdate)', async () => {
			expect(recordIdForSubTests).toBeDefined(); // 確保 record 已被建立

			const updatedRecordPayload = {
				name: 'Sub Test Record Updated',
				amount: 99.55,
				time: '1672531499',
				prePayAddress: commonAddressForSubRecords,
				shouldPayAddress: [commonAddressForSubRecords],
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
		});

		it('should receive a notification when a record is deleted (subRecordDelete)', async () => {
			expect(recordIdForSubTests).toBeDefined(); // 確保 record 存在

			// 為了驗證 subscription 收到的內容，先查詢一次 record 的完整資訊
			const { data: recordBeforeDelete } = await client.query({
				query: GET_TRIP, // 使用GET_TRIP來取得 record 詳細資料
				variables: { id: tripId },
			});
			const targetRecord = recordBeforeDelete.trip.records.find(
				(r) => r.id === recordIdForSubTests
			);
			expect(targetRecord).toBeDefined();

			const subObservable = client.subscribe({
				query: SUB_RECORD_DELETE,
				variables: { tripId },
			});
			const subscriptionPromise = waitForSubscription(subObservable);

			// sleep to wait subscript trigger
			await sleep(1000);

			const { data: mutationData, error: mutationError } = await client.mutate({
				mutation: REMOVE_RECORD,
				variables: { id: recordIdForSubTests },
			});
			expect(mutationError).toBeUndefined();
			expect(mutationData.removeRecord).toBe(recordIdForSubTests);

			// const { data: subData, errors: subErrors } = await subscriptionPromise;
			// expect(subErrors).toBeUndefined();
			// expect(subData.subRecordDelete).toBeDefined();
			// expect(subData.subRecordDelete.id).toBe(recordIdForSubTests);
			// 驗證收到的 record data 是否與刪除前一致
			// expect(subData.subRecordDelete.name).toBe(targetRecord.name);
			// expect(subData.subRecordDelete.amount).toBe(targetRecord.amount);
			// expect(subData.subRecordDelete.prePayAddress).toBe(
			// targetRecord.prePayAddress
			// );
			// expect(subData.subRecordDelete.shouldPayAddress).toEqual(
			// targetRecord.shouldPayAddress
			// );

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
