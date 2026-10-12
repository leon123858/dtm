import { gql } from '@apollo/client/core';
import { client } from '../src/apolloClient';
import { CREATE_TRIP, CREATE_RECORD, UPDATE_RECORD, CREATE_ADDRESS } from './graphql';

const READ_STATISTICS = gql`
	query ReadAddressStatistics($tripId: ID!, $history: Boolean!) {
		trip(tripId: $tripId, haveHistory: $history) {
			addresses { id statistics { totalExpense totalPrepaid activeRecordCount } }
			records {
				prePayAddress { id statistics { totalExpense totalPrepaid activeRecordCount } }
				shouldPayAddress { id statistics { totalExpense totalPrepaid activeRecordCount } }
			}
			moneyShare {
				input { address { id statistics { totalExpense totalPrepaid activeRecordCount } } }
				output { address { id statistics { totalExpense totalPrepaid activeRecordCount } } }
			}
		}
	}
`;

it('reports consistent current member totals across query fields and after updates', async () => {
	const tripId = (await client.mutate({
		mutation: CREATE_TRIP, variables: { input: { name: 'Address statistics' } },
	})).data.createTrip.id;
	const members = [];
	for (const name of ['A', 'B', 'C', 'Unused']) {
		const address = (await client.mutate({
			mutation: CREATE_ADDRESS, variables: { tripId, input: { name } },
		})).data.createAddress;
		members.push(address);
	}
	const [a, b, c, unused] = members.map(member => member.id);
	const read = async history => (await client.query({
		query: READ_STATISTICS, variables: { tripId, history },
	})).data.trip;
	const totals = trip => Object.fromEntries(trip.addresses.map(address => [address.id, {
		totalExpense: address.statistics.totalExpense,
		totalPrepaid: address.statistics.totalPrepaid,
		activeRecordCount: address.statistics.activeRecordCount,
	}]));
	const statistics = (totalExpense = 0, totalPrepaid = 0, activeRecordCount = 0) => ({ totalExpense, totalPrepaid, activeRecordCount });
	expect(totals(await read(false))).toEqual({ [a]: statistics(), [b]: statistics(), [c]: statistics(), [unused]: statistics() });
	const meal = {
		name: 'Meal', amount: 900, time: '1700000000000', prePayAddressId: a,
		shouldPayAddressIds: [a, b, c], category: 'NORMAL', extendPayMsg: [],
	};
	const record = (await client.mutate({ mutation: CREATE_RECORD, variables: { tripId, input: meal } })).data.createRecord;
	const transfer = {
		name: 'Repayment', amount: 100, time: '1700000000001', prePayAddressId: b,
		shouldPayAddressIds: [a], category: 'TRANSFER', extendPayMsg: [100],
	};
	await client.mutate({ mutation: CREATE_RECORD, variables: { tripId, input: transfer } });
	const current = await read(false);
	expect(totals(current)).toEqual({ [a]: statistics(400, 900, 2), [b]: statistics(300, 100, 2), [c]: statistics(300, 0, 1), [unused]: statistics() });
	expect(current.moneyShare.length).toBeGreaterThan(0);
	for (const tx of current.moneyShare) {
		expect(tx.output.address.statistics).toMatchObject(totals(current)[tx.output.address.id]);
		for (const input of tx.input) expect(input.address.statistics).toMatchObject(totals(current)[input.address.id]);
	}
	for (const item of current.records) {
		expect(item.prePayAddress.statistics).toMatchObject(totals(current)[item.prePayAddress.id]);
	}
	const revised = { ...meal, amount: 600 };
	const updated = (await client.mutate({
		mutation: UPDATE_RECORD, variables: { recordId: record.id, input: { old: meal, new: revised } },
	})).data.updateRecord;
	const expected = { [a]: statistics(300, 600, 2), [b]: statistics(200, 100, 2), [c]: statistics(200, 0, 1), [unused]: statistics() };
	expect(totals(await read(false))).toEqual(expected);
	expect(totals(await read(true))).toEqual(expected);
	await client.mutate({
		mutation: UPDATE_RECORD, variables: { recordId: updated.id, input: { old: revised, new: { ...revised, isDeleted: true } } },
	});
	const afterDelete = { [a]: statistics(100, 0, 1), [b]: statistics(0, 100, 1), [c]: statistics(), [unused]: statistics() };
	expect(totals(await read(false))).toEqual(afterDelete);
	expect(totals(await read(true))).toEqual(afterDelete);
});

it('isolates statistics for different trips', async () => {
	const tripIds = [];
	const members = [];
	for (let i = 0; i < 2; i++) {
		const tripId = (await client.mutate({ mutation: CREATE_TRIP, variables: { input: { name: `Statistics scope ${i}` } } })).data.createTrip.id;
		tripIds.push(tripId);
		members.push((await client.mutate({ mutation: CREATE_ADDRESS, variables: { tripId, input: { name: 'Member' } } })).data.createAddress.id);
	}
	const input = { name: 'First', amount: 10, prePayAddressId: members[0], shouldPayAddressIds: [members[0]], category: 'NORMAL' };
	for (const record of [input, { ...input, name: 'Second', amount: 20 }]) {
		await client.mutate({ mutation: CREATE_RECORD, variables: { tripId: tripIds[0], input: record } });
	}
	const query = gql`
		query SeparateTrips($first: ID!, $second: ID!) {
			first: trip(tripId: $first) { records { prePayAddress { statistics { totalExpense totalPrepaid activeRecordCount } } } }
			second: trip(tripId: $second) { addresses { statistics { totalExpense totalPrepaid activeRecordCount } } }
		}
	`;
	const data = (await client.query({ query, variables: { first: tripIds[0], second: tripIds[1] } })).data;
	expect(data.second.addresses[0].statistics).toMatchObject({ totalExpense: 0, totalPrepaid: 0, activeRecordCount: 0 });
});
