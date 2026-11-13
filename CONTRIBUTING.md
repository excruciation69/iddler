# Contributing

Thanks for wanting to make Iddler better!

## Before you start

- For bugs, open an issue with the steps to reproduce, your OS and the Iddler version (Settings → About).
- For new features, open an issue first so we can agree on the idea before you spend time on code.

## Development

```sh
go test -race ./...                       # the engine is tested against a fake Steam
IDDLER_UI_DIR=$PWD/ui UI_SERVE=1 go test ./internal/server -run TestServeUI -timeout 1h
```

The second command starts a demo hub with sample accounts; edit files in `ui/` and reload the page.

## Guidelines

- Go standard library only. No new dependencies.
- The interface is plain HTML, CSS and JavaScript without a framework. Keep animations on `transform` and `opacity`.
- Every user-facing string lives in `scripts/i18n.py` in both Russian and English. Run `python3 scripts/i18n.py` after editing it.
- Run `gofmt` and `go vet ./...` before opening a pull request.
