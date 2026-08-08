# Native releases

Atlas publishes five portable CLI archives and one desktop package per OS.
The desktop packages own `.atlas` consistently:

| OS | Package | Native registration |
|---|---|---|
| macOS | `Atlas-<version>-darwin-arm64.zip` | exported UTI `dev.felinestatemachine.atlas.volume` |
| Linux | `Atlas-<version>-linux-amd64.deb` | shared MIME `application/vnd.felinestatemachine.atlas` |
| Windows | `Atlas-<version>-windows-amd64-setup.exe` | `Atlas.Volume` per-user file association |

The metadata lives in `packaging/`; release scripts consume those files rather
than reproducing them in workflow YAML. `make release-packaging` parses and
checks that contract locally.

## Signing inputs

Unsigned workflow-dispatch builds exercise the same package recipes. A tagged
release becomes signed when the corresponding GitHub Actions secrets exist:

| Secret | Purpose |
|---|---|
| `MACOS_CERTIFICATE_P12_BASE64` | base64 Developer ID Application certificate |
| `MACOS_CERTIFICATE_PASSWORD` | certificate password |
| `MACOS_SIGNING_IDENTITY` | exact `codesign` identity |
| `MACOS_NOTARY_KEY_BASE64` | base64 App Store Connect `.p8` key |
| `MACOS_NOTARY_KEY_ID` | App Store Connect key ID |
| `MACOS_NOTARY_ISSUER_ID` | App Store Connect issuer UUID |
| `WINDOWS_CERTIFICATE_BASE64` | base64 Authenticode `.pfx` |
| `WINDOWS_CERTIFICATE_PASSWORD` | certificate password |

macOS signing uses hardened runtime and timestamping, submits with
`notarytool`, staples the accepted ticket, and recreates the final ZIP. Windows
signs and verifies both `Atlas.exe` and the installer with SHA-256 and a public
timestamp service. No key material is written to the repository or uploaded as
an artifact.

## Publication transaction

Tag publication is deliberately one-way:

1. required unit, compatibility-corpus, browser, and packaging gates pass;
2. all platform jobs produce the exact eight allowed assets;
3. packaged smoke builds Sample Region online and cache-only, requires
   byte-identical `.atlas` output, and reads the result through `measure`;
4. `SHA256SUMS` is generated and verified over the allowlist;
5. all nine files upload to a draft release;
6. remote names must exactly equal local names and every GitHub asset digest
   must equal its local SHA-256 before the draft becomes public.

An upload or comparison failure leaves a non-public draft for diagnosis; a
partial public release is never produced.
