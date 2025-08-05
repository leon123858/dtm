# DTM 專案分析報告

## 1. 專案總覽

**專案名稱:** Division Trip Money (dtm)

**核心目標:** 此專案是一個用 Go 語言編寫的費用分攤應用程式。旨在幫助團體活動（如旅行）的成員輕鬆計算每個人應支付或應收回的金額，以平衡帳目。

**操作模式:** 專案提供兩種主要的操作模式：
- **命令列介面 (CLI):** 用於快速處理 CSV 檔案，進行費用分攤計算。
- **Web 服務:** 提供一個功能更全面的 GraphQL API 服務，用於建立和管理旅程、參與者和費用記錄。

---

## 2. 核心功能

- **雙模式操作:**
  - **CLI 模式:** 從 CSV 檔案快速、輕鬆地計算費用分攤結果。
  - **Web 模式:** 提供功能齊全的 GraphQL API，用於對旅程、參與者和費用記錄進行 CRUD (建立、讀取、更新、刪除) 操作。
- **即時更新:** Web 模式支援 GraphQL Subscriptions，允許客戶端接收旅程資料變更的即時通知。
- **核心演算法:** `tx` 目錄下的程式碼負責核心的費用分攤邏輯，計算出最終的交易（誰該付錢給誰）。

---

## 3. 技術棧

- **後端語言:** Go (版本 1.23.3)
- **Web 框架:** Gin
- **API:** GraphQL (使用 `gqlgen` 函式庫)
- **資料庫:** PostgreSQL
- **ORM:** GORM
- **資料庫遷移:** `pressly/goose`
- **命令列介面:** `spf13/cobra`
- **訊息佇列 (MQ):**
  - RabbitMQ
  - Google Cloud Pub/Sub
  - (顯示專案可能採用微服務或事件驅動架構)
- **前端 E2E 測試:** Jest (位於 `e2e` 目錄)
- **基礎設施即程式碼 (IaC):** Terraform (位於 `infra` 目錄)
- **容器化:** Docker

---

## 4. 專案結構分析

```
.
├── cmd/            # 應用程式進入點 (server, cli, migrate)
├── config/         # 專案設定檔
├── db/             # 資料庫相關邏輯 (pg, mem)
├── e2e/            # 端對端測試 (Jest)
├── graph/          # GraphQL 核心 (schema, resolvers, generated code)
├── infra/          # 基礎設施設定 (Terraform)
├── migration/      # 資料庫遷移檔案
├── mq/             # 訊息佇列實作 (gcppubsub, rabbit)
├── tx/             # 核心費用分攤演算法
├── web/            # Web 伺服器 (Gin handler, middleware)
├── dtm.go          # 專案主進入點
├── go.mod          # Go 模組依賴
├── Makefile        # 開發用的指令稿
├── dockerfile      # Docker 容器設定
└── README.md       # 專案說明文件
```

- **`cmd/`**: 包含應用程式的主要進入點。`server.go` 啟動 GraphQL Web 服務，`share.go` 處理 CLI 模式，而 `migrate.go` 則用於執行資料庫遷移。
- **`graph/`**: 這是 GraphQL API 的核心。`schema.graphqls` 定義了 API 的結構，`resolver.go` 和 `schema.resolvers.go` 包含了實現這些 API 的業務邏輯。
- **`db/`**: 負責所有資料庫的互動。它被分成不同的後端實現，如 `pg` (PostgreSQL) 和 `mem` (記憶體)，這種結構使得更換或測試資料庫變得容易。
- **`tx/`**: 包含了專案最核心的演算法，即如何根據所有開銷記錄計算出最簡化的轉帳方案。
- **`mq/`**: 包含了與訊息佇列服務（如 RabbitMQ, GCP Pub/Sub）的整合程式碼，這表明系統可能透過事件驅動的方式處理非同步任務（例如，在記錄更新後通知其他系統）。
- **`e2e/`**: 包含用 JavaScript (Jest) 編寫的端對端測試，用於從使用者角度測試 GraphQL API 的功能是否正常。
- **`infra/`**: 存放 Terraform 程式碼，用於自動化部署專案所需的雲端基礎設施。
- **`Makefile`**: 提供了一系列方便的開發指令，如 `make test` (執行單元測試), `make serve` (啟動服務), `make gql` (重新產生 GraphQL 程式碼) 等。

---

## 5. GraphQL API 設計

根據 `graph/schema.graphqls`，API 的主要模型如下：

- **`Trip`**: 代表一個旅程，包含名稱、所有費用記錄 (`records`)、計算後的金流 (`moneyShare`) 和參與者列表 (`addressList`)。
- **`Record`**: 代表一筆費用，包含名稱、金額、預付者 (`prePayAddress`) 和應分攤者 (`shouldPayAddress`)。
- **`Tx`**: 代表一筆交易，說明了誰 (`input`) 應該付錢給誰 (`output`) 以及金額。
- **`Query`**: 用於查詢旅程的詳細資訊。
- **`Mutation`**: 用於執行寫入操作，如建立/更新旅程、新增/刪除費用記錄、新增/刪除參與者。
- **`Subscription`**: 用於即時監聽特定旅程的資料變更，例如當有新的費用記錄或參與者加入時，客戶端可以立即收到通知。

---

## 6. 開發與部署流程

- **開發:**
  - 開發者可以使用 `make` 指令來執行常見任務，例如格式化程式碼 (`make format`)、執行測試 (`make test`) 和啟動本地開發伺服器 (`make serve`)。
  - `make dev-docker` 指令可以快速啟動 PostgreSQL 和 RabbitMQ 的 Docker 容器，方便本地開發。
  - GraphQL 的程式碼是透過 `make gql` 指令自動產生的，確保 resolver 和 schema 之間的一致性。
- **測試:**
  - **單元測試:** `make test` 會執行 Go 的單元測試。
  - **E2E 測試:** `make testE2E` 會在 `e2e` 目錄下執行 Jest 測試，驗證整個系統的行為。
- **部署:**
  - `dockerfile` 和 `docker-build` 指令表明專案可以被打包成 Docker 映像檔進行部署。
  - `infra/` 目錄下的 Terraform 程式碼暗示了專案可以被部署到雲端平台（可能是 GCP，因為有 Pub/Sub 的整合）。
  - `.github/workflows` 下的 `CI.yml` 和 `CD.yaml` 表明專案設定了持續整合 (CI) 和持續部署 (CD) 的流程。
