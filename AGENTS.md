# Repository Guidelines

## Project Structure & Module Organization

- `cmd/notepad/` — service entry point. Flags: `-addr`, `-tls-addr`, `-unix`, `-prefix`, `-data`, `-users`.
- `internal/store/` — note CRUD, filename validation, atomic writes.
- `internal/auth/` — user accounts (PBKDF2 password hashes, `users.json`), sessions, login rate limiting.
- `internal/server/` — JSON API, gateway prefix stripping, embedded assets.
- `web/` — SPA (`index.html`, `assets/app.css`, `assets/app.js`), compiled in with `go:embed`.
- `tools/icongen/` — generates the 64/128/256 px icons.
- `packaging/` — fnOS package template: `manifest`, `cmd/main`, `config/privilege`, `app/ui/`.
- `scripts/build-fpk.sh` — produces `dist/notepad-<version>.fpk`.

## Build, Test, and Development Commands

```bash
make run                  # 本地启动 http://127.0.0.1:18180
make test                 # go test ./...
make vet                  # go vet ./...
make icons                # 生成图标到 build/icons
./scripts/build-fpk.sh    # 打包 fnOS 安装包到 dist/
```

If the Go build cache is read-only, prefix with `GOCACHE=/tmp/go-cache`. Add `-buildvcs=false` when the tree is not a git checkout.

## Coding Style & Naming Conventions

- Go: `gofmt` (tabs); run `gofmt -w .` before committing. **Standard library only** — do not add third-party dependencies.
- Frontend: plain ES2020, 2-space indent, no frameworks and no build step. Use relative URLs (`assets/…`, `api/…`) so the app also works behind `/app/notepad/`.
- Manifest keys are lowercase snake_case (`display_name`, `desktop_uidir`); `appname` must be lowercase ASCII.
- User-facing text, comments and docs are Chinese.

## Testing Guidelines

- Go's standard `testing` package; files sit next to the code (`internal/store/store_test.go`).
- Name tests `TestXxx`. Cover name validation, path-traversal rejection, atomic writes, API round-trips and gateway prefix handling.
- Handlers are tested with `httptest`, which needs no sockets. Run `make test` before packaging.

## Commit & Pull Request Guidelines

- This workspace has no git history; keep the existing convention: `feat:` / `fix:` / `docs:` prefix plus a Chinese summary, e.g. `fix: 修正 fpk 清单 platform 取值`.
- Bump `version` in `packaging/manifest` and add a `CHANGELOG.md` entry for any user-visible change.
- PRs should state the fpk version, which entry points were verified (`:18180`, `:18181`, `/app/notepad/`), and include a `tar -tzf` listing when the package layout changes.

## Security & Configuration

- A note name must never escape the notes directory; all file access goes through `internal/store`.
- Every `/api/*` route except `session`, `setup`, `login` and `logout` requires a valid session cookie; new routes must be wrapped in `api.requireAuth`.
- Passwords are never stored in the clear: `users.json` holds PBKDF2-HMAC-SHA256 hashes and must stay `0600`.
- Notes live in `${TRIM_PKGVAR}/notes` (fnOS: `/vol1/@appdata/notepad/notes`) and must survive upgrade and uninstall.
