# Retry a failed toolchain build

Connect to the PostgreSQL database and delete the `toolchain_build`
row for the failed version.

For example:

```sql
DELETE FROM toolchain_build WHERE status = 'failed' and version = 'v0.0.1-go1.24.0.linux-amd64';
```

Within 5 minutes, the sourcespotter daemon will retry the build.

# Migrate authorized_record public keys

Public keys in `authorized_record` are now type-prefixed: `\x00` for ed25519
and `\x01` for the SHA-256 hash of an ML-DSA public key.  Rows inserted before
ML-DSA support was added hold bare 32 byte ed25519 keys and must be prefixed:

```sql
UPDATE authorized_record SET pubkey = '\x00'::bytea || pubkey WHERE length(pubkey) = 32;
```

This only needs to be run once, and it is safe because a type-prefixed key is
33 bytes long.
