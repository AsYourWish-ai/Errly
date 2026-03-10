.PHONY: install up down restart logs status dev help

# Load .env if it exists
-include .env
export

## install  — guided first-time install (generates API key, starts server)
install:
	@bash install.sh

## up       — start server in background
up:
	docker compose up -d --build errly

## down     — stop all services
down:
	docker compose down

## restart  — restart the server
restart:
	docker compose restart errly

## logs     — tail server logs
logs:
	docker compose logs -f errly

## status   — show running containers
status:
	docker compose ps

## dev      — dev mode with auto-rebuild on file changes
dev:
	docker compose watch

## help     — show this help
help:
	@echo ""
	@echo "  Errly — available commands:"
	@echo ""
	@grep -E '^## ' Makefile | sed 's/## /  make /'
	@echo ""

.DEFAULT_GOAL := help
