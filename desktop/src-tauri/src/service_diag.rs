//! macOS k2 daemon diagnostics: launchd's view of the job, a durable history of
//! service lifecycle events, and the pkg installer trail.
//!
//! Why (#3911): when launchd refuses the daemon (disabled override / Login Items
//! → Allow in the Background off) `k2 run` never executes, so k2.log is silent
//! and desktop.log only says ServiceNotRunning. desktop.log is also a single
//! 20 MB file (plugin-log KeepOne), so the moment the service stopped coming
//! back — an update a month earlier — was long rotated away. These pieces keep
//! that evidence, and every one of them is readable without root.

use std::path::Path;

/// launchd label of the k2 daemon (k2 cmd/k2/service_darwin.go; same for both brands).
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
const DAEMON_LABEL: &str = "kaitu";

/// File name of the durable event history, kept next to desktop.log.
pub const HISTORY_FILE: &str = "service-history.log";
/// History is trimmed to its newest half once it passes this size.
const HISTORY_CAP: usize = 256 * 1024;

#[cfg(brand_overleap)]
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
const BUNDLE_ID: &str = "io.overleap.desktop";
#[cfg(not(brand_overleap))]
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
const BUNDLE_ID: &str = "io.kaitu.desktop";

/// installd names the package by file (`Installed "Kaitu_0.4.13_universal"`),
/// not bundle id — this is what tells which build a pkg install laid down.
#[cfg(brand_overleap)]
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
const PKG_INSTALLED_PREFIX: &str = "Installed \"Overleap_";
#[cfg(not(brand_overleap))]
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
const PKG_INSTALLED_PREFIX: &str = "Installed \"Kaitu_";

/// One-line launchd view of the daemon, e.g.
/// `state=running pid=412 runs=1 last_exit=(never exited) override=enabled plist=true`.
/// `state=not-loaded` = launchd does not know the job at all.
#[cfg(target_os = "macos")]
pub fn launchd_summary() -> String {
    use std::process::Command;
    let stdout = |args: &[&str]| {
        Command::new("launchctl")
            .args(args)
            .output()
            .map(|o| String::from_utf8_lossy(&o.stdout).to_string())
            .unwrap_or_default()
    };
    let print = stdout(&["print", &format!("system/{}", DAEMON_LABEL)]);
    let disabled = stdout(&["print-disabled", "system"]);
    let plist = Path::new(&format!("/Library/LaunchDaemons/{}.plist", DAEMON_LABEL)).exists();
    summarize(&print, &disabled, plist)
}

#[cfg(not(target_os = "macos"))]
pub fn launchd_summary() -> String {
    String::new()
}

/// Pure core of [`launchd_summary`]: top-level fields of `launchctl print`
/// (nested dictionaries skipped) plus the `print-disabled` override.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
fn summarize(print: &str, print_disabled: &str, plist_exists: bool) -> String {
    let field = |key: &str| {
        print
            .lines()
            .filter(|l| l.starts_with('\t') && !l.starts_with("\t\t"))
            .filter_map(|l| l.trim().split_once(" = "))
            .find(|(k, _)| *k == key)
            .map(|(_, v)| v.to_string())
    };
    let state = field("state").unwrap_or_else(|| "not-loaded".to_string());
    let key = format!("\"{}\" => ", DAEMON_LABEL);
    let over = print_disabled
        .lines()
        .map(str::trim)
        .find_map(|l| l.strip_prefix(key.as_str()))
        .map(|v| match v.trim() {
            "true" => "disabled".to_string(), // older macOS spelling
            "false" => "enabled".to_string(),
            v => v.to_string(),
        })
        .unwrap_or_else(|| "none".to_string());
    format!(
        "state={} pid={} runs={} last_exit={} override={} plist={}",
        state,
        field("pid").unwrap_or_else(|| "-".into()),
        field("runs").unwrap_or_else(|| "-".into()),
        field("last exit code").unwrap_or_else(|| "-".into()),
        over,
        plist_exists
    )
}

/// Log a service lifecycle event with launchd's view to desktop.log and append
/// it to the durable history. No-op off macOS (Windows has its own SCM trail).
pub fn record(event: &str) {
    #[cfg(target_os = "macos")]
    {
        let summary = launchd_summary();
        log::info!("[service-diag] {}: {}", event, summary);
        let line = format!(
            "{} v{} {}: {}\n",
            chrono::Local::now().format("%Y-%m-%dT%H:%M:%S%z"),
            env!("CARGO_PKG_VERSION"),
            event,
            summary
        );
        append_capped(&crate::get_desktop_log_dir().join(HISTORY_FILE), &line, HISTORY_CAP);
    }
    #[cfg(not(target_os = "macos"))]
    let _ = event;
}

/// Append `line`; once the file passes `cap`, keep only its newest half (cut at
/// a line boundary). Best-effort — diagnostics must never break the caller.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
fn append_capped(path: &Path, line: &str, cap: usize) {
    let mut content = std::fs::read_to_string(path).unwrap_or_default();
    content.push_str(line);
    if content.len() > cap {
        let mut cut = content.len() - cap / 2;
        while !content.is_char_boundary(cut) {
            cut += 1;
        }
        cut = content[cut..].find('\n').map_or(content.len(), |i| cut + i + 1);
        content = content[cut..].to_string();
    }
    if let Some(dir) = path.parent() {
        let _ = std::fs::create_dir_all(dir);
    }
    let _ = std::fs::write(path, content);
}

/// Our package's lines from `/var/log/install.log` (world-readable): install
/// start/finish and every line the pre/postinstall scripts printed, including
/// `k2 service install` output. Returns the newest `max_lines`.
#[cfg(target_os = "macos")]
pub fn installer_history(max_lines: usize) -> String {
    match std::fs::read("/var/log/install.log") {
        Ok(bytes) => filter_install_log(
            &String::from_utf8_lossy(&bytes),
            BUNDLE_ID,
            PKG_INSTALLED_PREFIX,
            max_lines,
        ),
        Err(e) => format!("/var/log/install.log unreadable: {}\n", e),
    }
}

/// Keep lines naming `bundle_id`, plus every line of a `package_script_service`
/// process that ran one of our scripts (its pid is announced on a line with
/// `Scripts/<bundle_id>`; the script's own output lines carry only the pid),
/// plus installd's `Installed "<Product>_<version>…"` line.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
fn filter_install_log(content: &str, bundle_id: &str, installed_prefix: &str, max_lines: usize) -> String {
    let script_dir = format!("Scripts/{}", bundle_id);
    let mut ours: std::collections::HashSet<&str> = std::collections::HashSet::new();
    let mut kept: Vec<&str> = Vec::new();
    for line in content.lines() {
        let pid = line
            .split_once("package_script_service[")
            .and_then(|(_, rest)| rest.split_once(']'))
            .map(|(pid, _)| pid);
        if let Some(pid) = pid {
            if line.contains(&script_dir) {
                ours.insert(pid);
            }
            if ours.contains(pid) {
                kept.push(line);
                continue;
            }
        }
        if line.contains(bundle_id) || line.contains(installed_prefix) {
            kept.push(line);
        }
    }
    let start = kept.len().saturating_sub(max_lines);
    let mut out = kept[start..].join("\n");
    out.push('\n');
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    const RUNNING: &str = "system/kaitu = {\n\tactive count = 1\n\tstate = running\n\tendpoints = {\n\t\tstate = active\n\t}\n\truns = 1\n\tpid = 412\n\tlast exit code = (never exited)\n}\n";

    #[test]
    fn summarize_running_job() {
        let s = summarize(RUNNING, "\t\t\"kaitu\" => enabled\n", true);
        assert_eq!(
            s,
            "state=running pid=412 runs=1 last_exit=(never exited) override=enabled plist=true"
        );
    }

    // The #3911 shape: launchd does not know the job and holds a disabled
    // override. Nested "state = active" must never pass for the job state.
    #[test]
    fn summarize_refused_job() {
        let nested_first = "system/kaitu = {\n\tendpoints = {\n\t\tstate = active\n\t}\n\tstate = spawn scheduled\n\truns = 7\n\tlast exit code = 3\n}\n";
        assert_eq!(
            summarize(nested_first, "", true),
            "state=spawn scheduled pid=- runs=7 last_exit=3 override=none plist=true"
        );
        assert_eq!(
            summarize("", "\t\t\"kaitu-watchdog\" => enabled\n\t\t\"kaitu\" => true\n", false),
            "state=not-loaded pid=- runs=- last_exit=- override=disabled plist=false"
        );
    }

    // Read back the real artifacts on the build Mac (daemon may or may not be
    // installed — either way the summary is well-formed, never empty).
    #[cfg(target_os = "macos")]
    #[test]
    fn real_launchd_summary_and_installer_history() {
        let s = launchd_summary();
        println!("launchd: {}", s);
        assert!(s.starts_with("state="), "{}", s);
        assert!(s.contains(" override=") && s.contains(" plist="), "{}", s);
        let h = installer_history(20);
        println!("installer:\n{}", h);
        assert!(h.lines().count() <= 20);
    }

    #[test]
    fn append_capped_keeps_newest_whole_lines() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("sub").join(HISTORY_FILE);
        for i in 0..100 {
            append_capped(&path, &format!("event-{:03} payload\n", i), 400);
        }
        let content = std::fs::read_to_string(&path).unwrap();
        assert!(content.len() <= 400, "len {}", content.len());
        assert!(content.ends_with("event-099 payload\n"));
        assert!(content.starts_with("event-"), "cut mid-line: {:?}", &content[..20]);
        assert!(!content.contains("event-000"));
    }

    // One trim: the newest half survives, starting on a whole line.
    #[test]
    fn append_capped_trim_keeps_half() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join(HISTORY_FILE);
        let prefill: String = (0..30).map(|i| format!("event-{:03} payload\n", i)).collect();
        std::fs::write(&path, &prefill).unwrap(); // 540 bytes, 18 per line
        append_capped(&path, "event-030 payload\n", 400);
        let content = std::fs::read_to_string(&path).unwrap();
        // 558 bytes - keep ~200 → cut at 358, rounded up to the next line (360).
        assert_eq!(content.len(), 198, "{:?}", content);
        assert!(content.starts_with("event-020 payload\n"), "{:?}", content);
        assert!(content.ends_with("event-030 payload\n"));
    }

    // Real install.log shape (macOS 26, our pkg + an unrelated vendor's).
    #[test]
    fn filter_install_log_keeps_our_package_and_script_output() {
        let log = "\
2026-10-09 18:34:28+07 host installd[20505]: PackageKit: ----- Begin install -----
2026-10-09 18:34:28+07 host package_script_service[25922]: PackageKit: Preparing to execute script \"postinstall\" in /tmp/PKInstallSandbox.BFGTFa/Scripts/io.kaitu.desktop.FRUIQK
2026-10-09 18:34:29+07 host package_script_service[25922]: ./postinstall: Installing service...
2026-10-09 18:34:29+07 host package_script_service[25922]: ./postinstall: Service installed and started.
2026-10-09 18:34:29+07 host installd[20505]: PackageKit: Writing receipt for io.kaitu.desktop to /
2026-10-09 18:34:29+07 host installd[20505]: Installed \"Kaitu_0.4.13_universal\" ()
2026-10-09 18:40:00+07 host package_script_service[30000]: PackageKit: Executing script \"postinstall\" in /tmp/X/Scripts/com.other.vendor.ABC
2026-10-09 18:40:01+07 host package_script_service[30000]: ./postinstall: other vendor output
2026-10-09 18:40:01+07 host installd[20505]: PackageKit: Writing receipt for com.other.vendor to /
2026-10-09 18:40:01+07 host installd[20505]: Installed \"OtherApp_1.0\" ()
";
        let got = filter_install_log(log, "io.kaitu.desktop", "Installed \"Kaitu_", 100);
        assert!(got.contains("./postinstall: Installing service..."));
        assert!(got.contains("Service installed and started."));
        assert!(got.contains("Writing receipt for io.kaitu.desktop"));
        assert!(!got.contains("other vendor"));
        assert!(!got.contains("com.other.vendor"));
        assert!(got.contains("Installed \"Kaitu_0.4.13_universal\""));
        assert!(!got.contains("OtherApp"));
        assert!(!got.contains("Begin install"));
        assert_eq!(got.lines().count(), 5);

        let tail = filter_install_log(log, "io.kaitu.desktop", "Installed \"Kaitu_", 2);
        assert_eq!(tail.lines().count(), 2);
        assert!(tail.ends_with("Installed \"Kaitu_0.4.13_universal\" ()\n"));
    }
}
