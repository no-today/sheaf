#!/bin/zsh
# Disposable demo workspace: $1/ws holds clones and worktrees; $1/remotes holds local origins.
# Usage: scripts/mkfixture.sh /tmp/readme/fixture
# The target is replaced on every run. All Git traffic stays on disk, including the demo HTTPS origins.
set -eu
FX=${1:?usage: mkfixture.sh <absolute disposable directory>}
[[ $FX == /* && $FX != / && $FX != $HOME && $FX != $PWD ]] || { echo "use an absolute disposable directory"; exit 1; }
rm -rf "$FX"
mkdir -p "$FX/ws" "$FX/remotes"
export GIT_CONFIG_GLOBAL=$FX/gitconfig GIT_CONFIG_NOSYSTEM=1
printf '[user]\n\tname = Alex Chen\n\temail = alex@example.com\n[init]\n\tdefaultBranch = master\n' > "$GIT_CONFIG_GLOBAL"
REQ=${SHEAF_FIXTURE_REQUIREMENT:-PAY-142}
B=${SHEAF_FIXTURE_BRANCH:-feat-$REQ}

# mk name state host: dirty, clean worktree, branch only, conflict, released, or landed.
mk() {
  local n=$1 state=$2 host=$3
  local up=$FX/remotes/up-$n origin=$FX/remotes/$n.git w=$FX/ws/$n
  local wt=$FX/ws/$n-$REQ
  git init -q "$up"
  cd "$up"
  mkdir -p internal/payments
  printf 'package payments\n\nfunc Capture(amount int) int {\n\treturn amount\n}\n' > internal/payments/capture.go
  printf '# %s\n\nPayment capture service.\n' "$n" > README.md
  git add .
  git commit -qm "Initialize $n"
  git clone -q --bare "$up" "$origin"
  git remote add origin "$origin"
  git branch release
  git push -q origin release
  git clone -q "$origin" "$w"
  cd "$w"
  git config user.name "Alex Chen"
  git config user.email alex@example.com
  git branch release origin/release
  # Recognizable hosts for the MR/PR preview, rewritten to this fixture's bare repo.
  local web=https://$host/sheaf-demo/$n.git
  git remote set-url origin "$web"
  git config "url.$origin.insteadOf" "$web"
  git checkout -qb "$B"
  case $n in
    payment-api)
      printf 'package payments\n\nfunc Capture(amount int) int {\n\tif amount <= 0 {\n\t\treturn 0\n\t}\n\treturn amount\n}\n' > internal/payments/capture.go
      ;;
    checkout-web)
      mkdir -p src/checkout
      printf 'export async function capturePayment(order) {\n  const key = `capture:${order.id}`;\n  return client.post("/payments/capture", {\n    amount: order.total,\n    idempotencyKey: key,\n  });\n}\n' > src/checkout/capture.ts
      ;;
    ledger-service)
      mkdir -p migrations
      printf 'CREATE TABLE payment_captures (\n    id UUID PRIMARY KEY,\n    order_id UUID NOT NULL,\n    amount_cents BIGINT NOT NULL,\n    idempotency_key TEXT NOT NULL UNIQUE\n);\n' > migrations/0142_payment_captures.sql
      ;;
    risk-engine|receipt-worker)
      printf 'package payments\n\nfunc Capture(amount int) int {\n\treturn min(amount, 100000)\n}\n' > internal/payments/capture.go
      ;;
    gateway)
      mkdir -p config
      printf 'routes:\n  - path: /payments/capture\n    upstream: payment-api\n    methods: [POST]\n    timeout: 5s\n' > config/routes.yml
      ;;
  esac
  git add .
  git commit -qm "$REQ Add payment capture to $n"
  git checkout -q master
  # Advance master after the feature branch was cut, so sync has work to preview.
  printf '\nHealth checks: /healthz\n' >> README.md
  git commit -qam "Document health checks"
  if [[ $state == conflict ]]; then
    printf 'package payments\n\nfunc Capture(amount int) int {\n\treturn max(amount, 0)\n}\n' > internal/payments/capture.go
    git commit -qam "Validate payment amounts"
  fi
  if [[ $state == released ]]; then
    git checkout -q release
    git merge -q --no-ff "$B" -m "Merge $REQ into release"
    git checkout -q master
  elif [[ $state == landed ]]; then
    git merge -q --no-ff "$B" -m "Merge $REQ into master"
  fi
  git push -q origin master release "$B"
  case $state in
    dirty|wt|conflict) git worktree add -q "$wt" "$B" ;;
  esac
  if [[ $state == dirty ]]; then
    printf 'package payments\n\nfunc Capture(amount int) int {\n\tif amount <= 0 {\n\t\treturn 0\n\t}\n\t// Retry requests must not capture the same payment twice.\n\treturn captureOnce(amount)\n}\n' > "$wt/internal/payments/capture.go"
    git -C "$wt" add internal/payments/capture.go
    printf '\nfunc captureOnce(amount int) int {\n\treturn amount\n}\n' >> "$wt/internal/payments/capture.go"
    printf 'package payments\n\nimport "testing"\n\nfunc TestCaptureRejectsZero(t *testing.T) {\n\tif got := Capture(0); got != 0 {\n\t\tt.Fatalf("Capture(0) = %%d", got)\n\t}\n}\n' > "$wt/internal/payments/capture_test.go"
  fi
  # A remote-only commit makes Fetch observable without any network access.
  cd "$up"
  git pull -q --ff-only origin master
  printf '\nMetrics: /metrics\n' >> README.md
  git commit -qam "Document metrics endpoint"
  git push -q origin master
}

mk payment-api dirty github.com
mk checkout-web wt github.com
mk ledger-service released gitlab.example.com
mk risk-engine conflict gitlab.example.com
mk receipt-worker tree gitlab.example.com
mk gateway landed github.com
printf 'done: SHEAF_WORKSPACE=%s/ws\nrequirement: %s\nbranch: %s\n' "$FX" "$REQ" "$B"
