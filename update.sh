#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$ROOT_DIR/scripts/common.sh"

load_config
require_tools
require_config

if ! git -C "$ROOT_DIR" diff --quiet || ! git -C "$ROOT_DIR" diff --cached --quiet; then
  die "в checkout движка есть локальные изменения tracked-файлов"
fi

old_revision=$(git -C "$ROOT_DIR" rev-parse HEAD)
upstream=$(git -C "$ROOT_DIR" rev-parse --abbrev-ref --symbolic-full-name '@{upstream}')
GIT_TERMINAL_PROMPT=0 GIT_SSH_COMMAND='ssh -o BatchMode=yes' \
  git -C "$ROOT_DIR" -c 'url.git@github.com:.insteadOf=https://github.com/' fetch --prune origin
target_revision=$(git -C "$ROOT_DIR" rev-parse "$upstream")
git -C "$ROOT_DIR" merge-base --is-ancestor "$old_revision" "$target_revision" || die "обновление движка не является fast-forward"

rollback() {
  printf 'откат движка к %s\n' "$old_revision" >&2
  git -C "$ROOT_DIR" reset --hard "$old_revision"
  "$ROOT_DIR/scripts/deploy.sh"
}

git -C "$ROOT_DIR" merge --ff-only "$target_revision"
if ! "$ROOT_DIR/scripts/deploy.sh"; then
  rollback || die "автоматический откат не удался"
  die "обновление не применено"
fi

printf 'движок обновлён: %s -> %s\n' "$old_revision" "$target_revision"
