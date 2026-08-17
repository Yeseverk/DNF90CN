# DNF90 Project Working Agreement

## Start Here

- Read `changlog.md` before changing code, deployment scripts, generated configuration, or runtime behavior.
- Keep `go-server` source-only. Runtime configuration, binaries, logs, PVF, database state, and PID files belong under `runtime`.
- Edit `runtime/config/instance.json` or `deploy/templates/instance.example.json`; do not hand-edit generated files under `runtime/configs`.
- Windows deployment entrypoints must stay BAT-only. Do not add PowerShell scripts or PowerShell calls.

## Source Boundaries

- `go-server/internal/app/dnf90` is the single composition root.
- `go-server/internal/services/dnfbridge` owns transport, session state, packet ordering, and current-client compatibility.
- `go-server/internal/modules/dnf` owns DNF domain rules and repository transactions.
- `go-server/internal/services/logic/dnf` owns repository assembly.
- Do not split the large bridge package merely for cosmetic directory changes. Extract only behind tests and current-client evidence.

## Data and Protocol Rules

- Current EXE behavior is authoritative for uncertain packet layouts.
- Never invent inventory, character, database, PVF, channel, or map data to hide a missing dependency.
- The local profile is single-account and local-only. The 90CN bootstrap/channel download endpoint, admin endpoint, and MySQL stay on loopback. Dynamic channel-directory advertisements and every game port must use an automatically detected or explicitly configured private LAN IPv4 and bind only that local interface; never use a wildcard or public address.
- The `90cn-decode-bypass-v1` protocol profile, asset hashes, and client hashes are a compatibility unit. Change them together and record evidence.

## Validation

Run from `go-server`:

```text
gofmt -w <changed-go-files>
go list -buildvcs=false ./...
go test -buildvcs=false -count=1 ./internal/modules/dnf/...
go test -buildvcs=false -count=1 ./internal/services/dnfbridge
go test -buildvcs=false -count=1 ./cmd/server/control
go vet ./cmd/server/control ./cmd/server/dnf90 ./cmd/server/doctor ./internal/app/dnf90 ./internal/services/dnfbridge
go build -buildvcs=false -o ..\runtime\bin\DNF90Server.exe ./cmd/server/dnf90
```

Run deployment preflight without a database:

```text
..\deploy\windows\control.bat check --skip-database
```

## Documentation

- Record meaningful code, deployment, database, test, and compatibility changes in `changlog.md`.
- Never record plaintext generated passwords in `changlog.md`, logs, screenshots, commits, or support bundles.
- The requested runtime profile deliberately uses a plaintext `root` DSN. Keep it loopback-only and never commit generated runtime configuration.
