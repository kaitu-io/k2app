# User Survey Campaign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a self-hosted survey system with two questionnaires (active + expired users), automated reward extension, in-app banner trigger, EDM email campaign, and admin analytics dashboard.

**Architecture:** New `SurveyResponse` GORM model + 2 API endpoints (submit + status) on Center API. Survey pages on Next.js web site (`/survey/[surveyKey]`) with shared form component. App banner trigger via localStorage counter + openExternal. EDM uses existing email infrastructure.

**Tech Stack:** Go/Gin/GORM (API), Next.js 15/React 19/Tailwind/shadcn (web pages), React/MUI/Zustand (webapp banner), existing EDM system

**Spec:** `docs/superpowers/specs/2026-03-25-user-survey-campaign-design.md`

---

## File Structure

### Center API (Go)
| File | Action | Purpose |
|------|--------|---------|
| `api/model.go` | Modify | Add `SurveyResponse` model + `VipSurveyReward` constant |
| `api/migrate.go` | Modify | Add `&SurveyResponse{}` to AutoMigrate |
| `api/api_survey.go` | Create | HTTP handlers: `api_survey_submit`, `api_survey_status` |
| `api/route.go` | Modify | Register `/api/survey/*` routes |
| `api/api_survey_test.go` | Create | Handler tests with mock DB |

### Web Site (Next.js)
| File | Action | Purpose |
|------|--------|---------|
| `web/src/app/[locale]/survey/[surveyKey]/page.tsx` | Create | Dynamic survey page |
| `web/src/app/[locale]/survey/_components/SurveyForm.tsx` | Create | Shared form component |
| `web/src/app/[locale]/survey/_components/SurveySuccess.tsx` | Create | Success/reward confirmation |
| `web/src/app/[locale]/survey/_components/surveyConfig.ts` | Create | Question definitions per survey_key |
| `web/src/lib/api.ts` | Modify | Add `submitSurvey()` + `getSurveyStatus()` methods |
| `web/messages/zh-CN/survey.json` | Create | Chinese survey translations |
| `web/messages/en-US/survey.json` | Create | English survey translations |
| `web/messages/namespaces.ts` | Modify | Register "survey" namespace |

### Webapp (React)
| File | Action | Purpose |
|------|--------|---------|
| `webapp/src/components/SurveyBanner.tsx` | Create | Survey promotion banner |
| `webapp/src/stores/vpn-machine.store.ts` | Modify | Increment connection counter on `connected` |

### Admin Dashboard
| File | Action | Purpose |
|------|--------|---------|
| `web/src/app/(manager)/manager/surveys/page.tsx` | Create | Survey analytics page |
| `web/src/lib/api.ts` | Modify | Add `getSurveyStats()` admin method |

---

## Task 1: Database Model + Migration

**Files:**
- Modify: `api/model.go` (add after line ~42 for constant, add struct at end of file)
- Modify: `api/migrate.go` (add to AutoMigrate list)

- [ ] **Step 1: Add `VipSurveyReward` constant to `model.go`**

After the existing `VipChangeType` constants (line ~41):

```go
VipSurveyReward  VipChangeType = "survey_reward"  // 问卷奖励
```

- [ ] **Step 2: Add `SurveyResponse` model to `model.go`**

Add at end of file:

```go
// SurveyResponse 问卷调查回复
type SurveyResponse struct {
	ID        uint64    `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UserID    uint64    `gorm:"not null;uniqueIndex:uk_user_survey" json:"userId"`
	User      *User     `gorm:"foreignKey:UserID" json:"-"`
	SurveyKey string    `gorm:"type:varchar(64);not null;uniqueIndex:uk_user_survey;index:idx_survey_key" json:"surveyKey"`
	Answers   string    `gorm:"type:json;not null" json:"answers"` // JSON: {"q1":"ai_tools","q2":"solo","q3":"..."}
	IPAddress string    `gorm:"type:varchar(45);default:''" json:"ipAddress"`
	RewardDays int      `gorm:"default:0" json:"rewardDays"`
}
```

- [ ] **Step 3: Add to AutoMigrate in `migrate.go`**

Add `&SurveyResponse{}` to the AutoMigrate call (after `&AdminApproval{}`):

```go
// Survey system
&SurveyResponse{},
```

- [ ] **Step 4: Run migration to verify**

Run: `cd api/cmd && go build -o kaitu-center . && ./kaitu-center migrate -c ../config.yml`

Expected: "database migration completed successfully" with no errors.

- [ ] **Step 5: Commit**

```bash
git add api/model.go api/migrate.go
git commit -m "feat(api): add SurveyResponse model + VipSurveyReward constant"
```

---

## Task 2: Survey API Handlers

**Files:**
- Create: `api/api_survey.go`
- Modify: `api/route.go` (add route group after existing `/api/user` or similar)

**Reference:**
- `api/response.go` for `Success()`, `Error()`, `ErrorCode` constants
- `api/logic_member.go:14` for `addProExpiredDays()` signature
- `api/middleware.go` for `AuthRequired()`, `ReqUser(c)`

- [ ] **Step 1: Create `api/api_survey.go` with submit handler**

```go
package center

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// Active survey keys — add new campaigns here
var activeSurveys = map[string]int{
	"active_2026q1":  30, // reward days
	"expired_2026q1": 30,
}

type SurveySubmitRequest struct {
	SurveyKey string          `json:"survey_key" binding:"required"`
	Answers   json.RawMessage `json:"answers" binding:"required"`
}

type SurveySubmitResponse struct {
	RewardDays   int   `json:"reward_days"`
	NewExpiredAt int64 `json:"new_expired_at"`
}

func api_survey_submit(c *gin.Context) {
	ctx := c.Request.Context()
	user := ReqUser(c)

	var req SurveySubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request")
		return
	}

	// Validate survey_key is active
	rewardDays, ok := activeSurveys[req.SurveyKey]
	if !ok {
		Error(c, ErrorInvalidOperation, "survey closed")
		return
	}

	// Validate answers is valid JSON object
	var answersMap map[string]any
	if err := json.Unmarshal(req.Answers, &answersMap); err != nil {
		Error(c, ErrorInvalidArgument, "answers must be a JSON object")
		return
	}

	// Single transaction: insert response + extend subscription
	var resp SurveySubmitResponse
	err := db.Get().Transaction(func(tx *gorm.DB) error {
		// Check duplicate (unique constraint will also catch this, but give friendly error)
		var existing SurveyResponse
		if err := tx.Where("user_id = ? AND survey_key = ?", user.ID, req.SurveyKey).
			First(&existing).Error; err == nil {
			return gorm.ErrDuplicatedKey
		}

		// Insert survey response
		response := &SurveyResponse{
			UserID:     user.ID,
			SurveyKey:  req.SurveyKey,
			Answers:    string(req.Answers),
			IPAddress:  c.ClientIP(),
			RewardDays: rewardDays,
		}
		if err := tx.Create(response).Error; err != nil {
			log.Errorf(ctx, "failed to create survey response for user %d: %v", user.ID, err)
			return err
		}

		// Reload user for fresh expiredAt
		var freshUser User
		if err := tx.First(&freshUser, user.ID).Error; err != nil {
			return err
		}

		// Extend subscription
		reason := "survey_" + req.SurveyKey
		history, err := addProExpiredDays(ctx, tx, &freshUser, VipSurveyReward, response.ID, rewardDays, reason)
		if err != nil {
			return err
		}
		_ = history

		resp.RewardDays = rewardDays
		resp.NewExpiredAt = freshUser.ExpiredAt
		return nil
	})

	if err != nil {
		if err == gorm.ErrDuplicatedKey {
			Error(c, ErrorConflict, "already submitted")
			return
		}
		log.Errorf(ctx, "survey submit transaction failed for user %d: %v", user.ID, err)
		Error(c, ErrorSystemError, "failed to submit survey")
		return
	}

	log.Infof(ctx, "user %d submitted survey %s, reward %d days, new expiry %d",
		user.ID, req.SurveyKey, resp.RewardDays, resp.NewExpiredAt)
	Success(c, &resp)
}

type SurveyStatusResponse struct {
	Submitted bool `json:"submitted"`
}

func api_survey_status(c *gin.Context) {
	user := ReqUser(c)
	surveyKey := c.Query("survey_key")
	if surveyKey == "" {
		Error(c, ErrorInvalidArgument, "survey_key is required")
		return
	}

	var count int64
	db.Get().Model(&SurveyResponse{}).
		Where("user_id = ? AND survey_key = ?", user.ID, surveyKey).
		Count(&count)

	Success(c, &SurveyStatusResponse{Submitted: count > 0})
}
```

- [ ] **Step 2: Register routes in `route.go`**

Add survey routes in the `/api` group (after existing route groups, before the closing brace). Use `AuthRequired()` middleware:

```go
// 问卷调查
survey := api.Group("/survey")
{
	survey.POST("/submit", AuthRequired(), api_survey_submit)
	survey.GET("/status", AuthRequired(), api_survey_status)
}
```

- [ ] **Step 3: Verify compilation**

Run: `cd api && go build ./...`

Expected: No errors.

- [ ] **Step 4: Commit**

```bash
git add api/api_survey.go api/route.go
git commit -m "feat(api): add survey submit + status endpoints"
```

---

## Task 3: Survey API Tests

**Files:**
- Create: `api/api_survey_test.go`

**Reference:**
- `api/mock_db_test.go` for `SetupMockDB(t)` helper
- Existing `api/api_*_test.go` files for test patterns

- [ ] **Step 1: Write tests for survey handlers**

```go
package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSurveyRouter(t *testing.T, user *User) (*gin.Engine, sqlmock.Sqlmock) {
	t.Helper()
	mock := SetupMockDB(t)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user", user)
		c.Next()
	})
	r.POST("/api/survey/submit", api_survey_submit)
	r.GET("/api/survey/status", api_survey_status)
	return r, mock
}

func TestSurveyStatus_NotSubmitted(t *testing.T) {
	user := &User{ID: 1}
	r, mock := setupSurveyRouter(t, user)

	mock.ExpectQuery("SELECT count.*FROM `survey_responses`").
		WithArgs(user.ID, "active_2026q1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/survey/status?survey_key=active_2026q1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp Response[SurveyStatusResponse]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrorNone, resp.Code)
	assert.False(t, resp.Data.Submitted)
}

func TestSurveyStatus_AlreadySubmitted(t *testing.T) {
	user := &User{ID: 1}
	r, mock := setupSurveyRouter(t, user)

	mock.ExpectQuery("SELECT count.*FROM `survey_responses`").
		WithArgs(user.ID, "active_2026q1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/survey/status?survey_key=active_2026q1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp Response[SurveyStatusResponse]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrorNone, resp.Code)
	assert.True(t, resp.Data.Submitted)
}

func TestSurveyStatus_MissingSurveyKey(t *testing.T) {
	user := &User{ID: 1}
	r, _ := setupSurveyRouter(t, user)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/survey/status", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp Response[DataAny]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrorInvalidArgument, resp.Code)
}

func TestSurveySubmit_InvalidSurveyKey(t *testing.T) {
	user := &User{ID: 1}
	r, _ := setupSurveyRouter(t, user)

	body, _ := json.Marshal(SurveySubmitRequest{
		SurveyKey: "nonexistent_survey",
		Answers:   json.RawMessage(`{"q1":"test"}`),
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/survey/submit", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp Response[DataAny]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrorInvalidOperation, resp.Code)
}

func TestSurveySubmit_InvalidAnswersJSON(t *testing.T) {
	user := &User{ID: 1}
	r, _ := setupSurveyRouter(t, user)

	body, _ := json.Marshal(map[string]any{
		"survey_key": "active_2026q1",
		"answers":    "not an object",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/survey/submit", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp Response[DataAny]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrorInvalidArgument, resp.Code)
}
```

- [ ] **Step 2: Run tests**

Run: `cd api && go test -run TestSurvey -v`

Expected: All tests pass.

- [ ] **Step 3: Commit**

```bash
git add api/api_survey_test.go
git commit -m "test(api): add survey handler unit tests"
```

---

## Task 4: Survey i18n + Question Config (Web)

**Files:**
- Create: `web/messages/zh-CN/survey.json`
- Create: `web/messages/en-US/survey.json`
- Create: `web/messages/ja/survey.json` (minimal)
- Create: `web/messages/zh-TW/survey.json` (minimal)
- Create: `web/messages/zh-HK/survey.json` (minimal)
- Create: `web/messages/en-GB/survey.json` (minimal)
- Create: `web/messages/en-AU/survey.json` (minimal)
- Modify: `web/messages/namespaces.ts` (register "survey" namespace)
- Create: `web/src/app/[locale]/survey/_components/surveyConfig.ts`

- [ ] **Step 1: Create zh-CN survey translations**

File: `web/messages/zh-CN/survey.json`

```json
{
  "title": "问卷调查",
  "subtitle_active": "填写 3 个问题，免费领取 1 个月使用权",
  "subtitle_expired": "填写 3 个问题，免费领取 30 天使用权",
  "submit": "提交",
  "submitting": "提交中...",
  "optional": "选填",
  "other": "其他",
  "otherPlaceholder": "请输入...",
  "progress": "第 {current} 题，共 {total} 题",
  "success_title": "感谢参与！",
  "success_reward": "已为你的账号延长 {days} 天",
  "success_new_expiry": "新的到期日期：{date}",
  "already_submitted": "你已经填写过这份问卷了",
  "already_submitted_desc": "奖励已发放到你的账号",
  "survey_closed": "本次问卷已结束，感谢关注",
  "not_found": "未找到该问卷",
  "login_required": "请先登录",
  "active_q1": "你平时用开途主要做什么？",
  "active_q1_a1": "用 ChatGPT / Claude / Gemini 等 AI 工具",
  "active_q1_a2": "工作需要，访问公司系统或境外服务",
  "active_q1_a3": "看 YouTube / Netflix 等视频",
  "active_q1_a4": "学习/查资料（Google、学术平台）",
  "active_q2": "这个账号现在是你一个人在用，还是和别人共用？",
  "active_q2_a1": "只有我自己用",
  "active_q2_a2": "和家人共用（2～3 人）",
  "active_q2_a3": "和朋友或同事共用（3 人以上）",
  "active_q3": "如果你要向朋友推荐开途，你会怎么说？",
  "active_q3_placeholder": "例如：「连接稳定，AI 工具都能用」",
  "expired_q1": "你当时为什么没有续费？",
  "expired_q1_a1": "价格有点贵，超出预算",
  "expired_q1_a2": "连接不够稳定，体验不好",
  "expired_q1_a3": "找到了其他更合适的工具",
  "expired_q1_a4": "暂时不需要了，以后可能回来",
  "expired_q1_a5": "忘记续费了，没有提醒",
  "expired_q2": "如果开途做了以下改进，哪个最能让你考虑回来？",
  "expired_q2_a1": "价格降低（比如出一个更便宜的单设备版）",
  "expired_q2_a2": "连接更稳定，断线更少",
  "expired_q2_a3": "支持更多设备同时使用",
  "expired_q2_a4": "有了中文客服，遇到问题能快速解决",
  "expired_q3": "你现在还有访问境外网络的需求吗？",
  "expired_q3_a1": "有，而且比之前更需要了",
  "expired_q3_a2": "有，和之前差不多",
  "expired_q3_a3": "暂时没有，但以后可能会有",
  "expired_q3_a4": "基本没有了"
}
```

- [ ] **Step 2: Create en-US survey translations**

File: `web/messages/en-US/survey.json`

```json
{
  "title": "Survey",
  "subtitle_active": "Answer 3 questions, get 1 month free",
  "subtitle_expired": "Answer 3 questions, get 30 days free",
  "submit": "Submit",
  "submitting": "Submitting...",
  "optional": "Optional",
  "other": "Other",
  "otherPlaceholder": "Please enter...",
  "progress": "Question {current} of {total}",
  "success_title": "Thank you!",
  "success_reward": "Your account has been extended by {days} days",
  "success_new_expiry": "New expiry date: {date}",
  "already_submitted": "You have already completed this survey",
  "already_submitted_desc": "Your reward has been applied",
  "survey_closed": "This survey has ended. Thank you for your interest.",
  "not_found": "Survey not found",
  "login_required": "Please log in first",
  "active_q1": "What do you mainly use Kaitu for?",
  "active_q1_a1": "AI tools (ChatGPT / Claude / Gemini)",
  "active_q1_a2": "Work (remote office, company systems)",
  "active_q1_a3": "Streaming (YouTube / Netflix)",
  "active_q1_a4": "Learning / research (Google, academic platforms)",
  "active_q2": "Is this account used by you alone or shared?",
  "active_q2_a1": "Only me",
  "active_q2_a2": "Shared with family (2-3 people)",
  "active_q2_a3": "Shared with friends/colleagues (3+)",
  "active_q3": "How would you describe Kaitu to a friend?",
  "active_q3_placeholder": "e.g. \"Stable connection, all AI tools work\"",
  "expired_q1": "Why didn't you renew?",
  "expired_q1_a1": "Too expensive",
  "expired_q1_a2": "Connection wasn't stable enough",
  "expired_q1_a3": "Found a better alternative",
  "expired_q1_a4": "Don't need it right now",
  "expired_q1_a5": "Forgot to renew, no reminder",
  "expired_q2": "What improvement would most likely bring you back?",
  "expired_q2_a1": "Lower price (e.g., single-device plan)",
  "expired_q2_a2": "More stable connection",
  "expired_q2_a3": "More simultaneous devices",
  "expired_q2_a4": "Chinese customer support",
  "expired_q3": "Do you still need access to overseas networks?",
  "expired_q3_a1": "Yes, even more than before",
  "expired_q3_a2": "Yes, about the same",
  "expired_q3_a3": "Not right now, but maybe later",
  "expired_q3_a4": "Basically no"
}
```

- [ ] **Step 3: Copy en-US to other English locales, zh-CN to other Chinese locales, create ja**

Copy `web/messages/en-US/survey.json` to `en-GB/survey.json` and `en-AU/survey.json`.
Copy `web/messages/zh-CN/survey.json` to `zh-TW/survey.json` and `zh-HK/survey.json`.
Create `web/messages/ja/survey.json` as a copy of `en-US/survey.json` (fallback — proper Japanese translation can follow later).

- [ ] **Step 4: Register "survey" namespace in `namespaces.ts`**

In `web/messages/namespaces.ts`, the file is auto-generated with a comment "DO NOT EDIT". However, since we're adding a new namespace, we need to add "survey" to the `namespaces` array and add the mapping entries.

Add `"survey"` to the end of the namespaces array (line 4):

```typescript
export const namespaces = ["common","nav","hero","auth","discovery","purchase","wallet","campaigns","admin","invite","install","theme","changelog","releases","k2","guide-parents","errors","licenseKeys","survey"] as const;
```

Add to `namespaceMapping` (before the closing `}`):

```typescript
  "survey": "survey"
```

- [ ] **Step 5: Create `surveyConfig.ts`**

File: `web/src/app/[locale]/survey/_components/surveyConfig.ts`

```typescript
export type QuestionType = "single" | "text";

export interface ChoiceOption {
  value: string;
  labelKey: string; // i18n key
  hasOther?: boolean; // show "Other" text input
}

export interface Question {
  id: string; // "q1", "q2", "q3"
  type: QuestionType;
  labelKey: string; // i18n key for question text
  required: boolean;
  options?: ChoiceOption[]; // for "single" type
  placeholderKey?: string; // for "text" type
}

export interface SurveyConfig {
  surveyKey: string;
  subtitleKey: string;
  questions: Question[];
}

export const surveys: Record<string, SurveyConfig> = {
  active_2026q1: {
    surveyKey: "active_2026q1",
    subtitleKey: "survey.subtitle_active",
    questions: [
      {
        id: "q1",
        type: "single",
        labelKey: "survey.active_q1",
        required: true,
        options: [
          { value: "ai_tools", labelKey: "survey.active_q1_a1" },
          { value: "work", labelKey: "survey.active_q1_a2" },
          { value: "streaming", labelKey: "survey.active_q1_a3" },
          { value: "learning", labelKey: "survey.active_q1_a4" },
          { value: "other", labelKey: "survey.other", hasOther: true },
        ],
      },
      {
        id: "q2",
        type: "single",
        labelKey: "survey.active_q2",
        required: true,
        options: [
          { value: "solo", labelKey: "survey.active_q2_a1" },
          { value: "family", labelKey: "survey.active_q2_a2" },
          { value: "friends", labelKey: "survey.active_q2_a3" },
        ],
      },
      {
        id: "q3",
        type: "text",
        labelKey: "survey.active_q3",
        required: false,
        placeholderKey: "survey.active_q3_placeholder",
      },
    ],
  },
  expired_2026q1: {
    surveyKey: "expired_2026q1",
    subtitleKey: "survey.subtitle_expired",
    questions: [
      {
        id: "q1",
        type: "single",
        labelKey: "survey.expired_q1",
        required: true,
        options: [
          { value: "expensive", labelKey: "survey.expired_q1_a1" },
          { value: "unstable", labelKey: "survey.expired_q1_a2" },
          { value: "alternative", labelKey: "survey.expired_q1_a3" },
          { value: "no_need", labelKey: "survey.expired_q1_a4" },
          { value: "forgot", labelKey: "survey.expired_q1_a5" },
        ],
      },
      {
        id: "q2",
        type: "single",
        labelKey: "survey.expired_q2",
        required: true,
        options: [
          { value: "cheaper", labelKey: "survey.expired_q2_a1" },
          { value: "stable", labelKey: "survey.expired_q2_a2" },
          { value: "more_devices", labelKey: "survey.expired_q2_a3" },
          { value: "support", labelKey: "survey.expired_q2_a4" },
          { value: "other", labelKey: "survey.other", hasOther: true },
        ],
      },
      {
        id: "q3",
        type: "single",
        labelKey: "survey.expired_q3",
        required: true,
        options: [
          { value: "more", labelKey: "survey.expired_q3_a1" },
          { value: "same", labelKey: "survey.expired_q3_a2" },
          { value: "maybe_later", labelKey: "survey.expired_q3_a3" },
          { value: "no", labelKey: "survey.expired_q3_a4" },
        ],
      },
    ],
  },
};
```

- [ ] **Step 6: Commit**

```bash
git add web/messages/ web/src/app/\[locale\]/survey/_components/surveyConfig.ts
git commit -m "feat(web): add survey i18n translations + question config"
```

---

## Task 5: Survey Web Pages

**Files:**
- Create: `web/src/app/[locale]/survey/_components/SurveyForm.tsx`
- Create: `web/src/app/[locale]/survey/_components/SurveySuccess.tsx`
- Create: `web/src/app/[locale]/survey/[surveyKey]/page.tsx`
- Modify: `web/src/lib/api.ts` (add survey API methods)

**Reference:**
- `web/src/contexts/AuthContext.tsx` for `useAuth()` pattern
- `web/src/lib/auth.ts:31` for `redirectToLogin()` with `?next=` param
- `web/src/i18n/routing.ts` for locale-aware `Link`, `usePathname`
- `web/src/lib/api.ts` for API client pattern

- [ ] **Step 1: Add survey API methods to `api.ts`**

Add to the `api` object in `web/src/lib/api.ts`:

```typescript
async submitSurvey(surveyKey: string, answers: Record<string, string>): Promise<{ reward_days: number; new_expired_at: number }> {
  return this.request('/api/survey/submit', {
    method: 'POST',
    body: JSON.stringify({ survey_key: surveyKey, answers }),
  });
},

async getSurveyStatus(surveyKey: string): Promise<{ submitted: boolean }> {
  return this.request(`/api/survey/status?survey_key=${surveyKey}`);
},
```

Note: Check the exact `request()` method pattern in `api.ts` — it may unwrap `data` automatically. Match the existing pattern.

- [ ] **Step 2: Create `SurveyForm.tsx`**

File: `web/src/app/[locale]/survey/_components/SurveyForm.tsx`

```tsx
"use client";

import { useState } from "react";
import { useTranslations } from "next-intl";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Input } from "@/components/ui/input";
import type { SurveyConfig } from "./surveyConfig";

interface SurveyFormProps {
  config: SurveyConfig;
  onSubmit: (answers: Record<string, string>) => Promise<void>;
  isSubmitting: boolean;
}

export function SurveyForm({ config, onSubmit, isSubmitting }: SurveyFormProps) {
  const t = useTranslations();
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const [otherTexts, setOtherTexts] = useState<Record<string, string>>({});
  const { questions } = config;

  const setAnswer = (questionId: string, value: string) => {
    setAnswers((prev) => ({ ...prev, [questionId]: value }));
  };

  const setOtherText = (questionId: string, value: string) => {
    setOtherTexts((prev) => ({ ...prev, [questionId]: value }));
  };

  const isComplete = questions.every((q) => {
    if (!q.required) return true;
    const answer = answers[q.id];
    if (!answer) return false;
    if (answer === "other" && !otherTexts[q.id]?.trim()) return false;
    return true;
  });

  const handleSubmit = async () => {
    // Merge "other" text into answers
    const finalAnswers: Record<string, string> = {};
    for (const q of questions) {
      const answer = answers[q.id];
      if (!answer && !q.required) continue;
      if (answer === "other" && otherTexts[q.id]) {
        finalAnswers[q.id] = `other: ${otherTexts[q.id]}`;
      } else if (q.type === "text") {
        if (answers[q.id]?.trim()) {
          finalAnswers[q.id] = answers[q.id].trim();
        }
      } else {
        finalAnswers[q.id] = answer;
      }
    }
    await onSubmit(finalAnswers);
  };

  return (
    <div className="space-y-8">
      {questions.map((q, index) => (
        <div key={q.id} className="space-y-3">
          <p className="text-sm text-muted-foreground">
            {t("survey.progress", { current: index + 1, total: questions.length })}
          </p>
          <h3 className="text-lg font-medium">
            {t(q.labelKey)}
            {!q.required && (
              <span className="ml-2 text-sm text-muted-foreground">
                ({t("survey.optional")})
              </span>
            )}
          </h3>

          {q.type === "single" && q.options && (
            <RadioGroup
              value={answers[q.id] || ""}
              onValueChange={(val) => setAnswer(q.id, val)}
              className="space-y-2"
            >
              {q.options.map((opt) => (
                <div key={opt.value}>
                  <div className="flex items-center space-x-2">
                    <RadioGroupItem value={opt.value} id={`${q.id}-${opt.value}`} />
                    <Label htmlFor={`${q.id}-${opt.value}`} className="cursor-pointer">
                      {t(opt.labelKey)}
                    </Label>
                  </div>
                  {opt.hasOther && answers[q.id] === "other" && (
                    <Input
                      className="mt-2 ml-6"
                      placeholder={t("survey.otherPlaceholder")}
                      value={otherTexts[q.id] || ""}
                      onChange={(e) => setOtherText(q.id, e.target.value)}
                    />
                  )}
                </div>
              ))}
            </RadioGroup>
          )}

          {q.type === "text" && (
            <Textarea
              placeholder={q.placeholderKey ? t(q.placeholderKey) : ""}
              value={answers[q.id] || ""}
              onChange={(e) => setAnswer(q.id, e.target.value)}
              rows={3}
            />
          )}
        </div>
      ))}

      <Button
        onClick={handleSubmit}
        disabled={!isComplete || isSubmitting}
        className="w-full"
        size="lg"
      >
        {isSubmitting ? t("survey.submitting") : t("survey.submit")}
      </Button>
    </div>
  );
}
```

- [ ] **Step 3: Create `SurveySuccess.tsx`**

File: `web/src/app/[locale]/survey/_components/SurveySuccess.tsx`

```tsx
"use client";

import { useTranslations } from "next-intl";
import { CheckCircle2 } from "lucide-react";

interface SurveySuccessProps {
  rewardDays: number;
  newExpiredAt: number; // Unix timestamp
}

export function SurveySuccess({ rewardDays, newExpiredAt }: SurveySuccessProps) {
  const t = useTranslations();
  const expiryDate = new Date(newExpiredAt * 1000).toLocaleDateString();

  return (
    <div className="flex flex-col items-center justify-center space-y-4 py-12 text-center">
      <CheckCircle2 className="h-16 w-16 text-green-500" />
      <h2 className="text-2xl font-bold">{t("survey.success_title")}</h2>
      <p className="text-lg text-muted-foreground">
        {t("survey.success_reward", { days: rewardDays })}
      </p>
      <p className="text-sm text-muted-foreground">
        {t("survey.success_new_expiry", { date: expiryDate })}
      </p>
    </div>
  );
}
```

- [ ] **Step 4: Create survey page `[surveyKey]/page.tsx`**

File: `web/src/app/[locale]/survey/[surveyKey]/page.tsx`

```tsx
"use client";

import { useState, useEffect } from "react";
import { useParams } from "next/navigation";
import { useTranslations } from "next-intl";
import Image from "next/image";
import { useAuth } from "@/contexts/AuthContext";
import { redirectToLogin } from "@/lib/auth";
import { api } from "@/lib/api";
import { toast } from "sonner";
import { surveys } from "../_components/surveyConfig";
import { SurveyForm } from "../_components/SurveyForm";
import { SurveySuccess } from "../_components/SurveySuccess";

export default function SurveyPage() {
  const t = useTranslations();
  const params = useParams();
  const { user, isAuthLoading, isAuthenticated } = useAuth();
  const surveyKey = params.surveyKey as string;

  const [status, setStatus] = useState<"loading" | "form" | "submitted" | "success" | "not_found" | "closed">("loading");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [result, setResult] = useState<{ rewardDays: number; newExpiredAt: number } | null>(null);

  const config = surveys[surveyKey];

  // Redirect to login if not authenticated
  useEffect(() => {
    if (!isAuthLoading && !isAuthenticated) {
      redirectToLogin();
    }
  }, [isAuthLoading, isAuthenticated]);

  // Check survey status once authenticated
  useEffect(() => {
    if (!isAuthenticated || !surveyKey) return;

    if (!config) {
      setStatus("not_found");
      return;
    }

    api.getSurveyStatus(surveyKey).then((data) => {
      setStatus(data.submitted ? "submitted" : "form");
    }).catch(() => {
      setStatus("form"); // Fail open — let submit endpoint catch errors
    });
  }, [isAuthenticated, surveyKey, config]);

  const handleSubmit = async (answers: Record<string, string>) => {
    setIsSubmitting(true);
    try {
      const data = await api.submitSurvey(surveyKey, answers);
      setResult({ rewardDays: data.reward_days, newExpiredAt: data.new_expired_at });
      setStatus("success");
    } catch (err: any) {
      if (err?.code === 409) {
        setStatus("submitted");
      } else {
        toast.error(err?.message || "Failed to submit survey");
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  if (isAuthLoading || status === "loading") {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-lg px-4 py-12">
      <div className="mb-8 text-center">
        <Image src="/kaitu-logo.svg" alt="Kaitu" width={120} height={40} className="mx-auto mb-4" />
        <h1 className="text-2xl font-bold">{t("survey.title")}</h1>
        {config && status === "form" && (
          <p className="mt-2 text-muted-foreground">{t(config.subtitleKey)}</p>
        )}
      </div>

      {status === "not_found" && (
        <p className="text-center text-muted-foreground">{t("survey.not_found")}</p>
      )}

      {status === "submitted" && (
        <div className="text-center space-y-2">
          <p className="text-lg">{t("survey.already_submitted")}</p>
          <p className="text-muted-foreground">{t("survey.already_submitted_desc")}</p>
        </div>
      )}

      {status === "form" && config && (
        <SurveyForm config={config} onSubmit={handleSubmit} isSubmitting={isSubmitting} />
      )}

      {status === "success" && result && (
        <SurveySuccess rewardDays={result.rewardDays} newExpiredAt={result.newExpiredAt} />
      )}
    </div>
  );
}
```

- [ ] **Step 5: Verify build**

Run: `cd web && yarn build`

Expected: Build succeeds with no errors. Check that `/survey/active_2026q1` and `/survey/expired_2026q1` routes are generated.

- [ ] **Step 6: Commit**

```bash
git add web/src/app/\[locale\]/survey/ web/src/lib/api.ts
git commit -m "feat(web): add survey pages with form + success states"
```

---

## Task 6: Webapp Survey Banner

**Files:**
- Create: `webapp/src/components/SurveyBanner.tsx`
- Modify: `webapp/src/stores/vpn-machine.store.ts` (increment counter on connected)
- Integrate banner into main layout (check where `AnnouncementBanner` is rendered)

**Reference:**
- `webapp/src/components/AnnouncementBanner.tsx` for banner pattern
- `webapp/src/stores/vpn-machine.store.ts` for state machine dispatch
- `webapp/src/services/` for cloudApi pattern

- [ ] **Step 1: Find where AnnouncementBanner is rendered**

Search: `grep -r "AnnouncementBanner" webapp/src/` to find the parent component. The new SurveyBanner should be rendered in the same location.

- [ ] **Step 2: Add connection counter increment**

In `webapp/src/stores/vpn-machine.store.ts`, in the dispatch function where state transitions to `connected`, add:

```typescript
// Increment survey connection counter
if (nextState === 'connected') {
  const key = 'k2_connect_success_count';
  const count = parseInt(localStorage.getItem(key) || '0', 10);
  localStorage.setItem(key, String(count + 1));
}
```

Place this after the state transition is applied (after `set({ state: nextState })`).

- [ ] **Step 3: Create `SurveyBanner.tsx`**

File: `webapp/src/components/SurveyBanner.tsx`

```tsx
import React, { useState, useEffect } from "react";
import { Box, Typography, Button, IconButton } from "@mui/material";
import CloseIcon from "@mui/icons-material/Close";
import CardGiftcardIcon from "@mui/icons-material/CardGiftcard";
import { useTranslation } from "react-i18next";
import { cloudApi } from "../services/cloud-api";

const SURVEY_KEY = "active_2026q1";
const CONNECT_COUNT_KEY = "k2_connect_success_count";
const DISMISS_KEY = `survey_dismissed_${SURVEY_KEY}`;
const CONNECT_THRESHOLD = 5;

interface SurveyBannerProps {
  userUuid?: string;
  isPaid?: boolean;
}

const SurveyBanner: React.FC<SurveyBannerProps> = ({ userUuid, isPaid }) => {
  const { t } = useTranslation();
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    // Quick checks first (no API call)
    if (!isPaid || !userUuid) return;
    if (localStorage.getItem(DISMISS_KEY) === "true") return;

    const count = parseInt(localStorage.getItem(CONNECT_COUNT_KEY) || "0", 10);
    if (count < CONNECT_THRESHOLD) return;

    // Check server-side status via cloudApi
    const checkStatus = async () => {
      try {
        const { code, data } = await cloudApi.get<{ submitted: boolean }>(
          `/api/survey/status?survey_key=${SURVEY_KEY}`
        );
        if (code === 0 && !data?.submitted) {
          setVisible(true);
        }
      } catch {
        // Fail silently — don't show banner if check fails
      }
    };
    checkStatus();
  }, [isPaid, userUuid]);

  const handleDismiss = () => {
    localStorage.setItem(DISMISS_KEY, "true");
    setVisible(false);
  };

  const handleClick = () => {
    const url = `https://kaitu.io/survey/${SURVEY_KEY}`;
    if (window._platform?.openExternal) {
      window._platform.openExternal(url);
    } else {
      window.open(url, "_blank");
    }
    handleDismiss();
  };

  if (!visible) return null;

  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "center",
        gap: 1,
        px: 2,
        py: 1,
        bgcolor: "primary.dark",
        borderRadius: 1,
        mb: 1,
      }}
    >
      <CardGiftcardIcon sx={{ fontSize: 20, color: "warning.main" }} />
      <Typography variant="body2" sx={{ flex: 1, color: "common.white" }}>
        {t("survey.banner_text", "填写 1 分钟问卷，免费领取 1 个月使用权")}
      </Typography>
      <Button size="small" variant="contained" color="warning" onClick={handleClick}>
        {t("survey.banner_cta", "立即填写")}
      </Button>
      <IconButton size="small" onClick={handleDismiss} sx={{ color: "common.white" }}>
        <CloseIcon fontSize="small" />
      </IconButton>
    </Box>
  );
};

export default SurveyBanner;
```


- [ ] **Step 4: Integrate banner into main layout**

Add `<SurveyBanner>` in the same parent component where `<AnnouncementBanner>` is rendered. Pass `userUuid` and `isPaid` from the auth/config stores.

- [ ] **Step 5: Verify dev server**

Run: `make dev-standalone` and check that the banner appears after conditions are met.

- [ ] **Step 6: Commit**

```bash
git add webapp/src/components/SurveyBanner.tsx webapp/src/stores/vpn-machine.store.ts
git commit -m "feat(webapp): add survey banner + connection counter"
```

---

## Task 7: Admin Survey Stats Page

**Files:**
- Create: `web/src/app/(manager)/manager/surveys/page.tsx`
- Modify: `web/src/lib/api.ts` (add admin stats endpoint)

**Reference:**
- `web/src/app/(manager)/manager/tickets/page.tsx` for admin page pattern
- Manager pages are Chinese-only, no i18n

Note: This task also requires a backend endpoint (`GET /app/surveys/stats`) to return aggregated data. Add this to `api/api_survey.go` (add `"strings"` to imports) and register in `route.go` under the admin group.

- [ ] **Step 1: Add admin stats endpoint to `api_survey.go`**

```go
type SurveyStatsResponse struct {
	SurveyKey  string         `json:"survey_key"`
	Total      int64          `json:"total"`
	Answers    map[string]any `json:"answers"` // per-question distribution
}

func api_admin_survey_stats(c *gin.Context) {
	surveyKey := c.Query("survey_key")
	if surveyKey == "" {
		Error(c, ErrorInvalidArgument, "survey_key is required")
		return
	}

	var responses []SurveyResponse
	db.Get().Where("survey_key = ?", surveyKey).Find(&responses)

	// Aggregate answers
	questionDist := make(map[string]map[string]int) // q1 -> {ai_tools: 5, work: 3, ...}
	var openTexts []map[string]any

	for _, r := range responses {
		var answers map[string]string
		if err := json.Unmarshal([]byte(r.Answers), &answers); err != nil {
			continue
		}
		for qID, answer := range answers {
			if questionDist[qID] == nil {
				questionDist[qID] = make(map[string]int)
			}
			// "other: ..." prefix means user typed free text in an "Other" option
			// Pure free-text questions (like active_q3) have no predefined values
			if strings.HasPrefix(answer, "other: ") || len(answer) > 50 {
				openTexts = append(openTexts, map[string]any{
					"user_id":    r.UserID,
					"question":   qID,
					"answer":     answer,
					"created_at": r.CreatedAt,
				})
				// Also count "other" in distribution
				if strings.HasPrefix(answer, "other: ") {
					questionDist[qID]["other"]++
				}
			} else {
				questionDist[qID][answer]++
			}
		}
	}

	stats := &SurveyStatsResponse{
		SurveyKey: surveyKey,
		Total:     int64(len(responses)),
		Answers: map[string]any{
			"distribution": questionDist,
			"open_texts":   openTexts,
		},
	}
	Success(c, stats)
}

```

Register in `route.go` admin group:

```go
admin.GET("/surveys/stats", api_admin_survey_stats)
```

- [ ] **Step 2: Add admin API method to `api.ts`**

```typescript
async getSurveyStats(surveyKey: string): Promise<{
  survey_key: string;
  total: number;
  answers: {
    distribution: Record<string, Record<string, number>>;
    open_texts: Array<{ user_id: number; question: string; answer: string; created_at: string }>;
  };
}> {
  return this.request(`/app/surveys/stats?survey_key=${surveyKey}`);
},
```

- [ ] **Step 3: Create admin surveys page**

File: `web/src/app/(manager)/manager/surveys/page.tsx`

```tsx
"use client";

import { useState, useEffect } from "react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { toast } from "sonner";

const SURVEY_KEYS = ["active_2026q1", "expired_2026q1"];

interface Stats {
  survey_key: string;
  total: number;
  answers: {
    distribution: Record<string, Record<string, number>>;
    open_texts: Array<{ user_id: number; question: string; answer: string; created_at: string }>;
  };
}

export default function SurveysPage() {
  const [surveyKey, setSurveyKey] = useState(SURVEY_KEYS[0]);
  const [stats, setStats] = useState<Stats | null>(null);
  const [isLoading, setIsLoading] = useState(false);

  useEffect(() => {
    const fetch = async () => {
      setIsLoading(true);
      try {
        const data = await api.getSurveyStats(surveyKey);
        setStats(data);
      } catch (err: any) {
        toast.error(err?.message || "加载失败");
      } finally {
        setIsLoading(false);
      }
    };
    fetch();
  }, [surveyKey]);

  const exportCSV = () => {
    if (!stats) return;
    const rows = [["Question", "Choice", "Count"]];
    for (const [qId, dist] of Object.entries(stats.answers.distribution)) {
      for (const [choice, count] of Object.entries(dist)) {
        rows.push([qId, choice, String(count)]);
      }
    }
    const csv = rows.map((r) => r.join(",")).join("\n");
    const blob = new Blob([csv], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `survey-${surveyKey}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">问卷统计</h1>
        <div className="flex items-center gap-4">
          <Select value={surveyKey} onValueChange={setSurveyKey}>
            <SelectTrigger className="w-48">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {SURVEY_KEYS.map((key) => (
                <SelectItem key={key} value={key}>{key}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button variant="outline" onClick={exportCSV} disabled={!stats}>
            导出 CSV
          </Button>
        </div>
      </div>

      {isLoading && <p>加载中...</p>}

      {stats && (
        <>
          <div className="rounded-lg border p-4">
            <p className="text-lg">总回收量：<strong>{stats.total}</strong></p>
          </div>

          {/* Per-question distribution */}
          {Object.entries(stats.answers.distribution).map(([qId, dist]) => (
            <div key={qId} className="space-y-2">
              <h3 className="font-medium">{qId}</h3>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>选项</TableHead>
                    <TableHead>数量</TableHead>
                    <TableHead>占比</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {Object.entries(dist)
                    .sort(([, a], [, b]) => b - a)
                    .map(([choice, count]) => (
                      <TableRow key={choice}>
                        <TableCell>{choice}</TableCell>
                        <TableCell>{count}</TableCell>
                        <TableCell>{stats.total > 0 ? ((count / stats.total) * 100).toFixed(1) : 0}%</TableCell>
                      </TableRow>
                    ))}
                </TableBody>
              </Table>
            </div>
          ))}

          {/* Open text responses */}
          {stats.answers.open_texts.length > 0 && (
            <div className="space-y-2">
              <h3 className="font-medium">开放回答</h3>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>用户 ID</TableHead>
                    <TableHead>问题</TableHead>
                    <TableHead>回答</TableHead>
                    <TableHead>时间</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {stats.answers.open_texts.map((item, i) => (
                    <TableRow key={i}>
                      <TableCell>{item.user_id}</TableCell>
                      <TableCell>{item.question}</TableCell>
                      <TableCell className="max-w-md">{item.answer}</TableCell>
                      <TableCell>{new Date(item.created_at).toLocaleString()}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </>
      )}
    </div>
  );
}
```

- [ ] **Step 4: Verify build**

Run: `cd web && yarn build`

Expected: Build succeeds. `/manager/surveys` page accessible.

- [ ] **Step 5: Commit**

```bash
git add api/api_survey.go api/route.go web/src/app/\(manager\)/manager/surveys/ web/src/lib/api.ts
git commit -m "feat: add admin survey stats page + API endpoint"
```

---

## Task 8: EDM Email Template

**Files:** No code changes — uses existing EDM infrastructure via admin dashboard.

- [ ] **Step 1: Create EDM template via admin dashboard**

Use the existing `/manager/edm` page to create two email templates:

**Template 1 (zh-CN):**
- Name: "问卷调查 - 过期用户召回 (zh-CN)"
- Subject: "我们想听听你的想法 — 填 3 道题，免费用 30 天"
- Content: HTML email with CTA button linking to `https://kaitu.io/survey/expired_2026q1`

**Template 2 (en-US):**
- Name: "Survey - Expired User Recall (en-US)"
- Subject: "We'd love your feedback — 3 questions, 30 days free"
- Content: HTML email with CTA button linking to `https://kaitu.io/en-US/survey/expired_2026q1`

Set Template 2 as a translation of Template 1 (via `originId`).

- [ ] **Step 2: Create EDM task targeting expired users**

Use the existing EDM task creation flow:
- Target: users where `expiredAt` is within past 180 days
- Template: the one created above
- The existing EDM system handles language matching, rate limiting, and idempotency

- [ ] **Step 3: Schedule reminder (7 days after initial send)**

Create a second EDM task 7 days later, targeting the same users but excluding those who have a row in `survey_responses` with `survey_key = 'expired_2026q1'`. This may require a custom target filter query or manual filtering.

---

## Task Summary

| Task | Scope | Estimate |
|------|-------|----------|
| 1. DB Model + Migration | Backend | Small |
| 2. Survey API Handlers | Backend | Medium |
| 3. Survey API Tests | Backend | Small |
| 4. i18n + Question Config | Web | Small |
| 5. Survey Web Pages | Web | Medium |
| 6. Webapp Survey Banner | Webapp | Medium |
| 7. Admin Stats Page | Web + Backend | Medium |
| 8. EDM Template | Ops (no code) | Small |

**Dependencies:**
- Task 1 must be done first (model needed by everything)
- Tasks 2-3 depend on Task 1
- Tasks 4-5 can run in parallel with Tasks 2-3
- Task 6 depends on Tasks 2 (needs API endpoints)
- Task 7 depends on Tasks 1-2
- Task 8 depends on Tasks 5 (needs survey page live)

**Parallel execution plan:**
- Wave 1: Task 1
- Wave 2: Tasks 2+3 (backend) || Tasks 4+5 (web) — in parallel
- Wave 3: Task 6 (webapp) || Task 7 (admin) — in parallel
- Wave 4: Task 8 (EDM — manual ops)
