# Release validation

Date: 2026-10-14. Checks are executed in an isolated working checkout. Screenshots use local synthetic/demo data. A source implementation, a passing automated test and external-service acceptance are separate claims.

## Commands

```bash
make verify
```

PASS: make verify including race tests, vet, govulncheck, frontend tests/build, three protocol-fixture E2E tests, production dependency audit and production-placeholder checks. Browser opened first-run setup. No real cloud-provider or signed desktop acceptance is claimed. Hardware, external credentials, signing and production deployment are separate setup/acceptance steps.
