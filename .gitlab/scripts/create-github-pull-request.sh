#!/bin/sh
set -eu

: "${CI_DEFAULT_BRANCH:=master}"
: "${GITHUB_REPOSITORY:=i-redbyte/Jank-Hunter}"
: "${GITHUB_BASE_BRANCH:=master}"
: "${GITHUB_EXPORT_BRANCH:=gitlab-master-sync}"

if [ -z "${GITHUB_SYNC_TOKEN:-}" ]; then
  echo "GITHUB_SYNC_TOKEN is required to push to GitHub and create a pull request."
  exit 1
fi

github_owner="${GITHUB_REPOSITORY%%/*}"
github_api="https://api.github.com/repos/${GITHUB_REPOSITORY}/pulls"

git remote remove github 2>/dev/null || true
git remote add github "https://x-access-token:${GITHUB_SYNC_TOKEN}@github.com/${GITHUB_REPOSITORY}.git"

git fetch origin "+refs/heads/${CI_DEFAULT_BRANCH}:refs/remotes/origin/${CI_DEFAULT_BRANCH}"
git fetch github "+refs/heads/${GITHUB_BASE_BRANCH}:refs/remotes/github/${GITHUB_BASE_BRANCH}"
git fetch github "+refs/heads/${GITHUB_EXPORT_BRANCH}:refs/remotes/github/${GITHUB_EXPORT_BRANCH}" 2>/dev/null || true

gitlab_sha="$(git rev-parse "origin/${CI_DEFAULT_BRANCH}")"
github_sha="$(git rev-parse "github/${GITHUB_BASE_BRANCH}")"

echo "GitLab ${CI_DEFAULT_BRANCH}: ${gitlab_sha}"
echo "GitHub ${GITHUB_BASE_BRANCH}: ${github_sha}"

if [ "$gitlab_sha" = "$github_sha" ]; then
  echo "GitHub already contains the GitLab branch tip."
  exit 0
fi

if git merge-base --is-ancestor "$gitlab_sha" "$github_sha"; then
  echo "GitLab ${CI_DEFAULT_BRANCH} is behind GitHub ${GITHUB_BASE_BRANCH}; no pull request is needed."
  exit 0
fi

git checkout -B "$GITHUB_EXPORT_BRANCH" "$gitlab_sha"
git push --force-with-lease github "HEAD:refs/heads/${GITHUB_EXPORT_BRANCH}"

head_ref="${github_owner}:${GITHUB_EXPORT_BRANCH}"
encoded_head="$(printf '%s' "$head_ref" | jq -sRr @uri)"
encoded_base="$(printf '%s' "$GITHUB_BASE_BRANCH" | jq -sRr @uri)"

existing_response="$(mktemp)"
existing_code="$(
  curl -sS -o "$existing_response" -w "%{http_code}" \
    -H "Accept: application/vnd.github+json" \
    -H "Authorization: Bearer ${GITHUB_SYNC_TOKEN}" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    "${github_api}?state=open&head=${encoded_head}&base=${encoded_base}"
)"

if [ "$existing_code" -lt 200 ] || [ "$existing_code" -ge 300 ]; then
  cat "$existing_response"
  exit 1
fi

existing_pr="$(jq -r '.[0].html_url // empty' "$existing_response")"
if [ -n "$existing_pr" ]; then
  echo "Pull request already exists: ${existing_pr}"
  exit 0
fi

title="Sync GitLab ${CI_DEFAULT_BRANCH} into GitHub ${GITHUB_BASE_BRANCH}"
body="Automated pull request from GitLab CI.

GitLab branch: ${CI_PROJECT_URL}/-/tree/${CI_DEFAULT_BRANCH}
GitHub base branch: https://github.com/${GITHUB_REPOSITORY}/tree/${GITHUB_BASE_BRANCH}
Pipeline: ${CI_PIPELINE_URL}"

payload="$(
  jq -n \
    --arg title "$title" \
    --arg head "$GITHUB_EXPORT_BRANCH" \
    --arg base "$GITHUB_BASE_BRANCH" \
    --arg body "$body" \
    '{title: $title, head: $head, base: $base, body: $body, maintainer_can_modify: true}'
)"

create_response="$(mktemp)"
create_code="$(
  curl -sS -o "$create_response" -w "%{http_code}" \
    -X POST \
    -H "Accept: application/vnd.github+json" \
    -H "Authorization: Bearer ${GITHUB_SYNC_TOKEN}" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    -d "$payload" \
    "$github_api"
)"

if [ "$create_code" -lt 200 ] || [ "$create_code" -ge 300 ]; then
  cat "$create_response"
  exit 1
fi

echo "Created pull request: $(jq -r '.html_url' "$create_response")"
