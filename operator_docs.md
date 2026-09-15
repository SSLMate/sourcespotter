# Retry a failed toolchain build

Connect to the PostgreSQL database and delete the `toolchain_build`
row for the failed version.

For example:

```sql
DELETE FROM toolchain_build WHERE status = 'failed' and version = 'v0.0.1-go1.24.0.linux-amd64';
```

Within 5 minutes, the sourcespotter daemon will retry the build.
