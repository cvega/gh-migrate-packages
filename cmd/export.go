package cmd

import (
    "os"
    "github.com/cvega/gh-migrate-packages/pkg/export"
    "github.com/spf13/cobra"
    "github.com/spf13/viper"
)

var exportCmd = &cobra.Command{
    Use:   "export",
    Short: "Creates CSV files of packages, versions, and their metadata in an organization",
    Long:  "Creates CSV files of packages, versions, and their metadata in an organization",
    Run: func(cmd *cobra.Command, args []string) {
        organization := cmd.Flag("organization").Value.String()
        token := cmd.Flag("token").Value.String()
        filePrefix := cmd.Flag("file-prefix").Value.String()
        ghHostname := cmd.Flag("hostname").Value.String()
        packageType := cmd.Flag("package-type").Value.String()

        if filePrefix == "" {
            filePrefix = organization
        }

        // Set ENV variables
        os.Setenv("GHMP_SOURCE_ORGANIZATION", organization)
        os.Setenv("GHMP_SOURCE_TOKEN", token)
        os.Setenv("GHMP_OUTPUT_FILE", filePrefix)
        os.Setenv("GHMP_SOURCE_HOSTNAME", ghHostname)
        os.Setenv("GHMP_PACKAGE_TYPE", packageType)

        // Bind ENV variables in Viper
        viper.BindEnv("SOURCE_ORGANIZATION")
        viper.BindEnv("SOURCE_TOKEN")
        viper.BindEnv("OUTPUT_FILE")
        viper.BindEnv("SOURCE_HOSTNAME")
        viper.BindEnv("PACKAGE_TYPE")

        export.CreateCSVs()
    },
}

func init() {
    rootCmd.AddCommand(exportCmd)

    exportCmd.Flags().StringP("organization", "o", "", "Organization to export packages from")
    exportCmd.MarkFlagRequired("organization")

    exportCmd.Flags().StringP("token", "t", "", "GitHub token")
    exportCmd.MarkFlagRequired("token")

    exportCmd.Flags().StringP("file-prefix", "f", "", "Output filenames prefix")
    exportCmd.Flags().StringP("hostname", "u", "", "GitHub Enterprise hostname url (optional)")
    exportCmd.Flags().StringP("package-type", "p", "", "Package type to export (container, npm, maven, nuget, rubygems)")
}
