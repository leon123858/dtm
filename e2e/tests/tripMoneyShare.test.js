import { client } from '../src/apolloClient'; // 確保路徑正確
import {
	GET_TRIP,
	CREATE_TRIP,
	CREATE_ADDRESS,
	DELETE_ADDRESS, // 引入 DELETE_ADDRESS
	CREATE_RECORD,
	UPDATE_RECORD,
} from './graphql';

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
	});

	// --- 測試 Record 和 Money Share ---
	describe('Record and Money Share Calculation', () => {
		it('should create multiple records and calculate moneyShare correctly', async () => {
			// Record 1: Alice 支付 100，Alice 和 Bob 分擔
			const record1 = {
				name: 'Dinner',
				amount: 100,
				time: '1672531199',
				prePayAddress: addressAlice,
				shouldPayAddress: [addressAlice, addressBob],
				category: 'NORMAL',
				extendPayMsg: [],
			};
			await client.mutate({
				mutation: CREATE_RECORD,
				variables: { tripId, input: record1 },
			});

			// Record 2: Bob 支付 60，Alice 和 Charlie 分擔
			const record2 = {
				name: 'Transport',
				amount: 60,
				time: '1672531299',
				prePayAddress: addressBob,
				shouldPayAddress: [addressAlice, addressCharlie],
				category: 'NORMAL',
				extendPayMsg: [],
			};
			await client.mutate({
				mutation: CREATE_RECORD,
				variables: { tripId, input: record2 },
			});

			// Record 3: Charlie 支付 90，Alice, Bob, Charlie 分擔
			const record3 = {
				name: 'Accommodation',
				amount: 90,
				time: '1672531399',
				prePayAddress: addressCharlie,
				shouldPayAddress: [addressAlice, addressBob, addressCharlie],
				category: 'NORMAL',
				extendPayMsg: [],
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

			// console.log(JSON.stringify(data.trip.moneyShare, null, 2));

			// 假設計算結果只有一筆交易 (某人付錢給另一人)
			// 注意：這個斷言高度依賴後端業務邏輯的具體實現
			// 根據之前的紀錄：
			// Alice: 預付100, 應付 (100/2 + 60/2 + 90/3) = 50 + 30 + 30 = 110. 結果: -10 (應付10)
			// Bob:   預付60,  應付 (100/2 + 90/3) = 50 + 30 = 80. 結果: -20 (應付20)
			// Charlie: 預付90,  應付 (60/2 + 90/3) = 30 + 30 = 60. 結果: +30 (應收30)
			// 最終交易: Alice付10給Charlie, Bob付20給Charlie
			expect(data.trip.moneyShare).toHaveLength(1); // 假設最終優化為一筆交易 Tx
			const transaction = data.trip.moneyShare[0];

			// 驗證付款方 (input)
			expect(transaction.input).toHaveLength(2);
			const alicePayment = transaction.input.find((p) => p.address === 'Alice');
			const bobPayment = transaction.input.find((p) => p.address === 'Bob');
			expect(alicePayment).toBeDefined();
			expect(bobPayment).toBeDefined();
			expect(alicePayment.amount).toBeCloseTo(10);
			expect(bobPayment.amount).toBeCloseTo(20);

			// 驗證收款方 (output)
			expect(transaction.output.address).toBe('Charlie');
			expect(transaction.output.amount).toBeCloseTo(30);
		});
	});

	// --- 測試 Record is Valid ---
	describe('Record Validity (isValid) Tests', () => {
		let localTripId;

		beforeAll(async () => {
			const { data } = await client.mutate({
				mutation: CREATE_TRIP,
				variables: { input: { name: 'Validity Test Trip' } },
			});
			localTripId = data.createTrip.id;
		});

		it('should mark a NORMAL record as invalid if its payers are removed', async () => {
			const tempAddress = 'TempPayer';
			const Payer = 'SinglePayer';
			await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId: localTripId, address: tempAddress },
			});
			await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId: localTripId, address: Payer },
			});

			const record = {
				name: 'Valid Record initially',
				amount: 100,
				prePayAddress: Payer,
				shouldPayAddress: [tempAddress],
				category: 'NORMAL',
				extendPayMsg: [],
			};
			const { data: recordData } = await client.mutate({
				mutation: CREATE_RECORD,
				variables: { tripId: localTripId, input: record },
			});
			const recordId = recordData.createRecord.id;
			expect(recordId).toBeDefined();
			expect(recordData.createRecord.isValid).toBe(true);

			// 驗證初始狀態為 valid
			let { data: tripData } = await client.query({
				query: GET_TRIP,
				variables: { tripId: localTripId },
			});
			expect(tripData.trip.records[0].isValid).toBe(true);

			// 移除唯一的付款人
			await client.mutate({
				mutation: DELETE_ADDRESS,
				variables: { tripId: localTripId, address: tempAddress },
			});

			// 再次查詢，驗證紀錄已變為 invalid
			({ data: tripData } = await client.query({
				query: GET_TRIP,
				variables: { tripId: localTripId },
			}));
			const targetRecord = tripData.trip.records.find((r) => r.id === recordId);
			// console.log('Target Record:', targetRecord);
			expect(targetRecord.isValid).toBe(false);
			// when record have inValid, trip should inValid
			expect(tripData.trip.isValid).toBe(false);

			// 加回來後更新加回去回到合法
			await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId: localTripId, address: tempAddress },
			});
			const { data: recordDataUpdated } = await client.mutate({
				mutation: UPDATE_RECORD,
				variables: { recordId: recordId, input: record },
			});
			expect(recordDataUpdated.updateRecord.isValid).toBe(true);
			expect(recordDataUpdated.updateRecord.id).toBe(recordId);
			({ data: tripData } = await client.query({
				query: GET_TRIP,
				variables: { tripId: localTripId },
			}));
			// console.log(JSON.stringify(tripData, null, 2));
			expect(tripData.trip.isValid).toBe(true);
		});

		it('should mark a FIX record as invalid if amounts do not sum up', async () => {
			const addr1 = 'FixPayer1';
			const addr2 = 'FixPayer2';
			await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId: localTripId, address: addr1 },
			});
			await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId: localTripId, address: addr2 },
			});

			const record = {
				name: 'Invalid FIX Record',
				amount: 100, // 總金額
				prePayAddress: addr1,
				shouldPayAddress: [addr1, addr2],
				category: 'FIX',
				extendPayMsg: [40, 50], // 金額總和 90，不等於 amount 100
			};

			await client.mutate({
				mutation: CREATE_RECORD,
				variables: { tripId: localTripId, input: record },
			});

			// 查詢並驗證紀錄為 invalid
			const { data: tripData } = await client.query({
				query: GET_TRIP,
				variables: { tripId: localTripId },
			});
			// console.log('Trip Data:', JSON.stringify(tripData, null, 2));
			const targetRecord = tripData.trip.records.find(
				(r) => r.name === 'Invalid FIX Record'
			);
			expect(targetRecord.isValid).toBe(false);
			// when record have inValid, trip should inValid
			expect(tripData.trip.isValid).toBe(false);
		});
	});

	// --- 測試混合模式 (FIX & NORMAL) ---
	describe('Mixed Mode Money Share Calculation', () => {
		let mixedTripId;
		const mixAlice = 'MixAlice',
			mixBob = 'MixBob',
			mixCharlie = 'MixCharlie';

		beforeAll(async () => {
			const { data } = await client.mutate({
				mutation: CREATE_TRIP,
				variables: { input: { name: 'Mixed Mode Test Trip' } },
			});
			mixedTripId = data.createTrip.id;
			await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId: mixedTripId, address: mixAlice },
			});
			await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId: mixedTripId, address: mixBob },
			});
			await client.mutate({
				mutation: CREATE_ADDRESS,
				variables: { tripId: mixedTripId, address: mixCharlie },
			});
		});

		it('should calculate moneyShare correctly with mixed NORMAL and FIX records', async () => {
			// Record 1 (NORMAL): Alice 支付 150，三人均分
			const recordNormal = {
				name: 'NORMAL Lunch',
				amount: 150,
				prePayAddress: mixAlice,
				shouldPayAddress: [mixAlice, mixBob, mixCharlie],
				category: 'NORMAL',
				extendPayMsg: [],
			};
			await client.mutate({
				mutation: CREATE_RECORD,
				variables: { tripId: mixedTripId, input: recordNormal },
			});

			// Record 2 (FIX): Bob 支付 100，指定分擔金額
			const recordFix = {
				name: 'FIX Tickets',
				amount: 100,
				prePayAddress: mixBob,
				shouldPayAddress: [mixAlice, mixBob, mixCharlie],
				category: 'FIX',
				extendPayMsg: [20, 30, 50], // Alice:20, Bob:30, Charlie:50
			};
			await client.mutate({
				mutation: CREATE_RECORD,
				variables: { tripId: mixedTripId, input: recordFix },
			});

			const { data } = await client.query({
				query: GET_TRIP,
				variables: { tripId: mixedTripId },
			});

			// 預期結果:
			// Alice: 預付 150. 應付 (150/3) + 20 = 50 + 20 = 70.  結果: +80 (應收)
			// Bob:   預付 100. 應付 (150/3) + 30 = 50 + 30 = 80.  結果: +20 (應收)
			// Charlie: 預付 0.  應付 (150/3) + 50 = 50 + 50 = 100. 結果: -100 (應付)
			// 最終交易: Charlie 付 100，其中 80 給 Alice，20 給 Bob
			expect(data.trip.moneyShare).toBeDefined();
			expect(data.trip.moneyShare).toHaveLength(2); // 預期有兩筆交易

			const charliePays = data.trip.moneyShare.filter(
				(tx) => tx.input[0].address === mixCharlie
			);
			expect(charliePays).toHaveLength(2);

			const paymentToAlice = charliePays.find(
				(tx) => tx.output.address === mixAlice
			);
			const paymentToBob = charliePays.find(
				(tx) => tx.output.address === mixBob
			);

			expect(paymentToAlice).toBeDefined();
			expect(paymentToBob).toBeDefined();

			expect(paymentToAlice.input[0].amount).toBeCloseTo(80);
			expect(paymentToBob.input[0].amount).toBeCloseTo(20);
		});
	});
});
