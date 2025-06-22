# Division Trip Money (dtm)

dtm is a cost-sharing application written in Go. It helps members of a trip or group activity easily calculate the amount each person should pay or receive to balance the accounts. This project offers two main modes of operation: a Command-Line Interface (CLI) for processing CSV files and a more comprehensive GraphQL API web service.

## 🌟 Features

- Dual-Mode Operation:
  - CLI Mode: Quickly and easily calculate cost-sharing results from a CSV file.
  - Web Mode: Provides a full-featured GraphQL API for creating and managing trips, participants, and expense records.
- Real-Time Updates: The web mode supports GraphQL Subscriptions, allowing clients to receive real-time notifications of changes to trip data.

## 🚀 Quick Start

### Prerequisites

- Go (1.18 or higher)
- Make (optional, for running quick commands)
- Docker (for running PostgreSQL and RabbitMQ in production mode)

### Installation

Clone this repository:

```bash
git clone https://github.com/leon123858/dtm
cd dtm
go mod tidy
```

### 🕹️ Usage

can use `go run dtm.go -h` for detail

#### CLI mode:

use CLI mode can quickly use core function directly

can check [sampleInput](./sampleInput.csv) and [sampleOutput](./sampleOutput.txt) for detail format

```bash
go run dtm.go share --input input.csv --output output.csv
```

#### Web Server Mode

The Web mode starts a full-featured GraphQL server, allowing you to perform CRUD operations on trips via an API and supports real-time communication.

This command starts the web server.

```bash
go run dtm.go serve
```

Once the server starts, you can open your browser to http://localhost:8080/ to use the GraphQL Playground.

More production mode settings can be checked in [dockerfile](./dockerfile)

Ref Frontend: [dtmf](https://github.com/leon123858/dtmf)
