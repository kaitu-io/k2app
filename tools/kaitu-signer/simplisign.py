"""
SimpliSign Desktop automation — login, status checking, keeper loop.

Merged from:
- scripts/ci/windows/simplisign_auto_login.py (TOTP generation, login flow)
- scripts/ci/windows/simplisign_keeper.py (periodic checking, tray icon detection)
"""

import logging
import threading
import time
import urllib.parse
from datetime import datetime

import pyotp

logger = logging.getLogger("kaitu-signer")

# Serialize all pywinauto UI access — keeper thread and sign loop must not
# interact with the SimpliSign UI concurrently.
_ui_lock = threading.Lock()

# Lazy import pywinauto — only available on Windows
_pywinauto = None


def _ensure_pywinauto():
    global _pywinauto
    if _pywinauto is not None:
        return _pywinauto

    try:
        import pywinauto as pw
        _pywinauto = pw
        return pw
    except ImportError:
        raise ImportError(
            "pywinauto is required for SimpliSign automation. "
            "Install: pip install pywinauto"
        )


# --- TOTP ---


def parse_otpauth_uri(uri):
    """
    Extract TOTP parameters from otpauth:// URI.

    Args:
        uri: otpauth://totp/Certum:user?secret=BASE32&issuer=Certum&algorithm=SHA256&digits=6&period=30

    Returns:
        dict with 'secret', 'algorithm', 'digits', 'period'
    """
    if not uri.startswith("otpauth://"):
        raise ValueError(f"Invalid TOTP URI format. Expected otpauth://, got: {uri[:20]}...")

    parsed = urllib.parse.urlparse(uri)
    params = urllib.parse.parse_qs(parsed.query)

    if "secret" not in params:
        raise ValueError("TOTP URI missing 'secret' parameter")

    return {
        "secret": params["secret"][0],
        "algorithm": params.get("algorithm", ["SHA1"])[0].upper(),
        "digits": int(params.get("digits", ["6"])[0]),
        "period": int(params.get("period", ["30"])[0]),
    }


def generate_totp(uri):
    """Generate current TOTP code from otpauth:// URI."""
    params = parse_otpauth_uri(uri)
    totp = pyotp.TOTP(
        params["secret"],
        digits=params["digits"],
        interval=params["period"],
        digest=params["algorithm"],
    )
    return totp.now()


# --- Status detection ---


class Status:
    UNKNOWN = "unknown"
    CONNECTED = "connected"
    DISCONNECTED = "disconnected"
    LOGIN_IN_PROGRESS = "login_in_progress"
    ERROR = "error"


def _check_tray_status():
    """
    Check SimpliSign status from system tray icon tooltip text.

    Returns True if connected, False otherwise.
    """
    pw = _ensure_pywinauto()
    from pywinauto import Desktop
    from pywinauto.keyboard import send_keys

    desktop = Desktop(backend="uia")
    taskbar = desktop.window(class_name="Shell_TrayWnd")

    try:
        overflow_btn = None
        for title in ["显示隐藏的图标", "Show hidden icons"]:
            try:
                overflow_btn = taskbar.child_window(title=title, control_type="Button")
                if overflow_btn.exists():
                    break
            except Exception:
                pass

        if overflow_btn and overflow_btn.exists():
            overflow_btn.click()
            time.sleep(0.5)

            overflow_win = desktop.window(
                class_name="TopLevelWindowForOverflowXamlIsland"
            )
            if overflow_win.exists():
                for child in overflow_win.descendants():
                    try:
                        text = child.window_text().lower()
                        if "simplysign" in text or "certum" in text:
                            logger.debug(f"Tray icon status: {text}")
                            send_keys("{ESC}")
                            time.sleep(0.2)
                            return "connected" in text
                    except Exception:
                        pass
                send_keys("{ESC}")
    except Exception as e:
        logger.debug(f"Error checking tray status: {e}")

    return False


def _find_tray_icon():
    """Find SimpliSign icon in system tray and return the element."""
    pw = _ensure_pywinauto()
    from pywinauto import Desktop

    desktop = Desktop(backend="uia")
    taskbar = desktop.window(class_name="Shell_TrayWnd")

    # Try notification area first
    try:
        notify_area = taskbar.child_window(class_name="TrayNotifyWnd")
        for child in notify_area.descendants():
            try:
                text = child.window_text().lower()
                if "simplysign" in text or "certum" in text:
                    logger.debug(f"Found tray icon in notify area: {text}")
                    return child
            except Exception:
                pass
    except Exception:
        pass

    # Try overflow area (hidden icons)
    try:
        overflow_btn = None
        for title in ["显示隐藏的图标", "Show hidden icons"]:
            try:
                overflow_btn = taskbar.child_window(title=title, control_type="Button")
                if overflow_btn.exists():
                    break
            except Exception:
                pass

        if overflow_btn and overflow_btn.exists():
            overflow_btn.click()
            time.sleep(0.5)

            overflow_win = desktop.window(
                class_name="TopLevelWindowForOverflowXamlIsland"
            )
            if overflow_win.exists():
                for child in overflow_win.descendants():
                    try:
                        text = child.window_text().lower()
                        if "simplysign" in text or "certum" in text:
                            logger.debug(f"Found tray icon in overflow: {text}")
                            return child
                    except Exception:
                        pass
    except Exception as e:
        logger.debug(f"Error searching overflow: {e}")

    return None


def _open_simplisign_window():
    """Double-click tray icon to open SimpliSign main window."""
    pw = _ensure_pywinauto()
    from pywinauto import Application, Desktop

    icon = _find_tray_icon()
    if not icon:
        logger.warning("Could not find SimpliSign tray icon")
        return None

    logger.debug("Double-clicking tray icon...")
    icon.double_click_input()
    time.sleep(1)

    desktop = Desktop(backend="uia")
    for _ in range(5):
        for win in desktop.windows():
            try:
                title = win.window_text()
                if "simplysign" in title.lower() or "certum" in title.lower():
                    logger.debug(f"Found SimpliSign window: {title}")
                    return win
            except Exception:
                pass
        time.sleep(0.5)

    # Fallback: connect by process name
    try:
        app = Application(backend="uia").connect(
            path="SimplySignDesktop.exe", timeout=3
        )
        windows = app.windows()
        if windows:
            return windows[0]
    except Exception:
        pass

    return None


# --- Public API ---


def check_status():
    """
    Check SimpliSign connection status.

    Thread-safe: acquires _ui_lock to prevent concurrent pywinauto access.
    Returns one of Status.CONNECTED, Status.DISCONNECTED, Status.ERROR.
    """
    with _ui_lock:
        return _check_status_unlocked()


def _check_status_unlocked():
    """Check status — caller must hold _ui_lock."""
    pw = _ensure_pywinauto()
    from pywinauto import Application

    try:
        # Verify process is running
        try:
            Application(backend="uia").connect(
                path="SimplySignDesktop.exe", timeout=3
            )
        except Exception:
            logger.error("SimplySignDesktop.exe not running")
            return Status.ERROR

        if _check_tray_status():
            logger.info("SimpliSign connected (tray status)")
            return Status.CONNECTED

        logger.info("SimpliSign disconnected (tray status)")
        return Status.DISCONNECTED

    except Exception as e:
        logger.warning(f"Could not check SimpliSign status: {e}")
        return Status.UNKNOWN


def perform_login(totp_uri, username):
    """
    Perform SimpliSign login.

    Thread-safe: acquires _ui_lock to prevent concurrent pywinauto access.
    Opens the SimpliSign window, fills Token + ID fields, clicks OK.

    Args:
        totp_uri: otpauth:// URI for TOTP generation.
        username: SimpliSign account username.

    Returns:
        True if login succeeded.
    """
    with _ui_lock:
        return _perform_login_unlocked(totp_uri, username)


def _perform_login_unlocked(totp_uri, username):
    """Login — caller must hold _ui_lock."""
    pw = _ensure_pywinauto()
    from pywinauto.keyboard import send_keys

    logger.info("Starting SimpliSign login...")
    otp_code = generate_totp(totp_uri)
    logger.info(f"Generated OTP: {otp_code}")

    win = _open_simplisign_window()
    if not win:
        logger.error("Could not open SimpliSign window")
        return False

    logger.info(f"Using SimpliSign window: {win.window_text()}")

    try:
        edit_fields = win.descendants(control_type="Edit")
        logger.debug(f"Found {len(edit_fields)} edit fields")

        if len(edit_fields) < 2:
            logger.error(f"Expected at least 2 edit fields, found {len(edit_fields)}")
            send_keys("{ESC}")
            return False

        # First field: Token (OTP)
        logger.info(f"Entering Token: {otp_code}")
        edit_fields[0].set_text("")
        edit_fields[0].type_keys(otp_code, with_spaces=True)
        time.sleep(0.3)

        # Second field: ID (username)
        logger.info(f"Entering ID: {username}")
        edit_fields[1].set_text("")
        edit_fields[1].type_keys(username, with_spaces=True)
        time.sleep(0.3)

        # Find and click OK button
        ok_btn = None
        for child in win.descendants(control_type="Button"):
            try:
                if child.window_text().lower() == "ok":
                    ok_btn = child
                    break
            except Exception:
                pass

        if ok_btn:
            logger.info(f"Clicking: {ok_btn.window_text()}")
            ok_btn.click_input()
        else:
            logger.info("No OK button found, pressing Enter")
            send_keys("{ENTER}")

        # Wait for login to complete
        logger.info("Waiting for login to complete...")
        time.sleep(5)

        connected = _check_tray_status()
        if connected:
            logger.info("Login successful!")
            return True
        else:
            logger.error("Login may have failed")
            return False

    except Exception as e:
        logger.error(f"Login error: {e}")
        try:
            send_keys("{ESC}")
        except Exception:
            pass
        return False


def check_and_login_if_needed(totp_uri, username):
    """
    Check SimpliSign status and login if disconnected.

    Thread-safe: acquires _ui_lock once for the entire check+login sequence.
    Returns True if connected (already or after login).
    """
    with _ui_lock:
        status = _check_status_unlocked()

        if status == Status.CONNECTED:
            logger.debug("SimpliSign already connected")
            return True
        elif status == Status.DISCONNECTED:
            logger.info("SimpliSign disconnected - attempting login")
            return _perform_login_unlocked(totp_uri, username)
        else:
            logger.warning(f"SimpliSign status: {status} - will retry next interval")
            return False
