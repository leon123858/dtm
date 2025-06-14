# ==============================================================================
# Makefile for Terraform (Plain Version)
#
# 為常用的 Terraform 指令提供簡潔的別名，並標準化執行流程。
# ==============================================================================

# --- 設定 ---

# Terraform Plan 檔案的輸出路徑
PLAN_FILE := tfplan.out

# 使用 .PHONY 來宣告這些 target 不是真正的檔案，避免與同名檔案衝突。
.PHONY: help init fmt validate plan apply destroy plan-destroy clean

# --- 主要指令 ---

# 預設指令，執行 `make` 時會顯示幫助訊息。
default: help

## deploy candy cmd
deploy:
	make init
	make plan
	make apply

## help: 顯示所有可用的指令與說明
help:
	@echo "----------------------------------------------------"
	@echo "  Makefile for Terraform"
	@echo "----------------------------------------------------"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

## init: 初始化 Terraform 工作目錄 (下載 providers)
init:
	@echo "==> Initializing Terraform..."
	@terraform init

## fmt: 格式化所有 .tf 檔案，使其風格一致
fmt:
	@echo "==> Formatting Terraform code..."
	@terraform fmt -recursive

## validate: 驗證 Terraform 程式碼語法是否正確
validate:
	@echo "==> Validating Terraform configuration..."
	@terraform validate

## plan: 產生執行計畫，預覽將要做的變更
plan: validate
	@echo "==> Creating execution plan..."
	@terraform plan -out=$(PLAN_FILE)

## apply: 套用執行計畫，實際部署資源
apply:
	@echo "==> Applying execution plan..."
	@if [ ! -f $(PLAN_FILE) ]; then \
		echo "Plan file '$(PLAN_FILE)' not found. Running 'make plan' first."; \
		make plan; \
	fi
	@terraform apply "$(PLAN_FILE)"
	@make clean

## destroy: 銷毀所有由 Terraform 管理的資源
destroy:
	@echo "WARNING: This will destroy all managed infrastructure!"
	make init
	make plan-destroy
	make apply

## plan-destroy: 預覽將要銷毀的資源
plan-destroy:
	@echo "==> Creating plan to DESTROY all managed infrastructure..."
	@terraform plan -destroy -out=$(PLAN_FILE)

## clean: 清理產生的暫存檔案
clean:
	@echo "==> Cleaning up temporary files..."
	@rm -f $(PLAN_FILE)
	@rm -rf .terraform.lock.hcl