#!/bin/sh
# Enterprise multi-slot UAT: if the mocksubs fixtures are present on the
# shared /etc/k2 volume, trust the mocksubs cert (it's a self-signed leaf
# used directly as its own trust anchor — the same pattern Go's own
# httptest.NewTLSServer relies on) and start the mock /api/subs server on
# localhost before k2r itself. Every other UAT phase has no mocksubs-*
# files and this is a no-op — consumer-mode k2r behavior is unchanged.
set -e

if [ -f /etc/k2/mocksubs.crt ]; then
    cp /etc/k2/mocksubs.crt /usr/local/share/ca-certificates/mocksubs.crt
    update-ca-certificates >/dev/null
fi

if [ -f /etc/k2/mocksubs-response.json ]; then
    mocksubs -cert /etc/k2/mocksubs.crt -key /etc/k2/mocksubs.key \
        -json /etc/k2/mocksubs-response.json -listen 127.0.0.1:8443 &
fi

exec k2r "$@"
