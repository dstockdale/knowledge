---
id: boop.plan.passkeys
kind: plan
status: active
title: Add passkey registration
scope:
  domains:
    - accounts
    - authentication
    - registration
  paths:
    - lib/boop/accounts/**
    - assets/js/features/auth/**
symbols:
  - RegisterForm
  - PasskeyCredential
relations:
  implements:
    - boop.spec.registration#Authentication Methods
  informed_by:
    - boop.adr.authentication-identity#Decision
created: 2026-06-19
---

# Constraints

Passkey registration must use the authentication identity boundary. It should not write WebAuthn credential material onto profile fields.

# Tasks

- [ ] Add passkey credential creation to the registration flow.
- [ ] Update `RegisterForm` to expose passkey setup.
- [ ] Validate that password registration still works.
