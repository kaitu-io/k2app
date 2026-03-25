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
