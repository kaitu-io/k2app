#!/usr/bin/env python3
"""
SimpliSign Keeper Daemon

A background daemon that monitors SimpliSign Desktop login status
and auto-logins when session expires or on startup.

SimpliSign runs as a system tray application:
- Double-click tray icon to open main window
- If "Connect" button visible -> need login
- If no "Connect" button -> already connected

Requirements:
    pip install pywinauto pyotp

Usage:
    python simplisign_keeper.py --totp-uri "otpauth://..." --username "user@example.com" [options]
"""

import argparse
import logging
import os
import sys
import time
import urllib.parse
from datetime import datetime
from logging.handlers import RotatingFileHandler
from threading import Thread, Event

import pyotp

try:
    from http.server import HTTPServer, BaseHTTPRequestHandler
    HTTP_AVAILABLE = True
except ImportError:
    HTTP_AVAILABLE = False

try:
    from pywinauto import Application, Desktop
    from pywinauto.findwindows import ElementNotFoundError
    from pywinauto.keyboard import send_keys
    PYWINAUTO_AVAILABLE = True
except ImportError:
    PYWINAUTO_AVAILABLE = False


DEFAULT_CHECK_INTERVAL = 300  # 5 minutes
DEFAULT_LOG_DIR = r"C:\actions-runner\logs"
DEFAULT_LOG_FILE = "simplisign_keeper.log"
DEFAULT_MAX_LOG_SIZE = 10 * 1024 * 1024  # 10MB
DEFAULT_LOG_BACKUP_COUNT = 3


class SimpliSignStatus:
    UNKNOWN = "unknown"
    CONNECTED = "connected"
    DISCONNECTED = "disconnected"
    LOGIN_IN_PROGRESS = "login_in_progress"
    ERROR = "error"


class SimpliSignKeeper:
    def __init__(self, totp_uri, username, pin=None, check_interval=DEFAULT_CHECK_INTERVAL):
        self.totp_uri = totp_uri
        self.username = username
        self.pin = pin
        self.check_interval = check_interval
        self.status = SimpliSignStatus.UNKNOWN
        self.last_check_time = None
        self.last_login_time = None
        self.login_count = 0
        self.error_count = 0
        self.logger = logging.getLogger("simplisign_keeper")
        self._stop_event = Event()
        self._current_window = None  # Cache the open window

    def parse_otpauth_uri(self, uri):
        if not uri.startswith('otpauth://'):
            raise ValueError(f"Invalid TOTP URI format")
        parsed = urllib.parse.urlparse(uri)
        params = urllib.parse.parse_qs(parsed.query)
        if 'secret' not in params:
            raise ValueError("TOTP URI missing 'secret' parameter")
        return {
            'secret': params['secret'][0],
            'algorithm': params.get('algorithm', ['SHA1'])[0].upper(),
            'digits': int(params.get('digits', ['6'])[0]),
            'period': int(params.get('period', ['30'])[0]),
        }

    def generate_totp(self):
        params = self.parse_otpauth_uri(self.totp_uri)
        totp = pyotp.TOTP(
            params['secret'],
            digits=params['digits'],
            interval=params['period'],
            digest=params['algorithm'],
        )
        return totp.now()

    def check_tray_status(self):
        """
        Check SimpliSign status from tray icon text.
        Returns True if connected, False otherwise.
        """
        desktop = Desktop(backend="uia")
        taskbar = desktop.window(class_name="Shell_TrayWnd")

        # Open overflow to see tray icons
        try:
            overflow_btn = None
            for title in ["显示隐藏的图标", "Show hidden icons"]:
                try:
                    overflow_btn = taskbar.child_window(title=title, control_type="Button")
                    if overflow_btn.exists():
                        break
                except:
                    pass

            if overflow_btn and overflow_btn.exists():
                overflow_btn.click()
                time.sleep(0.5)

                overflow_win = desktop.window(class_name="TopLevelWindowForOverflowXamlIsland")
                if overflow_win.exists():
                    for child in overflow_win.descendants():
                        try:
                            text = child.window_text().lower()
                            if "simplysign" in text or "certum" in text:
                                self.logger.debug(f"Tray icon status: {text}")
                                # Close overflow
                                send_keys("{ESC}")
                                time.sleep(0.2)
                                return "connected" in text
                        except:
                            pass
                    # Close overflow
                    send_keys("{ESC}")
        except Exception as e:
            self.logger.debug(f"Error checking tray status: {e}")

        return False

    def find_tray_icon(self):
        """Find SimpliSign icon in system tray."""
        desktop = Desktop(backend="uia")
        taskbar = desktop.window(class_name="Shell_TrayWnd")

        # Try notification area first
        try:
            notify_area = taskbar.child_window(class_name="TrayNotifyWnd")
            for child in notify_area.descendants():
                try:
                    text = child.window_text().lower()
                    if "simplysign" in text or "certum" in text:
                        self.logger.debug(f"Found tray icon in notify area: {text}")
                        return child
                except:
                    pass
        except:
            pass

        # Try overflow area (hidden icons)
        try:
            overflow_btn = None
            for title in ["显示隐藏的图标", "Show hidden icons"]:
                try:
                    overflow_btn = taskbar.child_window(title=title, control_type="Button")
                    if overflow_btn.exists():
                        break
                except:
                    pass

            if overflow_btn and overflow_btn.exists():
                self.logger.debug("Opening hidden icons overflow...")
                overflow_btn.click()
                time.sleep(0.5)

                overflow_win = desktop.window(class_name="TopLevelWindowForOverflowXamlIsland")
                if overflow_win.exists():
                    for child in overflow_win.descendants():
                        try:
                            text = child.window_text().lower()
                            if "simplysign" in text or "certum" in text:
                                self.logger.debug(f"Found tray icon in overflow: {text}")
                                return child
                        except:
                            pass
        except Exception as e:
            self.logger.debug(f"Error searching overflow: {e}")

        return None

    def open_simplisign_window(self):
        """Double-click tray icon to open SimpliSign window."""
        icon = self.find_tray_icon()
        if not icon:
            self.logger.warning("Could not find SimpliSign tray icon")
            return None

        self.logger.debug("Double-clicking tray icon...")
        icon.double_click_input()
        time.sleep(1)

        # Find the SimpliSign window
        desktop = Desktop(backend="uia")
        for _ in range(5):
            for win in desktop.windows():
                try:
                    title = win.window_text()
                    if "simplysign" in title.lower() or "certum" in title.lower():
                        self.logger.debug(f"Found SimpliSign window: {title}")
                        return win
                except:
                    pass
            time.sleep(0.5)

        # Also try connecting to the app
        try:
            app = Application(backend="uia").connect(path="SimplySignDesktop.exe", timeout=3)
            windows = app.windows()
            if windows:
                self.logger.debug(f"Found window via app connection")
                return windows[0]
        except:
            pass

        return None

    def check_simplisign_status(self):
        """
        Check SimpliSign status:
        1. First check tray icon status text (fast path)
        2. If disconnected, open window to get login form

        Stores window reference in self._current_window for use by perform_login.
        """
        if not PYWINAUTO_AVAILABLE:
            self.logger.error("pywinauto not available")
            return SimpliSignStatus.ERROR

        try:
            # Verify process is running
            try:
                app = Application(backend="uia").connect(path="SimplySignDesktop.exe", timeout=3)
            except:
                self.logger.error("SimplySignDesktop.exe not running")
                return SimpliSignStatus.ERROR

            # Fast path: check tray icon status first
            if self.check_tray_status():
                self.logger.info("SimpliSign connected (tray status)")
                self._current_window = None
                return SimpliSignStatus.CONNECTED

            # Tray shows disconnected - open window to get login form
            self.logger.info("SimpliSign disconnected (tray status)")
            win = self.open_simplisign_window()
            if win:
                self._current_window = win
            return SimpliSignStatus.DISCONNECTED

        except Exception as e:
            self.logger.warning(f"Could not check SimpliSign status: {e}")
            import traceback
            self.logger.debug(traceback.format_exc())
            self._current_window = None
            return SimpliSignStatus.UNKNOWN

    def perform_login(self):
        """
        Perform SimpliSign login:
        1. Use cached window from check_simplisign_status (or open new one)
        2. Fill in ID (username) and Token (OTP) fields
        3. Click OK button
        """
        if not PYWINAUTO_AVAILABLE:
            self.logger.error("pywinauto not available")
            return False

        self.status = SimpliSignStatus.LOGIN_IN_PROGRESS
        self.logger.info("Starting SimpliSign login...")

        try:
            # Generate fresh OTP
            otp_code = self.generate_totp()
            self.logger.info(f"Generated OTP: {otp_code}")

            # Use cached window from check_simplisign_status, or open new one
            win = self._current_window
            if not win:
                self.logger.debug("No cached window, opening new one...")
                win = self.open_simplisign_window()

            if not win:
                self.logger.error("Could not open SimpliSign window")
                self.status = SimpliSignStatus.ERROR
                self.error_count += 1
                return False

            self.logger.info(f"Using SimpliSign window: {win.window_text()}")

            # Find edit fields (ID and Token)
            edit_fields = win.descendants(control_type="Edit")
            self.logger.debug(f"Found {len(edit_fields)} edit fields")

            if len(edit_fields) >= 2:
                # First field: Token (OTP)
                self.logger.info(f"Entering Token: {otp_code}")
                edit_fields[0].set_text("")
                edit_fields[0].type_keys(otp_code, with_spaces=True)
                time.sleep(0.3)

                # Second field: ID (username)
                self.logger.info(f"Entering ID: {self.username}")
                edit_fields[1].set_text("")
                edit_fields[1].type_keys(self.username, with_spaces=True)
                time.sleep(0.3)
            else:
                self.logger.error(f"Expected at least 2 edit fields, found {len(edit_fields)}")
                send_keys("{ESC}")
                self.status = SimpliSignStatus.ERROR
                self.error_count += 1
                return False

            # Find and click OK button
            ok_btn = None
            for child in win.descendants(control_type="Button"):
                try:
                    text = child.window_text()
                    if text.lower() == "ok":
                        ok_btn = child
                        break
                except:
                    pass

            if ok_btn:
                self.logger.info(f"Clicking: {ok_btn.window_text()}")
                ok_btn.click_input()
            else:
                self.logger.info("No OK button found, pressing Enter")
                send_keys("{ENTER}")

            # Wait for login to complete (SimpliSign is slow)
            self.logger.info("Waiting for login to complete...")
            time.sleep(5)

            # Verify success by checking tray icon status text
            self._current_window = None  # Clear cached window
            connected = self.check_tray_status()
            if connected:
                self.logger.info("Login successful!")
                self.status = SimpliSignStatus.CONNECTED
                self.last_login_time = datetime.now()
                self.login_count += 1
                return True
            else:
                self.logger.error("Login may have failed")
                self.status = SimpliSignStatus.ERROR
                self.error_count += 1
                return False

        except Exception as e:
            self.logger.error(f"Login error: {e}")
            import traceback
            self.logger.debug(traceback.format_exc())
            try:
                send_keys("{ESC}")
            except:
                pass
            self.status = SimpliSignStatus.ERROR
            self.error_count += 1
            return False

    def check_and_login_if_needed(self):
        self.last_check_time = datetime.now()
        status = self.check_simplisign_status()
        self.status = status

        if status == SimpliSignStatus.DISCONNECTED:
            self.logger.info("SimpliSign disconnected - attempting login")
            return self.perform_login()
        elif status == SimpliSignStatus.CONNECTED:
            self.logger.debug("SimpliSign already connected")
            return True
        elif status == SimpliSignStatus.UNKNOWN:
            self.logger.warning("Could not determine status - will retry next interval")
            return False
        else:
            self.logger.error(f"SimpliSign in error state: {status}")
            return False

    def run(self):
        self.logger.info("=" * 60)
        self.logger.info("SimpliSign Keeper starting")
        self.logger.info(f"Username: {self.username}")
        self.logger.info(f"Check interval: {self.check_interval} seconds")
        self.logger.info("=" * 60)

        self.logger.info("Performing initial check...")
        self.check_and_login_if_needed()

        while not self._stop_event.is_set():
            self._stop_event.wait(self.check_interval)
            if self._stop_event.is_set():
                break
            self.logger.info(f"Periodic check (interval: {self.check_interval}s)")
            self.check_and_login_if_needed()

        self.logger.info("SimpliSign Keeper stopped")

    def stop(self):
        self.logger.info("Stop requested")
        self._stop_event.set()

    def get_status_dict(self):
        return {
            "status": self.status,
            "last_check": self.last_check_time.isoformat() if self.last_check_time else None,
            "last_login": self.last_login_time.isoformat() if self.last_login_time else None,
            "login_count": self.login_count,
            "error_count": self.error_count,
            "check_interval": self.check_interval,
        }


class HealthCheckHandler(BaseHTTPRequestHandler):
    keeper = None

    def log_message(self, format, *args):
        pass

    def do_GET(self):
        if self.path == "/health" or self.path == "/":
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            import json
            status = self.keeper.get_status_dict() if self.keeper else {"error": "not initialized"}
            self.wfile.write(json.dumps(status, indent=2).encode())
        else:
            self.send_response(404)
            self.end_headers()


def setup_logging(log_file, log_level=logging.INFO):
    log_dir = os.path.dirname(log_file)
    if log_dir and not os.path.exists(log_dir):
        os.makedirs(log_dir)

    logger = logging.getLogger("simplisign_keeper")
    logger.setLevel(log_level)

    # Remove existing handlers
    logger.handlers.clear()

    file_handler = RotatingFileHandler(
        log_file, maxBytes=DEFAULT_MAX_LOG_SIZE, backupCount=DEFAULT_LOG_BACKUP_COUNT)
    file_handler.setLevel(log_level)
    file_handler.setFormatter(logging.Formatter(
        "%(asctime)s [%(levelname)s] %(message)s", datefmt="%Y-%m-%d %H:%M:%S"))
    logger.addHandler(file_handler)

    console_handler = logging.StreamHandler(sys.stdout)
    console_handler.setLevel(log_level)
    console_handler.setFormatter(logging.Formatter(
        "%(asctime)s [%(levelname)s] %(message)s", datefmt="%H:%M:%S"))
    logger.addHandler(console_handler)

    return logger


def main():
    parser = argparse.ArgumentParser(description='SimpliSign Keeper Daemon')
    parser.add_argument('--totp-uri', default=os.environ.get('SIMPLISIGN_TOTP_URI'))
    parser.add_argument('--username', default=os.environ.get('SIMPLISIGN_USERNAME'))
    parser.add_argument('--pin', default=os.environ.get('SIMPLISIGN_PIN'))
    parser.add_argument('--check-interval', type=int, default=DEFAULT_CHECK_INTERVAL)
    parser.add_argument('--log-file', default=os.path.join(DEFAULT_LOG_DIR, DEFAULT_LOG_FILE))
    parser.add_argument('--http-port', type=int, default=None)
    parser.add_argument('--debug', action='store_true')
    parser.add_argument('--once', action='store_true')

    args = parser.parse_args()

    if not args.totp_uri:
        parser.error("--totp-uri required (or set SIMPLISIGN_TOTP_URI)")
    if not args.username:
        parser.error("--username required (or set SIMPLISIGN_USERNAME)")

    if not PYWINAUTO_AVAILABLE:
        print("ERROR: pywinauto required. Install: pip install pywinauto")
        sys.exit(1)

    log_level = logging.DEBUG if args.debug else logging.INFO
    setup_logging(args.log_file, log_level)

    keeper = SimpliSignKeeper(
        totp_uri=args.totp_uri,
        username=args.username,
        pin=args.pin,
        check_interval=args.check_interval,
    )

    if args.http_port and HTTP_AVAILABLE:
        HealthCheckHandler.keeper = keeper
        server = HTTPServer(('127.0.0.1', args.http_port), HealthCheckHandler)
        http_thread = Thread(target=server.serve_forever, daemon=True)
        http_thread.start()
        keeper.logger.info(f"HTTP health check: http://127.0.0.1:{args.http_port}/health")

    try:
        if args.once:
            keeper.logger.info("Running once...")
            success = keeper.check_and_login_if_needed()
            sys.exit(0 if success else 1)
        else:
            keeper.run()
    except KeyboardInterrupt:
        keeper.stop()
    except Exception as e:
        keeper.logger.error(f"Fatal: {e}")
        sys.exit(1)


if __name__ == '__main__':
    main()
