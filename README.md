# orzdba

Go rewrite of [orzdba](https://github.com/), a real-time MySQL & Linux host
monitoring tool originally written in Perl by the Taobao DBA team.

## Status

Under active development. The implementation roadmap lives in
[`go-rewrite-plan.md`](go-rewrite-plan.md) (v2.0) at the repo root.

Current milestone: **M8 — logsink rotation** (done). All planned milestones
M0–M8 are implemented; only M9 (behavior-alignment golden tests vs the Perl
original) remains.

- **M0** — repository skeleton, CI, package stubs.
- **M1** — CLI: pflag parsing, single-dash long-flag shim (`-sys`/`-mysql`),
  composite-flag expansion before the first sample, custom usage, title
  block, polling loop with `-C`/`-t`/`-i`, SIGINT/SIGTERM cleanup.
- **M2** — `/proc` system collectors: load, cpu, swap, net, disk; ANSI
  renderer with the two separator styles (sys blue-bold, mysql green),
  15-row header repeat, `nocolor` (zero ANSI bytes), `FormatBytesRate`
  (k/m). Unit tests with golden `/proc` samples.
- **M3** — MySQL connection + credential resolution: self-implemented my.cnf
  parser, priority merge (CLI > env > defaults-file > default search >
  inject), single long-lived `database/sql` connection (password in DSN,
  never in argv; logged DSN masks the password), title DB-name line +
  `SHOW VARIABLES` section (3 per line, G/M for byte-sized vars).
- **M4** — `-mysql`/`-innodb` collectors: com (QPS/TPS), hit, threads (with
  thread cache hit%), bytes, innodb_rows/pages/data/log, innodb_status
  (SHOW ENGINE INNODB STATUS parse). One `SHOW GLOBAL STATUS` per tick
  feeds all submodules; first-tick zeros; per-tick deltas/rates.
- **M5** — `SHOW ENGINE INNODB STATUS` text parse (history list, log
  unflushed/uncheckpointed, read views, queries inside/queued).
- **M6** — `-slave` (SHOW SLAVE STATUS), `-semi` (Rpl_semi_sync_*),
  `-hit full` (5-column extended hit: KeyBuffer/Index/Qcache/lor/Innodb).
- **M7** — `-rt` tcprstat RT collector: hardcoded `/usr/bin/tcprstat`,
  PID-tracked subprocess (no `killall`), 0600 log + O_EXCL lock,
  SIGTERM→200ms→SIGKILL cleanup, crash-retry-once. **Linux-only** (needs
  the tcprstat binary; errors cleanly on macOS per §11.1).
- **M8** — `internal/logsink`: stdout / single file (0600) / daily-rotated
  file (`-logfile_by_day`, suffix `.YYYY-MM-DD`). Day-rollover reprints the
  title and resets the `-C` counter (§7.13).

> **Deviations from Perl (intentional):**
> - Time column prints `YYYY-MM-DD HH:MM:SS` instead of `HH:MM:SS` (full
>   date per line; header widened to match).
> - `-n` net parsing uses `strings.Fields` (colon stays on the device name,
>   recv=field[1]/send=field[9]) — the Perl `split(/\s+|:/)` is off-by-one
>   and reads empty fields; this is the documented §7.1 fix.
> - Device-existence startup check (`-d`) is skipped when `/proc/diskstats`
>   is absent (non-Linux dev hosts) so the tool degrades to zeros instead of
>   false-erroring; on Linux a genuinely bad device name still errors (§11.1).

## Build

```bash
make build
# or
go build -o bin/orzdba ./cmd/orzdba
```

## Run

```bash
./bin/orzdba -h
```

## Project layout

```
cmd/orzdba/      CLI entrypoint
internal/metric/      shared sample types
internal/syscol/      /proc-based system collectors (load, cpu, swap, net, disk)
internal/mycol/       MySQL-based collectors (com, hit, innodb_*, threads, bytes, slave, semi)
internal/rtcol/       tcprstat-based DB response-time collector
internal/mysqlc/      MySQL connection + credential resolution (my.cnf, env, CLI, ldflags)
internal/render/      ANSI colors + column formatting
internal/logsink/     stdout / file / daily-rotated file sinks
testdata/        golden samples for unit tests
```

## Design constraints

- **Performance**: 0 fork per tick, ≤ 2 SQL per tick, RSS ≤ 30 MB.
- **Safety**: MySQL password never appears in process argv; log files 0600;
  tcprstat subprocess tracked by PID (no `killall`).
- **No shell-out**: `cat`/`grep`/`sed`/`awk`/`mysql`/`ifconfig` are all replaced
  by Go standard library or native drivers.

See the plan doc for the full performance & safety budget.
