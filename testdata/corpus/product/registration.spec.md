---
id: boop.spec.registration
kind: spec
status: current
title: Registration flow
scope:
  domains:
    - accounts
    - registration
  paths:
    - lib/boop/accounts/**
    - assets/js/features/auth/**
symbols:
  - RegisterForm
  - RegistrationController
relations:
  informed_by:
    - boop.adr.authentication-identity
created: 2026-06-18
---

# Authentication Methods

The registration flow can offer multiple authentication methods. Each method must create or attach an authentication identity and then complete the same user profile setup.

# UI Requirements

The `RegisterForm` must keep credential-specific fields separate from profile fields so that passkeys can be added without reshaping the whole flow.
