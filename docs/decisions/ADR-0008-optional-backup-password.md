# ADR-0008 — Optional backup password via standard age encryption

Status: accepted · Date: 2026-08-29

## Context

Backups may leave the server (Telegram delivery, manual download), so protection is desirable —
but the product also demands a simple default backup experience, and no custom cryptography is
acceptable.

## Decision

Archives are plain `tar.gz` by default. If the administrator sets the **single backup
password** (settable once from installer/CLI/panel, changeable later, stored encrypted at
rest), archives are additionally encrypted with **age** (age-encryption.org/v1, scrypt
passphrase recipient) via `filippo.io/age`. Restore asks for a password only for age-encrypted
archives.

## Consequences

- One password, one standard format, no key ceremony; scheduled backups work unattended using
  the stored password.
- Changing the password affects only new archives; old archives remain decryptable with their
  password.
- Plaintext archives are an explicit, documented choice (they never leave the box unless the
  Telegram sink or manual download is used — the UI warns when delivering unencrypted).

## Alternatives rejected

- Mandatory encryption: violates the "simple backup experience" product direction.
- Custom AEAD container: reinventing cryptography — forbidden.
- age identity files: an extra artifact to lose; a single password is the simpler product
  answer.
