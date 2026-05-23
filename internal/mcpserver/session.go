package mcpserver

// EphemeralCtxKey is the context key type used to signal that the current HTTP
// request is an ephemeral (stateless) MCP session — one where the client did
// not supply an Mcp-Session-Id header.
//
// Usage:
//
//	// In internal/cli/http.go, when no header is present:
//	ctx = context.WithValue(ctx, mcpserver.EphemeralCtxKey{}, true)
//
//	// In internal/mcpserver/tools.go, in the search handler:
//	ephemeral, _ := ctx.Value(mcpserver.EphemeralCtxKey{}).(bool)
//
// The context value is set at HTTP dispatch time (before HandleMessage) and
// read inside the tool handler. It is never set for stdio mode or for requests
// that carry a client-supplied session header — both of those want persistent
// once-per-session semantics.
type EphemeralCtxKey struct{}
