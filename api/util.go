package center

import (
	"context"
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/rs/xid"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
)

func generateId(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, xid.New().String())
}

func generateAccessKey() string {
	return generateId("ak")
}

// SafeGo 安全地执行 goroutine，自动处理 panic
func SafeGo(ctx context.Context, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				log.Errorf(ctx, "goroutine panic recovered: %v\nStack trace:\n%s", r, stack)
				slack.Send("alert", fmt.Sprintf("[kaitu]请关注有crash问题：%v\n\n堆栈信息：\n%s", r, stack))
			}
		}()
		fn()
	}()
}

// SafeGoWithContext 安全地执行带上下文的 goroutine，自动处理 panic
func SafeGoWithContext(ctx context.Context, fn func(context.Context)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				log.Errorf(ctx, "goroutine panic recovered: %v\nStack trace:\n%s", r, stack)
				slack.Send("alert", fmt.Sprintf("[kaitu]请关注有crash问题：%v\n\n堆栈信息：\n%s", r, stack))
			}
		}()
		fn(ctx)
	}()
}

// IsValidBCP47Language validates if a language code follows BCP 47 / IETF language tag standards
// Examples of valid tags: en, en-US, zh-CN, zh-Hans-CN, en-GB, ja, ko-KR
func IsValidBCP47Language(lang string) bool {
	if lang == "" {
		return false
	}

	// BCP 47 language tag regex pattern
	// Supports:
	// - Primary language subtag (2-3 letters): en, zh, ja
	// - Script subtag (4 letters): Hans, Hant, Latn
	// - Region subtag (2 letters or 3 digits): US, CN, 001
	// - Variants and extensions (separated by hyphens)
	pattern := `^[a-z]{2,3}(?:-[A-Z][a-z]{3})?(?:-[A-Z]{2}|\d{3})?(?:-[a-zA-Z0-9]+)*$`

	re := regexp.MustCompile(pattern)
	return re.MatchString(lang)
}

// NormalizeBCP47Language normalizes a BCP 47 language tag to standard format
// Converts to lowercase language code and uppercase region code
// Examples: en-us -> en-US, ZH-cn -> zh-CN
func NormalizeBCP47Language(lang string) string {
	parts := strings.Split(lang, "-")
	if len(parts) == 0 {
		return lang
	}

	// Lowercase the primary language subtag
	parts[0] = strings.ToLower(parts[0])

	// Process remaining parts
	for i := 1; i < len(parts); i++ {
		part := parts[i]
		if len(part) == 2 {
			// Region code (2 letters) - uppercase
			parts[i] = strings.ToUpper(part)
		} else if len(part) == 4 && regexp.MustCompile(`^[A-Za-z]{4}$`).MatchString(part) {
			// Script code (4 letters) - Title case (first letter uppercase)
			parts[i] = strings.Title(strings.ToLower(part))
		}
		// Other parts remain as-is
	}

	return strings.Join(parts, "-")
}

// BoolPtr 返回 bool 值的指针（用于 GORM *bool 字段）
func BoolPtr(b bool) *bool {
	return &b
}
