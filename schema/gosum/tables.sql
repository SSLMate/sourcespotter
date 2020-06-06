BEGIN;

CREATE SCHEMA gosum;

CREATE FUNCTION gosum.empty_collapsed_merkle_tree() RETURNS jsonb AS $$
	SELECT jsonb_build_object('size', 0, 'nodes', jsonb_build_array())
$$ LANGUAGE SQL IMMUTABLE;

CREATE TABLE gosum.db (
	id			serial NOT NULL,
	address			text NOT NULL,
	key			bytea NOT NULL,
	download_position	jsonb NOT NULL DEFAULT gosum.empty_collapsed_merkle_tree(),
	verified_position	jsonb NOT NULL DEFAULT gosum.empty_collapsed_merkle_tree(),
	PRIMARY KEY (id)
);
CREATE UNIQUE INDEX db_address ON gosum.db (address);

CREATE TABLE gosum.sth (
	db_id			int NOT NULL REFERENCES gosum.db,
	tree_size		bigint NOT NULL,
	root_hash		bytea NOT NULL,
	signature		bytea NOT NULL,
	observed_at		timestamptz NOT NULL DEFAULT statement_timestamp(),
	consistent		boolean,
	PRIMARY KEY (db_id, tree_size, root_hash)
);
CREATE INDEX sth_not_consistent ON gosum.sth ((1)) WHERE consistent IS DISTINCT FROM TRUE;

CREATE TABLE gosum.record (
	db_id			int NOT NULL REFERENCES gosum.db,
	position		bigint NOT NULL,
	module			text NOT NULL,
	version			text NOT NULL,
	source_sha256		bytea NOT NULL,
	gomod_sha256		bytea NOT NULL,
	root_hash		bytea NOT NULL,
	observed_at		timestamptz NOT NULL DEFAULT statement_timestamp(),
	PRIMARY KEY (db_id, position)
);
CREATE INDEX record_module ON gosum.record (module, version);

COMMIT;
