# ==============================================================================
# Makefile for Terraform (Plain Version)
# ==============================================================================

PLAN_FILE := tfplan.out

.PHONY: help init fmt validate plan apply destroy plan-destroy clean

default: help

## deploy candy cmd
deploy:
	make init
	make plan
	make apply

## help:
help:
	@echo "----------------------------------------------------"
	@echo "  Makefile for Terraform"
	@echo "----------------------------------------------------"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

## init:
init:
	@echo "==> Initializing Terraform..."
	@terraform init

## fmt:
fmt:
	@echo "==> Formatting Terraform code..."
	@terraform fmt -recursive

## validate:
validate:
	@echo "==> Validating Terraform configuration..."
	@terraform validate

## plan:
plan: validate
	@echo "==> Creating execution plan..."
	@terraform plan -out=$(PLAN_FILE)

## apply:
apply:
	@echo "==> Applying execution plan..."
	@if [ ! -f $(PLAN_FILE) ]; then \
		echo "Plan file '$(PLAN_FILE)' not found. Running 'make plan' first."; \
		make plan; \
	fi
	@terraform apply "$(PLAN_FILE)"
	@make clean

## destroy:
destroy:
	@echo "WARNING: This will destroy all managed infrastructure!"
	make init
	make plan-destroy
	make apply

## plan-destroy:
plan-destroy:
	@echo "==> Creating plan to DESTROY all managed infrastructure..."
	@terraform plan -destroy -out=$(PLAN_FILE)

## clean:
clean:
	@echo "==> Cleaning up temporary files..."
	@rm -f $(PLAN_FILE)
	@rm -rf .terraform.lock.hcl