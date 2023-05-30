package pubsub

import "github.com/spf13/cobra"

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pubsub",
		Short: "Publish/subscribe service adapter",
	}

	cmd.AddCommand(newRunCommand())

	return cmd
}
