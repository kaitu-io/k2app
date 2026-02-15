"""
Kaitu Signer — Windows system tray application for code signing.

Main process runs pystray tray icon UI.
Worker process (separate process) handles SQS polling + signing + pywinauto.

Separate processes avoid Windows message pump conflicts between pystray and pywinauto.
"""

import logging
import multiprocessing
import os
import sys
import threading
import time
from datetime import datetime

# Ensure our package directory is on the path (for PyInstaller)
if getattr(sys, "frozen", False):
    os.chdir(os.path.dirname(sys.executable))
    sys.path.insert(0, os.path.dirname(sys.executable))
else:
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import config

logger = logging.getLogger("kaitu-signer")


def _setup_main_logging():
    os.makedirs(config.LOG_DIR, exist_ok=True)
    log_path = os.path.join(config.LOG_DIR, config.LOG_FILE)

    root = logging.getLogger("kaitu-signer")
    root.setLevel(logging.DEBUG)
    root.handlers.clear()

    from logging.handlers import RotatingFileHandler

    fh = RotatingFileHandler(log_path, maxBytes=10 * 1024 * 1024, backupCount=3)
    fh.setLevel(logging.DEBUG)
    fh.setFormatter(logging.Formatter(
        "%(asctime)s [%(levelname)s] %(message)s", datefmt="%Y-%m-%d %H:%M:%S"
    ))
    root.addHandler(fh)

    ch = logging.StreamHandler()
    ch.setLevel(logging.INFO)
    ch.setFormatter(logging.Formatter(
        "%(asctime)s [%(levelname)s] %(message)s", datefmt="%H:%M:%S"
    ))
    root.addHandler(ch)


class SignerApp:
    def __init__(self):
        self.status = "Starting..."
        self.last_job = None
        self.job_count = 0
        self.error_count = 0
        self.worker_process = None
        self.status_queue = multiprocessing.Queue()   # worker → main
        self.command_queue = multiprocessing.Queue()   # main → worker
        self.stop_event = multiprocessing.Event()
        self.tray_icon = None

    def start_worker(self):
        """Spawn the worker process."""
        import worker

        self.worker_process = multiprocessing.Process(
            target=worker.run,
            args=(self.status_queue, self.command_queue, self.stop_event),
            daemon=True,
            name="kaitu-signer-worker",
        )
        self.worker_process.start()
        logger.info(f"Worker process started (pid={self.worker_process.pid})")

    def stop_worker(self):
        """Signal the worker process to stop."""
        self.stop_event.set()
        if self.worker_process and self.worker_process.is_alive():
            self.worker_process.join(timeout=10)
            if self.worker_process.is_alive():
                logger.warning("Worker did not stop gracefully, terminating")
                self.worker_process.terminate()

    def poll_status(self):
        """Read status messages from the worker process."""
        while True:
            try:
                msg = self.status_queue.get_nowait()
            except Exception:
                break

            msg_type = msg.get("type")

            if msg_type == "ready":
                self.status = "Idle — waiting for jobs"
                logger.info("Worker ready")

            elif msg_type == "job_start":
                run_id = msg.get("run_id", "?")
                self.status = f"Signing run {run_id}..."
                logger.info(f"Tray: job started run_id={run_id}")

            elif msg_type == "job_complete":
                run_id = msg.get("run_id", "?")
                success = msg.get("success", False)
                self.last_job = f"{run_id} ({'OK' if success else 'FAILED'})"
                if success:
                    self.job_count += 1
                    self.status = "Idle — waiting for jobs"
                    self._notify(f"Signed run {run_id}", "Signing completed successfully.")
                else:
                    self.error_count += 1
                    error = msg.get("error", "Unknown error")
                    self.status = f"Last job failed: {error[:50]}"
                    self._notify(f"Signing failed: run {run_id}", error[:200])

            elif msg_type == "error":
                self.status = f"Error: {msg.get('message', '')[:50]}"
                self.error_count += 1

        # Update tray tooltip
        if self.tray_icon:
            self.tray_icon.title = f"Kaitu Signer — {self.status}"

    def _notify(self, title, message):
        """Show a Windows notification via the tray icon."""
        if self.tray_icon:
            try:
                self.tray_icon.notify(title, message)
            except Exception:
                pass

    def _status_text(self):
        lines = [
            f"Status: {self.status}",
            f"Jobs completed: {self.job_count}",
            f"Errors: {self.error_count}",
        ]
        if self.last_job:
            lines.append(f"Last job: {self.last_job}")
        return "\n".join(lines)

    # --- Tray menu actions ---

    def on_show_status(self, icon, item):
        """Show status in a message box."""
        try:
            import ctypes
            ctypes.windll.user32.MessageBoxW(
                0, self._status_text(), "Kaitu Signer Status", 0x40
            )
        except Exception:
            logger.info(self._status_text())

    def on_open_log(self, icon, item):
        """Open the log file in the default editor."""
        log_path = os.path.join(config.LOG_DIR, config.LOG_FILE)
        if os.path.exists(log_path):
            os.startfile(log_path)

    def on_force_login(self, icon, item):
        """Trigger a SimpliSign login check in the worker."""
        try:
            self.command_queue.put_nowait({"type": "force_login"})
        except Exception:
            pass
        self._notify("SimpliSign", "Login check queued — may take up to 20s.")

    def on_quit(self, icon, item):
        """Quit the application."""
        logger.info("Quit requested from tray menu")
        self.stop_worker()
        icon.stop()

    # --- Main loop ---

    def run(self):
        """Start the tray icon and worker."""
        import pystray
        from PIL import Image

        _setup_main_logging()
        logger.info("=" * 60)
        logger.info("Kaitu Signer starting")
        logger.info("=" * 60)

        missing = config.validate()
        if missing:
            logger.error(f"Missing required config: {', '.join(missing)}")
            try:
                import ctypes
                ctypes.windll.user32.MessageBoxW(
                    0,
                    f"Missing required configuration:\n\n{chr(10).join(missing)}\n\nSet these as environment variables or in .env file.",
                    "Kaitu Signer — Configuration Error",
                    0x10,
                )
            except Exception:
                pass
            return

        # Load icon
        icon_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "icon.ico")
        if os.path.exists(icon_path):
            image = Image.open(icon_path)
        else:
            # Fallback: generate a simple colored icon
            image = Image.new("RGB", (64, 64), color=(0, 122, 204))

        menu = pystray.Menu(
            pystray.MenuItem("Status...", self.on_show_status),
            pystray.MenuItem("Open Log", self.on_open_log),
            pystray.MenuItem("Force SimpliSign Login", self.on_force_login),
            pystray.Menu.SEPARATOR,
            pystray.MenuItem("Quit", self.on_quit),
        )

        self.tray_icon = pystray.Icon(
            name="kaitu-signer",
            icon=image,
            title="Kaitu Signer — Starting...",
            menu=menu,
        )

        # Start worker process
        self.start_worker()

        # Start status polling thread
        def poll_loop():
            while not self.stop_event.is_set():
                self.poll_status()
                time.sleep(1)

        poll_thread = threading.Thread(target=poll_loop, daemon=True)
        poll_thread.start()

        # Run tray icon (blocks until quit)
        self.tray_icon.run()

        # Cleanup
        self.stop_worker()
        logger.info("Kaitu Signer stopped")


def main():
    # Required for PyInstaller + multiprocessing on Windows
    multiprocessing.freeze_support()

    app = SignerApp()
    app.run()


if __name__ == "__main__":
    main()
