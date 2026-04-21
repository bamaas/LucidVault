# ADR-001: Local Docker over Kubernetes

## Status
Accepted

## Context
Need a deployment strategy for a personal knowledge base service. Options: local Docker, Kubernetes, cloud VM, or bare metal.

## Decision
Run as a local Docker container on the laptop.

## Consequences
- Simple: no cluster management, no cloud costs, no sync complexity
- Volume mount for Obsidian vault — files are immediately available
- Limited to laptop uptime (acceptable for personal use)
- No HA, no scaling — not needed for single-user
