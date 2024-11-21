package cmd

import (
    "os"
    "github.com/spf13/cobra"
    "github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
    Use:   "migrate-packages",
    Short: "gh cli extension to assist in the migration of packages between GitHub organizations",
    Long:  `gh cli extension to assist in the migration of packages between GitHub organizations and enterprises`,
}

func Execute() {
    err := rootCmd.Execute()
    if err != nil {
        os.Exit(1)
    }
}

func init() {
    cobra.OnInitialize(initConfig)
}

func initConfig() {
    viper.SetEnvPrefix("GHMP")
    viper.AutomaticEnv()
}
