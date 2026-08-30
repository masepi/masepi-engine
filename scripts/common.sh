#!/bin/sh

die() {
  printf 'ошибка: %s\n' "$*" >&2
  exit 1
}

ROOT_DIR=${ROOT_DIR:?ROOT_DIR is required}
ENV_FILE=${MASEPI_ENV_FILE:-"$ROOT_DIR/.env"}

load_config() {
  [ -f "$ENV_FILE" ] || die "нет $ENV_FILE; скопируйте .env.example в .env"
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a

  resolve_ssh_dir
}

resolve_ssh_dir() {
  ssh_home=${HOME:-}
  if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != root ]; then
    command -v getent >/dev/null 2>&1 || die "не найден getent для определения домашнего каталога $SUDO_USER"
    passwd_entry=$(getent passwd "$SUDO_USER") || die "не найден пользователь $SUDO_USER"
    ssh_home=$(printf '%s\n' "$passwd_entry" | cut -d: -f6)
  fi
  [ -n "$ssh_home" ] || die "не удалось определить домашний каталог пользователя"
  SSH_DIR="$ssh_home/.ssh"
  export SSH_DIR
}

require_github_ssh_url() {
  case "$1" in
    git@github.com:*/*.git) ;;
    *) die "$2 должен быть SSH URL вида git@github.com:owner/repository.git" ;;
  esac
}

require_tools() {
  command -v git >/dev/null 2>&1 || die "не найден Git"
  command -v docker >/dev/null 2>&1 || die "не найден Docker"
  docker compose version >/dev/null 2>&1 || die "не найден Docker Compose plugin"
}

require_config() {
  : "${CONTENT_BRANCH:?CONTENT_BRANCH не задан в .env}"
  : "${WEBHOOK_REPOSITORY:?WEBHOOK_REPOSITORY не задан в .env}"
  : "${SITE_DOMAIN:?SITE_DOMAIN не задан в .env}"
  : "${SITE_TITLE:?SITE_TITLE не задан в .env}"
  : "${SITE_LANGUAGE:?SITE_LANGUAGE не задан в .env}"
  : "${BASE_URL:?BASE_URL не задан в .env}"
  : "${TLS_CERT_DIR:?TLS_CERT_DIR не задан в .env}"
  : "${TLS_CERT_FILE:?TLS_CERT_FILE не задан в .env}"
  : "${TLS_KEY_FILE:?TLS_KEY_FILE не задан в .env}"
  : "${WEBHOOK_SECRET:?WEBHOOK_SECRET не задан в .env}"
  : "${POLL_INTERVAL:?POLL_INTERVAL не задан в .env}"

  [ -d "$SSH_DIR" ] || die "не найден SSH-каталог $SSH_DIR"
  case "$CONTENT_BRANCH" in -*) die "CONTENT_BRANCH не может начинаться с '-'" ;; esac
  case "$TLS_CERT_FILE" in /*|..|../*|*/../*) die "TLS_CERT_FILE должен быть путём внутри TLS_CERT_DIR" ;; esac
  case "$TLS_KEY_FILE" in /*|..|../*|*/../*) die "TLS_KEY_FILE должен быть путём внутри TLS_CERT_DIR" ;; esac
}

set_engine_version() {
  ENGINE_VERSION=$(git -C "$ROOT_DIR" rev-parse --short HEAD 2>/dev/null || printf 'dev')
  export ENGINE_VERSION
}

compose() {
  docker compose --project-directory "$ROOT_DIR" --env-file "$ENV_FILE" "$@"
}

validate_compose() {
  compose config --quiet
}

deploy_current_engine() {
  compose build publisher
  compose run --rm publisher publish-once
  compose up -d --wait --wait-timeout 180
}
