# Client filesystem and terminals

These services protect client operations. They do **not** sandbox the harness
process or arbitrary commands it runs.

## Filesystem

- Maintain session-specific authorized roots and `os.Root`-based operation-time
  confinement. A lexical prefix check or prior symlink check is not a replacement.
- Preserve absolute-root validation, traversal/symlink protections, UTF-8 and
  size checks, and safe file replacement behavior.
- Do not broaden workspace access to fix a failing path. Additional roots must
  follow the existing explicit configuration and capability flow.
- Close directory handles on failed setup, session cleanup, and shutdown.

## Terminals

- Preserve session ownership checks, bounded UTF-8-safe output, cancellation,
  exit reporting, and process cleanup.
- Keep Unix/Windows process and filesystem behavior in the existing
  platform-specific files. Do not replace argument-based process launches with
  shell string concatenation.
- Permission and lifecycle changes must not leave orphaned processes or permit
  one session to access another session's terminals.

Run `go test -race ./internal/clientservices/...`. Use temporary roots and local
test processes; cover traversal, symlinks, session isolation, output limits, and
cleanup for relevant changes.
