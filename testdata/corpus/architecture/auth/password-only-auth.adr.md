---
id: boop.adr.password-only-auth
kind: adr
status: superseded
title: Password-only authentication
scope:
  domains:
    - accounts
    - authentication
  paths:
    - lib/boop/accounts/**
relations:
  superseded_by:
    - boop.adr.authentication-identity
created: 2026-01-10
---

# Decision

The original authentication model stored password login fields directly on the user record.

# Superseded Rationale

This was simple, but it did not leave room for passkeys or other credentials without overloading the profile model.
