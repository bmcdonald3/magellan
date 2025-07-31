package cmd

import (
	magellan "github.com/OpenCHAMI/magellan/pkg"
	"github.com/OpenCHAMI/magellan/pkg/secrets"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var GatherCmd = &cobra.Command{
	Use:     "gather [flags] <bmc_hostname_or_ip>...",
	Short:   "Gathers detailed FRU inventory from one or more BMCs",
	Long:    `Gathers detailed Field-Replaceable Unit (FRU) inventory (Processors, Memory, etc.) from a list of BMCs and outputs it in the SMD-compatible JSON format.`,
	Example: `  magellan gather 10.0.0.1 10.0.0.2 -u root -p calvin | magellan send`,
	Run: func(cmd *cobra.Command, args []string) {
		// 1. Check for hostnames in arguments
		if len(args) == 0 {
			log.Fatal().Msg("at least one BMC hostname or IP must be provided")
		}

		// 2. Set up parameters from flags
		// For the prototype, we assume static credentials from flags.
		store := secrets.NewStaticStore(username, password)
		params := &magellan.CollectParams{
			SecretStore: store,
			// You can add a flag for 'insecure' and pass it here if needed.
		}

		// 3. Call the core logic function
		if err := magellan.GatherFRUInventory(args, params); err != nil {
			log.Fatal().Err(err).Msg("failed to gather FRU inventory")
		}
	},
}

func init() {
	// Only define the flags necessary for the prototype
	GatherCmd.Flags().StringVarP(&username, "username", "u", "", "Set the BMC username")
	GatherCmd.Flags().StringVarP(&password, "password", "p", "", "Set the BMC password")
	// You might want to add an '--insecure' flag here as well for testing.

	rootCmd.AddCommand(GatherCmd)
}
