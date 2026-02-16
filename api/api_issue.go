package center

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// setAppUserIDHeader returns a middleware that sets X-App-User-ID header
// from the authenticated user ID for qtoolkit/github/issue handlers.
func setAppUserIDHeader() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := ReqUserID(c)
		if userID != 0 {
			c.Request.Header.Set("X-App-User-ID", fmt.Sprintf("%d", userID))
		}
		c.Next()
	}
}

// getGitHubIssueURL returns the GitHub issue URL for a given issue number
func getGitHubIssueURL(issueNumber int) string {
	owner := viper.GetString("github.owner")
	repo := viper.GetString("github.repo")
	return fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, issueNumber)
}
