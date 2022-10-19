package commands

import "github.com/spf13/cobra"

var Root = New()

func New() *cobra.Command {
	root := &cobra.Command{
		Use:               "dag",
		SilenceUsage:      true, // Don't show usage on errors
		DisableAutoGenTag: true,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}
	root.AddCommand(cmdSVG(), cmdText())
	return root
}
