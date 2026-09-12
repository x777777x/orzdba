# orzdba

A real-time MySQL & Linux/macOS host monitoring tool rewritten in Go, based on the Taobao DBA team's original Perl implementation.

## Features

Two metric domains, combinable in any way:

| Domain | Metrics | Trigger flag |
|--------|---------|--------------|
| **Host** | CPU load | `-l`/`--load` |
| | CPU usage | `-c`/`--cpu` |
| | Memory usage / full fields | `-m`/`--mem` |
| | Disk I/O (single or multiple disks) | `-d`/`--disk sda,sdb` |
| | Network rx/tx | `-n`/`--net eth0` |
| | Swap | `-s`/`--swap` |
| **Database (db)** | QPS/TPS, hit rates, InnoDB, threads, bytes, replication, semi-sync | `-mysql`/`-innodb`/`-slave`/`-semi`, etc. |

Three composite flags gather a whole domain in one command:

- `-sys`: host info bundle `-t -l -c -s -m` (no MySQL needed)
- `-mysql`: MySQL info bundle `-t -com -hit -T -B` (QPS/TPS, hit rates, threads, bytes)
- `-lazy`: common bundle `-t -l -c -s -com -hit` (host + common MySQL metrics)

### Global presentation flags

| Flag | Description |
|------|-------------|
| `--unit` | Default output is **raw numbers** (bytes/s, bytes, float percentages, no k/m/g suffix — ES-friendly for trending). Pass `--unit` to switch to human-readable units (k/m/g) |
| `--full` | Full column set for host modules (mem total/used/free/avail/buff/cached, 9 CPU columns, 8 net rx/tx columns, extended disk columns). Mem `used = total − available`, same definition as usage% (reclaimable page cache excluded) |

## Supported operating systems

| OS | System metric source | Notes |
|----|----------------------|-------|
| **Linux** | `/proc` (loadavg/stat/meminfo/vmstat/net/dev/diskstats) | Full support including disk queue/service times |
| **macOS** | Native APIs (sysctl / host_statistics / getifaddrs / IOKit) | load/cpu/mem/swap/net/disk; no iowait/steal; swap shows current usage; no disk queue/service time (`queue/await/svctm/%util` always 0) |
| Windows / BSD | — | Compiles; system metrics unimplemented (outputs 0) |

## Installation

```bash
make build
# or build directly (no build metadata injected; --version shows defaults)
go build -o bin/orzdba ./cmd/orzdba
```

`make build` injects the **version** (git tag/sha), **git commit**, and **build time** (UTC). Check with `orzdba --version`:

```
$ orzdba --version
orzdba e89276b-dirty
commit:    e89276b
built:     2026-08-31T09:14:41Z
```

## Quick start

### Linux

```bash
# Host info only (no MySQL)
./bin/orzdba -sys -i 1 -C 5

# Full MySQL monitoring, log file with daily rotation
./bin/orzdba -lazy -d sda -C 100 -i 2 -L /tmp/orzdba.log -logfile_by_day

# Host full-field monitoring (mem/cpu/net/disk), raw numbers (ES-friendly)
./bin/orzdba -m -c -n eth0 -d sda,sdb --full -i 1

# Human-readable units (k/m/g)
./bin/orzdba -lazy --unit -i 1
```

Linux disk devices are the block devices under `/dev/` (`sda`, `vda`, `nvme0n1`); interfaces are `eth0`/`ens33`, etc.

### macOS

```bash
# System info (macOS native APIs)
./bin/orzdba -sys -i 1 -C 5

# Memory/CPU/network full fields
./bin/orzdba -m -c -n en0 --full -i 1

# Disk I/O (macOS uses disk0/disk1 device names)
./bin/orzdba -d disk0 -i 1 -C 5
```

macOS disks are `disk0`/`disk1` (see `ls /dev/disk*`); interfaces are `en0`/`en1`, etc.

### Connecting to MySQL: authentication

Connects to `127.0.0.1:3306` by default. Credentials are merged **per field** by priority (a lower-priority source fills fields the higher ones leave empty):

| Priority | Source | Notes |
|----------|--------|-------|
| high | CLI: `--mysql-user`, `-H`, `-P`, `-S`, `--mysql-defaults-file` | Explicit flags; **the password is never passed on the command line** |
| ↑ | Env vars `ORZDBA_MYSQL_USER` / `ORZDBA_MYSQL_PASS` | Good for systemd `EnvironmentFile`/containers; `/proc/<pid>/environ` is only readable by the same user |
| | `/etc/orzdba.cnf` | **Recommended.** orzdba's own credential file; startup enforces 0600 + correct owner |
| | `/etc/my.cnf`, `/etc/mysql/my.cnf`, `~/.my.cnf` | Compatibility reads; warn-only |
| low | Compile-time injection (`-ldflags -X` on mysqlc inject fields) | Empty by default |

**There is no CLI password flag** — a password on the command line is visible to every local user via `ps(1)`/`/proc/<pid>/cmdline` and was removed during the security hardening.

#### Recommended authentication methods

**Method 1 — minimal-privilege account + `/etc/orzdba.cnf` (local or remote; recommended)**

Create a dedicated monitoring account with only the two grants orzdba needs (`PROCESS` for `SHOW ENGINE INNODB STATUS`, `REPLICATION CLIENT` for `SHOW SLAVE STATUS`; **no SELECT on business schemas** — even a leaked password cannot read data):

```sql
CREATE USER 'orzdba'@'localhost' IDENTIFIED BY 'strong-password';
GRANT PROCESS, REPLICATION CLIENT ON *.* TO 'orzdba'@'localhost';
```

```ini
# /etc/orzdba.cnf (must be 0600)
[client]
user = orzdba
password = strong-password
host = 127.0.0.1
port = 3306
```

```bash
./bin/orzdba -mysql -i 1 -C 5
```

**Method 2 — Unix socket (local only; the password never hits disk)**

```ini
# /etc/orzdba.cnf
[client]
user = orzdba
socket = /var/run/mysqld/mysqld.sock
```

```bash
./bin/orzdba -mysql -i 1
```

When `socket` is set it takes precedence over `host:port`.

**Method 3 — environment variables (systemd/container friendly)**

```bash
ORZDBA_MYSQL_USER=orzdba ORZDBA_MYSQL_PASS='xxx' ./bin/orzdba -mysql -i 1
```

**Method 4 — shared my.cnf (legacy compatibility)**

Credentials in the `[client]` section of any searched path work; orzdba only warns (never refuses) on shared my.cnf files. Be aware those files may also be read by other programs such as the mysql client — manage their permissions yourself.

#### What orzdba.cnf is for

- **orzdba's own credential file**: isolated from the `my.cnf` used by the mysql client, backup scripts, etc. Its `[client]` section is not read by other programs and cannot be overridden by them.
- **Enforced minimal permissions (fail-closed)**: at startup orzdba verifies the file is **0600 and owned by the running user or root**, otherwise it **refuses to start** and prints the fix (`chmod 600` / `chown`). A manual `chmod 644` mistake fails loudly instead of running with a world-readable password.
- **Highest priority**: it heads the credential search, so its fields override the shared my.cnf files.
- **Arbitrary path via flag**: a file given to `--mysql-defaults-file /path` gets the same strict check.
- **Deployment**: Ansible (`deploy/ansible/`) writes it with 0600 + correct owner and can create the minimal-privilege account — see [`deploy/ansible/README.en.md`](deploy/ansible/README.en.md).

#### Remote and TLS

```bash
./bin/orzdba -mysql -H 192.168.1.10 -P 3306 --mysql-tls -i 1
```

`--mysql-tls` enables full certificate verification (default CA validation, `ServerName` verified against the host). A private CA requires client CA config, which is not yet exposed. Note: **a remote `-H` is mutually exclusive with local system metrics** (see ops flags below).

### MySQL connection flags

| Flag | Description |
|------|-------------|
| `-H, --host` | MySQL host (default 127.0.0.1) |
| `-P, --port` | Port (default 3306) |
| `-S, --socket` | Connect via Unix socket |
| `--mysql-user` | Username |
| `--mysql-defaults-file` | Credential file path (strict 0600 + owner check) |
| `--mysql-timeout` | SQL/connect timeout (default 1s) |
| `--mysql-tls` | Enable TLS |

### Ops flags

| Flag | Description |
|------|-------------|
| `-C, --count` | Sample count. Emits **N+1 rows** (a leading baseline tick, matching the Perl original) |
| `--daemon` | Run in the background (daemonize; Unix only, not Windows). Without `-L`, writes `/tmp/orzdba.log` (daily-rotated) |
| `-L <path> --also-stdout` | Write to file and also to stdout (tee); add `-logfile_by_day` for daily rotation |
| `-noheader` | Suppress the title block and periodic headers |
| `--sep <s>` | Custom data-row column separator (default `\|`; `\t` is a tab; every numeric column uses it, the time column stays whole) |
| `-ip[=<addr>]` | Emit an IP column. Bare `-ip`: monitored host (local machine IP for a local MySQL, else the `-H` address); `-ip <addr>` uses that address verbatim |

> **Remote-MySQL mutual exclusion**: with `-H` pointing at a remote MySQL, specifying local system-metric flags (`-l`/`-c`/`-s`/`-m`/`-d`/`-n`/`-sys`/`-lazy`) is an error — local sys metrics mixed with a remote DB would be misleading. Only a local `-H` (`127.0.0.1`/`localhost`/this host's IP) may mix.

Examples:

```bash
# Background MySQL monitoring, daily-rotated log
./bin/orzdba --daemon -mysql -L /var/log/orzdba.log -logfile_by_day

# Foreground, to screen and file at once
./bin/orzdba -sys -L /tmp/orzdba.log --also-stdout -i 1

# No header, comma-separated (for spreadsheets/scripts)
./bin/orzdba -sys -noheader --sep , -i 1 -C 5
```

Run `orzdba -h` for the full flag list.

### Stop and signals

SIGTERM / SIGINT / SIGHUP (terminal hangup) all exit gracefully: the `-rt` tcprstat child is stopped and its lock/log files removed, then the log is closed. `kill -9` triggers no cleanup — the tcprstat child survives (keeps capturing); the port lock file is reclaimed automatically by the next instance.

> **Known limitation (`-rt` port lock)**: the lock uses a PID file with liveness probing; two rare defects are deliberately kept — ① PID reuse can misjudge a stale lock as held (start refused; after confirming per the error that the PID is not orzdba, delete the lock file to recover); ② two instances reclaiming the same stale lock within a sub-millisecond window may both hold it (duplicate capture; logs are per-PID files so data is not corrupted). A proper fix needs flock — rationale in `internal/rtcol/tcprstat.go`, `acquireLock`.

## Design highlights

- **One SQL per tick**: `StatusSource` issues a single `SHOW GLOBAL STATUS` per interval and shares the result with all MySQL submodules — no per-module queries like orzdba-go.
- **Single connection**: `MaxOpenConns=1`, no pool, avoiding connection churn; auto-reconnects when the connection drops.
- **Zero fork**: `/proc` reads and the MySQL protocol use the Go standard library; the only external command is `tcprstat` (`-rt`, Linux-only, requires `/usr/bin/tcprstat`).
- **Counter safety**: a rolled-back cumulative counter (e.g. MySQL restart) yields 0 rather than a negative rate; rates use the real sample window.

## Project layout

```
cmd/orzdba/           CLI entry: flag parsing, main loop, title block
internal/metric/      Shared types: Cell, Group, Color
internal/syscol/      System collectors: load, cpu, swap, net, disk (Linux /proc + macOS native, build-tag split)
internal/mycol/       MySQL collectors: com, hit, innodb_*, threads, bytes, slave, semi
internal/rtcol/       tcprstat response-time collector (Linux only)
internal/mysqlc/      MySQL connection & credential resolution (orzdba.cnf, my.cnf, env)
internal/render/      ANSI color rendering & column formatting
internal/logsink/     Output: stdout / single file / daily-rotated file
testdata/             Golden /proc samples for unit tests
```

## Testing

```bash
make test   # run all unit tests
```

Design docs and the roadmap live in [`go-rewrite-plan.md`](go-rewrite-plan.md) and `docs/`.
