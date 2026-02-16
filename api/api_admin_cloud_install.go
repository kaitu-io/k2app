package center

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kaitu-io/k2app/api/templates"
	"github.com/wordgate/qtoolkit/log"
)

// api_cloud_init_node_script returns the node installation script
// GET /manager/cloud/init-node.sh
// Public endpoint - no authentication required
func api_cloud_init_node_script(c *gin.Context) {
	ctx := c.Request.Context()
	log.Infof(ctx, "request for node installation script")

	c.Header("Content-Type", "text/x-shellscript; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=init-node.sh")
	c.String(200, string(templates.InitNodeScript))
}

// api_cloud_ssh_pubkey returns the SSH public key for node installation
// GET /manager/cloud/ssh-pubkey
// Public endpoint - no authentication required
func api_cloud_ssh_pubkey(c *gin.Context) {
	ctx := c.Request.Context()
	log.Infof(ctx, "request for SSH public key")

	pubKey, err := GetSSHPublicKeyForDisplay(ctx)
	if err != nil {
		log.Errorf(ctx, "failed to get SSH public key: %v", err)
		c.String(500, "Internal error")
		return
	}

	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(200, strings.TrimSpace(pubKey))
}

// api_cloud_docker_compose returns the docker-compose.yml for node deployment
// GET /manager/cloud/docker-compose.yml
// Public endpoint - no authentication required
func api_cloud_docker_compose(c *gin.Context) {
	ctx := c.Request.Context()
	log.Infof(ctx, "request for docker-compose.yml")

	c.Header("Content-Type", "text/yaml; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=docker-compose.yml")
	c.String(200, string(templates.DockerCompose))
}
