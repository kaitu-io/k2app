package center

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// Valid protocol names for validation
var validProtocols = map[string]bool{
	"k2:quic_bbr":    true,
	"k2:quic_brutal": true,
	"k2:tcp_ws":      true,
}

// Valid carriers for validation
var validCarriers = map[string]bool{
	"china_telecom":   true,
	"china_unicom":    true,
	"china_mobile":    true,
	"china_education": true,
	"":                true, // empty is valid (match all)
}

// Valid route types for validation
var validRouteTypes = map[string]bool{
	"cn2_gia":   true,
	"cn2_gt":    true,
	"cmin2":     true,
	"as9929":    true,
	"as4837":    true,
	"direct":    true,
	"congested": true,
	"unknown":   true,
	"":          true, // empty is valid (match all)
}

// Valid route qualities for validation
var validRouteQualities = map[string]bool{
	"excellent": true,
	"good":      true,
	"fair":      true,
	"poor":      true,
	"unusable":  true,
	"unknown":   true,
}

// versionPattern matches YYYY.MM.DD.N format
var versionPattern = regexp.MustCompile(`^\d{4}\.\d{2}\.\d{2}\.\d+$`)

// ========================= Request/Response Types =========================

// AdminCreateRulesRequest request for creating new rules version
type AdminCreateRulesRequest struct {
	Version string                   `json:"version" binding:"required"` // Format: YYYY.MM.DD.N
	Rules   AdminStrategyRulesConfig `json:"rules" binding:"required"`
}

// AdminStrategyRulesConfig strategy rules configuration
type AdminStrategyRulesConfig struct {
	Rules     []AdminRuleCondition `json:"rules" binding:"required,dive"`
	Protocols map[string]any       `json:"protocols"`
	Default   AdminDefaultConfig   `json:"default" binding:"required"`
}

// AdminRuleCondition defines a strategy rule
type AdminRuleCondition struct {
	ID            string   `json:"id" binding:"required"`
	Priority      int      `json:"priority" binding:"required,min=1,max=1000"`
	Match         RuleMatch `json:"match"`
	Action        RuleAction `json:"action" binding:"required"`
}

// RuleMatch defines conditions for rule matching
type RuleMatch struct {
	Carrier                []string `json:"carrier,omitempty"`
	NetworkType            []string `json:"network_type,omitempty"`
	RouteQuality           []string `json:"route_quality,omitempty"`
	IsPeakHour             *bool    `json:"is_peak_hour,omitempty"`
	HistoryFailureRateGt   *float32 `json:"history_failure_rate_gt,omitempty"`
}

// RuleAction defines the action when rule matches
type RuleAction struct {
	ProtocolChain []string `json:"protocol_chain" binding:"required,min=1"`
	Congestion    string   `json:"congestion,omitempty"`
	UseRelay      *bool    `json:"use_relay,omitempty"`
	TimeoutMs     int      `json:"timeout_ms,omitempty"`
}

// AdminDefaultConfig defines fallback behavior
type AdminDefaultConfig struct {
	ProtocolChain []string `json:"protocol_chain" binding:"required,min=1"`
	TimeoutMs     int      `json:"timeout_ms" binding:"required,min=1000,max=30000"`
}

// AdminRulesListItem item in rules list
type AdminRulesListItem struct {
	ID        uint64 `json:"id"`
	Version   string `json:"version"`
	IsActive  bool   `json:"isActive"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// AdminRulesDetailResponse detailed rules response
type AdminRulesDetailResponse struct {
	ID        uint64                   `json:"id"`
	Version   string                   `json:"version"`
	IsActive  bool                     `json:"isActive"`
	Content   AdminStrategyRulesConfig `json:"content"`
	CreatedAt int64                    `json:"createdAt"`
	UpdatedAt int64                    `json:"updatedAt"`
}

// ========================= Validation Functions =========================

// validateVersion checks if version matches YYYY.MM.DD.N format
func validateVersion(version string) error {
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("version must match format YYYY.MM.DD.N (e.g., 2026.01.23.1)")
	}
	return nil
}

// validateProtocolChain validates protocol names
func validateProtocolChain(chain []string) error {
	for _, p := range chain {
		if !validProtocols[p] {
			return fmt.Errorf("invalid protocol: %s (valid: k2:quic_bbr, k2:quic_brutal, k2:tcp_ws)", p)
		}
	}
	return nil
}

// validateCarriers validates carrier names
func validateCarriers(carriers []string) error {
	for _, c := range carriers {
		if !validCarriers[c] {
			return fmt.Errorf("invalid carrier: %s (valid: china_telecom, china_unicom, china_mobile, china_education)", c)
		}
	}
	return nil
}

// validateRouteQualities validates route quality values
func validateRouteQualities(qualities []string) error {
	for _, q := range qualities {
		if !validRouteQualities[q] {
			return fmt.Errorf("invalid route_quality: %s (valid: excellent, good, fair, poor, unusable, unknown)", q)
		}
	}
	return nil
}

// validateRulesConfig validates the entire rules configuration
func validateRulesConfig(config *AdminStrategyRulesConfig) error {
	// Validate default config
	if err := validateProtocolChain(config.Default.ProtocolChain); err != nil {
		return fmt.Errorf("default.protocol_chain: %w", err)
	}

	// Validate each rule
	ruleIDs := make(map[string]bool)
	for i, rule := range config.Rules {
		// Check unique rule ID
		if ruleIDs[rule.ID] {
			return fmt.Errorf("rules[%d]: duplicate rule id '%s'", i, rule.ID)
		}
		ruleIDs[rule.ID] = true

		// Validate match conditions
		if len(rule.Match.Carrier) > 0 {
			if err := validateCarriers(rule.Match.Carrier); err != nil {
				return fmt.Errorf("rules[%d].match.carrier: %w", i, err)
			}
		}
		if len(rule.Match.RouteQuality) > 0 {
			if err := validateRouteQualities(rule.Match.RouteQuality); err != nil {
				return fmt.Errorf("rules[%d].match.route_quality: %w", i, err)
			}
		}

		// Validate action
		if err := validateProtocolChain(rule.Action.ProtocolChain); err != nil {
			return fmt.Errorf("rules[%d].action.protocol_chain: %w", i, err)
		}
		if rule.Action.TimeoutMs != 0 && (rule.Action.TimeoutMs < 1000 || rule.Action.TimeoutMs > 30000) {
			return fmt.Errorf("rules[%d].action.timeout_ms must be between 1000 and 30000", i)
		}
	}

	return nil
}

// ========================= API Handlers =========================

// api_admin_strategy_list lists all strategy rules versions
//
// GET /app/strategy/rules
func api_admin_strategy_list(c *gin.Context) {
	log.Infof(c, "listing strategy rules versions")

	var rules []StrategyRules
	err := db.Get().Order("created_at DESC").Find(&rules).Error
	if err != nil {
		log.Errorf(c, "failed to list strategy rules: %v", err)
		Error(c, ErrorSystemError, "failed to list rules")
		return
	}

	items := make([]AdminRulesListItem, len(rules))
	for i, r := range rules {
		items[i] = AdminRulesListItem{
			ID:        r.ID,
			Version:   r.Version,
			IsActive:  r.IsActive != nil && *r.IsActive,
			CreatedAt: r.CreatedAt.Unix(),
			UpdatedAt: r.UpdatedAt.Unix(),
		}
	}

	log.Infof(c, "returning %d rules versions", len(items))
	ItemsAll(c, items)
}

// api_admin_strategy_get gets a specific rules version
//
// GET /app/strategy/rules/:version
func api_admin_strategy_get(c *gin.Context) {
	version := c.Param("version")
	log.Infof(c, "getting strategy rules version: %s", version)

	var rules StrategyRules
	err := db.Get().Where(&StrategyRules{Version: version}).First(&rules).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Warnf(c, "rules version not found: %s", version)
			Error(c, ErrorNotFound, "rules version not found")
			return
		}
		log.Errorf(c, "failed to get rules: %v", err)
		Error(c, ErrorSystemError, "failed to get rules")
		return
	}

	// Parse content JSON
	var content AdminStrategyRulesConfig
	if err := json.Unmarshal([]byte(rules.Content), &content); err != nil {
		log.Errorf(c, "failed to parse rules content: %v", err)
		Error(c, ErrorSystemError, "failed to parse rules content")
		return
	}

	response := AdminRulesDetailResponse{
		ID:        rules.ID,
		Version:   rules.Version,
		IsActive:  rules.IsActive != nil && *rules.IsActive,
		Content:   content,
		CreatedAt: rules.CreatedAt.Unix(),
		UpdatedAt: rules.UpdatedAt.Unix(),
	}

	log.Infof(c, "returning rules version %s", version)
	Success(c, &response)
}

// api_admin_strategy_create creates a new rules version
//
// POST /app/strategy/rules
func api_admin_strategy_create(c *gin.Context) {
	var req AdminCreateRulesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request: %v", err)
		Error(c, ErrorInvalidArgument, "invalid request format")
		return
	}

	log.Infof(c, "creating strategy rules version: %s", req.Version)

	// Validate version format
	if err := validateVersion(req.Version); err != nil {
		log.Warnf(c, "invalid version format: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// Validate rules configuration
	if err := validateRulesConfig(&req.Rules); err != nil {
		log.Warnf(c, "invalid rules config: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// Check if version already exists
	var existing StrategyRules
	err := db.Get().Where(&StrategyRules{Version: req.Version}).First(&existing).Error
	if err == nil {
		log.Warnf(c, "version already exists: %s", req.Version)
		Error(c, ErrorConflict, "version already exists")
		return
	}
	if err != gorm.ErrRecordNotFound {
		log.Errorf(c, "failed to check existing version: %v", err)
		Error(c, ErrorSystemError, "failed to check existing version")
		return
	}

	// Serialize content
	contentJSON, err := json.Marshal(req.Rules)
	if err != nil {
		log.Errorf(c, "failed to serialize rules: %v", err)
		Error(c, ErrorSystemError, "failed to serialize rules")
		return
	}

	// Create new rules (not active by default)
	isActive := false
	rules := StrategyRules{
		Version:  req.Version,
		Content:  string(contentJSON),
		IsActive: &isActive,
	}

	if err := db.Get().Create(&rules).Error; err != nil {
		log.Errorf(c, "failed to create rules: %v", err)
		Error(c, ErrorSystemError, "failed to create rules")
		return
	}

	log.Infof(c, "created rules version %s with ID %d", req.Version, rules.ID)
	result := struct {
		ID      uint64 `json:"id"`
		Version string `json:"version"`
	}{
		ID:      rules.ID,
		Version: rules.Version,
	}
	Success(c, &result)
}

// api_admin_strategy_activate activates a rules version
//
// PUT /app/strategy/rules/:version/activate
func api_admin_strategy_activate(c *gin.Context) {
	version := c.Param("version")
	log.Infof(c, "activating strategy rules version: %s", version)

	// Find the target version
	var target StrategyRules
	err := db.Get().Where(&StrategyRules{Version: version}).First(&target).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Warnf(c, "rules version not found: %s", version)
			Error(c, ErrorNotFound, "rules version not found")
			return
		}
		log.Errorf(c, "failed to find rules: %v", err)
		Error(c, ErrorSystemError, "failed to find rules")
		return
	}

	// Transaction: deactivate all, activate target
	err = db.Get().Transaction(func(tx *gorm.DB) error {
		// Deactivate all versions
		isActiveFalse := false
		if err := tx.Model(&StrategyRules{}).Where("is_active = ?", true).Update("is_active", &isActiveFalse).Error; err != nil {
			return err
		}

		// Activate target version
		isActiveTrue := true
		if err := tx.Model(&target).Update("is_active", &isActiveTrue).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		log.Errorf(c, "failed to activate rules: %v", err)
		Error(c, ErrorSystemError, "failed to activate rules")
		return
	}

	log.Infof(c, "activated rules version %s", version)
	result := struct {
		Version     string `json:"version"`
		ActivatedAt string `json:"activatedAt"`
	}{
		Version:     version,
		ActivatedAt: time.Now().Format(time.RFC3339),
	}
	Success(c, &result)
}

// api_admin_strategy_delete deletes a rules version (soft delete)
//
// DELETE /app/strategy/rules/:version
func api_admin_strategy_delete(c *gin.Context) {
	version := c.Param("version")
	log.Infof(c, "deleting strategy rules version: %s", version)

	// Find the target version
	var target StrategyRules
	err := db.Get().Where(&StrategyRules{Version: version}).First(&target).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Warnf(c, "rules version not found: %s", version)
			Error(c, ErrorNotFound, "rules version not found")
			return
		}
		log.Errorf(c, "failed to find rules: %v", err)
		Error(c, ErrorSystemError, "failed to find rules")
		return
	}

	// Cannot delete active version
	if target.IsActive != nil && *target.IsActive {
		log.Warnf(c, "cannot delete active rules version: %s", version)
		Error(c, ErrorInvalidArgument, "cannot delete active rules version")
		return
	}

	// Soft delete
	if err := db.Get().Delete(&target).Error; err != nil {
		log.Errorf(c, "failed to delete rules: %v", err)
		Error(c, ErrorSystemError, "failed to delete rules")
		return
	}

	log.Infof(c, "deleted rules version %s", version)
	result := struct {
		Version   string `json:"version"`
		DeletedAt string `json:"deletedAt"`
	}{
		Version:   version,
		DeletedAt: time.Now().Format(time.RFC3339),
	}
	Success(c, &result)
}
