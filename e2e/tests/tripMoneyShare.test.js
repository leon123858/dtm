import { client } from '../src/apolloClient';
import {
	GET_TRIP,
	CREATE_TRIP,
	CREATE_ADDRESS,
	DELETE_ADDRESS,
	CREATE_RECORD,
	UPDATE_RECORD,
} from './graphql';

describe('Trip with Money Share Logic End-to-End Tests', () => {
	let tripId;
	const testTripName = `Money Share Trip - ${Date.now()}`;
	const addressAlice = 'Alice';
	const addressBob = 'Bob';
	const addressCharlie = 'Charlie';

	beforeAll(async () => {
		// 1. create trip
		const { data: tripData } = await client.mutate({
			mutation: CREATE_TRIP,
			variables: { input: { name: testTripName } },
		});
		expect(tripData.createTrip.id).toBeDefined();
		tripId = tripData.createTrip.id;

		// 2. create addresses
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

		// verify addresses are added to trip
		const { data: fetchedTripData } = await client.query({
			query: GET_TRIP,
			variables: { tripId },
		});
		expect(fetchedTripData.trip.addressList).toEqual(
			expect.arrayContaining([addressAlice, addressBob, addressCharlie])
		);
	});

	// ---  Record and Money Share ---
	describe('Record and Money Share Calculation', () => {
		it('should create multiple records and calculate moneyShare correctly', async () => {
			// Record 1: Alice pay 100，Alice and Bob share
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

			// Record 2: Bob pay 60，Alice and Charlie share
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

			// Record 3: Charlie pay 90，Alice, Bob, Charlie share
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
			expect(data.trip.records).toHaveLength(3);
			expect(data.trip.moneyShare).toBeDefined();

			// console.log(JSON.stringify(data.trip.moneyShare, null, 2));

			// Alice: pay 100, should (100/2 + 60/2 + 90/3) = 50 + 30 + 30 = 110. get: -10 (output 10)
			// Bob:   pay 60,  should (100/2 + 90/3) = 50 + 30 = 80. get: -20 (output 20)
			// Charlie: pay 90,  should (60/2 + 90/3) = 30 + 30 = 60. get: +30 (input 30)
			// result: Alice pay 10 to Charlie, Bob pay 20 to Charlie
			expect(data.trip.moneyShare).toHaveLength(1);
			const transaction = data.trip.moneyShare[0];

			expect(transaction.input).toHaveLength(2);
			const alicePayment = transaction.input.find((p) => p.address === 'Alice');
			const bobPayment = transaction.input.find((p) => p.address === 'Bob');
			expect(alicePayment).toBeDefined();
			expect(bobPayment).toBeDefined();
			expect(alicePayment.amount).toBeCloseTo(10);
			expect(bobPayment.amount).toBeCloseTo(20);

			expect(transaction.output.address).toBe('Charlie');
			expect(transaction.output.amount).toBeCloseTo(30);
		});
	});

	// --- Record is Valid ---
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

			// valid
			let { data: tripData } = await client.query({
				query: GET_TRIP,
				variables: { tripId: localTripId },
			});
			expect(tripData.trip.records[0].isValid).toBe(true);

			await client.mutate({
				mutation: DELETE_ADDRESS,
				variables: { tripId: localTripId, address: tempAddress },
			});

			({ data: tripData } = await client.query({
				query: GET_TRIP,
				variables: { tripId: localTripId },
			}));
			const targetRecord = tripData.trip.records.find((r) => r.id === recordId);
			// console.log('Target Record:', targetRecord);
			expect(targetRecord.isValid).toBe(false);
			// when record have inValid, trip should inValid
			expect(tripData.trip.isValid).toBe(false);

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
				amount: 100,
				prePayAddress: addr1,
				shouldPayAddress: [addr1, addr2],
				category: 'FIX',
				extendPayMsg: [40, 50], // total 90，not equal to amount 100
			};

			await client.mutate({
				mutation: CREATE_RECORD,
				variables: { tripId: localTripId, input: record },
			});

			// invalid
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

	// --- record mode (FIX & NORMAL) ---
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
			// Record 1 (NORMAL): Alice pay 150，average split
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

			// Record 2 (FIX): Bob pay 100，fixed split
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

			// Expected result:
			// Alice: paid 150. Should pay (150/3) + 20 = 50 + 20 = 70. Net: +80 (to receive)
			// Bob:   paid 100. Should pay (150/3) + 30 = 50 + 30 = 80. Net: +20 (to receive)
			// Charlie: paid 0. Should pay (150/3) + 50 = 50 + 50 = 100. Net: -100 (to pay)
			// Final transactions: Charlie pays 100, with 80 to Alice and 20 to Bob
			expect(data.trip.moneyShare).toBeDefined();
			expect(data.trip.moneyShare).toHaveLength(2);

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

	// --- record mode (FIX & PART) ---
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

		it('should calculate moneyShare correctly with mixed NORMAL and PART records', async () => {
			// Record 1 (NORMAL): Alice pay 150，average split
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

			const recordFix = {
				name: 'FIX Tickets',
				amount: 100,
				prePayAddress: mixBob,
				shouldPayAddress: [mixAlice, mixBob, mixCharlie],
				category: 'PART',
				extendPayMsg: [2, 3, 5],
			};
			await client.mutate({
				mutation: CREATE_RECORD,
				variables: { tripId: mixedTripId, input: recordFix },
			});

			const { data } = await client.query({
				query: GET_TRIP,
				variables: { tripId: mixedTripId },
			});

			// Expected result:
			// Alice: paid 150. Should pay (150/3) + 20 = 50 + 20 = 70. Net: +80 (to receive)
			// Bob:   paid 100. Should pay (150/3) + 30 = 50 + 30 = 80. Net: +20 (to receive)
			// Charlie: paid 0. Should pay (150/3) + 50 = 50 + 50 = 100. Net: -100 (to pay)
			// Final transactions: Charlie pays 100, with 80 to Alice and 20 to Bob
			expect(data.trip.moneyShare).toBeDefined();
			expect(data.trip.moneyShare).toHaveLength(2);

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

	// --- record mode (FIX_BEFORE_AVERAGE) ---
	describe('FIX_BEFORE_AVERAGE Mode Money Share Calculation', () => {
		let mixedTripId;
		const mixAlice = 'MixAlice',
			mixBob = 'MixBob',
			mixCharlie = 'MixCharlie';

		beforeAll(async () => {
			const { data } = await client.mutate({
				mutation: CREATE_TRIP,
				variables: { input: { name: 'Fix Before Average Mode Test Trip' } },
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

		it('should calculate moneyShare correctly with FIX_BEFORE_AVERAGE record', async () => {
			// Record: Alice pays 200.
			// Bob has a fixed payment of 20.
			// Charlie pay more than average 40
			// Alice and Charlie share the rest of the average amount.
			const record = {
				name: 'FIX_BEFORE_AVERAGE Dinner',
				amount: 200,
				prePayAddress: mixAlice,
				shouldPayAddress: [mixAlice, mixBob, mixCharlie],
				category: 'FIX_BEFORE_NORMAL',
				extendPayMsg: [0, -20, 40],
			};
			await client.mutate({
				mutation: CREATE_RECORD,
				variables: { tripId: mixedTripId, input: record },
			});

			const { data } = await client.query({
				query: GET_TRIP,
				variables: { tripId: mixedTripId },
			});

			// Expected result:
			// Total amount: 200
			// Fixed payment (Bob): 20
			// Fixed payment (Charlie): 40 (Charlie pay more than average)
			// Amount to be averaged: 200 - 60 = 140
			// Number of people for average: 2 (Alice, Charlie)
			// Average amount: 140 / 2 = 70
			//
			// Alice: paid 200. Should pay 70. Net: +130 (to receive)
			// Bob:   paid 0.   Should pay 20. Net: -20 (to pay)
			// Charlie: paid 0. Should pay 110. Net: -110 (to pay)
			//
			// Final transactions: Bob pays 20 to Alice, Charlie pays 110 to Alice.
			expect(data.trip.moneyShare).toBeDefined();
			expect(data.trip.moneyShare).toHaveLength(1);

			const paymentToAlice = data.trip.moneyShare.filter(
				(tx) => tx.output.address === mixAlice
			);
			expect(paymentToAlice).toHaveLength(1);

			// console.log(JSON.stringify(data.trip.moneyShare, null, 2));
			expect(data.trip.moneyShare[0].output.address).toBe(mixAlice);
			expect(data.trip.moneyShare[0].output.amount).toBeCloseTo(130);

			expect(data.trip.moneyShare[0].input).toHaveLength(2);
			const bobPayment = data.trip.moneyShare[0].input.find(
				(p) => p.address === mixBob
			);
			const charliePayment = data.trip.moneyShare[0].input.find(
				(p) => p.address === mixCharlie
			);
			expect(bobPayment).toBeDefined();
			expect(charliePayment).toBeDefined();
			expect(bobPayment.amount).toBeCloseTo(20);
			expect(charliePayment.amount).toBeCloseTo(110);
		});
	});
});
