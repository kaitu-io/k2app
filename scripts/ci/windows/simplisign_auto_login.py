#!/usr/bin/env python3
"""
SimpliSign Desktop Auto-Login Script

Automates SimpliSign Desktop authentication using TOTP codes.
Designed for GitHub Actions CI/CD automation.

Reference: https://www.devas.life/how-to-automate-signing-your-windows-app-with-certum/

Requirements:
    pip install pywinauto pyotp

Usage:
    python simplisign_auto_login.py --totp-uri "otpauth://..." --username "user@example.com" [--pin "1234"]
"""

import argparse
import pyotp
import time
import sys
import urllib.parse
from pywinauto import Application
from pywinauto.keyboard import send_keys
from pywinauto.findwindows import ElementNotFoundError


def parse_otpauth_uri(uri):
    """
    Extract TOTP parameters from otpauth:// URI.

    Args:
        uri: otpauth://totp/Certum:username?secret=BASE32SECRET&issuer=Certum&algorithm=SHA256&digits=6&period=30

    Returns:
        dict with 'secret', 'algorithm', 'digits', 'period'
    """
    if not uri.startswith('otpauth://'):
        raise ValueError(f"Invalid TOTP URI format. Expected otpauth://, got: {uri[:20]}...")

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


def generate_totp(uri):
    """
    Generate current TOTP code from otpauth:// URI.

    Args:
        uri: Full otpauth:// URI with secret

    Returns:
        TOTP code (string)
    """
    params = parse_otpauth_uri(uri)

    # Map algorithm string to hashlib digest
    algorithm_map = {
        'SHA1': 'SHA1',
        'SHA256': 'SHA256',
        'SHA512': 'SHA512',
    }
    digest = algorithm_map.get(params['algorithm'], 'SHA1')

    totp = pyotp.TOTP(
        params['secret'],
        digits=params['digits'],
        interval=params['period'],
        digest=digest,
    )
    return totp.now()


def wait_for_window(title_re, timeout=10):
    """
    Wait for window to appear.

    Args:
        title_re: Window title regex pattern
        timeout: Maximum wait time in seconds

    Returns:
        True if window found, False if timeout
    """
    start_time = time.time()
    while time.time() - start_time < timeout:
        try:
            app = Application(backend="uia").connect(title_re=title_re, timeout=1)
            return True
        except Exception:
            time.sleep(0.5)
    return False


def login_simplisign(username, otp_code, pin=None):
    """
    Automate SimplySign Desktop login using pywinauto.

    This function:
    1. Checks if already logged in (no "Connect" button)
    2. Clicks "Connect" button if present
    3. Fills in username, OTP, and PIN (if provided)
    4. Clicks Login button
    5. Waits for authentication to complete

    Args:
        username: SimplySign account username
        otp_code: 6-digit TOTP code
        pin: Optional PIN code

    Returns:
        True if login successful, False otherwise
    """
    try:
        print("Connecting to SimplySign Desktop...")

        # Try to connect to SimplySign main window
        app = Application(backend="uia").connect(title_re=".*SimplySign.*", timeout=5)
        main_window = app.window(title_re=".*SimplySign.*")

        print("Found SimplySign Desktop window")

        # Check if already logged in
        try:
            connect_button = main_window.child_window(title="Connect", control_type="Button")
            if not connect_button.exists():
                print("Already logged in (no Connect button found)")
                return True
        except ElementNotFoundError:
            print("Already logged in (Connect button not found)")
            return True

        # Click Connect button
        print("Clicking Connect button...")
        connect_button.click()
        time.sleep(2)

        # Wait for login dialog
        if not wait_for_window(".*Login.*", timeout=10):
            print("ERROR: Login dialog did not appear")
            return False

        print("Login dialog appeared")
        login_dialog = app.window(title_re=".*Login.*")

        # Fill username
        print(f"Entering username: {username}")
        username_field = login_dialog.child_window(auto_id="usernameField", control_type="Edit")
        username_field.set_text(username)
        time.sleep(0.5)

        # Fill OTP
        print(f"Entering OTP: {otp_code}")
        otp_field = login_dialog.child_window(auto_id="otpField", control_type="Edit")
        otp_field.set_text(otp_code)
        time.sleep(0.5)

        # Fill PIN (if provided)
        if pin:
            print("Entering PIN")
            try:
                pin_field = login_dialog.child_window(auto_id="pinField", control_type="Edit")
                pin_field.set_text(pin)
                time.sleep(0.5)
            except ElementNotFoundError:
                print("PIN field not found (may not be required)")

        # Click Login button
        print("Clicking Login button...")
        login_button = login_dialog.child_window(title="Login", control_type="Button")
        login_button.click()

        # Wait for authentication
        print("Waiting for authentication...")
        time.sleep(5)

        # Verify success
        try:
            connected_text = main_window.child_window(title="Connected", control_type="Text")
            if connected_text.exists():
                print("✓ Login successful")
                return True
        except ElementNotFoundError:
            pass

        # Alternative check: Connect button should disappear
        try:
            connect_button = main_window.child_window(title="Connect", control_type="Button")
            if not connect_button.exists():
                print("✓ Login successful (Connect button disappeared)")
                return True
        except ElementNotFoundError:
            print("✓ Login successful (Connect button disappeared)")
            return True

        print("ERROR: Login failed - verify credentials")
        return False

    except Exception as e:
        print(f"ERROR during login: {e}")
        import traceback
        traceback.print_exc()
        return False


def main():
    parser = argparse.ArgumentParser(
        description='SimplySign Desktop Auto-Login',
        epilog='Example: python %(prog)s --totp-uri "otpauth://totp/Certum:user@example.com?secret=ABC123&issuer=Certum" --username "user@example.com"'
    )
    parser.add_argument('--totp-uri', required=True,
                        help='TOTP otpauth:// URI from SimplySign QR code')
    parser.add_argument('--username', required=True,
                        help='SimplySign account username')
    parser.add_argument('--pin', required=False, default=None,
                        help='SimplySign PIN (optional)')
    parser.add_argument('--test-otp-only', action='store_true',
                        help='Only generate and print OTP code (no login)')

    args = parser.parse_args()

    try:
        # Generate OTP
        otp_code = generate_totp(args.totp_uri)
        print(f"Generated OTP: {otp_code}")

        if args.test_otp_only:
            print("Test mode: OTP generation successful")
            sys.exit(0)

        # Perform login
        success = login_simplisign(args.username, otp_code, args.pin)

        if success:
            print("SimpliSign authentication completed successfully")
            sys.exit(0)
        else:
            print("SimpliSign authentication failed")
            sys.exit(1)

    except Exception as e:
        print(f"FATAL ERROR: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(1)


if __name__ == '__main__':
    main()
