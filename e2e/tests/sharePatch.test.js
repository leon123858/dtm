import { client } from '../src/apolloClient';
import { CREATE_TRIP, CREATE_ADDRESS, CREATE_RECORD, UPDATE_RECORD, GET_TRIP } from './graphql';

// Check both ordering and the association between addresses and split values.
function expectShares(record, values) {
	const ids = record.shouldPayAddress.map(address => address.id);
	expect(ids).toEqual(Object.keys(values).sort());
	expect(record.extendPayMsg).toEqual(ids.map(id => values[id]));
}

function snapshot(record) {
	return {
		name: record.name, amount: record.amount, time: record.time, category: record.category,
		prePayAddressId: record.prePayAddress.id,
		shouldPayAddressIds: record.shouldPayAddress.map(address => address.id),
		extendPayMsg: record.extendPayMsg,
	};
}

async function fixture(category = 'NORMAL') {
	const tripId = (await client.mutate({ mutation: CREATE_TRIP, variables: { input: { name: 'Member patches' } } })).data.createTrip.id;
	const ids = [];
	for (const name of ['Alice', 'Bob', 'Charlie']) {
		ids.push((await client.mutate({ mutation: CREATE_ADDRESS, variables: { tripId, input: { name } } })).data.createAddress.id);
	}
	const [a, b, c] = ids.sort();
	const old = { name: 'meal', amount: 20, time: '1234', category, prePayAddressId: a, shouldPayAddressIds: [b, a], extendPayMsg: [10, 10] };
	const root = (await client.mutate({ mutation: CREATE_RECORD, variables: { tripId, input: old } })).data.createRecord;
	const update = async (baseline, next, recordId = root.id) => (await client.mutate({ mutation: UPDATE_RECORD, variables: { recordId, input: { old: baseline, new: next } } })).data.updateRecord;
	const read = async (haveHistory = false) => (await client.query({ query: GET_TRIP, variables: { tripId, haveHistory } })).data.trip;
	return { a, b, c, old, root, update, read };
}

describe('Member patches through GraphQL', () => {
	it('merges independent stale updates and additions, with later writes winning on the same member', async () => {
		const { a, b, c, old, root, update, read } = await fixture();
		const first = await update(old, { ...old, extendPayMsg: [10, 11] });
		const second = await update(old, { ...old, shouldPayAddressIds: [b, a, c], extendPayMsg: [12, 10, 13] });
		expect(second.parentRecordId).toBe(first.id);
		expectShares(second, { [a]: 11, [b]: 12, [c]: 13 });
		const third = await update(old, { ...old, shouldPayAddressIds: [b, a, c], extendPayMsg: [14, 10, 15] });
		expect(third.parentRecordId).toBe(second.id);
		expectShares(third, { [a]: 11, [b]: 14, [c]: 15 });
		const baseline = snapshot(third);
		const reordered = { ...baseline, shouldPayAddressIds: [...baseline.shouldPayAddressIds].reverse(), extendPayMsg: [...baseline.extendPayMsg].reverse() };
		expect((await update(baseline, reordered)).id).toBe(third.id);
		const current = (await read()).records;
		expect(current.map(record => record.id)).toEqual([third.id]);
		expectShares(current[0], { [a]: 11, [b]: 14, [c]: 15 });
		const history = (await read(true)).records;
		expect(history).toHaveLength(4);
		for (const [id, values] of [[root.id, { [a]: 10, [b]: 10 }], [first.id, { [a]: 11, [b]: 10 }], [second.id, { [a]: 11, [b]: 12, [c]: 13 }], [third.id, { [a]: 11, [b]: 14, [c]: 15 }]]) {
			expectShares(history.find(record => record.id === id), values);
		}
	});

	it('ignores reorder and repeated deletion, and restores deleted members on stale updates', async () => {
		const { a, b, old, root, update, read } = await fixture();
		expect((await update(old, { ...old, shouldPayAddressIds: [a, b] })).id).toBe(root.id);
		const deleted = { ...old, shouldPayAddressIds: [a], extendPayMsg: [10] };
		const first = await update(old, deleted);
		expect((await update(old, deleted)).id).toBe(first.id);
		const recreated = await update(old, { ...old, extendPayMsg: [99, 10] });
		expect(recreated.parentRecordId).toBe(first.id);
		expectShares(recreated, { [a]: 10, [b]: 99 });
		const mixed = await update(old, { ...old, name: 'dinner', extendPayMsg: [99, 10] });
		expect(mixed.parentRecordId).toBe(recreated.id);
		expect(mixed.name).toBe('dinner');
		expectShares(mixed, { [a]: 10, [b]: 99 });
		// A subsequent edit updates the restored member.
		const restored = await update(snapshot(mixed), { ...snapshot(mixed), shouldPayAddressIds: [b, a], extendPayMsg: [20, 10] }, mixed.id);
		expect(restored.parentRecordId).toBe(mixed.id);
		expectShares(restored, { [a]: 10, [b]: 20 });
		expect((await read(true)).records).toHaveLength(5);
	});

	it.each(['empty recipients', 'unbalanced fixed amounts'])('persists, queries and repairs %s after merging valid edits', async scenario => {
		const empty = scenario === 'empty recipients';
		const { a, b, old, update, read } = await fixture(empty ? 'NORMAL' : 'FIX');
		const left = empty ? { ...old, shouldPayAddressIds: [a], extendPayMsg: [10] } : { ...old, amount: 30, extendPayMsg: [10, 20] };
		const right = empty ? { ...old, shouldPayAddressIds: [b], extendPayMsg: [10] } : { ...old, amount: 40, extendPayMsg: [30, 10] };
		const first = await update(old, left);
		expect(first.isValid).toBe(true);
		const merged = await update(old, right);
		expect(merged.parentRecordId).toBe(first.id);
		expect(merged.isValid).toBe(false);
		const trip = await read();
		expect(trip.isValid).toBe(false);
		expect(trip.records).toHaveLength(1);
		const current = trip.records[0];
		expect(current.id).toBe(merged.id);
		expect(current.isValid).toBe(false);
		expectShares(current, empty ? {} : { [a]: 20, [b]: 30 });
		const baseline = snapshot(current);
		const next = empty ? { ...baseline, shouldPayAddressIds: [a], extendPayMsg: [10] } : { ...baseline, amount: 50 };
		const repaired = await update(baseline, next, current.id);
		expect(repaired.parentRecordId).toBe(merged.id);
		expect(repaired.isValid).toBe(true);
		const after = await read();
		expect(after.isValid).toBe(true);
		expect(after.records.map(record => record.id)).toEqual([repaired.id]);
		expectShares(after.records[0], empty ? { [a]: 10 } : { [a]: 20, [b]: 30 });
		const history = (await read(true)).records;
		expect(history).toHaveLength(4);
		expect(history.filter(record => record.isActive).map(record => record.id)).toEqual([repaired.id]);
		expect(history.find(record => record.id === merged.id).isValid).toBe(false);
	});
});
