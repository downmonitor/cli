package cli

import "github.com/spf13/cobra"

func newCompletionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion <shell>",
		Short: "Generate shell completions for bash, zsh, fish or PowerShell",
		Long:  "Print a completion script to load from your shell profile.",
		Example: `  echo 'source <(downmonitor completion zsh)' >> ~/.zshrc
  downmonitor completion bash > /etc/bash_completion.d/downmonitor
  downmonitor completion fish > ~/.config/fish/completions/downmonitor.fish`,
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletionV2(cmd.OutOrStdout(), true)
			case "zsh":
				return cmd.Root().GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return cmd.Root().GenFishCompletion(cmd.OutOrStdout(), true)
			default:
				return cmd.Root().GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
			}
		},
	}

	return cmd
}
