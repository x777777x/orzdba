# orzdba — Ansible deployment

Deploys the orzdba binary, the orzdba-owned credential file `/etc/orzdba.cnf`
(0600, correct owner — orzdba **refuses to start** otherwise), a systemd unit,
and (optionally) the minimal-privilege MySQL monitoring account.

## Layout

```
ansible/
├── inventory.example
├── group_vars/
│   └── all.yml              # variables (password lives in a vault file)
├── playbooks/
│   ├── deploy.yml           # first-time: binary + cnf + unit + start (+ mysql user)
│   ├── start.yml            # start after a stop / manual downtime
│   ├── stop.yml             # stop the fleet
│   └── upgrade.yml          # rolling binary upgrade, batched, with rollback
├── releases/                # put the compiled binaries here (gitignored)
│   └── orzdba-linux-amd64
└── roles/orzdba/
    ├── defaults/main.yml
    ├── tasks/               # deploy / upgrade / mysql-user / health
    └── templates/           # orzdba.cnf.j2, orzdba.service.j2
```

## Build the binary

```bash
cd <repo root>
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o deploy/ansible/releases/orzdba-linux-amd64 ./cmd/orzdba
```

## Credentials (vault)

Put the real secrets in an ansible-vault file — never in `all.yml`:

```bash
ansible-vault create group_vars/all.vault.yml
#   orzdba_mysql_password: "the-real-secret"
#   orzdba_mysql_admin_password: "admin-secret"   # only if creating the account
```

The playbook renders the password into `/etc/orzdba.cnf` with mode 0600 and the
correct owner, so the only plaintext copy on the host is that root-readable
file. There is **no CLI password flag** — a password in argv is visible to
every local user via `ps(1)` and was removed.

## Minimal-privilege MySQL account

orzdba only needs two grants — `PROCESS` (for `SHOW ENGINE INNODB STATUS`) and
`REPLICATION CLIENT` (for `SHOW SLAVE STATUS`). Set
`orzdba_create_mysql_user: true` and the playbook creates the account with
exactly those grants on `localhost`. A leaked password is then bounded to
read-only server state, not the data.

If you use a dedicated OS user for the service (not root), set
`orzdba_service_user: orzdba` and the credential file will be owned by that
user; orzdba's strict check accepts "running user or root".

## What /etc/orzdba.cnf is for

- **orzdba-owned**: it does not share a `[client]` section with the mysql
  client, backup scripts, etc. — no cross-talk, no accidental overrides.
- **Program-enforced minimal permissions**: orzdba verifies at startup that
  the file is 0600 and owned by the running user or root, and refuses to
  start otherwise. The playbook writes the correct permissions; orzdba is the
  runtime backstop (a manual `chmod 644` fails closed).
- **The only plaintext password on the host**: the rendered password lives in
  this root-readable file — not in argv, not in the environment, not in a
  my.cnf that other programs can read.
- With Unix-socket auth (`orzdba_mysql_socket`), this file can omit the
  password entirely.

## Usage

```bash
# first deploy (ask for vault password when secrets are vaulted)
ansible-playbook -i inventory playbooks/deploy.yml --ask-vault-pass

# stop / start
ansible-playbook -i inventory playbooks/stop.yml
ansible-playbook -i inventory playbooks/start.yml

# rolling upgrade, 3 hosts at a time
ansible-playbook -i inventory \
  -e orzdba_bin=../releases/orzdba-linux-amd64-1.1.0 \
  -e orzdba_version=1.1.0 \
  -e orzdba_rollout_batch_size=3 \
  playbooks/upgrade.yml
```

Upgrade behavior: each batch backs up the running binary, installs the new
one, restarts, and health-checks (a fresh data row must appear in the daily
logfile within `orzdba_health_check_seconds`). On failure the batch restores
the previous binary, restarts it, and the play stops — untouched hosts stay on
the old version.

## Hardening notes

- `/etc/orzdba.cnf` is enforced 0600 + owner by **orzdba itself** at startup
  (not just by the playbook), so a manual `chmod 644` mistake fails closed.
- Exclude `/etc/orzdba.cnf` from generic backup jobs, or keep backups of it
  inside your secrets management (it is the one plaintext password on the host).
- Rotate the monitoring password periodically and re-run `deploy.yml`
  (the template + mysql-user grant are idempotent).
