package center

import (
	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
)

// api_create_connection_rating saves a user's connection quality rating.
// POST /api/user/connection-rating
func api_create_connection_rating(c *gin.Context) {
	userID := ReqUserID(c)
	if userID == 0 {
		Error(c, ErrorNotLogin, "authentication required")
		return
	}

	var req CreateConnectionRatingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "api_create_connection_rating: invalid request: %v", err)
		Error(c, ErrorInvalidArgument, "invalid request")
		return
	}

	rating := ConnectionRating{
		UserID:        userID,
		Rating:        req.Rating,
		FeedbackID:    req.FeedbackID,
		ServerDomain:  req.Server.Domain,
		ServerName:    req.Server.Name,
		ServerCountry: req.Server.Country,
		ServerSource:  req.Server.Source,
		DurationSec:   req.Connection.DurationSec,
		RuleMode:      req.Connection.RuleMode,
		OS:            req.Connection.OS,
		AppVersion:    req.Connection.AppVersion,
		PublicIP:      req.Network.PublicIP,
		ISP:           req.Network.ISP,
		UserCity:      req.Network.City,
		UserCountry:   req.Network.Country,
		NetworkType:   req.Network.NetworkType,
	}

	if err := db.Get().Create(&rating).Error; err != nil {
		// Duplicate (user_id, feedback_id) — silently succeed (idempotent)
		if util.DbIsDuplicatedErr(err) {
			log.Infof(c, "api_create_connection_rating: duplicate rating for user %d feedbackId=%s, ignoring",
				userID, req.FeedbackID)
			SuccessEmpty(c)
			return
		}
		log.Errorf(c, "api_create_connection_rating: failed to save: %v", err)
		Error(c, ErrorSystemError, "failed to save rating")
		return
	}

	log.Infof(c, "api_create_connection_rating: user %d rated %s, server=%s",
		userID, req.Rating, req.Server.Domain)
	SuccessEmpty(c)
}
