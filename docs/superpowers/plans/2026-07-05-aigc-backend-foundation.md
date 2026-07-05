# AIGC Backend Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the P0 backend foundation for media assets, TOS upload, generation jobs, job queue, storyboard asset binding, and Image2 asset persistence.

**Architecture:** Keep Eino ADK responsible for agent/tool decisions and interrupt/resume. Add business-layer persistence for assets and generation jobs, plus a Redis queue worker that writes assets and patches storyboards after provider completion. Keep media graph orchestration in Eino Compose Graph, but do not reimplement ADK runner or tool-calling behavior.

**Tech Stack:** Go 1.26, Gin, GORM/Postgres, Redis list queue, Volcengine TOS SDK, Eino ADK/Compose tools.

---

### Task 1: Asset Model, Store, And TOS Uploader

**Files:**
- Create: `internal/aigc/asset/models.go`
- Create: `internal/aigc/asset/postgres_store.go`
- Create: `internal/aigc/asset/tos_uploader.go`
- Modify: `internal/aigc/config/config.go`
- Modify: `.env.example`
- Test: `internal/aigc/asset/postgres_store_test.go`
- Test: `internal/aigc/config/config_test.go`

- [ ] Write failing tests for saving/listing an asset, deriving a public URL from `TOS_BASE_URL`, and loading `TOS_*` env config without hardcoding credentials.
- [ ] Implement `Asset` with fields: id, session_id, user_id, kind, source, mime_type, filename, size_bytes, storage_provider, bucket, object_key, url, metadata JSON, created_at.
- [ ] Implement `PostgresStore.AutoMigrate`, `Save`, `Get`, and `ListBySession`.
- [ ] Implement `Uploader` interface and TOS uploader using Volcengine SDK.
- [ ] Add sanitized TOS config status to avoid leaking secrets.
- [ ] Run `go test ./internal/aigc/asset ./internal/aigc/config`.

### Task 2: Asset Upload And Query API

**Files:**
- Modify: `internal/aigc/server/router.go`
- Modify: `cmd/aigc-agent/main.go`
- Test: `internal/aigc/server/router_test.go`

- [ ] Write failing tests for `POST /api/aigc/assets` with multipart file and `GET /api/aigc/assets/:id`.
- [ ] Add `AssetStore` and `AssetUploader` interfaces to the router config.
- [ ] Implement upload endpoint with fields: session_id, user_id, kind, source, and optional binding target fields.
- [ ] Save uploaded bytes through the uploader, persist asset metadata, and return asset JSON.
- [ ] Implement query endpoint by asset id.
- [ ] Run `go test ./internal/aigc/server`.

### Task 3: Storyboard Asset Binding

**Files:**
- Create: `internal/aigc/storyboard/binding.go`
- Modify: `internal/aigc/server/router.go`
- Test: `internal/aigc/storyboard/binding_test.go`
- Test: `internal/aigc/server/router_test.go`

- [ ] Write failing tests for binding an asset to key element asset_ids, shot keyframe/video asset id, and audio layer asset id.
- [ ] Implement binding by generating existing JSON patch ops and applying them through `StoryboardStore.ApplyPatch`.
- [ ] Support upload-time binding fields: `storyboard_id`, `base_version`, `target_type`, `target_id`, `asset_role`.
- [ ] Emit `storyboard.patch` payload in upload response when binding succeeds.
- [ ] Run `go test ./internal/aigc/storyboard ./internal/aigc/server`.

### Task 4: Generation Job Store And Redis Queue

**Files:**
- Create: `internal/aigc/generation/models.go`
- Create: `internal/aigc/generation/postgres_store.go`
- Create: `internal/aigc/generation/redis_queue.go`
- Modify: `cmd/aigc-agent/main.go`
- Test: `internal/aigc/generation/postgres_store_test.go`
- Test: `internal/aigc/generation/redis_queue_test.go`

- [ ] Write failing tests for job status transitions and Redis enqueue/dequeue JSON payload.
- [ ] Implement `GenerationJob` table with queued/running/succeeded/failed/cancelled status.
- [ ] Implement idempotent `Create`, `Get`, `UpdateStatus`, and `MarkSucceeded`.
- [ ] Implement Redis list queue using configured `AGENT_GENERATION_REDIS_LIST_KEY`.
- [ ] Run `go test ./internal/aigc/generation`.

### Task 5: Image2 Job/Asset Integration

**Files:**
- Modify: `internal/aigc/tools/image2.go`
- Modify: `internal/aigc/agent/deepseek.go`
- Test: `internal/aigc/tools/image2_test.go`

- [ ] Write failing tests proving Image2 output can be uploaded as an asset and returns only asset metadata plus provider summary.
- [ ] Add optional asset store/uploader dependencies to Image2 tool config.
- [ ] Decode `b64_json`, prepend image header only for frontend data URL, upload raw image bytes to object storage.
- [ ] Persist asset records with source `generated` and provider metadata.
- [ ] Keep the provider result compact to avoid large base64 in conversation history.
- [ ] Run `go test ./internal/aigc/tools ./internal/aigc/agent`.

### Task 6: Worker Skeleton And Job Status Events

**Files:**
- Create: `internal/aigc/generation/worker.go`
- Modify: `cmd/aigc-agent/main.go`
- Test: `internal/aigc/generation/worker_test.go`

- [ ] Write failing tests for running a queued job with a fake handler and marking it succeeded or failed.
- [ ] Implement worker loop with context cancellation, queue pop, status update, and handler dispatch by provider/type.
- [ ] Add job status payload shape compatible with `a2ui.EventJobStatus`.
- [ ] Wire worker startup in `cmd/aigc-agent/main.go`.
- [ ] Run `go test ./internal/aigc/generation ./cmd/aigc-agent`.

### Task 7: Media Generator Graph Uses Jobs And Bindings

**Files:**
- Modify: `internal/aigc/mediagraph/generator.go`
- Modify: `internal/aigc/tools/media_generator.go`
- Test: `internal/aigc/mediagraph/generator_test.go`
- Test: `internal/aigc/tools/media_generator_test.go`

- [ ] Write failing tests for binding existing asset ids and creating queued jobs for missing element, shot, and audio assets.
- [ ] Extend media graph input with asset binding and job dependencies.
- [ ] Patch storyboard status to generating/ready based on job creation result.
- [ ] Keep existing Compose checkpoint/interrupt behavior for reference image confirmation.
- [ ] Run `go test ./internal/aigc/mediagraph ./internal/aigc/tools`.

### Verification

- [ ] Run `/Users/figo/sdk/go1.26.3/bin/gofmt -w` on changed Go files.
- [ ] Run `/Users/figo/sdk/go1.26.3/bin/go test ./...`.
- [ ] Confirm no provider secrets are committed.
