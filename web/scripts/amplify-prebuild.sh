#!/bin/bash
# Amplify prebuild script - writes environment variables to .env.production
# This ensures Next.js SSR has access to runtime environment variables

echo "Writing environment variables to .env.production..."

# Create .env.production with the environment variables
cat > .env.production << EOF
DATABASE_URI=${DATABASE_URI}
PAYLOAD_SECRET=${PAYLOAD_SECRET}
S3_BUCKET=${S3_BUCKET}
S3_REGION=${S3_REGION}
AI_ENABLED=${AI_ENABLED:-false}
AI_PROVIDER=${AI_PROVIDER:-deepseek}
AI_API_KEY=${AI_API_KEY}
AI_MODEL=${AI_MODEL:-deepseek-chat}
JWT_SECRET=${JWT_SECRET}
EOF

echo "Environment variables written to .env.production"
echo "Contents (masked):"
echo "  DATABASE_URI: ${DATABASE_URI:0:30}..."
echo "  PAYLOAD_SECRET: ${PAYLOAD_SECRET:0:10}..."
echo "  S3_BUCKET: ${S3_BUCKET}"
echo "  S3_REGION: ${S3_REGION}"
echo "  AI_ENABLED: ${AI_ENABLED:-false}"
echo "  AI_PROVIDER: ${AI_PROVIDER:-deepseek}"
echo "  AI_API_KEY: ${AI_API_KEY:+configured}"
echo "  JWT_SECRET: ${JWT_SECRET:0:10}..."

echo ""
echo "Generating Payload CMS importMap..."
NODE_OPTIONS='--import tsx' yarn payload generate:importmap
echo "importMap generation completed"

echo ""
echo "Generating changelog from client/releases..."
node ../scripts/generate-changelog.js
echo "Changelog generation completed"
