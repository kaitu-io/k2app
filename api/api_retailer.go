package center

import (
	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
)

// ==================== 分销商等级信息 API ====================

// DataRetailerLevel 分销商等级信息响应
type DataRetailerLevel struct {
	Level             int    `json:"level"`             // 当前等级：1=L1推荐者, 2=L2分销商, 3=L3优质分销商, 4=L4合伙人
	LevelName         string `json:"levelName"`         // 等级名称
	FirstOrderPercent int    `json:"firstOrderPercent"` // 首单分成百分比
	RenewalPercent    int    `json:"renewalPercent"`    // 续费分成百分比
	PaidUserCount     int    `json:"paidUserCount"`     // 累计带来的付费用户数

	// 下一等级信息（如果已是最高级则为null）
	NextLevel            *int    `json:"nextLevel,omitempty"`            // 下一等级
	NextLevelName        *string `json:"nextLevelName,omitempty"`        // 下一等级名称
	NextLevelRequirement *int    `json:"nextLevelRequirement,omitempty"` // 下一等级所需用户数
	NeedContentProof     bool    `json:"needContentProof"`               // 下一等级是否需要内容证明
}

// api_get_retailer_level 获取当前用户的分销商等级信息
//
func api_get_retailer_level(c *gin.Context) {
	userID := ReqUserID(c)
	user := ReqUser(c)

	// 检查是否为分销商
	if user.IsRetailer == nil || !*user.IsRetailer {
		log.Warnf(c, "[api_get_retailer_level] user %d is not a retailer", userID)
		Error(c, ErrorForbidden, "not a retailer")
		return
	}

	// 获取分销商配置
	config, err := GetOrCreateRetailerConfig(c, userID)
	if err != nil {
		log.Errorf(c, "[api_get_retailer_level] failed to get retailer config for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "failed to get retailer config")
		return
	}

	// 构建响应
	levelInfo := config.GetLevelInfo()
	response := &DataRetailerLevel{
		Level:             config.Level,
		LevelName:         levelInfo.Name,
		FirstOrderPercent: config.FirstOrderPercent,
		RenewalPercent:    config.RenewalPercent,
		PaidUserCount:     config.PaidUserCount,
	}

	// 添加下一等级信息
	if nextInfo := config.GetNextLevelInfo(); nextInfo != nil {
		nextLevel := config.Level + 1
		response.NextLevel = &nextLevel
		response.NextLevelName = &nextInfo.Name
		response.NextLevelRequirement = &nextInfo.RequiredUsers
		response.NeedContentProof = nextInfo.NeedContentProof
	}

	log.Infof(c, "[api_get_retailer_level] user %d level info: L%d (%s), paidUsers=%d",
		userID, config.Level, levelInfo.Name, config.PaidUserCount)

	Success(c, response)
}

// DataRetailerStats 分销商统计数据
type DataRetailerStats struct {
	Level             int    `json:"level"`             // 当前等级
	LevelName         string `json:"levelName"`         // 等级名称
	FirstOrderPercent int    `json:"firstOrderPercent"` // 首单分成百分比
	RenewalPercent    int    `json:"renewalPercent"`    // 续费分成百分比
	PaidUserCount     int    `json:"paidUserCount"`     // 累计付费用户数

	// 升级进度
	NextLevel            *int    `json:"nextLevel,omitempty"`
	NextLevelName        *string `json:"nextLevelName,omitempty"`
	NextLevelRequirement *int    `json:"nextLevelRequirement,omitempty"`
	NeedContentProof     bool    `json:"needContentProof"`
	ProgressPercent      int     `json:"progressPercent"` // 升级进度百分比 (0-100)
}

// api_get_retailer_stats 获取分销商统计数据（包含升级进度）
//
func api_get_retailer_stats(c *gin.Context) {
	userID := ReqUserID(c)
	user := ReqUser(c)

	// 检查是否为分销商
	if user.IsRetailer == nil || !*user.IsRetailer {
		log.Warnf(c, "[api_get_retailer_stats] user %d is not a retailer", userID)
		Error(c, ErrorForbidden, "not a retailer")
		return
	}

	// 获取分销商配置
	config, err := GetOrCreateRetailerConfig(c, userID)
	if err != nil {
		log.Errorf(c, "[api_get_retailer_stats] failed to get retailer config for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "failed to get retailer config")
		return
	}

	levelInfo := config.GetLevelInfo()
	response := &DataRetailerStats{
		Level:             config.Level,
		LevelName:         levelInfo.Name,
		FirstOrderPercent: config.FirstOrderPercent,
		RenewalPercent:    config.RenewalPercent,
		PaidUserCount:     config.PaidUserCount,
		ProgressPercent:   0,
	}

	// 计算升级进度
	if nextInfo := config.GetNextLevelInfo(); nextInfo != nil {
		nextLevel := config.Level + 1
		response.NextLevel = &nextLevel
		response.NextLevelName = &nextInfo.Name
		response.NextLevelRequirement = &nextInfo.RequiredUsers
		response.NeedContentProof = nextInfo.NeedContentProof

		// 计算进度百分比
		currentLevelReq := levelInfo.RequiredUsers
		nextLevelReq := nextInfo.RequiredUsers
		if nextLevelReq > currentLevelReq {
			progress := (config.PaidUserCount - currentLevelReq) * 100 / (nextLevelReq - currentLevelReq)
			if progress < 0 {
				progress = 0
			}
			if progress > 100 {
				progress = 100
			}
			response.ProgressPercent = progress
		}
	} else {
		// 已是最高级
		response.ProgressPercent = 100
	}

	Success(c, response)
}
