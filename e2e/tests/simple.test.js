import { gql } from '@apollo/client/core';
import { client } from '../src/apolloClient';

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
	query GetTrip($tripId: ID!) {
		trip(tripId: $tripId) {
			id
			name
			addressList
			records {
				id
				name
				amount
				prePayAddress
				shouldPayAddress
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
		}
	}
`;

const UPDATE_RECORD = gql`
	mutation UpdateRecord($tripId: ID!, $recordId: ID!, $input: NewRecord!) {
		updateRecord(tripId: $tripId, recordId: $recordId, input: $input) {
			id
			name
			amount
			prePayAddress
			shouldPayAddress
		}
	}
`;

const REMOVE_RECORD = gql`
	mutation RemoveRecord($recordId: ID!) {
		removeRecord(recordId: $recordId)
	}
`;

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
				prePayAddress: 'Alice',
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
			recordId = data.createRecord.id;

			// 驗證紀錄是否真的被加入
			const { data: tripData } = await client.query({
				query: GET_TRIP,
				variables: { tripId },
			});
			expect(tripData.trip.records).toHaveLength(1);
			expect(tripData.trip.records[0].name).toBe(newRecord.name);
		});

		it('should update the created record', async () => {
			// 依賴前一個測試建立的 recordId
			expect(recordId).toBeDefined();

			const updatedRecord = {
				name: 'Expensive Dinner',
				amount: 500,
				prePayAddress: 'Alice',
				shouldPayAddress: ['Alice'],
			};

			const { data, error } = await client.mutate({
				mutation: UPDATE_RECORD,
				variables: {
					tripId,
					recordId,
					input: updatedRecord,
				},
			});

			expect(error).toBeUndefined();
			expect(data.updateRecord.name).toBe(updatedRecord.name);
			expect(data.updateRecord.amount).toBe(updatedRecord.amount);
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
