# API end-to-end tests

Start the service from the repository root with `E2E_AUTH_ENABLED=true make serve`.
Development mode loads the existing `.env`: configure `E2E_AUTH_EMAIL` and
`E2E_AUTH_PASSWORD` there alongside the normal GCIP/OAuth configuration.
Credentials stay on the server; Jest obtains real tokens through `/auth/password`.
Do not commit credentials. Login failure fails setup rather than skipping tests.

Run `make testE2E` from the repository root. To run the primary flows only:

```sh
cd e2e
npm test -- --runInBand tests/member.test.js tests/journey.test.js tests/sub.test.js
```

`GRAPHQL_ENDPOINT_HTTP` and `GRAPHQL_ENDPOINT_WS` override the default local
endpoints (`http://127.0.0.1:8080/query`, `ws://127.0.0.1:8080/subscription`).
The server needs access to the real identity provider. No fabricated tokens or
identity-provider mocks are used.

- `member`: real login/relogin/refresh, anonymous and malformed-credential
  boundaries, automatic collection, idempotence, timestamps, removal and errors.
- `journey`: member and anonymous collaboration, history, totals, settlement,
  cross-trip validation and unchanged state after rejected writes.
- `sub`: anonymous streams observe authenticated writes; all five event types,
  repeated events, deletion and isolation between two real trips.
- Remaining suites retain focused business-rule regressions (allocation modes,
  patch conflicts, history, names and statistics) using the anonymous Apollo client.

Trips are publicly readable and writable; collection is not an access grant.
Collection operations require authentication. Collection results are ordered by
trip ID, not `lastViewedAt`. One configured account cannot test cross-user isolation.
Tests filter to their own trip IDs and remove only their own collections in cleanup.
There is no delete-trip API: use a disposable development database for test trips.
The default development service uses in-memory storage and Go-channel messaging;
this run does not certify PostgreSQL or external message-queue deployment behavior.

The former `simple` CRUD and `password` smoke suites are covered by the journeys
and existing history regression. Subscription checks use positive event assertions;
a transport error is never treated as proof that an event was absent.
