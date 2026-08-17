# Bundled runtime inputs

The Windows release package is built with these local binary inputs:

- `mysql/mysql-8.4.10-winx64.zip`: unmodified MySQL Community Server 8.4.10 Windows x64 ZIP from the URL recorded in `../assets/mysql-portable.json`.
- `vcruntime/x64/*.dll`: Microsoft x64 application-local runtime files from the redistributable version recorded in `../assets/vcruntime-app-local.json`.

The manifests record each file's exact byte size and SHA256. The controller verifies those values before extraction or installation and then runs `mysqld.exe --version` before initialization. MySQL's upstream license is contained in its ZIP and is installed as `runtime/mysql/server/LICENSE`.

Large third-party binaries are intentionally excluded from Git history. A distributable ZIP must include the exact files named by both manifests.
