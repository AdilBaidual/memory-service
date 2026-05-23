.PHONY: up down build \
        test-component test-fixtures test-persistence \
        tests

# ─── Stack ───────────────────────────────────────────────────────────────────

up:
	docker compose up -d --build

down:
	docker compose down

build:
	docker compose build

# ─── Individual test commands ─────────────────────────────────────────────────

## Contract roundtrip + malformed input + concurrent sessions
## Tests run inside a Docker container and call the service via HTTP.
test-component: up
	docker compose -f docker-compose.yml -f docker-compose.test.yml \
		run --rm --build component-tests
	docker compose -f docker-compose.yml -f docker-compose.test.yml \
		rm -f component-tests 2>/dev/null || true

## Fixture quality test — measures recall quality, outputs X/Y hits per scenario.
## No assertions: copy the OVERALL line into CHANGELOG after each iteration.
test-fixtures: up
	docker compose -f docker-compose.yml -f docker-compose.test.yml \
		run --rm --build fixture-tests
	docker compose -f docker-compose.yml -f docker-compose.test.yml \
		rm -f fixture-tests 2>/dev/null || true

## Restart persistence test — runs on host, restarts the service container.
## Requires: docker compose stack already up (make up).
test-persistence:
	bash tests/test_persistence.sh

# ─── Run all tests ────────────────────────────────────────────────────────────

## Runs all three test types in sequence.
tests: test-component test-persistence test-fixtures
