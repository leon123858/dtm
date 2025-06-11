import pkg from '@apollo/client';
const { ApolloClient, InMemoryCache, split, HttpLink } = pkg;
import { getMainDefinition } from '@apollo/client/utilities';
import { GraphQLWsLink } from '@apollo/client/link/subscriptions';
import { createClient } from 'graphql-ws';

const { GRAPHQL_ENDPOINT_HTTP, GRAPHQL_ENDPOINT_WS } = {
	GRAPHQL_ENDPOINT_HTTP: 'http://127.0.0.1:8080/query',
	GRAPHQL_ENDPOINT_WS: 'ws://127.0.0.1:8080/subscription',
};

const httpLink = new HttpLink({
	uri: GRAPHQL_ENDPOINT_HTTP,
});

const wsLink = new GraphQLWsLink(
	createClient({
		url: GRAPHQL_ENDPOINT_WS,
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

// 建立 Apollo Client 實例
export const client = new ApolloClient({
	link: splitLink,
	cache: new InMemoryCache(),
	// 關閉快取，確保每次測試都是向伺服器發送真實請求
	defaultOptions: {
		watchQuery: { fetchPolicy: 'no-cache' },
		query: { fetchPolicy: 'no-cache' },
	},
});
