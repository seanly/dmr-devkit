# Turso compatibility evaluation

Run tape and plugin smoke tests comparing **modernc.org/sqlite** (baseline) and **tursogo** (Turso Database).

## Tape (dmr-devkit)

```bash
# CI baseline — modernc only
go test ./tape/sqlcompat/ -run TestEvalModernc -v

# Full matrix — requires tursogo
go test -tags turso_eval ./tape/sqlcompat/ -run TestEvalTursoCompare -v

# Turso native FTS (Tantivy) — not SQLite FTS5; maps DMR tapeSearch scenarios
go test -tags turso_eval ./tape/sqlcompat/ -run TestEvalTursoNativeFTS -v
```

## Plugins (dmr)

```bash
go test ./eval/turso/ -run TestPluginsModernc -v
go test -tags turso_eval ./eval/turso/ -run TestPluginsCompare -v
```

## Turso Sync spike (optional)

Requires Turso Cloud credentials:

```bash
export TURSO_SYNC_URL='libsql://...'
export TURSO_AUTH_TOKEN='...'
go test -tags turso_sync_eval ./tape/sqlcompat/ -run TestSyncSpike -v
```

## Impact report

See [turso-impact.md](../docs/eval/turso-impact.md) for the Go/No-Go decision.
