#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
LAB="$PWD"
export PATH="$LAB/sdk/tools/usr/bin:$PATH"
export LD_LIBRARY_PATH="$LAB/out/lib:$LAB/sdk/tools/usr/lib/x86_64-linux-gnu"
export PKG_CONFIG_PATH="$LAB/out/lib/pkgconfig"
export TMPDIR="$LAB/tmp"
export XDG_CONFIG_HOME="$LAB/tmp/config"
export XDG_CACHE_HOME="$LAB/tmp/cache"
export XDG_DATA_HOME="$LAB/tmp/data"
export HOME="$LAB/tmp/home"
# Simulate an inherited proxy even on clean shells; the non-networking hook must see the approved endpoint.
export HTTPS_PROXY=http://127.0.0.1:9 https_proxy=http://127.0.0.1:9
mkdir -p "$TMPDIR" "$HOME" "$LAB/tmp/fixtures"
[ "$(pkg-config --modversion freerdp3)" = 3.33.1 ]
cc -std=gnu11 -Werror -Wno-deprecated-declarations -Wall -Wextra probe/probe.c $(pkg-config --cflags --libs freerdp3 openssl) -o tmp/rdp-probe
cd tmp/fixtures
openssl req -x509 -newkey rsa:2048 -nodes -keyout root.key -out root.pem -days 1 -subj '/CN=Fixture CA'   -addext 'basicConstraints=critical,CA:TRUE' -addext 'keyUsage=critical,keyCertSign,cRLSign' >/dev/null 2>&1
openssl req -new -newkey rsa:2048 -nodes -keyout server.key -out server.csr -subj '/CN=Not accepted as a CN fallback' >/dev/null 2>&1
printf 'basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\nsubjectAltName=DNS:server-fixture.example.test\n' >server.ext
openssl x509 -req -in server.csr -CA root.pem -CAkey root.key -CAcreateserial -out leaf.pem -days 1 -extfile server.ext >/dev/null 2>&1
../rdp-probe root.pem leaf.pem
