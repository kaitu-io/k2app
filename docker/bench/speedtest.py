#!/usr/bin/env python3
"""Per-second throughput benchmark through SOCKS5 proxies.

Downloads a test file from an HTTP server through each SOCKS5 proxy,
measuring per-second throughput. The file server runs on the server container,
so the only bottleneck is the tunnel protocol.

Usage:
  python3 speedtest.py --server bench.example.com:10010 --protocols "hysteria2:10003,k2_quic_pcc:10005"
  python3 speedtest.py --server bench.example.com:10010 --size 2000MB --protocols "k2_quic_pcc:10005" --rounds 3
"""
import argparse
import json
import os
import socket
import struct
import time
import sys

SIZE_FILES = {
    "1MB":    "1M.bin",
    "10MB":   "10M.bin",
    "100MB":  "100M.bin",
    "2000MB": "2000M.bin",
}

# Per-transfer timeout (seconds). Abort if a single download exceeds this.
TRANSFER_TIMEOUT = 600  # 10 minutes


def socks5_connect(proxy_host, proxy_port, target_host, target_port):
    """Connect to target through SOCKS5 proxy."""
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s.settimeout(30)
    s.connect((proxy_host, int(proxy_port)))
    s.sendall(b'\x05\x01\x00')
    resp = s.recv(2)
    if resp[1] != 0:
        raise ConnectionError(f"SOCKS5 auth failed: {resp}")
    addr = target_host.encode()
    req = b'\x05\x01\x00\x03' + bytes([len(addr)]) + addr + struct.pack('!H', target_port)
    s.sendall(req)
    resp = s.recv(256)
    if len(resp) < 2 or resp[1] != 0:
        raise ConnectionError(f"SOCKS5 connect failed: status={resp[1] if len(resp)>1 else 'short'}")
    return s


def download_with_speed_log(proxy_port, host, path, label, interval=1.0, target_port=80, timeout=TRANSFER_TIMEOUT):
    """Download file via SOCKS5 proxy, return per-interval speed data."""
    print(f"  [{label}] Connecting via SOCKS5 :{proxy_port} -> {host}:{target_port}{path} ...", flush=True)
    sock = socks5_connect('127.0.0.1', proxy_port, host, target_port)

    req = f"GET {path} HTTP/1.1\r\nHost: {host}:{target_port}\r\nConnection: close\r\n\r\n"
    sock.sendall(req.encode())

    # Read headers
    header_buf = b''
    while b'\r\n\r\n' not in header_buf:
        chunk = sock.recv(4096)
        if not chunk:
            raise ConnectionError("Connection closed during headers")
        header_buf += chunk

    header_end = header_buf.index(b'\r\n\r\n') + 4
    body_start = header_buf[header_end:]

    # Parse content-length if available
    content_length = 0
    for line in header_buf[:header_end].decode(errors='replace').split('\r\n'):
        if line.lower().startswith('content-length:'):
            content_length = int(line.split(':')[1].strip())

    # Read body, track speed per interval
    total = len(body_start)
    speeds = []
    interval_bytes = len(body_start)
    last_time = time.time()
    start_time = last_time
    timed_out = False

    sock.settimeout(60)
    try:
        while True:
            # Check transfer timeout
            if time.time() - start_time > timeout:
                timed_out = True
                print(f"  [{label}] Transfer timeout ({timeout}s), reporting partial results", flush=True)
                break
            try:
                chunk = sock.recv(65536)
            except socket.timeout:
                break
            if not chunk:
                break
            total += len(chunk)
            interval_bytes += len(chunk)

            now = time.time()
            elapsed = now - last_time
            if elapsed >= interval:
                speed_kbps = interval_bytes / elapsed / 1024
                t = now - start_time
                speeds.append({"t": round(t, 1), "kbps": round(speed_kbps, 1)})
                pct = f" ({total*100//content_length}%)" if content_length else ""
                print(f"  [{label}] t={t:.0f}s  {speed_kbps:.0f} KB/s{pct}", flush=True)
                interval_bytes = 0
                last_time = now
    except Exception as e:
        print(f"  [{label}] Error: {e}", flush=True)

    # Final partial interval
    now = time.time()
    elapsed = now - last_time
    if elapsed > 0.1 and interval_bytes > 0:
        speed_kbps = interval_bytes / elapsed / 1024
        t = now - start_time
        speeds.append({"t": round(t, 1), "kbps": round(speed_kbps, 1)})

    sock.close()
    total_time = time.time() - start_time
    avg_speed = total / total_time / 1024 if total_time > 0 else 0
    avg_mbps = avg_speed * 8 / 1024

    status = "TIMEOUT" if timed_out else "Done"
    print(f"  [{label}] {status}: {avg_speed:.0f} KB/s ({avg_mbps:.1f} Mbps) in {total_time:.1f}s, {total} bytes", flush=True)

    return {
        "label": label,
        "port": proxy_port,
        "total_bytes": total,
        "content_length": content_length,
        "total_time": round(total_time, 2),
        "avg_kbps": round(avg_speed, 1),
        "avg_mbps": round(avg_mbps, 2),
        "timed_out": timed_out,
        "speeds": speeds,
    }


def parse_protocols(spec):
    """Parse 'name:port,name:port' into list of (name, port)."""
    pairs = []
    for item in spec.split(","):
        item = item.strip()
        if ":" not in item:
            raise ValueError(f"Bad protocol spec '{item}', expected name:port")
        name, port = item.rsplit(":", 1)
        pairs.append((name, int(port)))
    return pairs


def main():
    parser = argparse.ArgumentParser(description="Protocol speed benchmark via SOCKS5 proxies")
    parser.add_argument("--protocols", required=True, help="Comma-separated name:port pairs")
    parser.add_argument("--server", help="File server host:port")
    parser.add_argument("--url", help="Full HTTP download URL (alternative to --server)")
    parser.add_argument("--size", choices=list(SIZE_FILES.keys()), default="2000MB", help="Test file size")
    parser.add_argument("--rounds", type=int, default=3, help="Number of rounds per protocol")
    parser.add_argument("--output", default="/tmp/bench/results.json", help="JSON output path")
    parser.add_argument("--interval", type=float, default=1.0, help="Speed sampling interval in seconds")
    parser.add_argument("--timeout", type=int, default=TRANSFER_TIMEOUT, help="Per-transfer timeout in seconds")
    parser.add_argument("--scenario", default="", help="Scenario name (for metadata)")
    args = parser.parse_args()

    protocols = parse_protocols(args.protocols)

    if args.url:
        from urllib.parse import urlparse
        p = urlparse(args.url)
        host = p.hostname
        port_num = p.port or 80
        path = p.path
    elif args.server:
        parts = args.server.split(":")
        host = parts[0]
        port_num = int(parts[1]) if len(parts) > 1 else 80
        path = f"/{SIZE_FILES[args.size]}"
    else:
        parser.error("Either --server or --url is required")

    print(f"Benchmark: {host}:{port_num}{path}")
    print(f"Protocols: {', '.join(f'{n} (:{p})' for n, p in protocols)}")
    print(f"Rounds: {args.rounds}, Size: {args.size}, Timeout: {args.timeout}s")
    if args.scenario:
        print(f"Scenario: {args.scenario}")
    print()

    all_results = []

    for rd in range(1, args.rounds + 1):
        print(f"=== Round {rd}/{args.rounds} ===")
        for name, port in protocols:
            label = f"{name}" if args.rounds == 1 else f"{name}_r{rd}"
            try:
                result = download_with_speed_log(
                    port, host, path, label,
                    interval=args.interval, target_port=port_num, timeout=args.timeout,
                )
                result["round"] = rd
                result["protocol"] = name
                result["scenario"] = args.scenario
                result["size"] = args.size
                all_results.append(result)
            except Exception as e:
                print(f"  [{label}] FAILED: {e}", flush=True)
                all_results.append({
                    "label": label, "protocol": name, "port": port,
                    "round": rd, "error": str(e), "scenario": args.scenario, "size": args.size,
                    "total_bytes": 0, "content_length": 0, "total_time": 0,
                    "avg_kbps": 0, "avg_mbps": 0, "timed_out": False, "speeds": [],
                })
            time.sleep(2)
        print()

    # Save results
    os.makedirs(os.path.dirname(args.output) or ".", exist_ok=True)
    with open(args.output, "w") as f:
        json.dump(all_results, f, indent=2)

    print(f"Results saved to {args.output}")

    # Summary table
    print()
    print(f"{'Protocol':<20} {'Avg Speed':>12} {'Mbps':>8} {'Time':>8} {'Bytes':>12} {'Status':>8}")
    print("=" * 72)
    for r in all_results:
        if r.get("error"):
            print(f"{r['label']:<20} {'':>12} {'':>8} {'':>8} {'':>12} {'FAILED':>8}")
        else:
            status = "TIMEOUT" if r.get("timed_out") else "OK"
            print(f"{r['label']:<20} {r['avg_kbps']:>9.0f} KB/s {r['avg_mbps']:>7.2f} {r['total_time']:>7.1f}s {r['total_bytes']:>12,} {status:>8}")
    print("=" * 72)


if __name__ == "__main__":
    main()
