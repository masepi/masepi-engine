#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$ROOT_DIR/scripts/common.sh"

load_config
require_tools
require_config
set_engine_version
compose exec nginx nginx -t
compose exec nginx nginx -s reload
