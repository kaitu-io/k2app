#!/usr/bin/env bash
# Authenticode description (osslsigncode -n / signtool /d) for the brand being
# built. Read from K2_BRAND, which the Makefile exports into every build recipe
# and Tauri passes through to bundle.windows.signCommand. Kaitu keeps the
# historical literal byte-for-byte; the certificate subject itself is neutral
# (Wordgate LLC) and shared by both brands.
case "${K2_BRAND:-kaitu}" in
  overleap) echo "Overleap Desktop" ;;
  *)        echo "Kaitu Desktop" ;;
esac
