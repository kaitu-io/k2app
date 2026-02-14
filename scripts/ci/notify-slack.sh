#!/bin/bash
#
# Slack Notification Script for GitHub Actions CI/CD
#
# Usage:
#   ./notify-slack.sh <type> [options]
#
# Types:
#   test-failure   - Send aggregated test failure summary (uses SLACK_WEBHOOK_ALERT)
#   test-success   - Send test success notification (uses SLACK_WEBHOOK_ALERT)
#   build-failure  - Send build error notification (uses SLACK_WEBHOOK_ALERT)
#   deploy-success - Send deployment success notification (uses SLACK_WEBHOOK_RELEASE)
#
# Environment Variables:
#   SLACK_WEBHOOK_ALERT   - Webhook for alerts (test failures, build errors)
#   SLACK_WEBHOOK_RELEASE - Webhook for release notifications
#   SLACK_WEBHOOK_URL     - Fallback if specific webhooks not set
#   GITHUB_WORKFLOW       - Workflow name (from GitHub Actions)
#   GITHUB_RUN_ID         - Run ID (from GitHub Actions)
#   GITHUB_REPOSITORY     - Repository name (from GitHub Actions)
#   GITHUB_SERVER_URL     - GitHub server URL (from GitHub Actions)
#   GITHUB_SHA            - Commit SHA (from GitHub Actions)
#   GITHUB_ACTOR          - User who triggered the workflow
#
# Examples:
#   ./notify-slack.sh test-failure --platform "macOS" --log-file "test-output.log"
#   ./notify-slack.sh build-failure --platform "Windows" --error "Build failed at step X"
#   ./notify-slack.sh deploy-success --version "1.0.0" --platforms "macOS,Windows"

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
NOTIFICATION_TYPE="${1:-}"
shift || true

# Parse arguments
PLATFORM=""
LOG_FILE=""
ERROR_MESSAGE=""
VERSION=""
PLATFORMS=""

while [[ $# -gt 0 ]]; do
    case $1 in
        --platform)
            PLATFORM="$2"
            shift 2
            ;;
        --log-file)
            LOG_FILE="$2"
            shift 2
            ;;
        --error)
            ERROR_MESSAGE="$2"
            shift 2
            ;;
        --version)
            VERSION="$2"
            shift 2
            ;;
        --platforms)
            PLATFORMS="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Select webhook based on notification type
select_webhook() {
    local type="$1"
    case "$type" in
        deploy-success)
            # Release notifications go to release channel
            if [ -n "${SLACK_WEBHOOK_RELEASE:-}" ]; then
                echo "$SLACK_WEBHOOK_RELEASE"
            elif [ -n "${SLACK_WEBHOOK_URL:-}" ]; then
                echo "$SLACK_WEBHOOK_URL"
            fi
            ;;
        test-failure|test-success|build-failure|*)
            # Alerts go to alert channel
            if [ -n "${SLACK_WEBHOOK_ALERT:-}" ]; then
                echo "$SLACK_WEBHOOK_ALERT"
            elif [ -n "${SLACK_WEBHOOK_URL:-}" ]; then
                echo "$SLACK_WEBHOOK_URL"
            fi
            ;;
    esac
}

SLACK_WEBHOOK=$(select_webhook "$NOTIFICATION_TYPE")

# Validate webhook is available
if [ -z "$SLACK_WEBHOOK" ]; then
    echo -e "${RED}ERROR: No Slack webhook configured${NC}"
    echo "Set SLACK_WEBHOOK_ALERT and/or SLACK_WEBHOOK_RELEASE, or SLACK_WEBHOOK_URL as fallback"
    exit 1
fi

# Build workflow URL
WORKFLOW_URL="${GITHUB_SERVER_URL:-https://github.com}/${GITHUB_REPOSITORY:-unknown}/actions/runs/${GITHUB_RUN_ID:-0}"
COMMIT_SHORT="${GITHUB_SHA:0:7}"

# Function: Extract error summary from log file
extract_error_summary() {
    local log_file="$1"
    local max_lines=20

    if [ ! -f "$log_file" ]; then
        echo "Log file not found: $log_file"
        return
    fi

    # Extract FAIL lines, errors, and panics
    {
        grep -E "FAIL:|ERROR:|panic:|fatal error:" "$log_file" || true
    } | head -n "$max_lines" | sed 's/^/    /'

    # If output is too long, add truncation notice
    local total_errors
    total_errors=$(grep -cE "FAIL:|ERROR:|panic:|fatal error:" "$log_file" || echo "0")
    if [ "$total_errors" -gt "$max_lines" ]; then
        echo "    ... and $((total_errors - max_lines)) more errors"
    fi
}

# Function: Send Slack notification
send_slack() {
    local payload="$1"

    curl -X POST "$SLACK_WEBHOOK" \
        -H 'Content-Type: application/json' \
        -d "$payload" \
        --silent --show-error --fail

    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Slack notification sent${NC}"
    else
        echo -e "${RED}✗ Failed to send Slack notification${NC}"
        exit 1
    fi
}

# Notification handlers
case "$NOTIFICATION_TYPE" in
    test-failure)
        echo -e "${YELLOW}Sending test failure notification...${NC}"

        ERROR_SUMMARY=""
        if [ -n "$LOG_FILE" ]; then
            ERROR_SUMMARY=$(extract_error_summary "$LOG_FILE")
        elif [ -n "$ERROR_MESSAGE" ]; then
            ERROR_SUMMARY="$ERROR_MESSAGE"
        else
            ERROR_SUMMARY="No error details provided"
        fi

        PAYLOAD=$(cat <<EOF
{
  "text": "❌ Tests Failed on $PLATFORM",
  "blocks": [
    {
      "type": "header",
      "text": {
        "type": "plain_text",
        "text": "❌ Tests Failed on $PLATFORM"
      }
    },
    {
      "type": "section",
      "fields": [
        {
          "type": "mrkdwn",
          "text": "*Workflow:*\n${GITHUB_WORKFLOW:-Unknown}"
        },
        {
          "type": "mrkdwn",
          "text": "*Commit:*\n\`${COMMIT_SHORT}\`"
        },
        {
          "type": "mrkdwn",
          "text": "*Triggered by:*\n${GITHUB_ACTOR:-Unknown}"
        }
      ]
    },
    {
      "type": "section",
      "text": {
        "type": "mrkdwn",
        "text": "*Error Summary:*\n\`\`\`\n${ERROR_SUMMARY}\n\`\`\`"
      }
    },
    {
      "type": "actions",
      "elements": [
        {
          "type": "button",
          "text": {
            "type": "plain_text",
            "text": "View Workflow Run"
          },
          "url": "${WORKFLOW_URL}",
          "style": "danger"
        }
      ]
    }
  ]
}
EOF
)
        send_slack "$PAYLOAD"
        ;;

    test-success)
        echo -e "${GREEN}Sending test success notification...${NC}"

        PAYLOAD=$(cat <<EOF
{
  "text": "✅ Tests Passed on $PLATFORM",
  "blocks": [
    {
      "type": "section",
      "text": {
        "type": "mrkdwn",
        "text": "✅ *Tests Passed on $PLATFORM*\nWorkflow: ${GITHUB_WORKFLOW:-Unknown} | Commit: \`${COMMIT_SHORT}\`"
      }
    }
  ]
}
EOF
)
        send_slack "$PAYLOAD"
        ;;

    build-failure)
        echo -e "${RED}Sending build failure notification...${NC}"

        ERROR_TEXT="${ERROR_MESSAGE:-Build failed with unknown error}"

        PAYLOAD=$(cat <<EOF
{
  "text": "🛑 Build Failed on $PLATFORM",
  "blocks": [
    {
      "type": "header",
      "text": {
        "type": "plain_text",
        "text": "🛑 Build Failed on $PLATFORM"
      }
    },
    {
      "type": "section",
      "fields": [
        {
          "type": "mrkdwn",
          "text": "*Workflow:*\n${GITHUB_WORKFLOW:-Unknown}"
        },
        {
          "type": "mrkdwn",
          "text": "*Commit:*\n\`${COMMIT_SHORT}\`"
        }
      ]
    },
    {
      "type": "section",
      "text": {
        "type": "mrkdwn",
        "text": "*Error:*\n\`\`\`\n${ERROR_TEXT}\n\`\`\`"
      }
    },
    {
      "type": "actions",
      "elements": [
        {
          "type": "button",
          "text": {
            "type": "plain_text",
            "text": "View Workflow Run"
          },
          "url": "${WORKFLOW_URL}",
          "style": "danger"
        }
      ]
    }
  ]
}
EOF
)
        send_slack "$PAYLOAD"
        ;;

    deploy-success)
        echo -e "${GREEN}Sending deployment success notification...${NC}"

        PAYLOAD=$(cat <<EOF
{
  "text": "✅ Desktop Release v${VERSION} Complete",
  "blocks": [
    {
      "type": "header",
      "text": {
        "type": "plain_text",
        "text": "✅ Desktop Release v${VERSION} Published"
      }
    },
    {
      "type": "section",
      "text": {
        "type": "mrkdwn",
        "text": "Successfully built and deployed desktop applications"
      }
    },
    {
      "type": "section",
      "fields": [
        {
          "type": "mrkdwn",
          "text": "*Platforms:*\n${PLATFORMS}"
        },
        {
          "type": "mrkdwn",
          "text": "*Version:*\n\`${VERSION}\`"
        },
        {
          "type": "mrkdwn",
          "text": "*Commit:*\n\`${COMMIT_SHORT}\`"
        }
      ]
    },
    {
      "type": "actions",
      "elements": [
        {
          "type": "button",
          "text": {
            "type": "plain_text",
            "text": "View Release"
          },
          "url": "${GITHUB_SERVER_URL:-https://github.com}/${GITHUB_REPOSITORY:-unknown}/releases/tag/v${VERSION}",
          "style": "primary"
        }
      ]
    }
  ]
}
EOF
)
        send_slack "$PAYLOAD"
        ;;

    *)
        echo -e "${RED}ERROR: Unknown notification type: $NOTIFICATION_TYPE${NC}"
        echo ""
        echo "Usage: $0 <type> [options]"
        echo ""
        echo "Types:"
        echo "  test-failure   - Send test failure notification"
        echo "  test-success   - Send test success notification"
        echo "  build-failure  - Send build error notification"
        echo "  deploy-success - Send deployment success notification"
        exit 1
        ;;
esac
