---
id: boop.adr.authentication-identity
kind: adr
status: accepted
title: Separate authentication identity from user profile
scope:
  domains:
    - accounts
    - authentication
  paths:
    - lib/boop/accounts/**
    - assets/js/features/auth/**
symbols:
  - Boop.Accounts.User
  - Boop.Accounts.Identity
relations:
  supersedes:
    - boop.adr.password-only-auth
  informs:
    - boop.spec.registration
created: 2026-06-18
review_after: 2026-12-18
---

# Decision

Authentication identity is modeled separately from the user profile. A user may have more than one login method over time, and registration must attach each credential to the same account boundary.

# Consequences

Password, passkey, and future identity providers should be represented as authentication methods. Profile data should not become the credential authority.
