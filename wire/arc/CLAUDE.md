# K2ARC Vivace Congestion Control (wire/arc/)

866 lines. Custom congestion control for QUIC, optimized for GFW environments.

Implements `quic-go`'s `CongestionControl` interface via `github.com/apernet/quic-go`.

## Testing

```bash
go test ./wire/arc/...    # 26 tests
```
