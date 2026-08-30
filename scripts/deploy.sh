#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
. "$ROOT_DIR/scripts/common.sh"

load_config
require_tools
require_config
set_engine_version
validate_compose
deploy_current_engine
