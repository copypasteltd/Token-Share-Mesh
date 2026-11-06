# Synthetic rental test support

Shared test-only fixtures for packages on both sides of Go's `client/internal`
boundary. `client/internal/rentaltest` retains a compatible facade for existing
test imports. Production source must not import this package.

Fixtures create synthetic original identities, signed claims, prepared manifests
and optional native metadata when tests are eventually executed. Merely writing
these helpers executes nothing. They never establish GPU/engine readiness,
physical cleanup, SDK interoperability, content confidentiality, payment, usage
or billable rental hours. Current new definitions are uncompiled/unexecuted.
