package cloudprovider

import (
	"encoding/json"
	"fmt"
	"time"
)

// TrafficStats holds parsed traffic statistics
type TrafficStats struct {
	UsedBytes int64
	Interface string
}

// vnstatOutput represents vnstat JSON output structure
type vnstatOutput struct {
	Interfaces []struct {
		Name    string `json:"name"`
		Traffic struct {
			Month []struct {
				Date struct {
					Year  int `json:"year"`
					Month int `json:"month"`
				} `json:"date"`
				RX int64 `json:"rx"`
				TX int64 `json:"tx"`
			} `json:"month"`
		} `json:"traffic"`
	} `json:"interfaces"`
}

// parseVnstatJSON parses vnstat --json m output
func parseVnstatJSON(data []byte, iface string, now time.Time) (*TrafficStats, error) {
	var output vnstatOutput
	if err := json.Unmarshal(data, &output); err != nil {
		return nil, fmt.Errorf("parse vnstat json: %w", err)
	}

	currentYear := now.Year()
	currentMonth := int(now.Month())

	for _, intf := range output.Interfaces {
		if intf.Name != iface {
			continue
		}

		// Find current month's data
		for _, month := range intf.Traffic.Month {
			if month.Date.Year == currentYear && month.Date.Month == currentMonth {
				return &TrafficStats{
					UsedBytes: month.RX + month.TX,
					Interface: iface,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("no traffic data for interface %s in current month", iface)
}

// calcTrafficResetTime calculates the next traffic reset time
func calcTrafficResetTime(resetDay int, now time.Time) time.Time {
	if resetDay < 1 || resetDay > 28 {
		resetDay = 1
	}

	year, month, _ := now.Date()
	loc := now.Location()

	// If current day >= reset day, next reset is next month
	if now.Day() >= resetDay {
		month++
		if month > 12 {
			month = 1
			year++
		}
	}

	return time.Date(year, month, resetDay, 0, 0, 0, 0, loc)
}
