# 017 — CouchDB LiveSync for Obsidian Multi-Device Access

**Status:** Accepted

## Context

LucidVault currently runs as a local Docker container (ADR-001) with the Obsidian vault mounted as a volume. This means the vault is only accessible on the machine running the container. Users want to access and edit the vault from any device (phone, tablet, second laptop) using Obsidian.

[Obsidian LiveSync](https://github.com/vrtmrz/obsidian-livesync) is a community plugin that synchronizes Obsidian vaults via a self-hosted CouchDB instance. It uses CouchDB's replication protocol to propagate changes between clients in near-real-time.

[livesync-bridge](https://github.com/vrtmrz/livesync-bridge) is an official companion tool from the same author. It acts as a bidirectional sync bridge between CouchDB (LiveSync format) and a local filesystem. It handles all LiveSync document format concerns internally — chunking, path encoding, metadata, revisions — and exposes a plain directory of files.

The goal is to make the vault accessible from any device via Obsidian LiveSync, while LucidVault continues to read and write plain files on disk without any awareness of CouchDB or the LiveSync protocol.

This ADR supersedes ADR-001's decision to run locally. The deployment target moves to Kubernetes to support always-on availability and co-located CouchDB.

## Decision

Deploy livesync-bridge as a sidecar alongside LucidVault in Kubernetes, sharing a persistent volume. The bridge handles all CouchDB/LiveSync synchronization. LucidVault requires zero code changes.

## Architecture

```text
Obsidian clients (any device)
       |
       | LiveSync plugin
       v
  CouchDB (K8s StatefulSet)
       |
       | LiveSync protocol (internal format, chunking, revisions)
       v
  livesync-bridge (K8s sidecar container)
       |
       | Plain filesystem reads/writes (Deno.watch / chokidar)
       v
  Shared PersistentVolume (/vault)
       ^
       | Plain filesystem reads/writes (as today)
       |
  LucidVault (K8s main container)
```

### Key Insight

livesync-bridge fully encapsulates the LiveSync document format. It watches the filesystem for changes and pushes them to CouchDB, and it watches CouchDB's `_changes` feed and writes updates to the filesystem. LucidVault never interacts with CouchDB — it only sees files on disk, exactly as it does today.

### Data Flow: LucidVault -> Obsidian

```text
Pipeline produces file (wiki/, raw/, index.md)
  -> vault.Write*() writes to shared PV (unchanged)
  -> livesync-bridge detects file change via filesystem watcher
  -> Bridge converts to LiveSync document format and pushes to CouchDB
  -> LiveSync plugin on Obsidian clients pulls the change
```

### Data Flow: Obsidian -> LucidVault

```text
User creates inbox/my-link.md in Obsidian
  -> LiveSync plugin pushes to CouchDB
  -> livesync-bridge receives _changes event
  -> Bridge writes plain file to shared PV at inbox/my-link.md
  -> LucidVault's existing poll loop picks it up
  -> Scrape -> enrich -> vault write -> bridge syncs result back
  -> Obsidian clients see enriched content
```

### Data Flow: MCP / Raindrop -> Obsidian

```text
MCP add_bookmark / Raindrop sync writes to inbox/ on shared PV
  -> LucidVault poll loop picks it up (unchanged)
  -> Pipeline processes it (unchanged)
  -> Bridge detects new vault files, syncs to CouchDB
  -> Obsidian clients see it
```

No changes needed to MCP server, Raindrop sync, or any LucidVault code.

## livesync-bridge Capabilities

Based on the [livesync-bridge repository](https://github.com/vrtmrz/livesync-bridge):

- **Peer types**: CouchDB peers (with optional E2EE) and storage peers (local filesystem)
- **Bidirectional sync**: Watches both CouchDB `_changes` feed and local filesystem
- **File watching**: Uses `Deno.watch` by default, `chokidar` for Linux compatibility
- **Offline reconciliation**: `scanOfflineChanges` detects changes made while the bridge was down
- **Custom processors**: Can execute scripts on file modifications/deletions (could be used for pipeline triggering, but not needed — LucidVault's poll loop already handles this)
- **Selective sync**: Supports `baseDir` for syncing specific subdirectories
- **Chunk configuration**: Handles LiveSync's content-defined chunking internally
- **Reset mode**: `--reset` flag rescans all storage/databases (useful for initial seeding)
- **Deployment**: Runs via Deno directly or containerized via Docker

## Edge Cases and Risks

### 1. File Write Race Conditions

**Problem**: LucidVault writes a file, the bridge detects it and syncs to CouchDB, then the poll loop processes the same file and writes output. Meanwhile, the bridge is still syncing the first version. Could the bridge overwrite the second version with the first?

**Mitigation**: The bridge uses filesystem watching with mtime tracking. As long as writes are atomic (write to temp file, then rename), the bridge will see the final state. LucidVault should ensure atomic file writes — this is already good practice. The bridge's own `scanOfflineChanges` provides a safety net.

**Residual risk**: If two writes happen in rapid succession, the bridge may sync an intermediate state. This is cosmetic — the final state will sync shortly after.

### 2. Echo Loops via the Bridge

**Problem**: LucidVault writes a file -> bridge syncs to CouchDB -> bridge receives its own change back from CouchDB -> bridge writes file to disk -> LucidVault's poll loop re-processes it.

**Mitigation**: The bridge is designed to handle this — it tracks which changes it originated and skips them on the return path. This is a core feature of any bidirectional sync tool. However, LucidVault's poll loop also has natural protection: the `store.Store` deduplicates by normalized URL, so re-processing an already-processed bookmark is a no-op. Notes are tracked by content hash, so unchanged content is skipped.

**Residual risk**: If the bridge does not perfectly suppress echo, LucidVault's idempotency (SQLite dedup) serves as a second line of defense. Worst case: a file is re-enriched unnecessarily, but the output is identical.

### 3. Conflict Resolution

**Problem**: Two Obsidian clients modify the same file simultaneously. CouchDB stores both revisions as a conflict. The bridge must decide which version to write to disk.

**Mitigation**: CouchDB has deterministic conflict resolution (picks a winner based on revision tree depth, then rev hash). The bridge writes the winning revision to disk. The losing revision is preserved in CouchDB and accessible in Obsidian via LiveSync's conflict resolution UI.

**LucidVault-specific concern**: If a user edits a `wiki/` file in Obsidian, their edit will be overwritten the next time LucidVault's pipeline regenerates it (e.g., if the source bookmark is reprocessed). This is by design — `wiki/` and `raw/` are generated output.

**Recommendation**: Document that `wiki/`, `raw/`, and `index.md` are LucidVault-managed and should not be edited in Obsidian. Optionally, configure LiveSync's selective sync to make these folders read-only on Obsidian clients.

### 4. Deleted Files

**Problem**: User deletes a file in Obsidian. LiveSync propagates the deletion to CouchDB. The bridge deletes the file from disk. What if the user deletes a LucidVault-generated file?

**Mitigation**: Define clear ownership semantics:

- `inbox/` — user-owned, deletions propagate (LucidVault deletes inbox files after processing anyway)
- `notes/` — user-owned, deletions propagate both ways
- `wiki/` — LucidVault-generated, deletion from Obsidian is fine (will be regenerated if the source is reprocessed; otherwise, the deletion stands)
- `raw/` — LucidVault-generated, same as wiki/
- `index.md` — LucidVault-managed, deletion from Obsidian triggers rebuild on next pipeline run (UpdateIndex is idempotent)
- `soul.md`, `CLAUDE.md` — user-owned, deletions propagate

**Sync scope decisions**:

- `raw/` — **Synced** (see note below). Large files, limited value on mobile, but livesync-bridge has no per-directory exclusion mechanism. Syncing everything is simplest.
- `wiki/` — **Synced, documented as read-only**. Edits in Obsidian will be overwritten on next pipeline run. No technical enforcement — document the convention.
- `index.md` — **Synced, documented as read-only**. Vault navigation hub; regenerated by pipeline.
- `notes/` — **Full bidirectional sync**. User-owned content, primary Obsidian editing target.
- `inbox/` — **Full bidirectional sync**. Core use case: drop URL from phone.
- `soul.md`, `CLAUDE.md` — **Synced**. User-owned, rarely changed.

Bridge configuration: sync entire vault. livesync-bridge does not support per-directory exclusion or ignore patterns. The only scoping mechanism is `baseDir` on CouchDB peers, which selects a subdirectory to sync — not an exclusion. Using multiple CouchDB peers with separate `baseDir` values to exclude `raw/` was considered but rejected as fragile and complex. If `raw/` files cause storage pressure on mobile, users can configure Obsidian LiveSync's selective sync on the client side.

### 5. Initial Vault Seeding

**Problem**: When setting up for the first time, the existing vault (potentially hundreds of files) needs to be loaded into CouchDB so Obsidian clients can access it.

**Mitigation**: livesync-bridge supports a `--reset` mode that rescans all storage and databases. On first deployment:

1. Deploy LucidVault with existing vault data on the PV
2. Deploy the bridge with `--reset` flag
3. Bridge scans all files and bulk-syncs to CouchDB
4. Obsidian clients connect and pull the full vault

No custom bulk-load code needed in LucidVault.

### 6. CouchDB Unavailability

**Problem**: If CouchDB goes down, the bridge can't sync. LucidVault continues writing to disk. When CouchDB recovers, the bridge must catch up.

**Mitigation**: LucidVault is unaffected — it only reads/writes disk. The bridge handles reconnection and uses `scanOfflineChanges` to detect files that changed while CouchDB was down. CouchDB's `_changes` feed has sequence IDs, so the bridge resumes from where it left off.

### 7. Shared PV Write Contention

**Problem**: Both LucidVault and the bridge write to the same PersistentVolume. Concurrent writes to the same file could corrupt it.

**Mitigation**: In practice, they write to different files at different times:

- LucidVault writes pipeline output (wiki/, raw/, index.md) and deletes inbox files
- The bridge writes files from Obsidian (inbox/, notes/)

The only overlap is if an Obsidian user edits a file that LucidVault is simultaneously regenerating. This is the conflict scenario from edge case #3 — the last writer wins on disk, and the bridge syncs the final state.

**Kubernetes concern**: The PV must support `ReadWriteMany` (RWX) access mode if LucidVault and the bridge run as separate pods. If they run as containers in the same pod (sidecar pattern), `ReadWriteOnce` (RWO) suffices.

### 8. Bridge Reliability and Restarts

**Problem**: If the bridge crashes or restarts, changes made during downtime must not be lost.

**Mitigation**: The bridge's `scanOfflineChanges` feature detects filesystem changes made while it was not running. On restart, it reconciles disk state with CouchDB state. CouchDB's `_changes` feed is persistent — the bridge resumes from its last known sequence ID.

### 9. CouchDB Compaction

**Problem**: CouchDB accumulates document revisions. Without compaction, the database grows unboundedly.

**Mitigation**: Configure CouchDB auto-compaction. Follow LiveSync's recommended compaction settings. Note: aggressive compaction while clients are offline for extended periods can break conflict resolution. Use conservative settings (e.g., compact when revision count exceeds 100 or database size exceeds a threshold).

### 10. CouchDB Authentication and Security

**Problem**: CouchDB must be network-accessible for Obsidian clients. An unauthenticated instance is a data leak.

**Mitigation**:

- CouchDB admin credentials via Kubernetes secrets
- Per-database user credentials for LiveSync clients
- TLS termination via Kubernetes Ingress (or CouchDB native TLS)
- LiveSync plugin supports authenticated HTTPS connections
- Network policy: only allow CouchDB traffic from the bridge pod and external Ingress

### 11. LiveSync Plugin Version Compatibility

**Problem**: LiveSync's internal document format may change between plugin versions, breaking the bridge.

**Mitigation**: The bridge is maintained by the same author as LiveSync, so format compatibility is expected to be maintained. However:

- Pin both the LiveSync plugin version and the bridge container image version
- Test upgrades in a staging environment before rolling out
- Monitor the livesync-bridge repository for breaking changes

This is significantly lower risk than maintaining our own format implementation (the approach rejected in this ADR).

### 12. Bridge Performance with Large Vaults

**Problem**: File watchers can become expensive with thousands of files. The bridge may lag or miss events.

**Mitigation**: The bridge supports both `Deno.watch` and `chokidar`, both of which use OS-level file notification APIs (inotify on Linux, FSEvents on macOS). These scale well to tens of thousands of files. The `scanOfflineChanges` feature provides a periodic full-scan fallback.

**Decision**: Sync entire vault including `raw/` (see sync scope decisions in edge case #4). livesync-bridge has no per-directory exclusion. OS-level file watchers handle the volume fine; mobile storage is the only concern, addressable via client-side LiveSync selective sync.

### 13. E2EE Considerations

**Problem**: LiveSync supports end-to-end encryption. If enabled on Obsidian clients, the bridge must use the same passphrase to decrypt/encrypt documents.

**Mitigation**: Configure the bridge's CouchDB peer with the same E2EE passphrase used in Obsidian's LiveSync plugin settings. Store the passphrase in a Kubernetes secret. If E2EE is not needed (CouchDB is on a private network with TLS), skip it to reduce complexity.

### 14. SQLite Database Location

**Problem**: LucidVault's SQLite database (dedup state) lives on disk. If the PV is shared, should the database be on the shared volume?

**Mitigation**: The SQLite database should NOT be on the shared PV. It is internal to LucidVault and should not be synced to Obsidian or touched by the bridge.

**Decision**: Use a separate small PersistentVolumeClaim for the SQLite database. This ensures dedup state survives pod restarts without requiring reconciliation logic.

## Deployment Topology

### Kubernetes Resources

```yaml
# Pod: lucidvault (with sidecar)
containers:
  - name: lucidvault         # Main container
    image: lucidvault:latest
    volumeMounts:
      - name: vault
        mountPath: /vault
      - name: db
        mountPath: /data      # SQLite database

  - name: livesync-bridge     # Sidecar container
    image: ghcr.io/vrtmrz/livesync-bridge:latest  # or pinned version
    volumeMounts:
      - name: vault
        mountPath: /vault
      - name: bridge-config
        mountPath: /config

volumes:
  - name: vault               # Shared vault PV (RWO, sidecar pattern)
    persistentVolumeClaim:
      claimName: vault-pvc
  - name: db                  # SQLite PV (separate, small)
    persistentVolumeClaim:
      claimName: db-pvc
  - name: bridge-config       # Bridge configuration
    configMap:
      name: livesync-bridge-config

---
# StatefulSet: couchdb
containers:
  - name: couchdb
    image: couchdb:3
    volumeMounts:
      - name: couchdb-data
        mountPath: /opt/couchdb/data

---
# Ingress: couchdb (for external Obsidian clients)
# TLS termination, authentication required
```

### Bridge Configuration (conceptual)

```json
{
  "peers": [
    {
      "type": "couchdb",
      "name": "livesync-db",
      "url": "http://couchdb:5984",
      "database": "obsidian-vault",
      "username": "...",
      "password": "..."
    },
    {
      "type": "storage",
      "name": "vault-disk",
      "path": "/vault"
    }
  ]
}
```

Exact configuration format to be confirmed from bridge documentation.

## New Environment Variables (LucidVault)

None. LucidVault requires zero configuration changes. All sync configuration is in the bridge's config.

The only deployment-level change is that `VAULT_PATH` points to the shared PV mount (e.g., `/vault`) instead of a local Docker volume mount.

## New Package (LucidVault)

None. No new Go code is needed.

## Impact on Existing Code

- **LucidVault codebase**: Zero changes. The sync layer is entirely external.
- **Dockerfile**: No changes. LucidVault's image is unchanged.
- **Deployment manifests**: New Kubernetes manifests for the pod (with sidecar), CouchDB StatefulSet, PVCs, Ingress, ConfigMaps, and Secrets. These live in a new `deploy/k8s/` directory.
- **ADR-001**: Superseded for the sync-enabled deployment. Local Docker remains valid for users who don't need multi-device access.

## Deployment Packaging

Provide two deployment options to cover different user environments:

1. **Docker Compose** — For local or single-server deployments. A single `docker-compose.yml` that brings up LucidVault, livesync-bridge, and CouchDB with shared volumes and preconfigured networking. Lowest friction for users who don't run Kubernetes. This also replaces the current "run locally with Docker" workflow from ADR-001, adding sync as an opt-in capability.

2. **Helm Chart** — For Kubernetes deployments. Packages the full topology (LucidVault + bridge sidecar pod, CouchDB StatefulSet, PVCs, Ingress, Secrets). Configurable via `values.yaml` for CouchDB credentials, TLS, vault size, sync exclusions, and optional Raindrop/MCP settings.

Both options should include sensible defaults that work out of the box with minimal configuration (CouchDB credentials and Obsidian LiveSync passphrase at minimum).

Implementation details for both are tracked separately from this ADR.

## Implementation Phases

### Phase 1: Proof of Concept

- Deploy CouchDB locally (Docker)
- Install LiveSync plugin in Obsidian, configure to use local CouchDB
- Deploy livesync-bridge locally, point at same CouchDB and a test directory
- Verify: create file in test directory -> appears in Obsidian
- Verify: create file in Obsidian -> appears in test directory
- Verify: LucidVault pipeline output written to test directory -> appears in Obsidian

### Phase 2: Kubernetes Deployment

- Write K8s manifests (LucidVault + bridge sidecar, CouchDB StatefulSet, PVCs)
- Configure CouchDB authentication and TLS via Ingress
- Deploy to cluster and test with Obsidian clients

### Phase 3: Production Hardening

- Pin bridge and CouchDB image versions
- Configure CouchDB compaction
- Set up monitoring (CouchDB metrics, bridge health)
- Document selective sync / folder exclusion for raw/
- Test failure scenarios (CouchDB down, bridge restart, pod restart)
- Seed existing vault using bridge's `--reset` mode

## Alternatives Considered

### 1. Custom CouchDB sync layer in LucidVault (original draft)

Implement LiveSync's document format directly in Go — a CouchDB writer, `_changes` listener, echo loop prevention, reconciliation, and chunking. Rejected because:

- High implementation effort (reverse-engineering an undocumented format)
- Ongoing maintenance burden (format changes break the implementation)
- livesync-bridge already solves this problem and is maintained by the LiveSync author

### 2. Obsidian Sync (official)

Paid service, closed protocol. Cannot integrate server-side — LucidVault can't participate in the sync.

### 3. Syncthing / Resilio

File-level sync between devices. Doesn't integrate with Obsidian's live reload. Requires Syncthing on every device, which isn't possible on iOS.

### 4. Git-based sync

Commit and push on change, pull on other devices. Doesn't work on mobile (no Git client in Obsidian mobile). Merge conflicts in Markdown are painful.

### 5. S3/MinIO + sync plugin

Some Obsidian plugins support S3-compatible storage. Less mature than LiveSync, no real-time sync.

### 6. Write directly to CouchDB, skip disk entirely

Eliminate the local filesystem. Rejected: the entire pipeline assumes filesystem I/O, and disk-first is simpler to debug and recover from.

## Consequences

- LucidVault gains multi-device vault access via any Obsidian client with LiveSync
- **Zero code changes to LucidVault** — the sync layer is entirely infrastructure
- Deployment complexity increases (Kubernetes, CouchDB, bridge sidecar, TLS, auth) but no application complexity increases
- LiveSync format coupling is the bridge's problem, not ours — maintained by the same author as the plugin
- The inbox-as-single-entry-point pattern (ADR-014) proves its value: Obsidian-created inbox files are indistinguishable from any other source
- ADR-001 (local Docker) is superseded for the sync-enabled deployment; local Docker remains valid for users who don't need multi-device access
- Introduces a dependency on livesync-bridge (Deno-based, third-party) — if the project is abandoned, we'd need to fall back to the custom sync layer approach (alternative #1)
