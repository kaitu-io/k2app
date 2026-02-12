# Knowledge Base

Distilled knowledge from validated feature implementations.

## Purpose

This directory contains reusable patterns, decisions, and lessons learned from implemented features. Each entry links to the tests that validate it (bidirectional traceability).

## Files

### [architecture-decisions.md](architecture-decisions.md)
Key architectural choices made during implementation, with rationale and validation.

Entries:
- L4 Proxy Model vs L3 VPN
- wire.Dialer/Listener L4 Interfaces
- Provider Callback Model
- Binary Split: k2 (Client) + k2s (Server)
- TransportManager: QUIC Primary, TCP-WS Fallback
- k2rule.Init(&Config{}) Unified API
- ECH Config Forging from DNS Template
- ECH Config Rotation with Shared HPKE Key
- Chinese CA Blocklist via VerifyPeerCertificate
- Server Zero-Config with Auto-Provisioning Flow
- SNI Cleanup: Outer SNI = ECH Public Name

### [testing-strategies.md](testing-strategies.md)
Proven testing patterns that worked across features.

Patterns:
- Interface Mocking for Wire Transport
- In-Process Server for E2E Tests
- Compile-Time Interface Checks
- Platform Build Tag Testing
- Bidirectional Pipe Testing
- Test Fixtures for Common Configs
- Table-Driven Routing Tests
- Binary Wire Format Testing with Builder Functions
- DNS Query Testing with Conditional Skips
- Fallback Chain Testing
- Auto-Migration Testing

### [framework-gotchas.md](framework-gotchas.md)
Dependency-specific issues and their solutions.

Gotchas:
- sing-tun gVisor Stack Connection Ownership
- smux DefaultConfig vs Custom Config
- QUIC Datagram vs Stream for UDP
- net.Pipe Synchronous Blocking
- bufio.Reader Required for Stream Protocol
- k2rule GeoIP Path Requirement
- sing-tun AutoRoute vs Manual Route
- miekg/dns: Domain Name Must End with Dot
- ECH Binary Format: Big-Endian Multi-Byte Fields
- Go crypto/tls ECH: Must Use X25519, Not P-256

### [task-splitting.md](task-splitting.md)
Patterns for breaking features into mergeable incremental tasks.

Patterns:
- Foundation Tasks First
- Parallel Independence
- Interface-First Implementation
- Integration Last
- Vertical Slice per Transport
- Dependency Graph Visualization
- Test-First for Interfaces, Test-During for Logic
- Incremental Merges, Not Long-Lived Branches
- Shared Test Fixtures

## Usage

When starting a new feature:
1. Review relevant knowledge entries (similar architecture, same dependencies)
2. Apply validated patterns to new context
3. Update entries if patterns evolve or new insights emerge

When completing a feature:
1. Distill new knowledge (what worked, what failed, what was retried)
2. Add entries to appropriate files with source feature and date
3. Link to validating tests (bidirectional)

## Format

Each entry follows this structure:

```markdown
## Pattern/Decision Name (feature-name, YYYY-MM-DD)

**Context**: Why this was needed

**Decision/Pattern**: What was done

**Validation**: Tests/files that prove this works

**Benefits/Trade-offs**: Pros and cons
```

## Maintenance

- When entry proven wrong: update or remove it
- When entry superseded: mark as deprecated, link to replacement
- When entry duplicates: merge into one entry with multiple sources
- Keep entries evergreen: remove outdated implementation details
