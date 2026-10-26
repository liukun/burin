#!/bin/sh
# Creates a self-signed code-signing certificate in the login keychain, so
# that TCC permissions (Accessibility, Input Monitoring) survive rebuilds.
# Ad-hoc signed apps are identified by their hash, which every build
# changes. Idempotent: an existing identity is reported and kept.
#
# Usage: scripts/signing-cert.sh [name]   (default name: Burin Signing)
set -eu

name=${1:-"Burin Signing"}
keychain="$HOME/Library/Keychains/login.keychain-db"

if security find-identity -v -p codesigning | grep -q "\"$name\""; then
	echo "identity \"$name\" already exists:"
	security find-identity -v -p codesigning | grep "\"$name\""
	exit 0
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

cat > "$tmp/cert.conf" <<CONF
[req]
distinguished_name = dn
x509_extensions = ext
prompt = no
[dn]
CN = $name
[ext]
keyUsage = critical, digitalSignature
extendedKeyUsage = critical, codeSigning
basicConstraints = critical, CA:false
CONF

echo "generating certificate \"$name\" (valid 10 years)"
# The key must be in the traditional RSA PEM format: the security tool reads
# neither PKCS8 keys nor the PKCS12 files that OpenSSL 3+ writes by default.
openssl genrsa -traditional -out "$tmp/key.pem" 2048 2>/dev/null
openssl req -x509 -new -key "$tmp/key.pem" -days 3650 \
	-config "$tmp/cert.conf" -out "$tmp/cert.pem" 2>/dev/null

echo "importing into login keychain (codesign may access the key without asking)"
security import "$tmp/key.pem" -k "$keychain" -t priv -f openssl -T /usr/bin/codesign >/dev/null
security import "$tmp/cert.pem" -k "$keychain" -t cert -f pemseq >/dev/null

echo "trusting the certificate for code signing (may ask for your login password)"
security add-trusted-cert -p codeSign -k "$keychain" "$tmp/cert.pem"

if ! security find-identity -v -p codesigning | grep -q "\"$name\""; then
	echo "identity \"$name\" not found after import" >&2
	exit 1
fi
security find-identity -v -p codesigning | grep "\"$name\""

cat <<MSG

Done. Next:
  1. Set "macos": { "signingIdentity": "$name" } in mygo.json (go tool mygo build),
     and pass -sign "$name" to go tool mygo dev.
  2. Clear the stale TCC entry once, then grant again from the tray menu:
       tccutil reset Accessibility io.github.liukun.burin
MSG
