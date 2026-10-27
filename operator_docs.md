# Retry a failed toolchain build

Connect to the PostgreSQL database and delete the `toolchain_build`
row for the failed version.

For example:

```sql
DELETE FROM toolchain_build WHERE status = 'failed' and version = 'v0.0.1-go1.24.0.linux-amd64';
```

Within 5 minutes, the sourcespotter daemon will retry the build.

# Enable STH cosigning on an existing deployment

Apply the schema changes to the database:

```sql
BEGIN;
CREATE TABLE cosigned_sth (
	db_id			int NOT NULL REFERENCES db,
	tree_size		bigint NOT NULL,
	sth_id			bigint NOT NULL REFERENCES sth,
	cosigned_at		timestamptz NOT NULL,
	cosignature		bytea NOT NULL,
	PRIMARY KEY (db_id, tree_size)
);
CREATE FUNCTION before_cosigned_sth_insert() RETURNS trigger AS $$
BEGIN
	IF NEW.tree_size <= (SELECT coalesce(MAX(tree_size), 0) FROM cosigned_sth WHERE db_id = NEW.db_id) THEN
		RAISE EXCEPTION 'cosigned tree size % for db % does not exceed previously cosigned tree size', NEW.tree_size, NEW.db_id;
	END IF;
	IF NOT EXISTS (
		SELECT 1
		FROM sth
		JOIN record ON (record.db_id, record.position) = (sth.db_id, sth.tree_size - 1)
		WHERE sth.sth_id = NEW.sth_id AND sth.db_id = NEW.db_id AND sth.tree_size = NEW.tree_size AND record.root_hash = sth.root_hash
	) THEN
		RAISE EXCEPTION 'STH % does not match the root hash calculated from downloaded records', NEW.sth_id;
	END IF;
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER cosigned_sth_insert BEFORE INSERT ON cosigned_sth FOR EACH ROW EXECUTE PROCEDURE before_cosigned_sth_insert();
CREATE UNIQUE INDEX cosigned_sth_sth_id ON cosigned_sth (sth_id);
ALTER TABLE toolchain_build
	ADD COLUMN cosigned_sth_id bigint REFERENCES cosigned_sth (sth_id),
	ADD COLUMN inclusion_proof bytea;
COMMIT;
```

Generate a key with `sourcespotter-cosign-keygen -domain DOMAIN -out FILE`, set
`Cosign.KeyFile` to the file's path in the config file, and restart the `-sumdb`
daemon.  The verifier key is printed by the keygen command.

# The cosigned STH has stopped advancing

The `sourcespotter_sumdb_cosigned_tree_size` metric stops increasing (and the
"Largest Cosigned STH" on the sumdb dashboard lags the "Largest Verified STH")
when a `golang.org/toolchain` record in the tree does not have a
`toolchain_build` row with status `equal` or `skipped`.  This is expected for
around 20 minutes after a Go release while the new toolchains are rebuilt.  If
it persists, find the blocking toolchain:

```sql
SELECT record.position, record.version, toolchain_build.status, toolchain_build.message
FROM record
LEFT JOIN toolchain_build USING (version)
WHERE record.module = 'golang.org/toolchain'
  AND (toolchain_build.status IS NULL OR toolchain_build.status NOT IN ('equal', 'skipped'))
ORDER BY record.position
LIMIT 10;
```

A `failed` build can be retried as described above.  An `unequal` build means
the toolchain could not be reproduced and needs investigation; Source Spotter
intentionally does not cosign past it.  A missing row means the toolchain
auditor (`-toolchain`) is not running or has not gotten to it yet.

Cosignatures are never revoked.  If a previously cosigned toolchain is later
rebuilt and found to be unequal, no new STHs are cosigned until the problem is
resolved, but the previously cosigned STH continues to be served.

# Attestations are missing for some toolchains

`attestation.api.DOMAIN/golang.org/toolchain@VERSION` returns 503 with "not yet
been verified as reproducible" when the `toolchain_build` row has no inclusion proof.  After
each cosigning attempt, the `-sumdb` daemon computes proofs for verified
toolchains whose record position is below the latest cosigned tree size, so
this resolves itself once the cosigned STH covers the record.  If it persists,
check the daemon log: a failure to download hash tiles from the checksum
database ends the pass early and is retried on the next pass (every minute).
The first pass after enabling cosigning computes proofs for every existing
toolchain and takes a few minutes.

Deleting a `toolchain_build` row to retry a build also discards its proof; a
new proof is computed after the rebuild succeeds.
