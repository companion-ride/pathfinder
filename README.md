# pathfinder

Routing service for Companion. Builds driving routes through the 2GIS Routing API, stores them in PostGIS and answers questions about them: where the nearest point of a route is for a passenger and how to walk there. The main client is rides; behind the gateway it is mounted at `/api/pathfinder`.

Go 1.25, Gin, PostgreSQL + PostGIS, Redis.

## API

All `/routes` endpoints require a JWT signed with the shared `JWT_SECRET_KEY` (`Authorization: Bearer ...`). Full spec: `/docs` (Swagger UI) and `/docs/swagger.json`.

| Method | Path | |
|---|---|---|
| POST | `/routes` | build a route from origin to destination via 2GIS, save it to PostGIS, cache it in Redis for 3 h |
| POST | `/routes/custom` | save a driver-drawn polyline as a route; distance by haversine, duration from the configured average speed, no 2GIS call |
| GET | `/routes/{id}` | route from Redis, falls back to PostGIS |
| GET | `/routes/{id}/raw` | raw 2GIS response, while it is cached |
| POST | `/routes/{id}/closest-point` | project a passenger coordinate onto the route |
| GET | `/routes/{id}/closest-points` | closest-point results cached for the route |
| POST | `/routes/{id}/walk-to-closest` | closest point plus a walking path to it from the 2GIS pedestrian API |
| GET | `/routes/{id}/walks` | cached walking results |
| GET | `/health` | DB health |

`/app` (map debug page) and `/config` (returns the 2GIS key for it) are registered only with `ENABLE_DEBUG_PAGES=true`. Keep it off in production.

## 2GIS keys

`DOUBLE_GIS_API_KEYS` takes a comma-separated list. When 2GIS answers 429 the client moves on to the next key and cools the limited one down for as long as its `Retry-After` header says. `DOUBLE_GIS_API_KEY` (a single key) is read only when the list is empty.

Route mode (`fastest`), traffic mode (`jam`), locale (`kk`), request timeout (30 s) and the average speed for custom routes (11.11 m/s) are defaults that config-service can override; they are polled every 30 s when `CONFIG_SERVICE_URL` and `CONFIG_SERVICE_API_KEY` are set.

## Configuration

See `.env.example`.

| Var | |
|---|---|
| `JWT_SECRET_KEY` | required, same value as auth-service |
| `DB_HOST`, `DB_PORT`, `DB_DATABASE`, `DB_USERNAME` | required; `DB_PASSWORD`, `DB_SCHEMA` optional. Migrations run on startup |
| `DOUBLE_GIS_API_KEYS` | required (or `DOUBLE_GIS_API_KEY`) |
| `REDIS_ADDR`, `REDIS_PASSWORD` | Redis for the route cache. In `docker-compose.prod.yml` Redis is not published and will not start without `REDIS_PASSWORD` |
| `PORT` | default 8080 |
| `DOUBLEGIS_BASE_URL` | default `https://routing.api.2gis.com/routing/7.0.0/global` |
| `CONFIG_SERVICE_URL`, `CONFIG_SERVICE_API_KEY` | optional, see above |
| `GATEWAY_URL`, `GATEWAY_SECRET`, `SERVICE_HOST` | self-registration with api-gateway |
| `SWAGGER_HOST`, `SWAGGER_BASE_PATH` | override host and base path in the served spec |
| `ENABLE_DEBUG_PAGES` | see above |

## Local Development

```bash
cp .env.example .env   # fill in DOUBLE_GIS_API_KEYS and JWT_SECRET_KEY
```

With everything in Docker, set `DB_HOST=psql_bp` and `REDIS_ADDR=redis:6379` in `.env`, then:

```bash
make docker-run        # service on :8080 + postgis + redis
```

To run the service on the host, keep `localhost` in `.env` and start only the dependencies:

```bash
docker compose up -d psql_bp redis
make run               # or `make watch` for live reload
```

`make swagger` regenerates `docs/swagger.json`.

## Testing

```bash
make test       # all tests, including DB tests that start a PostGIS container (requires Docker)
make itest      # only the DB tests
```

## Deploy

Pushes to `main` run `.github/workflows/deploy.yml` on a self-hosted runner, which starts `docker-compose.prod.yml` with values from the repository secrets.
