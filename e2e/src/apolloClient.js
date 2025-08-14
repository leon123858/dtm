import pkg from '@apollo/client';
const { ApolloClient, InMemoryCache, split, HttpLink } = pkg;
import { getMainDefinition } from '@apollo/client/utilities';
import { GraphQLWsLink } from '@apollo/client/link/subscriptions';
import { createClient } from 'graphql-ws';

const { GRAPHQL_ENDPOINT_HTTP, GRAPHQL_ENDPOINT_WS } = {
	GRAPHQL_ENDPOINT_HTTP: 'http://127.0.0.1:8080/query',
	GRAPHQL_ENDPOINT_WS: 'ws://127.0.0.1:8080/subscription',
};

let WebSocketImpl;
if (typeof window === 'undefined') {
	try {
		const wsPkg = await import('ws');
		WebSocketImpl = wsPkg.WebSocket;
	} catch (e) {
		console.warn(
			"Could not import 'ws' module. This is expected in browser environments."
		);
	}
} else {
	WebSocketImpl = window.WebSocket;
}

const httpLink = new HttpLink({
	uri: GRAPHQL_ENDPOINT_HTTP,
});

const wsLink = new GraphQLWsLink(
	createClient({
		url: GRAPHQL_ENDPOINT_WS,
		...(WebSocketImpl && { webSocketImpl: WebSocketImpl }),
	})
);

const splitLink = split(
	({ query }) => {
		const definition = getMainDefinition(query);
		return (
			definition.kind === 'OperationDefinition' &&
			definition.operation === 'subscription'
		);
	},
	wsLink,
	httpLink
);

export const client = new ApolloClient({
	link: splitLink,
	cache: new InMemoryCache(),
	defaultOptions: {
		watchQuery: { fetchPolicy: 'no-cache' },
		query: { fetchPolicy: 'no-cache' },
	},
});
