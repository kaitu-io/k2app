package center

import "github.com/gin-gonic/gin"

// VersionResponse 版本信息响应
type VersionResponse struct {
	Version   string `json:"version"`   // 版本号
	GitCommit string `json:"gitCommit"` // Git commit hash
	BuildTime string `json:"buildTime"` // 构建时间
}

// api_get_version 获取服务版本信息
func api_get_version(c *gin.Context) {
	Success(c, &VersionResponse{
		Version:   Version,
		GitCommit: GitCommit,
		BuildTime: BuildTime,
	})
}
