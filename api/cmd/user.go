package main

import (
	"context"
	"fmt"
	"strings"

	center "github.com/kaitu-io/k2app/api"
	"github.com/spf13/cobra"
	"github.com/wordgate/qtoolkit/util"
)

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage users",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// This will initialize viper from the config file
		util.SetConfigFile(configFile)
		// This will prompt for a password if not available and initialize the crypto key
		_ = getOrInitSecretKeyAndCA(true)
		return nil
	},
}

var userAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new user via --email flag",
	Run: func(cmd *cobra.Command, args []string) {
		email, _ := cmd.Flags().GetString("email")
		_, err := center.AddUser(context.Background(), email)
		if err != nil {
			fmt.Printf("Error adding user: %v\n", err)
			return
		}
		fmt.Printf("User with email %s added successfully.\n", email)
	},
}

var userSetAdminCmd = &cobra.Command{
	Use:   "set-admin",
	Short: "Grant admin privileges to a user via --email flag",
	Run: func(cmd *cobra.Command, args []string) {
		email, _ := cmd.Flags().GetString("email")
		err := center.SetUserAdminStatus(context.Background(), email, true)
		if err != nil {
			fmt.Printf("Error setting admin status: %v\n", err)
			return
		}
		fmt.Printf("User %s is now an admin.\n", email)
	},
}

var userDelAdminCmd = &cobra.Command{
	Use:   "del-admin",
	Short: "Revoke admin privileges from a user via --email flag",
	Run: func(cmd *cobra.Command, args []string) {
		email, _ := cmd.Flags().GetString("email")
		err := center.SetUserAdminStatus(context.Background(), email, false)
		if err != nil {
			fmt.Printf("Error revoking admin status: %v\n", err)
			return
		}
		fmt.Printf("Admin privileges revoked for user %s.\n", email)
	},
}

var userSetRetailerCmd = &cobra.Command{
	Use:   "set-retailer",
	Short: "Grant retailer privileges to a user via --email flag",
	Run: func(cmd *cobra.Command, args []string) {
		email, _ := cmd.Flags().GetString("email")
		err := center.SetUserRetailerStatus(context.Background(), email, true)
		if err != nil {
			fmt.Printf("Error setting retailer status: %v\n", err)
			return
		}
		fmt.Printf("User %s is now a retailer.\n", email)
	},
}

var userDelRetailerCmd = &cobra.Command{
	Use:   "del-retailer",
	Short: "Revoke retailer privileges from a user via --email flag",
	Run: func(cmd *cobra.Command, args []string) {
		email, _ := cmd.Flags().GetString("email")
		err := center.SetUserRetailerStatus(context.Background(), email, false)
		if err != nil {
			fmt.Printf("Error revoking retailer status: %v\n", err)
			return
		}
		fmt.Printf("Retailer privileges revoked for user %s.\n", email)
	},
}

var userSendEmailCmd = &cobra.Command{
	Use:   "send-email",
	Short: "Send an email to a user via --email flag",
	Long: `Send an email to a user. The email content supports multiline text.

Examples:
  # Simple message
  center user send-email --email user@example.com --subject "Test" --content "Hello World"

  # Multiline message (use shell quoting)
  center user send-email --email user@example.com --subject "Test" --content "Line 1
Line 2
Line 3"
`,
	Run: func(cmd *cobra.Command, args []string) {
		email, _ := cmd.Flags().GetString("email")
		subject, _ := cmd.Flags().GetString("subject")
		content, _ := cmd.Flags().GetString("content")

		if email == "" {
			fmt.Println("Error: email is required")
			return
		}
		if subject == "" {
			fmt.Println("Error: subject is required")
			return
		}
		if content == "" {
			fmt.Println("Error: content is required")
			return
		}

		// Convert \n in content to actual newlines (for shell escaping)
		content = strings.ReplaceAll(content, "\\n", "\n")

		// Convert plain text to HTML with line breaks
		htmlContent := convertTextToHTML(content)

		ctx := context.Background()
		err := center.SendSystemEmail(ctx, email, subject, htmlContent)

		if err != nil {
			fmt.Printf("Error sending email: %v\n", err)
			return
		}
		fmt.Printf("Email sent successfully to %s\n", email)
		fmt.Printf("Subject: %s\n", subject)
		fmt.Printf("Content preview: %s\n", truncateString(content, 50))
	},
}

// convertTextToHTML converts plain text to HTML with proper line breaks
func convertTextToHTML(text string) string {
	// Escape HTML special characters
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	text = strings.ReplaceAll(text, "\"", "&quot;")

	// Convert newlines to <br>
	text = strings.ReplaceAll(text, "\n", "<br>\n")

	return fmt.Sprintf(`<html><body><p>%s</p></body></html>`, text)
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func init() {
	// User commands
	userCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "./config.yml", "配置文件路径 (必填)")
	userCmd.MarkPersistentFlagRequired("config")

	// Add subcommand
	userAddCmd.Flags().String("email", "", "User's email address")
	userAddCmd.MarkFlagRequired("email")

	// Set-admin subcommand
	userSetAdminCmd.Flags().String("email", "", "User's email address")
	userSetAdminCmd.MarkFlagRequired("email")

	// Del-admin subcommand
	userDelAdminCmd.Flags().String("email", "", "User's email address")
	userDelAdminCmd.MarkFlagRequired("email")

	// Set-retailer subcommand
	userSetRetailerCmd.Flags().String("email", "", "User's email address")
	userSetRetailerCmd.MarkFlagRequired("email")

	// Del-retailer subcommand
	userDelRetailerCmd.Flags().String("email", "", "User's email address")
	userDelRetailerCmd.MarkFlagRequired("email")

	// Send-email subcommand
	userSendEmailCmd.Flags().String("email", "", "Recipient's email address (required)")
	userSendEmailCmd.Flags().String("subject", "", "Email subject (required)")
	userSendEmailCmd.Flags().String("content", "", "Email content (supports multiline, required)")
	userSendEmailCmd.MarkFlagRequired("email")
	userSendEmailCmd.MarkFlagRequired("subject")
	userSendEmailCmd.MarkFlagRequired("content")

	// Add all subcommands to userCmd
	userCmd.AddCommand(
		userAddCmd,
		userSetAdminCmd,
		userDelAdminCmd,
		userSetRetailerCmd,
		userDelRetailerCmd,
		userSendEmailCmd,
	)
}
