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
published checksums, so a workflow using the action inherits the same guarantee
without running cosign itself.
