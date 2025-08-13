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
	let tripId; // 用於儲存測試流程中建立的 Trip ID
	let recordId; // 用於儲存測試流程中建立的 Record ID
	const testTripName = `Test Trip - ${Date.now()}`;

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
				variables: { tripId },
			});

			expect(error).toBeUndefined();
			expect(data.trip.id).toBe(tripId);
			expect(data.trip.name).toBe(testTripName);
			expect(data.trip.isValid).toBe(true); // 驗證新欄位
			expect(data.trip.records).toEqual([]); // 初始應為空
			expect(data.trip.addressList).toEqual([]); // 初始應為空
		});

		it('should throw an error for a non-existent trip', async () => {
			try {
				await client.query({
					query: GET_TRIP,
					variables: { tripId: '8bc14c40-214d-4c1d-bbd0-ebb4d5a4eee3' }, // 使用一個確保不存在的 ID
				});
				// 如果代碼執行到這裡，說明沒有拋出錯誤，這是不符合預期的
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

			// 驗證地址是否真的被加入
			const { data } = await client.query({
				query: GET_TRIP,
				variables: { tripId: tripId },
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

			// 驗證地址是否真的被移除
			const { data: queryData } = await client.query({
				query: GET_TRIP,
				variables: { tripId },
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
				prePayAddress: 'Alice',
				shouldPayAddress: ['Alice'],
				category: 'NORMAL', // 新增欄位
				extendPayMsg: [], // 新增欄位
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
			expect(data.createRecord.category).toBe(newRecord.category); // 驗證新欄位
			expect(data.createRecord.isValid).toBe(true); // 驗證新欄位
			recordId = data.createRecord.id;

			// 驗證紀錄是否真的被加入
			const { data: tripData } = await client.query({
				query: GET_TRIP,
				variables: { tripId },
			});
			expect(tripData.trip.records).toHaveLength(1);
			expect(tripData.trip.records[0].name).toBe(newRecord.name);
			expect(tripData.trip.records[0].time).toBe(newRecord.time);
			expect(tripData.trip.records[0].category).toBe('NORMAL'); // 驗證新欄位
		});

		it('should update the created record', async () => {
			// 依賴前一個測試建立的 recordId
			expect(recordId).toBeDefined();

			const updatedRecord = {
				name: 'Expensive Dinner',
				amount: 500,
				prePayAddress: 'Alice',
				shouldPayAddress: ['Alice'],
				category: 'FIX', // 更新欄位
				extendPayMsg: [500], // 更新欄位
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
			expect(data.updateRecord.category).toBe(updatedRecord.category); // 驗證更新
			expect(data.updateRecord.extendPayMsg).toEqual(
				updatedRecord.extendPayMsg
			); // 驗證更新
		});

		it('should remove the record', async () => {
			// 依賴前一個測試建立的 recordId
			expect(recordId).toBeDefined();

			const { data, error } = await client.mutate({
				mutation: REMOVE_RECORD,
				variables: { recordId },
			});

			expect(error).toBeUndefined();
			expect(data.removeRecord).toBe(recordId);

			// 驗證紀錄是否真的被移除
			const { data: tripData } = await client.query({
				query: GET_TRIP,
				variables: { tripId },
			});
			expect(tripData.trip.records).toHaveLength(0);
		});
	});
});
