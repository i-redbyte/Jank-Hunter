#!/bin/sh
set -eu

: "${CI_DEFAULT_BRANCH:=master}"
: "${GITHUB_REPOSITORY:=i-redbyte/Jank-Hunter}"
: "${GITHUB_BASE_BRANCH:=master}"

if [ -z "${CI_SERVER_HOST:-}" ] || [ -z "${CI_PROJECT_PATH:-}" ]; then
  echo "This script is intended to run inside GitLab CI."
  exit 1
fi

if [ -n "${GITLAB_PUSH_TOKEN:-}" ]; then
  git remote set-url origin "https://oauth2:${GITLAB_PUSH_TOKEN}@${CI_SERVER_HOST}/${CI_PROJECT_PATH}.git"
else
  if [ -z "${CI_JOB_TOKEN:-}" ]; then
    echo "CI_JOB_TOKEN is required unless GITLAB_PUSH_TOKEN is provided."
    exit 1
  fi
  git remote set-url origin "https://gitlab-ci-token:${CI_JOB_TOKEN}@${CI_SERVER_HOST}/${CI_PROJECT_PATH}.git"
fi

git remote remove github 2>/dev/null || true
git remote add github "https://github.com/${GITHUB_REPOSITORY}.git"

git fetch origin "+refs/heads/${CI_DEFAULT_BRANCH}:refs/remotes/origin/${CI_DEFAULT_BRANCH}"
git fetch github "+refs/heads/${GITHUB_BASE_BRANCH}:refs/remotes/github/${GITHUB_BASE_BRANCH}"

gitlab_sha="$(git rev-parse "origin/${CI_DEFAULT_BRANCH}")"
github_sha="$(git rev-parse "github/${GITHUB_BASE_BRANCH}")"

echo "GitLab ${CI_DEFAULT_BRANCH}: ${gitlab_sha}"
echo "GitHub ${GITHUB_BASE_BRANCH}: ${github_sha}"

if [ "$gitlab_sha" = "$github_sha" ]; then
  echo "GitLab is already up to date with GitHub."
  exit 0
fi

if git merge-base --is-ancestor "$gitlab_sha" "$github_sha"; then
  echo "Fast-forwarding GitLab ${CI_DEFAULT_BRANCH} to GitHub ${GITHUB_BASE_BRANCH}."
  git checkout -B "$CI_DEFAULT_BRANCH" "$gitlab_sha"
  git merge --ff-only "$github_sha"
  git push origin "HEAD:refs/heads/${CI_DEFAULT_BRANCH}"
  exit 0
fi

echo "GitLab ${CI_DEFAULT_BRANCH} contains commits that are not in GitHub ${GITHUB_BASE_BRANCH}."
echo "Open and merge a GitHub pull request from GitLab first, then rerun this sync."
exit 1
