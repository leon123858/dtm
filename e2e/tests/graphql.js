// file: tests/graphql.js

import { gql } from '@apollo/client/core';

// --- Mutations ---
export const CREATE_TRIP = gql`
	mutation CreateTrip($input: NewTrip!) {
		createTrip(input: $input) {
			id
			name
			addresses { id name }
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
			addresses { id name }
			isValid
			records {
				id
			}
		}
	}
`;

export const CREATE_ADDRESS = gql`
	mutation CreateAddress($tripId: ID!, $input: NewAddress!) {
		createAddress(tripId: $tripId, input: $input) { id name }
	}
`;

export const UPDATE_ADDRESS = gql`
	mutation UpdateAddress($tripId: ID!, $addressId: ID!, $input: NewAddress!) {
		updateAddress(tripId: $tripId, addressId: $addressId, input: $input) { id name }
	}
`;

export const DELETE_ADDRESS = gql`
	mutation DeleteAddress($tripId: ID!, $addressId: ID!) {
		deleteAddress(tripId: $tripId, addressId: $addressId) { id name }
	}
`;

export const CREATE_RECORD = gql`
	mutation CreateRecord($tripId: ID!, $input: NewRecord!) {
		createRecord(tripId: $tripId, input: $input) {
			id
			name
			amount
			time
			prePayAddress { id name }
			shouldPayAddress { id name }
			extendPayMsg
			category
			parentRecordId
			isDeleted
			isActive
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
			prePayAddress { id name }
			shouldPayAddress { id name }
			extendPayMsg
			category
			parentRecordId
			isDeleted
			isActive
			isValid
		}
	}
`;

// --- Queries ---
export const GET_TRIP = gql`
	query GetTrip($tripId: ID!) {
		trip(tripId: $tripId) {
			id
			name
			addresses { id name }
			isValid
			records {
				id
				name
				amount
				time
				prePayAddress { id name }
				shouldPayAddress { id name }
				extendPayMsg
				category
				parentRecordId
				isDeleted
				isActive
				isValid
			}
			moneyShare {
				input {
					amount
					address { id name }
				}
				output {
					amount
					address { id name }
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
			prePayAddress { id name }
			shouldPayAddress { id name }
			extendPayMsg
			category
			parentRecordId
			isDeleted
			isActive
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
			prePayAddress { id name }
			shouldPayAddress { id name }
			extendPayMsg
			category
			parentRecordId
			isDeleted
			isActive
			isValid
		}
	}
`;

export const SUB_ADDRESS_CREATE = gql`
	subscription SubAddressCreate($tripId: ID!) {
		subAddressCreate(tripId: $tripId) { id name }
	}
`;

export const SUB_ADDRESS_UPDATE = gql`
	subscription SubAddressUpdate($tripId: ID!) {
		subAddressUpdate(tripId: $tripId) { id name }
	}
`;

export const SUB_ADDRESS_DELETE = gql`
	subscription SubAddressDelete($tripId: ID!) {
		subAddressDelete(tripId: $tripId) { id name }
	}
`;
