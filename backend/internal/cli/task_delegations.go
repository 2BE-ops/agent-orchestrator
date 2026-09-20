package cli

import "github.com/spf13/cobra"

// newTaskDelegationCommands exposes immutable sealed-context receipts. A
// delegation proves what exact frozen context a native execution received.
func newTaskDelegationCommands(ctx *commandContext) []*cobra.Command {
	return newTaskAttemptReadCommands(ctx, "delegation", "delegations", "sealed context delegation", "number")
}
