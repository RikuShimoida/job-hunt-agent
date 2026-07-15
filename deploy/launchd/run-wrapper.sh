#!/usr/bin/env bash
# launchd から job-hunt-agent run を起動するラッパー。
#
# なぜラッパーで .env を読むか: config.LoadEnv は os.Getenv のみで .env を自動読み込みしない。
# launchd から直接バイナリを起動すると秘密情報が空になり、ErrMissingWebhookURL /
# ErrMissingGoogleCredentials で起動時停止する。ここで .env を環境へ展開してから起動する。
set -euo pipefail

# なぜ WorkingDirectory 任せにせず自分でも cd するか: DATABASE_URL の既定が相対パス
# (./job-hunt-agent.db) であり、起動ディレクトリがずれると別の場所に空 DB が作られるため。
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
cd "${repo_root}"

if [[ ! -f .env ]]; then
	echo "run-wrapper: .env が見つかりません (${repo_root}/.env)" >&2
	exit 1
fi

if [[ ! -x bin/job-hunt-agent ]]; then
	echo "run-wrapper: bin/job-hunt-agent がありません。make build を実行してください" >&2
	exit 1
fi

set -a
# shellcheck disable=SC1091
source .env
set +a

exec bin/job-hunt-agent run
