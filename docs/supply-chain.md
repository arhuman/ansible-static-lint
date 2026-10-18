# Verifying a downloaded release

Releases ship `checksums.txt` signed with cosign in keyless mode. The checksum
alone only proves the archive arrived intact, since it travels from the same
release; the signature is what ties it to this repository's release workflow:

```sh
cosign verify-blob checksums.txt \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/arhuman/ansible-static-lint/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

The bundle carries the signature and the certificate together. Releases up to
v0.1.0 shipped them as separate `checksums.txt.sig` and `checksums.txt.pem`
files; verify those with `--signature` and `--certificate` instead.

Once the checksum file is trusted, verify the archive against it:

```sh
sha256sum --check --ignore-missing checksums.txt
```

Each archive also ships an SBOM alongside it as
`<archive>.sbom.json`.

The GitHub Action downloads a release binary and checks it against these
published checksums. That catches a corrupted download but not a tampered
release: the action does not run cosign, so provenance needs the bundle check
above.

## Homebrew cask

`brew install --cask arhuman/tap/ansible-static-lint` downloads the same
release archive as a manual download, and Homebrew checks it against the
SHA-256 the cask records. That checksum is written by goreleaser from the
archive it built, so the cask cannot drift from the release it names.

What this does not do is verify the cosign signature: Homebrew checks the
digest, not the provenance. To verify provenance, follow the archive checks
above on the release the cask points at.

The cask also clears the macOS quarantine attribute on install. astl's
archives are cosign-signed but not Apple-notarized, and Gatekeeper checks
notarization, so without that step a downloaded binary refuses to run. It
weakens a macOS protection deliberately; notarization is the real fix and is
not in place today.

## PyPI wheels

The wheels on PyPI are packed from these same archives, not rebuilt. The
release workflow runs the two verifications above before packing anything, so
the binary inside a wheel is byte-identical to the one covered by the signature
you can check here. Each wheel also carries the archive's SBOM at
`<dist-info>/sboms/astl.spdx.json`.

Wheels are uploaded through PyPI trusted publishing and carry PEP 740
attestations, which record that this repository's `release.yml` built them.
Note what each layer proves: the attestation binds the upload to a workflow,
while the cosign signature above is what binds the binary to its build. PyPI
verifies the first for you; the second is the check documented here.

```sh
pip download --no-deps ansible-static-lint
```

To confirm a wheel's binary against a release, unzip it and compare the
`.data/scripts/astl` member's SHA-256 with the one in the archive named by
`checksums.txt` for your platform.
