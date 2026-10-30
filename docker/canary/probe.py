#!/usr/bin/env python3
"""probe.py — availability probe through the canary client's SOCKS5 port.

  docker/canary/probe.py <out.log> [--long]

Every 200 ms one short request (3 s cap) → "S <start> <end> <http_code|ERR>".
--long adds a rate-limited 100 MB download loop → "L <start> <end> rc=.. code size".
Stop with Ctrl-C / kill. analyze.py joins this with restart-cycle.sh's EV lines.
"""
import subprocess, sys, threading, time
SOCKS = "127.0.0.1:10805"
out = open(sys.argv[1], "a", buffering=1)

def short():
    while True:
        t = time.time()
        r = subprocess.run(["curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "3",
                            "--socks5-hostname", SOCKS, "http://www.gstatic.com/generate_204"],
                           capture_output=True, text=True)
        out.write(f"S {t:.3f} {time.time():.3f} {r.stdout or 'ERR'}\n")
        time.sleep(max(0, 0.2 - (time.time() - t)))

def long():
    while True:
        t = time.time()
        r = subprocess.run(["curl", "-s", "-o", "/dev/null", "-w", "%{http_code} %{size_download}",
                            "--limit-rate", "100k", "--max-time", "600", "--socks5-hostname", SOCKS,
                            "https://proof.ovh.net/files/100Mb.dat"], capture_output=True, text=True)
        out.write(f"L {t:.3f} {time.time():.3f} rc={r.returncode} {r.stdout}\n")
        time.sleep(0.5)

if "--long" in sys.argv:
    threading.Thread(target=long, daemon=True).start()
short()
