package center

import (
	"github.com/gin-gonic/gin"
)

type myPermissionsResponse struct {
	IsAdmin bool     `json:"is_admin"`
	Roles   uint64   `json:"roles"`
	Groups  []string `json:"groups"`
}

// allGroups is the complete set of permission groups available to superadmins.
var allGroups = []string{
	"nodes", "nodes.write",
	"tunnels", "tunnels.write",
	"cloud", "cloud.write",
	"users", "users.write",
	"orders",
	"campaigns", "campaigns.write",
	"license_keys", "license_keys.write",
	"plans", "plans.write",
	"announcements", "announcements.write",
	"stats",
	"device_logs",
	"feedback_tickets", "feedback_tickets.write",
	"retailers", "retailers.write",
	"edm",
	"approvals", "approvals.write",
	"wallet", "wallet.write",
	"strategy", "strategy.write",
	"surveys",
	"admins",
}

// roleGroupMap maps each role bitmask to its permitted groups.
var roleGroupMap = map[uint64][]string{
	RoleDevopsViewer: {"nodes", "tunnels", "cloud", "users", "orders", "device_logs", "feedback_tickets", "stats"},
	RoleDevopsEditor: {"nodes", "nodes.write", "tunnels", "tunnels.write", "cloud", "cloud.write", "users", "orders", "device_logs", "feedback_tickets", "stats", "strategy", "strategy.write"},
	RoleSupport:      {"users", "orders", "device_logs", "feedback_tickets", "feedback_tickets.write"},
	RoleMarketing:    {"users", "orders", "retailers", "retailers.write", "edm", "campaigns", "campaigns.write", "license_keys", "license_keys.write", "stats", "surveys", "announcements", "announcements.write"},
}

// api_admin_my_permissions returns permission groups for the current user
// based on their role bitmask. Superadmins get all groups.
func api_admin_my_permissions(c *gin.Context) {
	user := ReqUser(c)
	if user == nil {
		Error(c, ErrorNotLogin, "not logged in")
		return
	}

	isAdmin := user.IsAdmin != nil && *user.IsAdmin

	if isAdmin {
		Success(c, &myPermissionsResponse{
			IsAdmin: true,
			Roles:   user.Roles,
			Groups:  allGroups,
		})
		return
	}

	// Collect unique groups from all matching roles
	seen := make(map[string]bool)
	var groups []string
	for role, roleGroups := range roleGroupMap {
		if HasRole(user.Roles, role) {
			for _, g := range roleGroups {
				if !seen[g] {
					seen[g] = true
					groups = append(groups, g)
				}
			}
		}
	}
	if groups == nil {
		groups = []string{}
	}

	Success(c, &myPermissionsResponse{
		IsAdmin: false,
		Roles:   user.Roles,
		Groups:  groups,
	})
}
