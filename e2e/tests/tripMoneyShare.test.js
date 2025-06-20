import { gql } from '@apollo/client/core';
import { client } from '../src/apolloClient'; // 確保路徑正確

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

// 更新 GET_TRIP 查詢，包含 moneyShare
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
			moneyShare {
				# 根據您的 schema，欄位名稱為 moneyShare
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

const CREATE_RECORD = gql`
	mutation CreateRecord($tripId: ID!, $input: NewRecord!) {
		createRecord(tripId: $tripId, input: $input) {
			id
			name
			amount
			prePayAddress
			shouldPayAddress
		}
	}
`;

describe('Trip with Money Share Logic End-to-End Tests', () => {
	let tripId;
	const testTripName = `Money Share Trip - ${Date.now()}`;
	const addressAlice = 'Alice';
	const addressBob = 'Bob';
	const addressCharlie = 'Charlie';

	// 在所有測試開始前，先建立一個共用的旅程和地址
	beforeAll(async () => {
		// 1. 創建 Trip
		const { data: tripData } = await client.mutate({
			mutation: CREATE_TRIP,
			variables: { input: { name: testTripName } },
		});
		expect(tripData.createTrip.id).toBeDefined();
		tripId = tripData.createTrip.id;

		// 2. 創建地址
		await client.mutate({
			mutation: CREATE_ADDRESS,
			variables: { tripId, address: addressAlice },
		});
		await client.mutate({
			mutation: CREATE_ADDRESS,
			variables: { tripId, address: addressBob },
		});
		await client.mutate({
			mutation: CREATE_ADDRESS,
			variables: { tripId, address: addressCharlie },
		});

		// 驗證地址是否成功添加
		const { data: fetchedTripData } = await client.query({
			query: GET_TRIP,
			variables: { tripId },
		});
		expect(fetchedTripData.trip.addressList).toEqual(
			expect.arrayContaining([addressAlice, addressBob, addressCharlie])
		);
		// console.log('Setup complete. Trip ID:', tripId);
	});

	// --- 測試 Record 和 Money Share ---
	describe('Record and Money Share Calculation', () => {
		it('should create multiple records and calculate moneyShare correctly', async () => {
			// Record 1: Alice 支付 100，Alice 和 Bob 分擔
			const record1 = {
				name: 'Dinner',
				amount: 100,
				prePayAddress: addressAlice,
				shouldPayAddress: [addressAlice, addressBob],
			};
			await client.mutate({
				mutation: CREATE_RECORD,
				variables: { tripId, input: record1 },
			});

			// Record 2: Bob 支付 60，Alice 和 Charlie 分擔
			const record2 = {
				name: 'Transport',
				amount: 60,
				prePayAddress: addressBob,
				shouldPayAddress: [addressAlice, addressCharlie],
			};
			await client.mutate({
				mutation: CREATE_RECORD,
				variables: { tripId, input: record2 },
			});

			// Record 3: Charlie 支付 90，Alice, Bob, Charlie 分擔
			const record3 = {
				name: 'Accommodation',
				amount: 90,
				prePayAddress: addressCharlie,
				shouldPayAddress: [addressAlice, addressBob, addressCharlie],
			};
			await client.mutate({
				mutation: CREATE_RECORD,
				variables: { tripId, input: record3 },
			});

			// Fetch the trip with all details, including moneyShare
			const { data, error } = await client.query({
				query: GET_TRIP,
				variables: { tripId },
			});

			expect(error).toBeUndefined();
			expect(data.trip.id).toBe(tripId);
			expect(data.trip.records).toHaveLength(3); // 應該有 3 筆記錄
			expect(data.trip.moneyShare).toBeDefined(); // 確保 moneyShare 存在

			// console.log(data.trip.moneyShare);

			for (let input of data.trip.moneyShare[0].input) {
				if (input.address == 'Alice') {
					expect(data.trip.moneyShare[0].input[1].amount).toBeCloseTo(10, 1);
				} else if (input.address == 'Bob') {
					expect(data.trip.moneyShare[0].input[0].amount).toBeCloseTo(20, 1);
				} else {
					fail('expect address are above');
				}
			}
			expect(data.trip.moneyShare[0].output.amount).toBeCloseTo(30, 2); // Charlie 應收 30
			expect(data.trip.moneyShare[0].output.address).toBe('Charlie');
		});
	});
});
