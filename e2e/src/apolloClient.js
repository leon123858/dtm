import pkg from '@apollo/client';
const { ApolloClient, InMemoryCache, split, HttpLink } = pkg;
import { getMainDefinition } from '@apollo/client/utilities';
import { GraphQLWsLink } from '@apollo/client/link/subscriptions';
import { createClient } from 'graphql-ws';

// 環境變數
const { GRAPHQL_ENDPOINT_HTTP, GRAPHQL_ENDPOINT_WS } = {
	GRAPHQL_ENDPOINT_HTTP: 'http://127.0.0.1:8080/query',
	GRAPHQL_ENDPOINT_WS: 'ws://127.0.0.1:8080/subscription',
};

// 根據環境條件式導入 ws 模組
let WebSocketImpl;
if (typeof window === 'undefined') {
	// 非瀏覽器環境 (例如 Node.js)
	// 動態導入 'ws'，避免在瀏覽器環境下打包或執行
	// 注意：在某些打包工具中，可能需要額外的配置來處理動態導入
	// 例如，如果你使用 Webpack，它可能會在打包時包含 'ws'，即使是動態導入。
	// 在這種情況下，你需要考慮 Webpack 的 externals 配置，或者使用更明確的環境變數來控制打包。
	// 對於大部分的 SSR 框架 (如 Next.js)，它們會自動處理伺服器端模組。
	try {
		const wsPkg = await import('ws'); // 使用動態導入
		WebSocketImpl = wsPkg.WebSocket;
	} catch (e) {
		console.warn(
			"Could not import 'ws' module. This is expected in browser environments."
		);
		// 如果在非瀏覽器環境中仍然無法導入，這裡可以選擇拋出錯誤或提供一個 fallback
		// 在大多數情況下，如果你在 Node.js 環境中，它應該能正常導入
	}
} else {
	// 瀏覽器環境
	WebSocketImpl = window.WebSocket; // 使用瀏覽器原生的 WebSocket
}

const httpLink = new HttpLink({
	uri: GRAPHQL_ENDPOINT_HTTP,
});

// 建立 WebSocketLink
const wsLink = new GraphQLWsLink(
	createClient({
		url: GRAPHQL_ENDPOINT_WS,
		// 只有在 WebSocketImpl 存在時才傳入 webSocketImpl
		...(WebSocketImpl && { webSocketImpl: WebSocketImpl }),
		// 或者更簡潔地寫作：
		// webSocketImpl: WebSocketImpl || undefined,
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
