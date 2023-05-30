package commands

import (
	"github.com/cirruslabs/core-services/pkg/commands/pubsub"
	"github.com/spf13/cobra"
)

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cs",
		Short: "Core Services",
	}

	cmd.AddCommand(pubsub.NewCommand())

	return cmd
}
