package cli

import (
	"fmt"

	"github.com/bakadream/real-browser-cli/internal/version"
	"github.com/spf13/cobra"
)

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := fmt.Fprintf(output, "version: %s\n", version.String()); err != nil {
				return err
			}
			if version.Commit != "" {
				if _, err := fmt.Fprintf(output, "commit: %s\n", version.Commit); err != nil {
					return err
				}
			}
			if version.Date != "" {
				if _, err := fmt.Fprintf(output, "date: %s\n", version.Date); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
