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
		if len(args) == 0 {
			log.Fatal().Msg("at least one BMC hostname or IP must be provided")
		}

		store := secrets.NewStaticStore(username, password)
		params := &magellan.CollectParams{
			SecretStore: store,
		}

		if err := magellan.GatherFRUInventory(args, params); err != nil {
			log.Fatal().Err(err).Msg("failed to gather FRU inventory")
		}
	},
}

func init() {
	GatherCmd.Flags().StringVarP(&username, "username", "u", "", "Set the BMC username")
	GatherCmd.Flags().StringVarP(&password, "password", "p", "", "Set the BMC password")

	rootCmd.AddCommand(GatherCmd)
}
