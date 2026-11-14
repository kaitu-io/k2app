# Box

*Final as of 2026-11-10. Decisions: see docs/decisions.md. Technology selection: see docs/stack.md. The hardware information comes from one round of online research (research/wyse-3040-linux-2026-11-10.md). Everything marked "single source" or "unverified" must be confirmed on a real unit.*

## Hardware: Dell Wyse 3040
| Item | Status |
|---|---|
| CPU | Atom x5-Z8350, 4 cores. Hardware crypto instructions are supported according to spec sites; confirm on a real unit |
| Memory / storage | 2 GB / 8 GB eMMC, both soldered |
| Network | 1 gigabit Ethernet port (Realtek; needs non-free firmware under Debian); no Wi-Fi |
| Power draw | 4 W idle, 5 W running |
| Power supply | There are 5V and 12V versions; match the label on the unit. Someone has burned a unit by plugging in the wrong one |
| Purchase cost | 100–140 RMB per unit, with power supply. First batch: 10 units |
| BIOS | Default password `Fireport` |

## How it connects in the home: Gateway Mode
- One Ethernet cable to the existing home router. The home router is not replaced. The Box does not provide Wi-Fi.
- **Sharing**: the Box connects outbound by itself. The Host does not need to configure anything. Hole punching is tried first; if it fails, traffic goes through the Platform's Relay. A Host who opens a port on the home router can raise the Direct Rate and have less Relay Fee deducted.
- **Own use**: for any device in the home that should go out through someone else's home IP, point its gateway and DNS at the Box. Other devices are not affected. Phones and computers can also use the app directly.

## System
- Minimal Debian, pre-installed as a whole-disk Image.
- The root partition is read-only. There are two Slots plus one data partition. An Update is written to the other Slot; if boot fails, Rollback is automatic.
- Kernel WireGuard for the tunnel, and one Go daemon (the Box Agent) that configures it and does everything Overleap-specific. See docs/stack.md.
- Hardware watchdog. The BIOS is set to power on automatically after power loss.
- **First binding**: a QR code is on the bottom of the Box. The Host scans it with the app to Claim the Box. No monitor or keyboard is connected at any point.

## Rules on the Box
- By default, P2P downloads and high-risk ports are blocked.
- Traffic is routed per whole site (together with its CDN and API domains): the page and its resources must leave from the same IP.
- The Host's own traffic always goes first. The shared speed never exceeds 70% of Uplink.
- Traffic is metered against the Data Allowance of each Seat. Over the Data Allowance, the Seat gets a Throttle to 256 kbps.
- The Host can perform a Cut-off on a Session.

## Operations
- **Update**: Images are signed. Rollout is staged: Platform-run Seed Boxes → 10% → all. If the failure rate or the offline rate of any stage crosses its threshold, the Rollout stops automatically.
- **Logs**: system logs are written to memory only, with a size limit, and never to disk. This saves eMMC life.
- **Telemetry reported to the Platform**: online status, version, CPU and memory, throughput, Uplink speed test, Direct Rate, reboot and crash reasons, and whether the IP has been flagged by threat-intelligence databases.
- **Operational logs never contain the domains and IPs accessed.** That kind of record travels only through the Connection Record path and is Sealed at the moment of writing (see kyc-committee.md). A test must guard this.
- **Troubleshooting**: detailed logs are uploaded only when the Host taps "Send Diagnostic Bundle" in the app. A Support Session must be switched on by the Host, is time-limited and closes automatically.
- **Admin dashboard**: uptime of each Box, version distribution, Direct Rate per country, Update results per Rollout stage.

## Speed
- In the research, the same machine did 516 Mbps of plain forwarding and only about 135 Mbps running OpenVPN. The slow part is OpenVPN (single-threaded; every packet is copied back and forth between kernel and program). We do not use it.
- Most homes hit the limit of their uplink first, not the Box. Only homes with symmetric fibre will hit the limit of the Box.
- Target: our program does at least 200 Mbps on this machine. If it is lower, optimise in the order below, and compare every step with the same load test.
  1. Confirm the hardware crypto instructions are available and use AES-GCM.
  2. Use all four cores.
  3. Turn on batched send and receive (GSO/GRO) for UDP-based transport.
  4. Use in-kernel forwarding (splice) for TCP-based transport.
  5. NIC multi-queue, and interrupts spread across the four cores.

## Hardware test checklist (take one real unit, install Debian 12 or 13)

### Installation
- [ ] Read the label on the unit and confirm whether the power supply is 5V or 12V.
- [ ] BIOS: set `AC Recovery` to Power On. Note whether the mainboard battery still has charge (cut power, power on again, check whether the settings are still there).
- [ ] During installation, install GRUB to the EFI removable media path. Otherwise the unit does not boot after installation.
- [ ] Install `firmware-realtek`.

### Item 1: CPU and speed with no monitor attached
- [ ] `grep -m1 -o aes /proc/cpuinfo` prints output.
- [ ] Run `openssl speed -evp aes-128-gcm` and `-evp chacha20-poly1305`; note the numbers.
- [ ] Unplug the monitor and reboot. `cat /sys/devices/system/cpu/cpu*/cpufreq/scaling_cur_freq`: is it stuck at about 480 MHz? If it is stuck, plug in a DP dummy plug and test again.
- [ ] Measure plain forwarding with `iperf3`. Then run our Box Agent and measure Mbps in both cases, Direct and Relayed. Watch the usage of the four cores in `top` at the same time.

### Item 2: reboot and power loss
- [ ] Write a file in `/etc/modprobe.d/` that disables `dw_dmac` and `dw_dmac_core`, then run `update-initramfs -u`.
- [ ] Reboot automatically 100 times in a row. Not one may fail.
- [ ] Pull the power and plug it back 10 times. Each time the unit must power on by itself and connect to the Platform.

### Item 3: 24/7 stability
- [ ] Run under load for 72 hours without a crash or a hang. Note the kernel version; there are reports that the 6.1 kernel crashes after half an hour to two hours.
- [ ] If it hangs, add the kernel parameter `intel_idle.max_cstate=1` and test again.
- [ ] The eMMC can be written normally. Run `mmc extcsd read /dev/mmcblk0` and note the life-time estimate.
- [ ] Apply an Update back and forth between the two Slots 10 times. Deliberately write a bad Image and confirm the automatic Rollback.

### Numbers to note
Memory use (idle, and with 5 Load Units), temperature, measured power draw.
