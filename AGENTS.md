# Repository Instructions

- Do not add legacy compatibility paths, fallback behavior, or silent reconstruction for old data unless the user explicitly asks for it. New code should fail or report the missing required state directly so the model and implementation stay simple and reviewable.
