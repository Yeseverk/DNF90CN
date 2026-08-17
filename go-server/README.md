# Go server source

This module is the minimal source closure needed to build and test the current DNF90 service.

Key locations:

- `cmd/server/dnf90`: product entry.
- `cmd/server/doctor`: deployment preflight.
- `cmd/server/control`: native BAT-facing deployment controller.
- `internal/app/dnf90`: single composition root.
- `internal/services/dnfbridge`: current-client transport and session implementation.
- `internal/modules/dnf`: DNF domain modules.
- `internal/services/logic/dnf`: MySQL repository assembly.
- `internal/platform`: only framework packages reachable by the DNF90 build/tests.

Runtime assets and generated configs intentionally do not live in this module. Build from this directory:

```text
go build -buildvcs=false -mod=readonly -trimpath -o ..\runtime\bin\DNF90Control.exe ./cmd/server/control
go build -buildvcs=false -mod=readonly -trimpath -o ..\runtime\bin\DNF90Server.exe ./cmd/server/dnf90
go build -buildvcs=false -mod=readonly -trimpath -o ..\runtime\bin\DNF90Doctor.exe ./cmd/server/doctor
```
