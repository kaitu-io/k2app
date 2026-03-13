#!/bin/bash
set -e

# SimplySign auto-login for macOS
# Automates Certum SimplySign Desktop login via TOTP + accessibility API.
#
# Requires: python3, pyotp (pip3 install pyotp), pkcs11-tool (brew install opensc)
# Usage: SIMPLISIGN_TOTP_URI="otpauth://..." ./simplisign-login.sh

PKCS11_MODULE="/usr/local/lib/SimplySignPKCS/SimplySignPKCS-MS-1.1.24.dylib"
MAX_LOGIN_ATTEMPTS=3

# --- Helpers ---

get_menu_items() {
    osascript << 'EOF' 2>&1
tell application "System Events"
    tell process "SimplySign Desktop"
        click menu bar item 1 of menu bar 2
        delay 0.3
        set menuNames to name of every menu item of menu 1 of menu bar item 1 of menu bar 2
        key code 53
        return menuNames
    end tell
end tell
EOF
}

is_connected() {
    get_menu_items | grep -q "Disconnect from cloud"
}

has_pkcs11_token() {
    pkcs11-tool --module "$PKCS11_MODULE" --list-slots 2>&1 | grep -q "token label"
}

generate_totp() {
    python3 -c "
import pyotp, time, urllib.parse, sys
uri = sys.argv[1]
parsed = urllib.parse.urlparse(uri)
params = urllib.parse.parse_qs(parsed.query)
secret = params['secret'][0]
algo = params.get('algorithm', ['SHA1'])[0]
digits = int(params.get('digits', ['6'])[0])
period = int(params.get('period', ['30'])[0])
totp = pyotp.TOTP(secret, digits=digits, interval=period, digest=algo)
remaining = period - (int(time.time()) % period)
if remaining < 10:
    time.sleep(remaining + 1)
print(totp.now())
" "$SIMPLISIGN_TOTP_URI"
}

get_window_text() {
    osascript << 'EOF' 2>/dev/null
tell application "System Events"
    tell process "SimplySign Desktop"
        try
            tell window "SimplySign Desktop"
                set allElements to entire contents
                set output to {}
                repeat with e in allElements
                    try
                        if role of e is "AXStaticText" then
                            set v to value of e
                            if v is not "" and v is not missing value then
                                set end of output to v
                            end if
                        end if
                    end try
                end repeat
                return output as text
            end tell
        on error
            return ""
        end try
    end tell
end tell
EOF
}

has_login_window() {
    osascript << 'EOF' 2>/dev/null | grep -q "SimplySign Desktop"
tell application "System Events"
    tell process "SimplySign Desktop"
        try
            return name of every window
        on error
            return ""
        end try
    end tell
end tell
EOF
}

close_window() {
    osascript << 'EOF' 2>/dev/null
tell application "System Events"
    tell process "SimplySign Desktop"
        try
            tell window "SimplySign Desktop"
                set allBtns to every button
                repeat with b in allBtns
                    try
                        set d to description of b
                        if d is not "close button" and d is not "zoom button" and d is not "minimize button" then
                            perform action "AXPress" of b
                            return "clicked"
                        end if
                    end try
                end repeat
            end tell
        on error
            return "error"
        end try
    end tell
end tell
EOF
}

fill_totp_and_login() {
    local code="$1"
    osascript << EOF 2>&1
tell application "System Events"
    tell process "SimplySign Desktop"
        set frontmost to true
        delay 0.5
        tell window "SimplySign Desktop"
            tell scroll area 1
                set wa to first UI element whose role is "AXWebArea"
                set fields to every UI element of wa whose role is "AXTextField"
                if (count of fields) < 2 then
                    return "error:no_fields"
                end if
                -- Focus token field (AXPress unreliable for WebKit inputs)
                set tokenField to item 2 of fields
                set focused of tokenField to true
                delay 0.5
                -- Click to ensure cursor is in field
                perform action "AXPress" of tokenField
                delay 0.5
                -- Select all + delete + type code via keystrokes
                keystroke "a" using command down
                delay 0.3
                key code 51
                delay 0.3
                keystroke "${code}"
                delay 2.0
                -- Click Login button (WebView needs time to process input and enable button)
                set btns to every UI element of wa whose role is "AXButton"
                if (count of btns) > 0 then
                    perform action "AXPress" of item 1 of btns
                    return "ok"
                else
                    return "error:no_button"
                end if
            end tell
        end tell
    end tell
end tell
EOF
}

# --- Main ---

echo "=== SimplySign Auto-Login ==="

# Fast path: check PKCS#11 token directly (works even from non-GUI sessions)
if has_pkcs11_token; then
    echo "PKCS#11 token already available. Done!"
    exit 0
fi

echo "PKCS#11 token not available. Attempting GUI login..."

# GUI login requires SIMPLISIGN_TOTP_URI
if [ -z "$SIMPLISIGN_TOTP_URI" ]; then
    echo "ERROR: SIMPLISIGN_TOTP_URI not set and PKCS#11 token not available." >&2
    echo "Either log in to SimplySign Desktop manually, or set SIMPLISIGN_TOTP_URI." >&2
    exit 1
fi

# Ensure app is running
if ! pgrep -f "SimplySign Desktop" > /dev/null; then
    echo "Starting SimplySign Desktop..."
    open "/Applications/SimplySign Desktop.app"
    sleep 3
fi

# Already connected via menu bar?
if is_connected; then
    echo "Already connected."
    echo "Waiting for PKCS#11 token..."
    for i in $(seq 1 10); do sleep 1; has_pkcs11_token && echo "Ready." && exit 0; done
    echo "WARNING: Connected but no PKCS#11 token." >&2
    exit 1
fi

# Login loop with retry
for attempt in $(seq 1 $MAX_LOGIN_ATTEMPTS); do
    echo ""
    echo "--- Login attempt $attempt/$MAX_LOGIN_ATTEMPTS ---"

    # Close any existing window
    if has_login_window; then
        close_window
        sleep 1
    fi

    # Open login
    echo "Opening login..."
    osascript << 'EOF' 2>&1
tell application "System Events"
    tell process "SimplySign Desktop"
        click menu bar item 1 of menu bar 2
        delay 0.3
        click menu item "Connect with cloud" of menu 1 of menu bar item 1 of menu bar 2
    end tell
end tell
EOF

    # Wait for WebView to load (the white screen issue)
    echo "Waiting for WebView to load..."
    loaded=false
    for i in $(seq 1 15); do
        sleep 1
        text=$(get_window_text)
        if echo "$text" | grep -qi "E-MAIL\|email\|token\|Login"; then
            loaded=true
            echo "WebView loaded."
            break
        fi
    done

    if [ "$loaded" = "false" ]; then
        echo "WebView didn't load (white screen). Retrying..."
        continue
    fi

    # Generate TOTP
    echo "Generating TOTP..."
    TOTP=$(generate_totp)
    echo "TOTP ready."

    # Fill and submit
    echo "Filling TOTP and clicking Login..."
    result=$(fill_totp_and_login "$TOTP")
    echo "  Result: $result"

    if echo "$result" | grep -q "error"; then
        echo "Form fill failed. Retrying..."
        continue
    fi

    # Wait for login result
    echo "Waiting for login result..."
    login_ok=false
    for i in $(seq 1 20); do
        sleep 1
        text=$(get_window_text)

        if echo "$text" | grep -qi "succesfull\|successful"; then
            login_ok=true
            echo "Login successful!"
            break
        fi

        if echo "$text" | grep -qi "error\|incorrect\|invalid\|failed"; then
            echo "Login error: $text"
            break
        fi

        # Check if window closed (auto-redirect success)
        if ! has_login_window; then
            if is_connected; then
                login_ok=true
                echo "Login successful (window auto-closed)."
                break
            fi
        fi
    done

    if [ "$login_ok" = "true" ]; then
        # Click Close button if window still showing success
        if has_login_window; then
            echo "Clicking Close..."
            close_window
            sleep 5
        fi

        # Verify connection (menu bar may take time to update)
        echo "Verifying connection..."
        for i in $(seq 1 10); do
            if is_connected; then
                echo "SimplySign connected!"
                for j in $(seq 1 15); do
                    if has_pkcs11_token; then
                        echo "PKCS#11 token ready. Done!"
                        exit 0
                    fi
                    sleep 1
                done
                echo "WARNING: Connected but PKCS#11 token slow to appear."
                exit 0
            fi
            sleep 2
        done
        echo "WARNING: Login succeeded but connection not verified via menu bar."

        # Fallback: check PKCS#11 token directly (menu bar may be unreliable)
        for i in $(seq 1 10); do
            if has_pkcs11_token; then
                echo "PKCS#11 token ready (detected via fallback). Done!"
                exit 0
            fi
            sleep 1
        done
    fi

    echo "Attempt $attempt failed."
done

echo "ERROR: All login attempts failed." >&2
exit 1
