package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/wordgate/qtoolkit/unred"
	"github.com/wordgate/qtoolkit/util"
)

var unredCmd = &cobra.Command{
	Use:   "unred",
	Short: "Manage unred short links",
}

var unredCreateCmd = &cobra.Command{
	Use:   "create <path> <target_url> [expire_unix]",
	Short: "Create a short link",
	Long:  `Create an unred short link. expire_unix is optional (0 or omit = no expiry).`,
	Args:  cobra.RangeArgs(2, 3),
	Run: func(cmd *cobra.Command, args []string) {
		util.SetConfigFile(configFile)

		path := args[0]
		targetURL := args[1]
		var expireAt int64
		if len(args) == 3 {
			if _, err := fmt.Sscanf(args[2], "%d", &expireAt); err != nil {
				fmt.Fprintf(os.Stderr, "invalid expire_unix: %s\n", args[2])
				os.Exit(1)
			}
		}

		resp, err := unred.CreateLink(path, targetURL, expireAt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if !resp.Success {
			fmt.Fprintf(os.Stderr, "API error: %s\n", resp.Message)
			os.Exit(1)
		}

		fmt.Println(resp.URL)
	},
}

var unredDeleteCmd = &cobra.Command{
	Use:   "delete <path>",
	Short: "Delete a short link",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		util.SetConfigFile(configFile)

		resp, err := unred.DeleteLink(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if !resp.Success {
			fmt.Fprintf(os.Stderr, "API error: %s\n", resp.Message)
			os.Exit(1)
		}

		fmt.Println("deleted")
	},
}

func init() {
	unredCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "./config.yml", "config file path")
	unredCmd.AddCommand(unredCreateCmd)
	unredCmd.AddCommand(unredDeleteCmd)
	rootCmd.AddCommand(unredCmd)
}
