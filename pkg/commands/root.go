package commands

import "github.com/spf13/cobra"

var Root = New()

func New() *cobra.Command {
	root := &cobra.Command{
		Use:               "dag",
		SilenceUsage:      true, // Don't show usage on errors
		SilenceErrors:     true, // Don't show errors on errors
		DisableAutoGenTag: true,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}
	root.AddCommand(
		cmdSVG(),
		cmdText(),
		cmdPod(),
		cmdCache(),
	)
	return root
}
