#!/bin/sh
set -eu

fail() {
    echo "compliance check failed: $1" >&2
    exit 1
}

for path in \
    cserver/lookup.h \
    internal/vector/id_lookup.go \
    scripts/gen_c_id_lookup.py \
    scripts/gen_id_lookup.py
do
    [ ! -e "$path" ] || fail "$path must not be present"
done

if rg -n "test-data|KnownIDApproved|EXACT_ID_LOOKUP|expected_approved" \
    README.md Dockerfile Dockerfile.runtime docker-compose.yml cmd internal scripts \
    -g '!scripts/check_compliance.sh' >/tmp/rinha-compliance-rg.log
then
    cat /tmp/rinha-compliance-rg.log >&2
    fail "preview payload lookup references remain"
fi

[ -f LICENSE ] || fail "LICENSE is required"
rg -q "MIT License" LICENSE || fail "LICENSE must be MIT"
