import pkg from '@apollo/client';
const { ApolloClient, InMemoryCache, HttpLink } = pkg;
import fetch from 'cross-fetch';

// 從環境變數讀取後端 API 的 URL
const { GRAPHQL_ENDPOINT } = {
	GRAPHQL_ENDPOINT: 'http://127.0.0.1:8080/query',
};

if (!GRAPHQL_ENDPOINT) {
	throw new Error(
		'GraphQL endpoint is not defined. Please set GRAPHQL_ENDPOINT in your .env file.'
	);
}

// 建立 HTTP link
const httpLink = new HttpLink({
	uri: GRAPHQL_ENDPOINT,
	fetch,
});

// 建立 Apollo Client 實例
export const client = new ApolloClient({
	link: httpLink,
	cache: new InMemoryCache(),
	// 關閉快取，確保每次測試都是向伺服器發送真實請求
	defaultOptions: {
		watchQuery: { fetchPolicy: 'no-cache' },
		query: { fetchPolicy: 'no-cache' },
	},
});
