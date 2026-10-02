# ACP boundary

## Navigation

- `client.go`: process/connection lifetime and adapter session operations.
- `callbacks.go`: agent requests for client services and user interaction.
- `normalize.go`: ACP data to protocol-independent harness events.
- `client_test.go`, `services_test.go`: round trips and service behavior.

## Invariants

- Keep `github.com/Tangerg/acp` types here, in the demo transport, or in the
  client-service integration—not in the app or TUI contracts.
- Negotiate and check capabilities. Unsupported operations should remain
  distinguishable from authentication failures and transport errors.
- Keep session bookkeeping synchronized. Clean up filesystem/terminal resources
  and connection lifetimes on errors and shutdown.
- Preserve message IDs when supplied, replay semantics, structured tool data,
  and offered permission option IDs. Do not invent permission outcomes or
  silently approve requests.
- Distinguish cancellation from rewind. Stable ACP support in this client does
  not imply message editing, context truncation, or file rollback support.
- Implement native extensions only after verifying the installed bridge/API
  contract and its negotiation. Target the connected harness instance; do not
  accidentally use a separate background service.

## Testing

Run `go test -race ./internal/acpclient/...`; prefer in-memory ACP agents and
fake services. Test cancellation while permissions are pending, configuration,
replay, and resource cleanup when touching those flows.

Live harness tests use `OPENCODE_INTEGRATION` or `CLAUDE_INTEGRATION` and are
disabled by default. Leave them disabled unless the user authorizes real-agent
integration testing.
