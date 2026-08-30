# Postiz database backups

SOPS/age-encrypted `pg_dump` snapshots of the `postiz` Postgres database
(`postiz-postgresql-0` in the `postiz` namespace). Restoring one of these
recreates every user account, organization, and channel connection
(Bluesky OAuth session, and the manually-seeded LinkedIn/Instagram bridge
`Integration` rows) without redoing any manual setup.

## Restore

```sh
SOPS_AGE_KEY_FILE=~/.config/sops/age/keys.txt \
  sops -d --input-type binary --output-type binary \
  clusters/archlinux/apps/postiz/backups/postiz-db-YYYY-MM-DD.sql.enc \
  > /tmp/postiz-restore.sql

kubectl -n postiz cp /tmp/postiz-restore.sql postiz-postgresql-0:/tmp/restore.sql
kubectl -n postiz exec postiz-postgresql-0 -- psql -U postiz -d postiz -f /tmp/restore.sql
rm /tmp/postiz-restore.sql   # don't leave the plaintext dump lying around
```

The dump was taken with `--clean --if-exists`, so it drops and recreates
objects in place — safe to run against a freshly-provisioned (empty) Postgres
right after Flux brings the `postiz-postgresql` StatefulSet up.

## Take a new backup

```sh
kubectl -n postiz exec postiz-postgresql-0 -- pg_dump -U postiz -d postiz --clean --if-exists \
  > /tmp/postiz-db-backup.sql

sops --input-type binary --output-type binary \
  -e /tmp/postiz-db-backup.sql \
  > clusters/archlinux/apps/postiz/backups/postiz-db-$(date +%F).sql.enc

rm /tmp/postiz-db-backup.sql
```

Commit and push the resulting `.sql.enc` file — nothing else in this
directory should ever be unencrypted (see `.gitignore`).
