#!/bin/bash
SKILL_NAME="$1"
LOCAL_SETTINGS="${CLAUDE_PROJECT_DIR}/.claude/settings.local.json"

[ -f "$LOCAL_SETTINGS" ] || exit 0

CURRENT=$(jq -r --arg name "$SKILL_NAME" '.skillOverrides[$name] // "on"' "$LOCAL_SETTINGS" 2>/dev/null || echo "on")

if [ "$CURRENT" != "on" ]; then
  jq --arg name "$SKILL_NAME" 'del(.skillOverrides[$name])' "$LOCAL_SETTINGS" > "${LOCAL_SETTINGS}.tmp" \
    && mv "${LOCAL_SETTINGS}.tmp" "$LOCAL_SETTINGS"
  jq -n --arg msg "Note: 'grilling' was left disabled from a previous session — re-enabled it for this one." \
    '{systemMessage: $msg}'
fi