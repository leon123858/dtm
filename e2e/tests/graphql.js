// file: tests/graphql.js

import { gql } from '@apollo/client/core';

// --- Mutations ---
export const CREATE_TRIP = gql`
	mutation CreateTrip($input: NewTrip!) {
		createTrip(input: $input) {
			id
			name
			addressList
			isValid
			records {
				id
			}
		}
	}
`;

export const UPDATE_TRIP = gql`
	mutation UpdateTrip($tripId: ID!, $input: NewTrip!) {
		updateTrip(tripId: $tripId, input: $input) {
			id
			name
			addressList
			isValid
			records {
				id
			}
		}
	}
`;

export const CREATE_ADDRESS = gql`
	mutation CreateAddress($tripId: ID!, $address: String!) {
		createAddress(tripId: $tripId, address: $address)
	}
`;

export const DELETE_ADDRESS = gql`
	mutation DeleteAddress($tripId: ID!, $address: String!) {
		deleteAddress(tripId: $tripId, address: $address)
	}
`;

export const CREATE_RECORD = gql`
	mutation CreateRecord($tripId: ID!, $input: NewRecord!) {
		createRecord(tripId: $tripId, input: $input) {
			id
			name
			amount
			time
			prePayAddress
			shouldPayAddress
			extendPayMsg
			category
			isValid
		}
	}
`;

export const UPDATE_RECORD = gql`
	mutation UpdateRecord($recordId: ID!, $input: EditRecord!) {
		updateRecord(recordId: $recordId, input: $input) {
			id
			name
			amount
			time
			prePayAddress
			shouldPayAddress
			extendPayMsg
			category
			isValid
		}
	}
`;

export const REMOVE_RECORD = gql`
	mutation RemoveRecord($recordId: ID!) {
		removeRecord(recordId: $recordId)
	}
`;

// --- Queries ---
export const GET_TRIP = gql`
	query GetTrip($tripId: ID!) {
		trip(tripId: $tripId) {
			id
			name
			addressList
			isValid
			records {
				id
				name
				amount
				time
				prePayAddress
				shouldPayAddress
				extendPayMsg
				category
				isValid
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

// --- Subscriptions ---
export const SUB_RECORD_CREATE = gql`
	subscription SubRecordCreate($tripId: ID!) {
		subRecordCreate(tripId: $tripId) {
			id
			name
			amount
			time
			prePayAddress
			shouldPayAddress
			extendPayMsg
			category
			isValid
		}
	}
`;

export const SUB_RECORD_UPDATE = gql`
	subscription SubRecordUpdate($tripId: ID!) {
		subRecordUpdate(tripId: $tripId) {
			id
			name
			amount
			time
			prePayAddress
			shouldPayAddress
			extendPayMsg
			category
			isValid
		}
	}
`;

export const SUB_RECORD_DELETE = gql`
	subscription SubRecordDelete($tripId: ID!) {
		subRecordDelete(tripId: $tripId)
	}
`;

export const SUB_ADDRESS_CREATE = gql`
	subscription SubAddressCreate($tripId: ID!) {
		subAddressCreate(tripId: $tripId)
	}
`;

export const SUB_ADDRESS_DELETE = gql`
	subscription SubAddressDelete($tripId: ID!) {
		subAddressDelete(tripId: $tripId)
	}
`;
