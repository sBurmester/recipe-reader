# Recipe Reader

Imports recipes from Instagram saved posts, extracts structured data, and serves a searchable web UI. See `docs/superpowers/plans/2026-09-05-recipe-reader-implementation.md` for the implementation plan.

## Development

    cp .env.example .env
    make db-up      # starts Postgres via docker-compose
    make frontend   # builds web/ and embeds it
    make run

## Configuration

Configuration is handled by [kong](https://github.com/alecthomas/kong): every setting can be
supplied as a command-line flag or an environment variable, with flags taking precedence over
the environment and the environment over the built-in defaults. Run `recipe-reader --help` for
the full list. See `.env.example` for the environment-variable names and their defaults.

## Testing

    make check   # gofmt + go vet + golangci-lint + govulncheck + go test
                 # go test spins up ephemeral Postgres containers via testcontainers-go — Docker must be running.
