# HLSA2 lab 2-3 - Load Testing, Stress Testing, Benchmark Methodology
#
# One-line reproducibility (from a clean checkout):
#   make run-sut && make perf-baseline
#
# Most targets are thin wrappers around docker compose and the scripts
# in scripts/. Read scripts/run-experiment.sh first to understand what
# each `perf-*` target actually runs.

SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c

LAB_ROOT := $(dir $(realpath $(firstword $(MAKEFILE_LIST))))
.DEFAULT_GOAL := help

.PHONY: help run-sut stop logs ps env-fingerprint validate-workload \
        perf-baseline perf-closed-loop perf-regression perf-soak \
        analyze-regression analyze-omission \
        clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
	  awk 'BEGIN {FS=":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

run-sut: ## Bring up the full stack (sut + downstream + postgres + observability)
	docker compose up -d --build
	@echo
	@echo "Waiting for healthchecks..."
	@sleep 15
	@docker compose ps

stop: ## Stop and remove all containers (preserves named volumes)
	docker compose down

clean: ## Stop AND remove named volumes - destroys postgres + prometheus state
	docker compose down -v

logs: ## Tail logs from all services
	docker compose logs -f --tail=50

ps: ## Show service status
	docker compose ps

env-fingerprint: ## Print the environment fingerprint to stdout
	bash perf/env.sh

validate-workload: ## Validate perf/workload.json against the schema (requires ajv-cli, optional)
	@if command -v ajv >/dev/null 2>&1; then \
	  ajv validate -s perf/workload.schema.json -d perf/workload.json; \
	else \
	  echo "ajv-cli not installed; skipping schema validation."; \
	  echo "Install with: npm install -g ajv-cli ajv-formats"; \
	fi

perf-baseline: ## Run the open-loop baseline experiment
	bash scripts/run-experiment.sh baseline

perf-closed-loop: ## Run the closed-loop generator (for the omission demo)
	bash scripts/run-experiment.sh closed-loop

perf-regression: ## Run the three-run regression protocol (baseline x3, candidate x3)
	bash scripts/three-run-regression.sh

perf-soak: ## Run the 4-hour soak test
	bash scripts/run-experiment.sh soak

analyze-regression: ## Compute median, sigma, deltap99, 2-sigma decision over the latest baseline+candidate runs
	python3 scripts/analyze-regression.py perf/results/baseline perf/results/candidate

analyze-omission: ## Compare the latest closed-loop and open-loop runs side by side
	python3 scripts/coordinated-omission-summary.py perf/results/coordinated-omission
