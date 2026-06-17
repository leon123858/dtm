# Repository Guidelines

## Project Structure & Module Organization

This repository is a Go expense-splitting service with CLI and GraphQL API modes. `dtm.go` is the main entry point. `cmd/` contains Cobra commands for `serve`, `share`, and migrations. Core settlement logic lives in `tx/`; GraphQL schema, resolvers, models, and generated code live in `graph/`; HTTP setup is in `web/`. Persistence code is split between `db/pg`, `db/mem`, and shared `db/db` interfaces. Message queue adapters are under `mq/`. Database migrations are in `migration/`. End-to-end GraphQL tests are in `e2e/`, and Terraform infrastructure is in `infra/`.

## Build, Test, and Development Commands

- `make format`: runs `go fmt ./...` and `go vet ./...`.
- `make test`: runs all Go tests with `go test ./... -v -count=1`.
- `make serve`: starts the GraphQL service with `go run dtm.go serve`.
- `make cli`: runs the CSV sharing example using `sampleInput.csv` and `sampleOutput.txt`.
- `make gql`: regenerates gqlgen output after schema changes.
- `make dev-docker`: starts local PostgreSQL and RabbitMQ containers, then runs migrations.
- `make testE2E`: runs Jest tests in `e2e/`; start the service first.
- `make build`: builds `./bin/app`.

## Coding Style & Naming Conventions

Use standard Go formatting; run `make format` before submitting changes. Keep package names short and lowercase, matching existing directories such as `tx`, `web`, `graph`, and `pg`. Go test files use the `*_test.go` suffix and should sit beside the code they cover. Generated GraphQL files such as `graph/generated.go` and `graph/model/models_gen.go` should be updated via `make gql`, not manually edited unless absolutely necessary.

## Testing Guidelines

Add focused Go unit tests for changes in core logic, DB adapters, MQ adapters, and resolvers. Prefer deterministic tests and run `make test` before opening a PR. For API behavior, add Jest tests under `e2e/tests/*.test.js`; shared client helpers belong in `e2e/src` or existing helper files.

## Commit & Pull Request Guidelines

Recent commits use short, imperative summaries such as `debug SetNoSmallValue edge case` and `format gql`. Keep commits focused and describe the behavior changed. PRs should include a brief summary, test results (`make test`, `make testE2E` when relevant), linked issues if any, and screenshots or GraphQL examples for API-visible behavior.

## Security & Configuration Tips

Do not commit database passwords, cloud credentials, or local `.env` files. Use Docker for local PostgreSQL/RabbitMQ, and document any required environment variables in the PR when adding new configuration.
