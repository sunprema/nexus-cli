// Package cli implements Nexus's command surface: the bicameral
// Code/Explainer branch workflow (see the Project Nexus PRD and
// implementation plan this repo builds toward).
package cli

import (
	"github.com/spf13/cobra"
)

// NewRootCmd builds the 'nexus' root command.
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "nexus",
		Short: "Bicameral Code/Explainer branch workflow",
		Long: `Nexus decouples machine-generated code from human-readable intent.

An 'explainer' branch mirrors the repository's file structure with short,
jargon-free Markdown narrating why each change was made. 'main' is always
the source of truth: nothing here rewrites main to match the explainer.`,
		SilenceErrors: true,
	}
	cmd.AddCommand(newNexusInitCmd())
	cmd.AddCommand(newNexusSyncCmd())
	cmd.AddCommand(newNexusNarratedCmd())
	cmd.AddCommand(newNexusCheckCmd())
	cmd.AddCommand(newNexusShowCmd())
	cmd.AddCommand(newNexusDiffCmd())
	cmd.AddCommand(newNexusMapCmd())
	cmd.AddCommand(newNexusTourCmd())
	cmd.AddCommand(newNexusPostCommitHookCmd())
	return cmd
}
