# Query Trip 高併發實驗

驗證 request 內的 DataLoader 去重：多個 GraphQL 欄位與 aliases 共用 backing fetch，讀取次數不隨 records 數量或 HTTP 併發數增加。不同 requests 各有 loader，因此相同 trip 被 N 個 requests 查詢時，讀取總數正常地隨 N 線性增加。

## 重跑

需要 Go、可連線的 PostgreSQL，以及可建立 schema 的測試帳號。預設連到本機 `localhost:5432` 的 `postgres` database/user；可用 `TEST_DATABASE_URL` 指定連線（支援 PostgreSQL URL 或 keyword DSN）。不要把密碼寫入報告或版本控制。

```sh
# Loader correctness and race detection; no external service required.
go test ./adapters/db/db -run 'TestTripDataLoaderConcurrent' -race -count=1

# Each benchmark operation is one fresh request loader with concurrent callers.
go test ./adapters/db/db -run '^$' -bench '^BenchmarkTripDataLoaderConcurrent$' -benchmem -count=3

# Full HTTP/PostgreSQL experiment: 96 measured rounds and one negative control.
make bench-trip > /tmp/dtm-trip-bench-full.log 2>&1

# A focused HTTP experiment; anchor each subtest component (records10 also matches records1000 otherwise).
DTM_TRIP_BENCH=1 GIN_MODE=release go test ./web \
  -run '^TestTripQueryPostgresLoad$/^records10$/^full$/^workers32$/^round1$' \
  -count=1 -v

# Standard Go HTTP benchmark: 1 operation = 1 request, 10 records, 8 aliases.
DTM_TRIP_BENCH=1 GIN_MODE=release go test ./web -run '^$' \
  -bench '^BenchmarkTripQueryPostgres$' -benchtime=1024x -count=3 -timeout=20m
```

一般 `make test` 不會自動啟動長時間實驗；設定 `DTM_TRIP_BENCH=1` 才會執行。啟用後若 PostgreSQL 不可用，測試會失敗，不會跳過。

Harness 使用隨機命名的獨立 schema，在每條 DB 連線設定該 schema 的 `search_path`，套用 repository 的正式 migrations，結束時只刪除該 schema。若程序被強制終止，可能留下 `trip_bench_*` schema；確認沒有實驗執行後才手動清理。不要使用既有會 TRUNCATE 應用程式資料的 PG unit-test helper 跑此實驗。

## 情境與讀取預算

每組使用 1、32、128、256 個固定 HTTP workers，執行 1,024 requests，重複三輪。每個 trip 分別有 10 或 1,000 筆有效 records，另外有一筆已被更新的 predecessor 和一筆已刪除 record。每筆金額 20，由 payer 預付、member 負擔，因此總分帳金額為有效筆數 × 20。`isActive` 表示鏈尾，包含已刪除的鏈尾；history 有 N+1 筆 active，但只有 N 筆參與分帳。

| 查詢 | Trips/request | TripRecords/request | TripAddresses/request | Records/request | SELECT/request |
|---|---:|---:|---:|---:|---:|
| metadata | 1 | 0 | 0 | 0 | 1 |
| full | 1 | 1 | 1 | 0 | 3 |
| aliases8 | 1 | 1 | 1 | 0 | 3 |
| latest + history | 1 | 2 | 1 | 0 | 4 |
| aliases8 無快取對照 | 8 | 24 | 8 | 0 | 40 |

以上 counter 預算同時適用於 `Batches` 和 `Keys`。一次 batch 不一定只有一個 key；本實驗每個 request 只查同一個 trip，故兩者相同。`TripRecords.Keys` 是 trip key 數，並非回傳的 record 筆數。History 與 latest 使用不同 record-list loader，因此兩次讀取是必要的。

完整查詢包含 records 巢狀付款資料、addresses、moneyShare、isValid。無快取對照每次 Reader 呼叫建立新 loader；`records`、`moneyShare`、`isValid` 各自讀一次 record list，8 個 aliases 因而共 40 次 SELECT。對照只驗證讀取計數，不比較其延遲。

每次回應檢查 HTTP status、GraphQL errors、trip 身分、records 筆數與唯一性、active/deleted/history 狀態、巢狀付款資料與分帳金額；任何錯誤或超出預算都使實驗失敗。

## 量測方法與限制

- 使用正式 GraphQL handler 和 request loader middleware 的本機 HTTP server，以及真實 PG adapter。沒有經過 reverse proxy、TLS、gzip、存取日誌或 MQ；這些不影響本次驗證的去重路徑。
- 保留目前 dataloadgen v0.0.8 的預設 16ms batch wait；低併發延遲包含這段等待。
- PostgreSQL connection pool 為 32，HTTP client 上限為 256，request timeout 為 30 秒。每輪先做一個暖機 request，再重設 counters；暖機、fixture、migrations 與清理不計入結果。
- `DataLoaderDebug` 計 backing fetch 次數與 key 數；獨立 GORM logger 計實際 SELECT，沒有逐筆列印 SQL。兩組計數必須吻合。
- Counters 是 process-global。各輪循序執行，等所有 workers 結束才取 snapshot。原子欄位不代表多欄位 snapshot/reset 是一致的交易；量測期間不能重設。
- 每列 `TRIP_BENCH` JSON 包含 throughput、p50/p95/p99、錯誤數、SQL、四類 counters，以及連線池等待量。PoolWaitMS 是所有等待的加總，可能大於牆鐘時間。
- 延遲包含 HTTP 傳輸、完整 JSON decode 與回應正確性檢查；client 與 server 共用同一程序及 CPU。這是固定併發的 closed-loop 實驗，不能當成生產環境最大 QPS 或開放到達率下的 SLA。
- SQL/request 不應隨資料量增加，但 payload、CPU、記憶體與延遲仍可能增加。Race detector 的執行結果不作效能比較。
