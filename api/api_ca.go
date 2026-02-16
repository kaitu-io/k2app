package center

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
)

// api_ca_get 获取 CA 证书
//
func api_ca_get(c *gin.Context) {
	log.Infof(c, "request to get CA certificate")
	ca, _, err := GetCa(c)
	if err != nil {
		log.Errorf(c, "failed to get ca: %v", err)
		Error(c, ErrorSystemError, "failed to get ca")
		return
	}
	log.Infof(c, "successfully retrieved CA certificate")
	c.Data(http.StatusOK, "application/x-x509-ca-cert", ca)
}
