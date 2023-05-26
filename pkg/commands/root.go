package commands

import (
	"github.com/cirruslabs/cirrus-backbone-services/pkg/commands/pubsub"
	"github.com/spf13/cobra"
)

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cbs",
		Short: "Cirrus Backbone Services",
	}

	cmd.AddCommand(pubsub.NewCommand())

	return cmd
}
