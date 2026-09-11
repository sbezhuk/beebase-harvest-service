# beebase-harvest-service

Harvest tracking service for [BeeBase](https://github.com/sbezhuk/beebase-auth-service#trust-model),
an open-source backend for a beekeeper management application split into
microservices. See [CLAUDE.md](https://github.com/sbezhuk/beebase-auth-service/blob/main/CLAUDE.md)
for the architectural rules this service follows.

Register/login/refresh live in `beebase-auth-service`, apiaries in
`beebase-apiary-service`, hives in `beebase-hive-service`, inspections in
`beebase-inspection-service` — this service only manages harvest records
(honey, pollen, propolis, wax collected from a hive), and never trusts a
user ID or a hive's ownership from anywhere but a verified access token
and a live check against hive-service.

Related services: `beebase-auth-service` (users, refresh tokens, JWT
issuing), `beebase-hive-service`, `beebase-gateway` (single entry point
for clients).

## Requirements

- Go 1.27+
- PostgreSQL 16 (or Docker, to run it for you)
- [golang-migrate](https://github.com/golang-migrate/migrate) CLI, for applying
  migrations outside Docker: `make migrate-install`
- A running `beebase-auth-service` (or anything serving a compatible
  JWKS document) reachable at `AUTH_JWKS_URL`
- A running `beebase-hive-service` reachable at `HIVE_SERVICE_URL`

## Quick start

```bash
cp .env.example .env
# point AUTH_JWKS_URL and HIVE_SERVICE_URL at running services, e.g.
#   http://localhost:8081/.well-known/jwks.json and http://localhost:8083

# Option A: run Postgres in Docker, app on the host
docker compose up -d postgres
make migrate-up
make run

# Option B: run everything in Docker (migrations run once, automatically)
docker compose up --build
```

Verify it's up:

```bash
curl http://localhost:8080/health   # liveness — always 200 while the process is up
curl http://localhost:8080/ready    # readiness — 200 only if the database is reachable

TOKEN=...   # an access_token from auth-service's /api/v1/auth/register or /login
HIVE_ID=... # a hive that TOKEN's owner created via hive-service

curl -X POST http://localhost:8080/api/v1/hives/$HIVE_ID/harvest \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"product":"HONEY","amount":12.5,"unit":"kg"}'

curl "http://localhost:8080/api/v1/hives/$HIVE_ID/harvest" -H "Authorization: Bearer $TOKEN"
```

The full API surface is documented in [api/openapi.yaml](api/openapi.yaml).

Note: this repo's `docker-compose.yml` is for standalone single-service
development only. To run the full BeeBase stack together, use
`beebase-gateway`'s docker-compose, which builds every service from
sibling checkouts and routes between them.

## Configuration

All configuration is via environment variables (see
[.env.example](.env.example) for the full list — it is a template only,
never read by the app, Docker Compose, or deployment tooling; copy it
once to create your real `.env`, which is what actually gets loaded).
Production configuration is generated at deploy time from AWS SSM
Parameter Store (see `beebase-gateway/deploy/deploy.sh`) — `.env.example`
is never used as a fallback, in development or in production.

| Variable                   | Default                    | Description                              |
| --------------------------- | --------------------------- | ----------------------------------------- |
| `APP_ENV`                  | `development`               | `development` or `production`             |
| `LOG_LEVEL`                 | `info`                       | `debug`, `info`, `warn`, `error`           |
| `HTTP_PORT`                 | `8080`                       | Port the HTTP server listens on           |
| `HTTP_READ_TIMEOUT`         | `5s`                         | Request read timeout                      |
| `HTTP_WRITE_TIMEOUT`        | `10s`                        | Response write timeout                    |
| `HTTP_IDLE_TIMEOUT`         | `60s`                        | Keep-alive idle timeout                   |
| `HTTP_SHUTDOWN_TIMEOUT`     | `15s`                        | Max time to wait for graceful shutdown    |
| `DATABASE_URL`              | *(required)*                 | PostgreSQL DSN                            |
| `DATABASE_CONNECT_TIMEOUT`  | `5s`                         | Timeout for the initial DB connection      |
| `AUTH_JWKS_URL`             | *(required)*                 | auth-service's public key endpoint, used to verify access tokens |
| `HIVE_SERVICE_URL`          | *(required)*                 | hive-service's base URL, used to confirm hive ownership on every operation |
| `TEST_DATABASE_URL`         | *(unset)*                    | Used only by `make test-integration`, never by the app |

## Project structure

```
cmd/server/                       entry point: wires config, logger, db, services, server
api/openapi.yaml                    API contract
migrations/                         SQL migrations (golang-migrate format)
internal/
  domain/harvest/                     Harvest entity, Product/Unit enums, Repository port; no infrastructure dependency
  application/harvest/                use cases: create, get, list, update, delete;
                                        HiveVerifier port (hive ownership check, on every call)
  platform/hiveclient/                HiveVerifier implemented by calling hive-service over HTTP
  repository/postgres/                domain port implemented against PostgreSQL (pgx, explicit SQL)
  transport/http/                    chi router, health/ready handlers
    harvest/                            harvest HTTP handlers, request validation, responses
```

logger, JSON response/error helpers, the graceful-shutdown server wrapper,
and JWKS-based access-token verification (`RequireAuth` middleware) all
come from [beebase-common](https://github.com/sbezhuk/beebase-common),
shared by every BeeBase service.

## Domain

Harvest is an independent domain, not nested under Inspection:

```
User -> Apiary -> Hive -> Harvest
```

A hive may have zero, one, or several harvest records - at most one per
product (`HONEY`, `POLLEN`, `PROPOLIS`, `WAX`), enforced by
`UNIQUE (hive_id, product)`. Each product only accepts certain units (no
automatic conversion, e.g. Honey is never converted between `kg` and
`l`): `HONEY` → `kg`/`l`, `POLLEN` → `g`/`kg`, `PROPOLIS` → `g`,
`WAX` → `g`. `amount` must be `>= 0`.

## Ownership

Unlike every other nested resource in BeeBase, harvest-service **does
not** denormalize hive ownership onto its own rows, and has no
`user_id`/foreign-key relationship to hive-service's database at all
(hive-service and harvest-service each own a separate database, per the
project's microservice convention). Instead, every operation - create,
get, list, update, delete - forwards the caller's own access token to
hive-service's `GET /api/v1/hives/{hiveId}` and trusts its answer live,
**on every call**:

- a 200 means whoever holds that token owns that hive (and, transitively,
  its apiary - hive-service's own check is itself transitive against
  apiary-service);
- a 404 means they don't (or it doesn't exist) - collapsed into the same
  `404 hive_not_found` response either way, so a hive's existence can't
  be probed.

This is a deliberate departure from the "verify once, denormalize
afterward" pattern the rest of BeeBase uses (see e.g. inspection-service
denormalizing its verified hive owner): harvest never caches that
ownership decision, so a hive's access changing (e.g. ownership transfer,
were that ever added) takes effect on harvest immediately, at the cost of
one extra cross-service call per request.

A request for another user's harvest returns the same `404
harvest_not_found` (or `404 hive_not_found`, if the hive check fails
first) as one that doesn't exist, never a `403`.

**Known limitation:** if a hive is deleted upstream, its harvest records
here are not cascade-deleted or notified - there's no event bus or outbox
between services yet (CLAUDE.md defers full synchronization), and
hive-service's own delete cascade (which today reaches inspection-service
and media-service) does not yet call harvest-service. Those harvest
records become orphaned but remain independently accessible via the API
until this is addressed - the same limitation inspection-service already
documents for its own relationship to hive/apiary deletion.

## Development

```bash
make run                # go run ./cmd/server
make fmt                # go fmt ./...
make vet                # go vet ./...
make test               # unit tests: go test ./...
make lint               # golangci-lint run

make migrate-up         # apply migrations to DATABASE_URL
make migrate-down       # roll back the last migration
make migrate-new name=add_something   # scaffold a new migration pair

make build              # build binary into bin/
```

### Integration tests

Integration tests exercise the PostgreSQL repository and the full HTTP
CRUD flow — including a real JWKS round trip, a fake hive-service
standing in for the real cross-service ownership check, and two
independently authenticated users proving cross-user access is
impossible — against a real database. They're gated on
`TEST_DATABASE_URL` and skip themselves (not fail) if it's unset, and
every test runs inside a transaction that's rolled back afterward, so
they never leave rows behind or need manual cleanup.

```bash
docker compose up -d postgres
createdb -h localhost -p 5437 -U beebase beebase_harvest_test
migrate -path migrations -database "$TEST_DATABASE_URL" up

TEST_DATABASE_URL=postgres://beebase:beebase@localhost:5437/beebase_harvest_test?sslmode=disable \
  make test-integration
```
