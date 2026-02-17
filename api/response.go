package center

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
)

type ErrorCode int

const (
	ErrorNone               ErrorCode = 0   // 成功
	ErrorInvalidOperation   ErrorCode = 400 // 无效操作
	ErrorNotLogin           ErrorCode = 401 // 未登录
	ErrorPaymentRequired    ErrorCode = 402 // 要求支付
	ErrorForbidden          ErrorCode = 403 // 权限不足
	ErrorNotFound           ErrorCode = 404 // 未找到
	ErrorNotSupported       ErrorCode = 405 // 不支持的操作
	ErrorUpgradeRequired    ErrorCode = 406 // 需要升级客户端
	ErrorConflict           ErrorCode = 409 // 冲突
	ErrorInvalidArgument    ErrorCode = 422 // 无效参数
	ErrorTooEarly           ErrorCode = 425 // 资源尚未就绪
	ErrorTooManyRequests    ErrorCode = 429 // 请求过于频繁
	ErrorSystemError        ErrorCode = 500 // 系统错误
	ErrorServiceUnavailable ErrorCode = 503 // 服务不可用

	// 自定义的错误码从400 + 001开始
	ErrorInvalidCampaignCode     ErrorCode = 400001 // 无效活动码
	ErrorInvalidClientClock      ErrorCode = 400002 // 客户端时间戳无效
	ErrorInvalidVerificationCode ErrorCode = 400003 // 验证码错误
	ErrorInvalidInviteCode       ErrorCode = 400004 // 邀请码无效
	ErrorSelfInvitation          ErrorCode = 400005 // 不能使用自己的邀请码
	ErrorInvalidCredentials      ErrorCode = 400006 // 无效的登录凭证
)

type DataAny struct{}

type Response[T any] struct {
	Code    ErrorCode `json:"code"`              // 返回码，0为成功，非0为失败
	Message string    `json:"message,omitempty"` // 错误信息
	Data    *T        `json:"data,omitempty"`    // 响应数据
}

type Pagination struct {
	Page     int   `json:"page"`     // 当前页码
	PageSize int   `json:"pageSize"` // 每页数量
	Total    int64 `json:"total"`    // 总记录数
}

func (p *Pagination) Offset() int {
	if p.Page <= 1 {
		return 0
	}
	// 1-based pagination: page=1 is the first page (offset=0)
	return (p.Page - 1) * p.PageSize
}

func PaginationFromRequest(c *gin.Context) *Pagination {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("pageSize"))
	if page < 1 {
		log.Debugf(c, "invalid page number %d, resetting to 1", page)
		page = 1
	}
	if pageSize <= 0 {
		log.Debugf(c, "invalid page size %d, resetting to 10", pageSize)
		pageSize = 10
	}
	if pageSize > 100 {
		log.Debugf(c, "page size %d exceeds 100, resetting to 100", pageSize)
		pageSize = 100
	}
	p := &Pagination{Page: page, PageSize: pageSize, Total: 0}
	log.Debugf(c, "pagination parsed from request: page=%d, pageSize=%d", p.Page, p.PageSize)
	return p
}

type ListResult[T any] struct {
	Items      []T         `json:"items"`
	Pagination *Pagination `json:"pagination"`
}

func Success[T any](c *gin.Context, data *T) {
	log.Debugf(c, "request to %s succeeded", c.Request.URL.Path)
	c.JSON(http.StatusOK, Response[T]{
		Data: data,
	})
}

func SuccessEmpty(c *gin.Context) {
	log.Debugf(c, "request to %s succeeded", c.Request.URL.Path)
	c.JSON(http.StatusOK, Response[DataAny]{
		Data: &DataAny{}, // 空数据
	})
}
func ItemsAll[T any](c *gin.Context, items []T) {
	log.Debugf(c, "request to %s succeeded with a list response (total: %d)", c.Request.URL.Path, len(items))
	c.JSON(http.StatusOK, Response[ListResult[T]]{
		Data: &ListResult[T]{
			Items: items,
		},
	})
}

func List[T any](c *gin.Context, items []T, pagination *Pagination) {
	if pagination == nil {
		log.Warnf(c, "pagination is nil, using empty pagination")
		ItemsAll(c, items)
		return
	}
	log.Debugf(c, "request to %s succeeded with a list response (total: %d)", c.Request.URL.Path, pagination.Total)
	c.JSON(http.StatusOK, Response[ListResult[T]]{
		Data: &ListResult[T]{
			Items:      items,
			Pagination: pagination,
		},
	})
}

// ListWithData is an alias for List, kept for backward compatibility of existing callers.
func ListWithData[T any](c *gin.Context, items []T, pagination *Pagination) {
	List(c, items, pagination)
}

func Error(c *gin.Context, code ErrorCode, message string) {
	log.Debugf(c, "request to %s failed with error code %d: %s", c.Request.URL.Path, code, message)
	c.JSON(http.StatusOK, Response[DataAny]{
		Code:    code,
		Message: message,
	})
}

func ErrorE(c *gin.Context, e error) {
	if r, ok := e.(rerr); ok {
		Error(c, r.code, r.message)
	} else {
		Error(c, ErrorSystemError, "server error")
	}
}

type rerr struct {
	code    ErrorCode
	message string
}

func (r rerr) Error() string {
	return fmt.Sprintf("[%d]%s", r.code, r.message)
}

func e(code ErrorCode, message string) rerr {
	return rerr{code, message}
}
