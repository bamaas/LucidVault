# 017 — CouchDB LiveSync for Obsidian Multi-Device Access

**Status:** Accepted

## Context

LucidVault runs as a local Docker container with the Obsidian vault mounted as a volume, making the vault only accessible on the machine running the container. Users want multi-device access to the vault via Obsidian (phone, tablet, second laptop). Obsidian LiveSync is a community plugin that synchronizes vaults via a self-hosted CouchDB instance using CouchDB's replication protocol. livesync-bridge is an official companion tool from the same author that acts as a bidirectional sync bridge between CouchDB (LiveSync format) and a local filesystem, handling all document format concerns internally.

## Decision

Deploy livesync-bridge as a sidecar alongside LucidVault, sharing a persistent volume. The bridge handles all CouchDB/LiveSync synchronization. LucidVault requires zero code changes. Provide Docker Compose for local/single-server deployments and Kubernetes manifests for production.

## Consequences

- Multi-device vault access via any Obsidian client with LiveSync
- Zero code changes to LucidVault — sync layer is entirely infrastructure
- Deployment complexity increases (CouchDB, bridge sidecar, TLS, auth) but no app complexity increases
- Supersedes ADR-001 for sync-enabled deployments; local Docker remains valid
- Introduces dependency on livesync-bridge (Deno-based, third-party)
- Full design doc: `docs/design/017-couchdb-livesync.md`
