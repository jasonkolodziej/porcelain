# Local UI Preview

The repository ships a self-contained Docker workflow so the dashboard can be
exercised in a real browser **without bypassing mTLS**. The image bakes a
developer CA, a server certificate, and a client certificate into `/pki`.

## Build and run

```sh
cd porcelain
docker compose up --build
```

The daemon listens on `https://localhost:8443` with mandatory client
certificate verification.

## Import the developer client certificate

Copy the PKCS#12 bundle out of the running container and import it into the
browser keychain:

```sh
docker compose cp porcelain:/pki/client/client.p12 ./client.p12
# Password: porcelain
```

Once the certificate is imported and selected for `localhost`, the dashboard
loads and the "Peer CN" badge shows the verified subject.

The CA certificate (`/pki/ca/ca.crt`) can be imported into the browser's
trusted roots if you would prefer the address bar to stop warning about an
unknown server certificate. Both flows still require a valid client cert.

## Regenerate developer PKI

```sh
docker compose build --no-cache
```

This re-runs `docker/generate-dev-pki.sh` inside the build pipeline, producing
a fresh CA, server certificate, and client bundle. **Never deploy these
artefacts** — they are intended for local UI smoke tests only.
