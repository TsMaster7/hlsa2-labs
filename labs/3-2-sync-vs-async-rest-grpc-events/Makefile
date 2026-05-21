# HLSA2 lab 3-2 - Synchronous vs Asynchronous Communication
#
# One-line reproducibility from a clean checkout:
#   make up && make env-fingerprint && make bench-protocols
#
# Every target maps to a step in the topic-245 step-by-step guide. Read
# the per-target docstring before running.

SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c

LAB_ROOT := $(dir $(realpath $(firstword $(MAKEFILE_LIST))))
.DEFAULT_GOAL := help

# Knobs the topic guide references on the command line. Override via
#   make bench-protocols RUNS=5 PAYLOAD=large TARGET_RPS=1000
RUNS              ?= 3
TARGET_RPS        ?= 200
PAYLOAD           ?= small
LABEL             ?=
DURATION          ?= 5m
WINDOW            ?= 24h
HOP               ?=
MODE              ?= latency
P99_MS            ?= 400
ERROR_RATE        ?= 0.02
FLOW_CONTROL      ?= off
CONSUMER_MODE     ?= idempotent
CIRCUIT_BREAKER   ?= off
CANDIDATE         ?=

.PHONY: help up down stop logs ps topics env-fingerprint \
        bench-protocols analyze-protocols \
        bench-sync-chain analyze-sync-chain inject-fault clear-fault \
        bench-async-overload analyze-async \
        seed-events replay assert-idempotent analyze-replay \
        regression analyze \
        check-submission gen-proto clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

# --- Stack lifecycle --------------------------------------------------

up: ## Bring up the full stack (lookup, chain, gateway, producer, consumer, redpanda, postgres, observability)
	docker compose up -d --build
	@echo
	@echo "Waiting for healthchecks..."
	@sleep 20
	@docker compose ps

down: ## Stop and remove all containers (preserves named volumes)
	docker compose down

stop: down ## Alias for `make down`

clean: ## Stop AND remove named volumes - destroys postgres + prometheus + redpanda state
	docker compose down -v

logs: ## Tail logs from all services
	docker compose logs -f --tail=50

ps: ## Show service status
	docker compose ps

topics: ## List Redpanda topics (the 'events' topic must exist after `make up`)
	docker compose exec redpanda rpk topic list

# --- Environment fingerprint (step 2) ---------------------------------

env-fingerprint: ## Capture host + container fingerprint to perf/results/env.txt and meta.json
	bash perf/env.sh

# --- Step 3: protocol bench (REST vs gRPC) ----------------------------

bench-protocols: ## REST vs gRPC, RUNS=3 by default. Use PAYLOAD=large TARGET_RPS=1000 for the chatty regime.
	RUNS=$(RUNS) TARGET_RPS=$(TARGET_RPS) PAYLOAD=$(PAYLOAD) bash scripts/run-bench-protocols.sh

analyze-protocols: ## Aggregate per-protocol p50/p95/p99/p99.9, CPU, wire bytes across runs
	python3 scripts/analyze-protocols.py perf/results/rest perf/results/grpc

# --- Step 4: synchronous chain availability tax -----------------------

bench-sync-chain: ## Drive gateway -> auth -> pricing -> inventory. LABEL=healthy|faulted.
	LABEL=$(LABEL) CIRCUIT_BREAKER=$(CIRCUIT_BREAKER) bash scripts/run-sync-chain.sh

inject-fault: ## Inject latency+error into a chain hop. HOP=pricing P99_MS=400 ERROR_RATE=0.02
	HOP=$(HOP) MODE=$(MODE) P99_MS=$(P99_MS) ERROR_RATE=$(ERROR_RATE) bash scripts/inject-fault.sh

clear-fault: ## Reset fault injection on a hop. HOP=pricing
	HOP=$(HOP) bash scripts/clear-fault.sh

analyze-sync-chain: ## Compare measured E2E availability against product(per-hop)
	python3 scripts/analyze-sync-chain.py perf/results/sync-chain

# --- Step 5: async overload + backpressure ----------------------------

bench-async-overload: ## Drive producer past consumer's sustainable rate. FLOW_CONTROL=on|off LABEL=...
	FLOW_CONTROL=$(FLOW_CONTROL) LABEL=$(LABEL) DURATION=$(DURATION) bash scripts/run-async-overload.sh

analyze-async: ## Side-by-side: max queue depth, max event-to-effect latency, shed count
	python3 scripts/analyze-async.py perf/results/async

# --- Step 6: replay + idempotency -------------------------------------

seed-events: ## Populate the 'events' topic with WINDOW=24h of partitioned events
	WINDOW=$(WINDOW) bash scripts/seed-events.sh

replay: ## Spin a fresh consumer group from offset 0 over WINDOW
	WINDOW=$(WINDOW) CONSUMER_MODE=$(CONSUMER_MODE) bash scripts/replay.sh

assert-idempotent: ## Replay the same window twice and compare final-state hash
	CONSUMER_MODE=$(CONSUMER_MODE) bash scripts/assert-idempotent.sh

analyze-replay: ## Print replay throughput + peak Postgres connections + write latency
	python3 scripts/analyze-replay.py perf/results/replay

# --- Step 7: targeted improvement regression -------------------------

regression: ## 3 baseline + 3 candidate runs. CANDIDATE=grpc-hot-path|async-side-effect|dlq-retry-budget
	CANDIDATE=$(CANDIDATE) RUNS=$(RUNS) bash scripts/regression.sh

analyze: ## Per-metric median/sigma/delta + 2-sigma decision. CANDIDATE=...
	python3 scripts/analyze-regression.py perf/results/regression/$(CANDIDATE)/baseline perf/results/regression/$(CANDIDATE)/candidate

# --- Step 9: submission checker ---------------------------------------

check-submission: ## Verify every filename cited in docs/review.md actually exists
	bash scripts/check-submission.sh

# --- Proto codegen (optional, only if you edit lookup.proto) ----------

gen-proto: ## Regenerate proto bindings (requires protoc + protoc-gen-go + protoc-gen-go-grpc)
	@if ! command -v protoc >/dev/null 2>&1; then \
	  echo "protoc not on PATH. Skipping codegen - committed lookup.pb.go is what builds the binaries."; \
	  echo "Install: https://grpc.io/docs/protoc-installation/"; \
	  exit 1; \
	fi
	protoc \
	  --go_out=. --go_opt=paths=source_relative \
	  --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	  proto/lookup.proto
