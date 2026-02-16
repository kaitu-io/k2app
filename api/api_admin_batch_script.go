package center

import (
	"encoding/base64"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// ========================= Script Management API =========================

// api_admin_batch_scripts_create creates a new batch script (encrypted storage)
func api_admin_batch_scripts_create(c *gin.Context) {
	ctx := c.Request.Context()

	var req CreateBatchScriptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// Encrypt script content
	encrypted, err := secretEncrypt(ctx, []byte(req.Content))
	if err != nil {
		log.Errorf(ctx, "Failed to encrypt script content: %v", err)
		Error(c, ErrorSystemError, "Failed to encrypt script content")
		return
	}

	// Store encrypted content as base64
	encryptedB64 := base64.StdEncoding.EncodeToString(encrypted)

	script := SlaveBatchScript{
		Name:            req.Name,
		Description:     req.Description,
		Content:         encryptedB64,
		ExecuteWithSudo: req.ExecuteWithSudo,
	}

	if err := db.Get().Create(&script).Error; err != nil {
		log.Errorf(ctx, "Failed to create script: %v", err)
		Error(c, ErrorSystemError, "Failed to create script")
		return
	}

	// Save initial version
	if err := saveScriptVersion(c, script.ID, encryptedB64); err != nil {
		log.Warnf(ctx, "Failed to save initial script version: %v", err)
		// Non-fatal, continue
	}

	resp := BatchScriptResponse{
		ID:              script.ID,
		Name:            script.Name,
		Description:     script.Description,
		ExecuteWithSudo: script.ExecuteWithSudo,
		CreatedAt:       script.CreatedAt,
		UpdatedAt:       script.UpdatedAt,
	}

	Success(c, &resp)
}

// api_admin_batch_scripts_list lists all batch scripts (paginated)
func api_admin_batch_scripts_list(c *gin.Context) {
	ctx := c.Request.Context()

	pagination := PaginationFromRequest(c)

	var scripts []SlaveBatchScript
	var total int64

	if err := db.Get().Model(&SlaveBatchScript{}).Count(&total).Error; err != nil {
		log.Errorf(ctx, "Failed to count scripts: %v", err)
		Error(c, ErrorSystemError, "Failed to count scripts")
		return
	}

	if err := db.Get().
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Order("created_at DESC").
		Find(&scripts).Error; err != nil {
		log.Errorf(ctx, "Failed to list scripts: %v", err)
		Error(c, ErrorSystemError, "Failed to list scripts")
		return
	}

	var items []BatchScriptResponse
	for _, script := range scripts {
		items = append(items, BatchScriptResponse{
			ID:              script.ID,
			Name:            script.Name,
			Description:     script.Description,
			ExecuteWithSudo: script.ExecuteWithSudo,
			CreatedAt:       script.CreatedAt,
			UpdatedAt:       script.UpdatedAt,
		})
	}

	pagination.Total = total
	ListWithData(c, items, pagination)
}

// api_admin_batch_scripts_detail gets script detail with decrypted content
func api_admin_batch_scripts_detail(c *gin.Context) {
	ctx := c.Request.Context()

	var uri struct {
		ID uint64 `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	var script SlaveBatchScript
	if err := db.Get().First(&script, uri.ID).Error; err != nil {
		log.Errorf(ctx, "Script not found: %v", err)
		Error(c, ErrorNotFound, "Script not found")
		return
	}

	// Decrypt script content
	encryptedBytes, err := base64.StdEncoding.DecodeString(script.Content)
	if err != nil {
		log.Errorf(ctx, "Failed to decode encrypted content: %v", err)
		Error(c, ErrorSystemError, "Failed to decode script content")
		return
	}

	decrypted, err := secretDecrypt(ctx, encryptedBytes)
	if err != nil {
		log.Errorf(ctx, "Failed to decrypt script content: %v", err)
		Error(c, ErrorSystemError, "Failed to decrypt script content")
		return
	}

	resp := BatchScriptDetailResponse{
		ID:              script.ID,
		Name:            script.Name,
		Description:     script.Description,
		Content:         string(decrypted),
		ExecuteWithSudo: script.ExecuteWithSudo,
		CreatedAt:       script.CreatedAt,
		UpdatedAt:       script.UpdatedAt,
	}

	Success(c, &resp)
}

// api_admin_batch_scripts_update updates a script
func api_admin_batch_scripts_update(c *gin.Context) {
	ctx := c.Request.Context()

	var uri struct {
		ID uint64 `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	var req UpdateBatchScriptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// Check if script exists
	var script SlaveBatchScript
	if err := db.Get().First(&script, uri.ID).Error; err != nil {
		log.Errorf(ctx, "Script not found: %v", err)
		Error(c, ErrorNotFound, "Script not found")
		return
	}

	// Encrypt new script content
	encrypted, err := secretEncrypt(ctx, []byte(req.Content))
	if err != nil {
		log.Errorf(ctx, "Failed to encrypt script content: %v", err)
		Error(c, ErrorSystemError, "Failed to encrypt script content")
		return
	}

	// Store encrypted content as base64
	encryptedB64 := base64.StdEncoding.EncodeToString(encrypted)

	// Update script - use Select() to explicitly update all fields including false booleans
	script.Name = req.Name
	script.Description = req.Description
	script.Content = encryptedB64
	script.ExecuteWithSudo = req.ExecuteWithSudo

	if err := db.Get().Model(&script).Select("Name", "Description", "Content", "ExecuteWithSudo").Updates(&script).Error; err != nil {
		log.Errorf(ctx, "Failed to update script: %v", err)
		Error(c, ErrorSystemError, "Failed to update script")
		return
	}

	// Save version history
	if err := saveScriptVersion(c, script.ID, encryptedB64); err != nil {
		log.Warnf(ctx, "Failed to save script version: %v", err)
		// Non-fatal, continue
	}

	// Reload to get updated timestamps
	if err := db.Get().First(&script, uri.ID).Error; err != nil {
		log.Errorf(ctx, "Failed to reload script: %v", err)
		Error(c, ErrorSystemError, "Failed to reload script")
		return
	}

	resp := BatchScriptDetailResponse{
		ID:              script.ID,
		Name:            script.Name,
		Description:     script.Description,
		Content:         req.Content, // Return plain text content
		ExecuteWithSudo: script.ExecuteWithSudo,
		CreatedAt:       script.CreatedAt,
		UpdatedAt:       script.UpdatedAt,
	}

	Success(c, &resp)
}

// saveScriptVersion saves the current script content as a new version
func saveScriptVersion(ctx *gin.Context, scriptID uint64, encryptedContent string) error {
	// Get the next version number
	var maxVersion struct {
		Max int
	}
	if err := db.Get().Model(&SlaveBatchScriptVersion{}).
		Select("COALESCE(MAX(version), 0) as max").
		Where("script_id = ?", scriptID).
		Scan(&maxVersion).Error; err != nil {
		return err
	}

	version := SlaveBatchScriptVersion{
		ScriptID:  scriptID,
		Version:   maxVersion.Max + 1,
		Content:   encryptedContent,
		CreatedBy: 0, // TODO: get current user ID
	}

	return db.Get().Create(&version).Error
}

// api_admin_batch_scripts_delete deletes a script (check if in use)
func api_admin_batch_scripts_delete(c *gin.Context) {
	ctx := c.Request.Context()

	var uri struct {
		ID uint64 `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// Check if script exists
	var script SlaveBatchScript
	if err := db.Get().First(&script, uri.ID).Error; err != nil {
		log.Errorf(ctx, "Script not found: %v", err)
		Error(c, ErrorNotFound, "Script not found")
		return
	}

	// Check if script is in use by any pending/running tasks
	var count int64
	if err := db.Get().Model(&SlaveBatchTask{}).
		Where(&SlaveBatchTask{ScriptID: uri.ID}).
		Where("status IN (?)", []string{"pending", "running"}).
		Count(&count).Error; err != nil {
		log.Errorf(ctx, "Failed to check script usage: %v", err)
		Error(c, ErrorSystemError, "Failed to check script usage")
		return
	}

	if count > 0 {
		Error(c, ErrorForbidden, "Script is in use by pending or running tasks")
		return
	}

	// Delete script
	if err := db.Get().Delete(&script).Error; err != nil {
		log.Errorf(ctx, "Failed to delete script: %v", err)
		Error(c, ErrorSystemError, "Failed to delete script")
		return
	}

	Success(c, &map[string]any{"success": true})
}

// api_admin_batch_scripts_versions lists version history for a script
func api_admin_batch_scripts_versions(c *gin.Context) {
	ctx := c.Request.Context()

	var uri struct {
		ID uint64 `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// Check if script exists
	var script SlaveBatchScript
	if err := db.Get().First(&script, uri.ID).Error; err != nil {
		log.Errorf(ctx, "Script not found: %v", err)
		Error(c, ErrorNotFound, "Script not found")
		return
	}

	// Load versions
	var versions []SlaveBatchScriptVersion
	if err := db.Get().Where("script_id = ?", uri.ID).
		Order("version DESC").
		Find(&versions).Error; err != nil {
		log.Errorf(ctx, "Failed to load script versions: %v", err)
		Error(c, ErrorSystemError, "Failed to load versions")
		return
	}

	var items []BatchScriptVersionResponse
	for _, v := range versions {
		items = append(items, BatchScriptVersionResponse{
			Version:   v.Version,
			CreatedAt: v.CreatedAt,
			CreatedBy: v.CreatedBy,
		})
	}

	ListWithData(c, items, &Pagination{Total: int64(len(items))})
}

// api_admin_batch_scripts_version_detail gets a specific version with content
func api_admin_batch_scripts_version_detail(c *gin.Context) {
	ctx := c.Request.Context()

	var uri struct {
		ID      uint64 `uri:"id" binding:"required"`
		Version int    `uri:"version" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// Load version
	var version SlaveBatchScriptVersion
	if err := db.Get().Where("script_id = ? AND version = ?", uri.ID, uri.Version).
		First(&version).Error; err != nil {
		log.Errorf(ctx, "Version not found: %v", err)
		Error(c, ErrorNotFound, "Version not found")
		return
	}

	// Decrypt content
	encryptedBytes, err := base64.StdEncoding.DecodeString(version.Content)
	if err != nil {
		log.Errorf(ctx, "Failed to decode encrypted content: %v", err)
		Error(c, ErrorSystemError, "Failed to decode script content")
		return
	}

	decrypted, err := secretDecrypt(ctx, encryptedBytes)
	if err != nil {
		log.Errorf(ctx, "Failed to decrypt script content: %v", err)
		Error(c, ErrorSystemError, "Failed to decrypt script content")
		return
	}

	resp := BatchScriptVersionDetailResponse{
		Version:   version.Version,
		Content:   string(decrypted),
		CreatedAt: version.CreatedAt,
		CreatedBy: version.CreatedBy,
	}

	Success(c, &resp)
}

// api_admin_batch_scripts_version_restore restores a previous version
func api_admin_batch_scripts_version_restore(c *gin.Context) {
	ctx := c.Request.Context()

	var uri struct {
		ID      uint64 `uri:"id" binding:"required"`
		Version int    `uri:"version" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// Load version to restore
	var version SlaveBatchScriptVersion
	if err := db.Get().Where("script_id = ? AND version = ?", uri.ID, uri.Version).
		First(&version).Error; err != nil {
		log.Errorf(ctx, "Version not found: %v", err)
		Error(c, ErrorNotFound, "Version not found")
		return
	}

	// Load script
	var script SlaveBatchScript
	if err := db.Get().First(&script, uri.ID).Error; err != nil {
		log.Errorf(ctx, "Script not found: %v", err)
		Error(c, ErrorNotFound, "Script not found")
		return
	}

	// Save current content as a new version before restoring
	if err := saveScriptVersion(c, script.ID, script.Content); err != nil {
		log.Warnf(ctx, "Failed to save current version before restore: %v", err)
	}

	// Update script content with version content
	if err := db.Get().Model(&script).Update("content", version.Content).Error; err != nil {
		log.Errorf(ctx, "Failed to restore script: %v", err)
		Error(c, ErrorSystemError, "Failed to restore script")
		return
	}

	// Decrypt content for response
	encryptedBytes, err := base64.StdEncoding.DecodeString(version.Content)
	if err != nil {
		log.Errorf(ctx, "Failed to decode encrypted content: %v", err)
		Error(c, ErrorSystemError, "Failed to decode script content")
		return
	}

	decrypted, err := secretDecrypt(ctx, encryptedBytes)
	if err != nil {
		log.Errorf(ctx, "Failed to decrypt script content: %v", err)
		Error(c, ErrorSystemError, "Failed to decrypt script content")
		return
	}

	// Reload script to get updated timestamp
	if err := db.Get().First(&script, uri.ID).Error; err != nil {
		log.Errorf(ctx, "Failed to reload script: %v", err)
	}

	resp := BatchScriptDetailResponse{
		ID:              script.ID,
		Name:            script.Name,
		Description:     script.Description,
		Content:         string(decrypted),
		ExecuteWithSudo: script.ExecuteWithSudo,
		CreatedAt:       script.CreatedAt,
		UpdatedAt:       script.UpdatedAt,
	}

	Success(c, &resp)
}

// api_admin_batch_scripts_test tests a script on a single node
func api_admin_batch_scripts_test(c *gin.Context) {
	ctx := c.Request.Context()

	var uri struct {
		ID uint64 `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	var req TestBatchScriptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// Load script
	var script SlaveBatchScript
	if err := db.Get().First(&script, uri.ID).Error; err != nil {
		log.Errorf(ctx, "Script not found: %v", err)
		Error(c, ErrorNotFound, "Script not found")
		return
	}

	// Load node
	var node SlaveNode
	if err := db.Get().First(&node, req.NodeID).Error; err != nil {
		log.Errorf(ctx, "Node not found: %v", err)
		Error(c, ErrorNotFound, "Node not found")
		return
	}

	// Decrypt script content
	encryptedBytes, err := base64.StdEncoding.DecodeString(script.Content)
	if err != nil {
		log.Errorf(ctx, "Failed to decode encrypted content: %v", err)
		Error(c, ErrorSystemError, "Failed to decode script content")
		return
	}

	decrypted, err := secretDecrypt(ctx, encryptedBytes)
	if err != nil {
		log.Errorf(ctx, "Failed to decrypt script content: %v", err)
		Error(c, ErrorSystemError, "Failed to decrypt script content")
		return
	}

	// Execute script on node (synchronous test execution)
	startTime := time.Now().UnixMilli()

	// Ensure .tasks directory exists
	mkdirCmd := "mkdir -p /home/ubuntu/.tasks"
	if _, err := node.SSHExec(ctx, mkdirCmd); err != nil {
		Error(c, ErrorSystemError, "Failed to create .tasks directory: "+err.Error())
		return
	}

	// Upload script to node with unique test filename
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	scriptName := fmt.Sprintf("test_%s_%d.sh", timestamp, uri.ID)
	remotePath := fmt.Sprintf("/home/ubuntu/.tasks/%s", scriptName)

	if err := node.SSHCopyFile(ctx, decrypted, remotePath); err != nil {
		Error(c, ErrorSystemError, "Failed to upload script: "+err.Error())
		return
	}

	// Add execute permission
	chmodCmd := fmt.Sprintf("chmod +x %s", remotePath)
	if _, err := node.SSHExec(ctx, chmodCmd); err != nil {
		Error(c, ErrorSystemError, "Failed to chmod script: "+err.Error())
		return
	}

	// Execute script (with optional sudo)
	execCmd := remotePath
	if script.ExecuteWithSudo {
		execCmd = "sudo " + remotePath
	}

	result, err := node.SSHExec(ctx, execCmd)
	endTime := time.Now().UnixMilli()
	duration := endTime - startTime

	// Clean up test script
	cleanupCmd := fmt.Sprintf("rm -f %s", remotePath)
	_, _ = node.SSHExec(ctx, cleanupCmd)

	if err != nil {
		resp := TestBatchScriptResponse{
			Stdout:   "",
			Stderr:   "",
			ExitCode: -1,
			Duration: duration,
			Error:    err.Error(),
		}
		Success(c, &resp)
		return
	}

	resp := TestBatchScriptResponse{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
		Duration: duration,
		Error:    "",
	}

	Success(c, &resp)
}
