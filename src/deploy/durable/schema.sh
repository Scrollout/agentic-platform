#!/bin/sh
# The `durable-schema` job (research R1): makes the Temporal and visibility databases in the
# `durable-store` PostgreSQL and brings their schemas to the version the pinned admin-tools image
# carries. Run once per start; safe to run again, and a second run changes nothing.
#
# The password comes from the file named by DURABLE_PASSWORD_FILE and reaches temporal-sql-tool
# through SQL_PASSWORD, never its command line.
#
# Each database is first brought to the pinned schema version with `update-schema`: when that
# succeeds the database is already set up and nothing else runs, so a second run changes nothing.
# When it fails, the database or its version tables are missing, and the job makes them: `create`
# (a database that already exists is the one error it accepts, recognised by its message) and
# `setup-schema`, then `update-schema` again, all of which must succeed. A wrong password or an
# unreachable store fails `create`, so the job fails.
#
# The namespace `ap` is not made here: it needs the running server, which starts only after this
# job has completed. The engine registers it when it connects (ADR 0070, src/deploy/README.md).
set -eu

: "${DURABLE_PASSWORD_FILE:?DURABLE_PASSWORD_FILE names the file holding the store's password}"
: "${POSTGRES_SEEDS:?POSTGRES_SEEDS names the store's host}"
: "${POSTGRES_USER:?POSTGRES_USER names the store's user}"
SQL_PASSWORD=$(cat "$DURABLE_PASSWORD_FILE")
export SQL_PASSWORD
port=${DB_PORT:-5432}
schemas=/etc/temporal/schema/postgresql/v12

tool() { temporal-sql-tool --plugin postgres12 --ep "$POSTGRES_SEEDS" -u "$POSTGRES_USER" -p "$port" "$@"; }

for pair in temporal:temporal temporal_visibility:visibility; do
  db=${pair%%:*}
  versioned=$schemas/${pair#*:}/versioned
  echo "schema: $db"
  if tool --db "$db" update-schema -d "$versioned" >/dev/null 2>&1; then
    continue
  fi
  if ! out=$(tool --db "$db" create 2>&1); then
    case $out in
      *"already exists"*) ;;
      *) echo "schema: create of $db failed: $out" >&2; exit 1 ;;
    esac
  fi
  tool --db "$db" setup-schema -v 0.0
  tool --db "$db" update-schema -d "$versioned"
done
echo "schema: up to date"
