#!/bin/sh
# Generates a developer CA, a server certificate (with localhost SANs), and a
# client certificate / PKCS#12 bundle. Outputs land under /pki:
#
#   /pki/ca/ca.crt              - root CA (import into trusted roots if desired)
#   /pki/server/server.crt|key  - mTLS server material consumed by porcelain
#   /pki/server/ca.crt          - client trust roots used by porcelain
#   /pki/client/client.crt|key  - developer client cert
#   /pki/client/client.p12      - browser-importable bundle (password: porcelain)
#
# Intended for local UI smoke tests only — never ship this material.

set -eu

DAYS=${PORCELAIN_DEV_PKI_DAYS:-30}
CN_CA=${PORCELAIN_DEV_PKI_CA_CN:-Porcelain Dev CA}
CN_SERVER=${PORCELAIN_DEV_PKI_SERVER_CN:-localhost}
CN_CLIENT=${PORCELAIN_DEV_PKI_CLIENT_CN:-developer@porcelain.local}
P12_PASSWORD=${PORCELAIN_DEV_PKI_P12_PASSWORD:-porcelain}

mkdir -p /pki/ca /pki/server /pki/client

# Root CA -----------------------------------------------------------------
openssl genrsa -out /pki/ca/ca.key 4096 >/dev/null 2>&1
openssl req -x509 -new -key /pki/ca/ca.key -sha256 -days "$DAYS" \
    -subj "/CN=$CN_CA" -out /pki/ca/ca.crt

# Server cert -------------------------------------------------------------
cat > /tmp/server.cnf <<EOF
[req]
default_bits       = 2048
prompt             = no
default_md         = sha256
distinguished_name = dn
req_extensions     = v3_req
[dn]
CN = $CN_SERVER
[v3_req]
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt
[alt]
DNS.1 = localhost
DNS.2 = porcelain.local
IP.1  = 127.0.0.1
IP.2  = ::1
EOF

openssl genrsa -out /pki/server/server.key 2048 >/dev/null 2>&1
openssl req -new -key /pki/server/server.key -out /tmp/server.csr -config /tmp/server.cnf
openssl x509 -req -in /tmp/server.csr \
    -CA /pki/ca/ca.crt -CAkey /pki/ca/ca.key -CAcreateserial \
    -days "$DAYS" -sha256 \
    -extensions v3_req -extfile /tmp/server.cnf \
    -out /pki/server/server.crt

cp /pki/ca/ca.crt /pki/server/ca.crt

# Client cert -------------------------------------------------------------
cat > /tmp/client.cnf <<EOF
[req]
default_bits       = 2048
prompt             = no
default_md         = sha256
distinguished_name = dn
req_extensions     = v3_req
[dn]
CN = $CN_CLIENT
[v3_req]
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
EOF

openssl genrsa -out /pki/client/client.key 2048 >/dev/null 2>&1
openssl req -new -key /pki/client/client.key -out /tmp/client.csr -config /tmp/client.cnf
openssl x509 -req -in /tmp/client.csr \
    -CA /pki/ca/ca.crt -CAkey /pki/ca/ca.key -CAcreateserial \
    -days "$DAYS" -sha256 \
    -extensions v3_req -extfile /tmp/client.cnf \
    -out /pki/client/client.crt

# NOTE: -legacy keeps the bundle on RC2/3DES + SHA-1 MAC so macOS `security
# import` and Windows CryptoAPI accept it. OpenSSL 3 defaults (PBES2/AES-256 +
# SHA-256 MAC) decode fine with `openssl` and curl, but macOS Keychain rejects
# them with "MAC verification failed".
openssl pkcs12 -export -legacy \
    -inkey /pki/client/client.key \
    -in /pki/client/client.crt \
    -certfile /pki/ca/ca.crt \
    -name "$CN_CLIENT" \
    -passout "pass:$P12_PASSWORD" \
    -out /pki/client/client.p12

# Normalize permissions so the runtime image can read them as nonroot.
chmod -R a+rX /pki

echo "Developer PKI generated:"
echo "  CA:           /pki/ca/ca.crt"
echo "  Server cert:  /pki/server/server.crt"
echo "  Server key:   /pki/server/server.key"
echo "  Client cert:  /pki/client/client.crt"
echo "  Client p12:   /pki/client/client.p12 (password: $P12_PASSWORD)"
