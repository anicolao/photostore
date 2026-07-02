# Repository Instructions

- Do not add legacy compatibility paths, fallback behavior, or silent reconstruction for old data unless the user explicitly asks for it. New code should fail or report the missing required state directly so the model and implementation stay simple and reviewable.
- Prefer tracer-bullet implementation for every feature: build a visible end-to-end path from UI to backend, backed by E2E tests that validate the feature. It is better to partially implement a feature end-to-end with visibility and observability than to complete an isolated layer that cannot be exercised through the product.
