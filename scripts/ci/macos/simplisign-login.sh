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

# True only when this process can drive the macOS Accessibility API (osascript
# + System Events). An interactive Terminal/desktop session with Accessibility
# granted returns 0; the GitHub Actions runner service context cannot and
# osascript fails with error -1719. This gates whether we may read the cloud
# menu bar or attempt a GUI login at all.
has_accessibility() {
    osascript -e 'tell application "System Events" to get name of first process' >/dev/null 2>&1
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

# Count AXTextField (+ AXSecureTextField) anywhere in the SimplySign window.
# Walks `entire contents` so the count is robust to changes in the parent
# hierarchy (scroll area, group wrappers, etc. that SimplySign may add).
count_login_fields() {
    osascript << 'EOF' 2>/dev/null
tell application "System Events"
    tell process "SimplySign Desktop"
        try
            tell window "SimplySign Desktop"
                set n to 0
                set elems to entire contents
                repeat with e in elems
                    try
                        set r to role of e
                        if r is "AXTextField" or r is "AXSecureTextField" then
                            set n to n + 1
                        end if
                    end try
                end repeat
                return n as text
            end tell
        on error
            return "0"
        end try
    end tell
end tell
EOF
}

# Dump the SimplySign Desktop window UI tree (roles + descriptions). For
# diagnostic logging when field detection fails — lets the next CI run
# surface what actually changed without needing a live Mac mini session.
dump_ui_tree() {
    osascript << 'EOF' 2>/dev/null
tell application "System Events"
    tell process "SimplySign Desktop"
        try
            tell window "SimplySign Desktop"
                set out to ""
                set elems to entire contents
                repeat with e in elems
                    try
                        set r to role of e
                        set d to ""
                        try
                            set d to description of e
                        end try
                        set v to ""
                        try
                            set v to value of e
                        end try
                        set out to out & r & " | " & d & " | " & v & "
"
                    end try
                end repeat
                return out
            end tell
        on error errMsg
            return "dump_failed: " & errMsg
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
            -- Walk entire contents so the field/button discovery is
            -- robust to SimplySign reshaping the parent hierarchy
            -- (scroll area, intermediate groups, etc.).
            set allFields to {}
            set allButtons to {}
            set elems to entire contents
            repeat with e in elems
                try
                    set r to role of e
                    if r is "AXTextField" or r is "AXSecureTextField" then
                        set end of allFields to e
                    else if r is "AXButton" then
                        set end of allButtons to e
                    end if
                end try
            end repeat
            if (count of allFields) is 0 then
                return "error:no_fields"
            end if
            -- TOTP is always the last input on the form: works for
            -- 2-field (email + token) and 1-field (token-only when
            -- email was remembered from prior session) flows.
            set tokenField to item -1 of allFields
            try
                set focused of tokenField to true
            end try
            delay 0.5
            try
                perform action "AXPress" of tokenField
            end try
            delay 0.5
            keystroke "a" using command down
            delay 0.3
            key code 51
            delay 0.3
            keystroke "${code}"
            delay 2.0
            -- Find the form's Login button. SimplySign WebView buttons
            -- have description = missing value or "button"; window
            -- chrome buttons have "close button" / "zoom button" /
            -- "minimize button". Exclude chrome, take the first
            -- remaining button — that's the form's primary action.
            set loginBtn to missing value
            repeat with b in allButtons
                set d to ""
                try
                    set d to description of b
                end try
                if d is not "close button" and d is not "zoom button" and d is not "minimize button" then
                    set loginBtn to b
                    exit repeat
                end if
            end repeat
            if loginBtn is not missing value then
                perform action "AXPress" of loginBtn
                return "ok"
            else
                return "error:no_button"
            end if
        end tell
    end tell
end tell
EOF
}

# --- Main ---

echo "=== SimplySign Auto-Login ==="

# Two execution contexts need different handling:
#
#   * Headless (the GitHub Actions runner service) — cannot drive the macOS
#     Accessibility API; any osascript/System Events call fails with error
#     -1719. This context only CONSUMES a cloud session that an interactive
#     session established; it must never attempt a GUI login. The PKCS#11 slot
#     is a cheap liveness gate here. It is deliberately NON-authoritative: a
#     stale cached slot can outlive a dropped cloud session, so the real
#     dead-session check is the signing preflight
#     (scripts/ci/windows-sign-preflight.sh), which runs headlessly right
#     before the heavy Tauri bundle and surfaces the real osslsigncode error
#     (CKR_ATTRIBUTE_TYPE_INVALID) fast — instead of an opaque "failed to run
#     bash" 18 minutes in.
#
#   * Interactive (Terminal/desktop with Accessibility granted) — can read the
#     real cloud state via the menu bar and drive a GUI TOTP re-login. Here the
#     menu bar ("Disconnect from cloud") is authoritative, so a cached slot
#     with a dead cloud session correctly triggers a re-login instead of a
#     false pass.
if ! has_accessibility; then
    echo "Headless context (no Accessibility). Not attempting GUI login."
    if has_pkcs11_token; then
        echo "PKCS#11 slot present — signing is verified by the preflight before"
        echo "the build. Done!"
        exit 0
    fi
    echo "ERROR: no PKCS#11 token and no GUI/Accessibility to log in." >&2
    echo "Establish the SimplySign cloud session from an interactive session:" >&2
    echo "  make simplisign-login   (in a Terminal/desktop with Accessibility granted)" >&2
    exit 1
fi

# --- Interactive context below (Accessibility available) ---

# Ensure the app is running before we probe the menu bar (the cloud-session
# check reads it).
if ! pgrep -f "SimplySign Desktop" > /dev/null; then
    echo "SimplySign Desktop not running — starting it..."
    open "/Applications/SimplySign Desktop.app"
    sleep 3
fi

# Authoritative readiness: the cloud session is live (menu bar shows
# "Disconnect from cloud") AND the PKCS#11 slot is exposed.
if is_connected && has_pkcs11_token; then
    echo "SimplySign cloud session live and PKCS#11 token available. Done!"
    exit 0
fi

if has_pkcs11_token && ! is_connected; then
    echo "PKCS#11 slot is present but the SimplySign cloud session is DOWN"
    echo "(menu bar shows 'Connect with cloud'). The cached slot CANNOT sign —"
    echo "re-establishing the cloud session before proceeding."
else
    echo "SimplySign cloud session not ready. Attempting GUI login..."
fi

# GUI login requires SIMPLISIGN_TOTP_URI
if [ -z "$SIMPLISIGN_TOTP_URI" ]; then
    echo "ERROR: SimplySign cloud session is not live and SIMPLISIGN_TOTP_URI is not set." >&2
    echo "Reconnect manually: open 'SimplySign Desktop' -> 'Connect with cloud' + approve on phone," >&2
    echo "or set SIMPLISIGN_TOTP_URI to enable automated login." >&2
    exit 1
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

    # Wait for WebView form to be interactable.
    # Previous version matched on static text ("Login", "E-MAIL") which
    # appears as a page header before the <input> elements register with
    # the macOS Accessibility tree — fill_totp_and_login then raced and
    # saw zero fields. Wait for the actual AXTextField/AXSecureTextField
    # count instead.
    echo "Waiting for WebView form fields..."
    loaded=false
    for i in $(seq 1 30); do
        sleep 1
        n=$(count_login_fields)
        if [ "$n" -ge 1 ] 2>/dev/null; then
            loaded=true
            echo "WebView form ready ($n field(s) detected after ${i}s)."
            break
        fi
    done

    if [ "$loaded" = "false" ]; then
        echo "WebView didn't expose any form fields after 30s."
        echo "----- diagnostic: window list -----"
        osascript -e 'tell application "System Events" to tell process "SimplySign Desktop" to return name of every window' 2>&1 || true
        echo "----- diagnostic: visible static text -----"
        get_window_text 2>&1 || true
        echo "----- diagnostic: UI tree (role | description | value) -----"
        dump_ui_tree 2>&1 || true
        echo "----- diagnostic: screenshot -----"
        SHOT_PATH="${RUNNER_TEMP:-/tmp}/simplisign-attempt-${attempt}.png"
        if screencapture -x "$SHOT_PATH" 2>&1; then
            echo "screenshot saved: $SHOT_PATH ($(stat -f%z "$SHOT_PATH" 2>/dev/null) bytes)"
        else
            echo "screencapture failed"
        fi
        echo "----- end diagnostics -----"
        echo "Retrying..."
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
        echo "Form fill failed: $result"
        echo "----- UI tree dump (role | description | value) -----"
        dump_ui_tree
        echo "----- end dump -----"
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
