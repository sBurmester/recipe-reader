# Recipe Reader — Task Breakdown

Per-task working files split out of [the implementation plan](../2026-09-05-recipe-reader-implementation.md) for independent tracking (e.g. one subagent or one PR per file). Each file is self-contained: Files/Interfaces to consume-and-produce, and TDD-style checkbox steps, copied verbatim from the plan.

Check a task's own checkboxes as its steps complete, and flip its **Status** line once done.


## Phase 0: Bootstrap & Tooling

- [x] [Task 1: Project Scaffold & Tooling](01-project-scaffold-tooling.md)
- [x] [Task 2: Configuration Package](02-configuration-package.md)

## Phase 1: Domain & Persistence (sqlc + Postgres)

- [ ] [Task 3: Database Schema, Migrations, sqlc Codegen & Domain Models](03-database-schema-migrations-sqlc-codegen-domain-models.md)
- [ ] [Task 4: Recipe Repository (CRUD + Search)](04-recipe-repository-crud-search.md)
- [ ] [Task 5: Lookup Repository (Categories, Units, Ingredients)](05-lookup-repository-categories-units-ingredients.md)

## Phase 2: Recipe Extraction Engine

- [ ] [Task 6: Extractor Interface & Unit Normalization](06-extractor-interface-unit-normalization.md)
- [ ] [Task 7: Rule-Based Extractor](07-rule-based-extractor.md)
- [ ] [Task 8: LLM-Based Extractor (Anthropic API)](08-llm-based-extractor-anthropic-api.md)
- [ ] [Task 9: Hybrid Extractor](09-hybrid-extractor.md)

## Phase 3: Instagram Integration

- [ ] [Task 10: Instagram Client Wrapper (Login & Session Persistence)](10-instagram-client-wrapper-login-session-persistence.md)
- [ ] [Task 11: Saved-Posts & Collection Fetching](11-saved-posts-collection-fetching.md)

## Phase 4: Import Pipeline

- [ ] [Task 12: Import Pipeline (Fetch → Extract → Store)](12-import-pipeline-fetch-extract-store.md)
- [ ] [Task 13: Background Worker (Scheduled + Manual Trigger)](13-background-worker-scheduled-manual-trigger.md)

## Phase 5: REST API

- [ ] [Task 14: Router, Middleware & Health Check](14-router-middleware-health-check.md)
- [ ] [Task 15: Recipe Handlers (CRUD + Search)](15-recipe-handlers-crud-search.md)
- [ ] [Task 16: Lookup Handlers (Categories, Units, Ingredients)](16-lookup-handlers-categories-units-ingredients.md)
- [ ] [Task 17: Import Trigger & Status Handlers](17-import-trigger-status-handlers.md)
- [ ] [Task 18: `main.go` Wiring & Graceful Shutdown](18-main-go-wiring-graceful-shutdown.md)

## Phase 6: Frontend (Vanilla TS + Vite)

- [ ] [Task 19: Vite Scaffold, API Client & Shared Types](19-vite-scaffold-api-client-shared-types.md)
- [ ] [Task 20: Recipe List Page (Search, Filter, Pagination)](20-recipe-list-page-search-filter-pagination.md)
- [ ] [Task 21: Recipe Detail/Edit Page](21-recipe-detail-edit-page.md)
- [ ] [Task 22: Import Status Page, Router & Styling](22-import-status-page-router-styling.md)

## Phase 7: Packaging & CI

- [ ] [Task 23: Single-Binary Packaging (go:embed, Dockerfile, docker-compose)](23-single-binary-packaging-go-embed-dockerfile-docker-compose.md)
- [ ] [Task 24: CI Workflow & Final Documentation](24-ci-workflow-final-documentation.md)
