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

### macOS

Safari and Chrome read the login keychain — Firefox keeps its own NSS database.

```sh
# Pull the bundle and the CA out of the container.
docker compose cp porcelain:/pki/client/client.p12 ./client.p12
docker compose cp porcelain:/pki/ca/ca.crt ./porcelain-dev-ca.crt

# Import the client identity into the login keychain (Safari/Chrome).
security import ./client.p12 \
  -k ~/Library/Keychains/login.keychain-db \
  -P porcelain \
  -T /Applications/Safari.app \
  -T "/Applications/Google Chrome.app"
```

> **macOS gotcha:** if `security import` fails with `MAC verification failed
> during PKCS12 import (wrong password?)`, your `client.p12` was produced with
> OpenSSL 3 defaults (PBES2/AES-256 + SHA-256 MAC) which macOS Keychain still
> rejects. The bundled `docker/generate-dev-pki.sh` now emits `-legacy` p12s
> automatically — pull a fresh one (`docker compose build --no-cache &&
> docker compose cp ...`). To re-encode an existing `client.p12` in place
> without rebuilding:
>
> ```sh
> openssl pkcs12 -in ./client.p12 -passin pass:porcelain -nodes -out /tmp/c.pem
> openssl pkcs12 -export -legacy -in /tmp/c.pem -passout pass:porcelain \
>   -name porcelain-dev-client -out ./client.p12
> rm /tmp/c.pem
> ```

```sh
# Trust the dev CA system-wide so the address bar stops warning.
sudo security add-trusted-cert -d -r trustRoot \
  -k /Library/Keychains/System.keychain ./porcelain-dev-ca.crt
```

For Firefox: *Settings → Privacy & Security → Certificates → View Certificates*,
import `client.p12` under *Your Certificates* and `porcelain-dev-ca.crt` under
*Authorities* (check "Trust this CA to identify websites").

For curl / scripted access:

```sh
curl --cert-type P12 --cert ./client.p12:porcelain \
     --cacert ./porcelain-dev-ca.crt \
     https://localhost:8443/healthz
```

To remove later, open *Keychain Access*, delete the `porcelain-dev-client`
identity from *login*, and the dev CA from *System*.

## Regenerate developer PKI

```sh
docker compose build --no-cache
```

This re-runs `docker/generate-dev-pki.sh` inside the build pipeline, producing
a fresh CA, server certificate, and client bundle. **Never deploy these
artefacts** — they are intended for local UI smoke tests only.
