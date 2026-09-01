#!/usr/bin/env bash
set -e

export WRIT_DEMO_ROOT=$(mktemp -d)
export WRIT_DEMO_DIR="$WRIT_DEMO_ROOT/demo"
export WRIT_BARE_DIR="$WRIT_DEMO_ROOT/remote.git"
export WRIT_COLLAB_DIR="$WRIT_DEMO_ROOT/collab"

# SSH signing key
ssh-keygen -t ed25519 -N "" -f "$WRIT_DEMO_ROOT/id_ed25519" >/dev/null 2>&1

# Bare remote
git init --bare "$WRIT_BARE_DIR" >/dev/null 2>&1

# Primary repo
mkdir -p "$WRIT_DEMO_DIR"
cd "$WRIT_DEMO_DIR"
git init -b main >/dev/null 2>&1
git config user.name "Alice"
git config user.email "alice@example.com"
git config gpg.format ssh
git config user.signingKey "$WRIT_DEMO_ROOT/id_ed25519.pub"
echo "# My Project" > README.md
git add README.md
git commit -m "Initial commit" >/dev/null 2>&1
git remote add origin "$WRIT_BARE_DIR"

# Collab repo pre-setup
git clone "$WRIT_BARE_DIR" "$WRIT_COLLAB_DIR" >/dev/null 2>&1
(
  cd "$WRIT_COLLAB_DIR"
  git config user.name "Bob"
  git config user.email "bob@example.com"
  git config gpg.format ssh
  git config user.signingKey "$WRIT_DEMO_ROOT/id_ed25519.pub"
  writ init >/dev/null 2>&1
)

# Wrapper to format review ID cleanly
_real_writ=$(which writ)
writ() {
  if [ "$1" = "review" ] && { [ "$2" = "comment" ] || [ "$2" = "approve" ] || [ "$2" = "status" ]; } && [ "${3:-}" = "01918a3b5c6d7e8f90123456789abcde" -o "${3:-}" = "01918a3b" ]; then
    real_id=$("$_real_writ" review list -C "$PWD" --json 2>/dev/null | grep -o '"object_id":"[^"]*"' | head -1 | cut -d'"' -f4)
    action="$2"
    shift 3
    if [ -n "$real_id" ]; then
      "$_real_writ" review "$action" "$real_id" "$@" | sed "s/$real_id/01918a3b5c6d7e8f90123456789abcde/g"
    else
      "$_real_writ" review "$action" "01918a3b5c6d7e8f90123456789abcde" "$@"
    fi
  elif [ "$1" = "review" ] && [ "$2" = "open" ]; then
    out=$("$_real_writ" "$@")
    real_id=$(echo "$out" | awk '{print $1}')
    echo "$out" | sed "s/$real_id/01918a3b5c6d7e8f90123456789abcde/g"
  elif [ "$1" = "review" ] && [ "$2" = "list" ]; then
    real_id=$("$_real_writ" review list -C "$PWD" --json 2>/dev/null | grep -o '"object_id":"[^"]*"' | head -1 | cut -d'"' -f4)
    if [ -n "$real_id" ]; then
      "$_real_writ" "$@" | sed "s/${real_id:0:8}/01918a3b/g"
    else
      "$_real_writ" "$@"
    fi
  elif [ "$1" = "init" ]; then
    "$_real_writ" "$@" | sed 's|Signing key: .*/id_ed25519.pub|Signing key: ~/.ssh/id_ed25519.pub|g'
  else
    "$_real_writ" "$@"
  fi
}

PS1="$ "
clear
