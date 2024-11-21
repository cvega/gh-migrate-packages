package cmd

import (
    "os"
    "github.com/cvega/gh-migrate-packages/pkg/sync"
    "github.com/spf13/cobra"
    "github.com/spf13/viper"
)

var syncCmd = &cobra.Command{
    Use:   "sync",
    Short: "Migrates packages from a source organization to a target organization",
    Long:  "Migrates packages, versions, and metadata from a source organization to a target organization",
    Run: func(cmd *cobra.Command, args []string) {
        sourceOrg := cmd.Flag("source-organization").Value.String()
        targetOrg := cmd.Flag("target-organization").Value.String()
        sourceToken := cmd.Flag("source-token").Value.String()
        targetToken := cmd.Flag("target-token").Value.String()
        mappingFile := cmd.Flag("mapping-file").Value.String()
        ghHostname := cmd.Flag("source-hostname").Value.String()
        packageType := cmd.Flag("package-type").Value.String()
        skipExisting := cmd.Flag("skip-existing").Value.String()

        // Set ENV variables
        os.Setenv("GHMP_SOURCE_ORGANIZATION", sourceOrg)
        os.Setenv("GHMP_TARGET_ORGANIZATION", targetOrg)
        os.Setenv("GHMP_SOURCE_TOKEN", sourceToken)
        os.Setenv("GHMP_TARGET_TOKEN", targetToken)
        os.Setenv("GHMP_MAPPING_FILE", mappingFile)
        os.Setenv("GHMP_SOURCE_HOSTNAME", ghHostname)
        os.Setenv("GHMP_PACKAGE_TYPE", packageType)
        os.Setenv("GHMP_SKIP_EXISTING", skipExisting)

        // Bind ENV variables in Viper
        viper.BindEnv("SOURCE_ORGANIZATION")
        viper.BindEnv("TARGET_ORGANIZATION")
        viper.BindEnv("SOURCE_TOKEN")
        viper.BindEnv("TARGET_TOKEN")
        viper.BindEnv("MAPPING_FILE")
        viper.BindEnv("SOURCE_HOSTNAME")
        viper.BindEnv("PACKAGE_TYPE")
        viper.BindEnv("SKIP_EXISTING")

        sync.SyncPackages()
    },
}

func init() {
    rootCmd.AddCommand(syncCmd)

    syncCmd.Flags().StringP("source-organization", "s", "", "Source Organization to sync packages from")
    syncCmd.MarkFlagRequired("source-organization")

    syncCmd.Flags().StringP("target-organization", "t", "", "Target Organization to sync packages to")
    syncCmd.MarkFlagRequired("target-organization")

    syncCmd.Flags().StringP("source-token", "a", "", "Source Organization GitHub token")
    syncCmd.MarkFlagRequired("source-token")

    syncCmd.Flags().StringP("target-token", "b", "", "Target Organization GitHub token")
    syncCmd.MarkFlagRequired("target-token")

    syncCmd.Flags().StringP("mapping-file", "m", "", "Mapping file path for package name mappings")
    syncCmd.Flags().StringP("source-hostname", "u", "", "GitHub Enterprise source hostname url (optional)")
    syncCmd.Flags().StringP("package-type", "p", "", "Package type to sync (container, npm, maven, nuget, rubygems)")
    syncCmd.Flags().BoolP("skip-existing", "k", false, "Skip existing packages to save API requests")
}
