#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$ROOT_DIR/scripts/common.sh"

load_config
require_tools

chmod 600 "$ENV_FILE"

require_config
set_engine_version
state_dir="$ROOT_DIR/var"
mkdir -p "$state_dir/site/releases"
validate_compose
deploy_current_engine

printf '\nСайт запущен: https://%s/\n' "$SITE_DOMAIN"
printf 'GitHub webhook URL: https://%s/hooks/content\n' "$SITE_DOMAIN"
printf 'GitHub webhook secret: %s\n' "$WEBHOOK_SECRET"
printf 'Event: push, Content type: application/json\n'
