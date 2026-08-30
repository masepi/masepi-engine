#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$ROOT_DIR/scripts/common.sh"

load_config
require_tools

if [ -z "${WEBHOOK_SECRET:-}" ]; then
  if command -v openssl >/dev/null 2>&1; then
    WEBHOOK_SECRET=$(openssl rand -hex 32)
  else
    WEBHOOK_SECRET=$(dd if=/dev/urandom bs=32 count=1 2>/dev/null | od -An -tx1 | tr -d ' \n')
  fi
  printf '\nWEBHOOK_SECRET=%s\n' "$WEBHOOK_SECRET" >> "$ENV_FILE"
  export WEBHOOK_SECRET
fi
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
