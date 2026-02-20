#!/bin/bash
# Amplify prebuild script - generates changelog for the website

echo "Generating changelog from releases/..."
node ../scripts/generate-changelog.js
echo "Changelog generation completed"
