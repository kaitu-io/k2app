package center

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
)

// api_csr_submit handles CSR submission
// POST /csr/submit
// This is a public API - no authentication required
// Domain ownership is verified via challenge-response
func api_csr_submit(c *gin.Context) {
	var req CSRSubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "[CSR] Invalid request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// Process CSR submission
	resp, err := ProcessCSRSubmit(c.Request.Context(), &req)
	if err != nil {
		log.Errorf(c, "[CSR] Failed to process CSR submit: %v", err)

		// Determine error code based on error message
		errMsg := err.Error()
		if strings.Contains(errMsg, "invalid public key") {
			Error(c, ErrorInvalidArgument, errMsg)
		} else if strings.Contains(errMsg, "sslip.io or nip.io") {
			Error(c, ErrorInvalidArgument, errMsg)
		} else {
			Error(c, ErrorSystemError, errMsg)
		}
		return
	}

	log.Infof(c, "[CSR] CSR submitted successfully: requestId=%s, domains=%v",
		resp.RequestID, req.Domains)

	Success(c, resp)
}

// api_csr_verify handles CSR verification and certificate issuance
// POST /csr/verify
// This is a public API - verification is done via challenge-response on the domain
func api_csr_verify(c *gin.Context) {
	var req CSRVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "[CSR] Invalid verify request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// Process CSR verification
	resp, err := ProcessCSRVerify(c.Request.Context(), &req)
	if err != nil {
		log.Errorf(c, "[CSR] Failed to verify CSR: %v", err)

		// Determine error code based on error message
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "expired") {
			Error(c, ErrorNotFound, errMsg)
		} else if strings.Contains(errMsg, "signature verification") {
			Error(c, ErrorForbidden, errMsg)
		} else if strings.Contains(errMsg, "domain verification") {
			Error(c, ErrorSystemError, errMsg)
		} else {
			Error(c, ErrorSystemError, errMsg)
		}
		return
	}

	log.Infof(c, "[CSR] Certificate issued: requestId=%s, serial=%s, domains=%v",
		req.RequestID, resp.SerialNumber, resp.Domains)

	Success(c, resp)
}
