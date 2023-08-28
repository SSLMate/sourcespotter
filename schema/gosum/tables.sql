BEGIN;

CREATE SCHEMA gosum;

CREATE TABLE gosum.db (
	db_id			serial NOT NULL,
	address			text NOT NULL,
	key			bytea NOT NULL,
	download_position	jsonb NOT NULL DEFAULT jsonb_build_object(),
	verified_position	jsonb NOT NULL DEFAULT jsonb_build_object(),
	enabled			boolean NOT NULL DEFAULT TRUE,
	PRIMARY KEY (db_id)
);
CREATE UNIQUE INDEX db_address ON gosum.db (address);

CREATE TABLE gosum.sth (
	sth_id			bigserial NOT NULL,
	db_id			int NOT NULL REFERENCES gosum.db,
	tree_size		bigint NOT NULL,
	root_hash		bytea NOT NULL,
	signature		bytea NOT NULL,
	observed_at		timestamptz NOT NULL DEFAULT statement_timestamp(),
	source			text NOT NULL,
	consistent		boolean,

	PRIMARY KEY (sth_id)
);
CREATE UNIQUE INDEX sth_unique ON gosum.sth (db_id, tree_size, root_hash);
CREATE INDEX sth_inconsistent ON gosum.sth (db_id) WHERE consistent = FALSE;
CREATE INDEX sth_unverified ON gosum.sth (db_id, tree_size) WHERE consistent IS NULL;

CREATE FUNCTION gosum.before_sth_insert() RETURNS trigger AS $$
BEGIN
	CASE
	WHEN NEW.tree_size = 0 THEN
		NEW.consistent = (NEW.root_hash = '\xe3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855');
	WHEN NEW.tree_size <= (SELECT (verified_position->>'size')::bigint FROM gosum.db WHERE db_id = NEW.db_id) THEN
		NEW.consistent = (NEW.root_hash = (SELECT root_hash FROM gosum.record WHERE db_id = NEW.db_id AND position = (NEW.tree_size - 1)));
	ELSE
	END CASE;
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER sth_insert BEFORE INSERT ON gosum.sth FOR EACH ROW EXECUTE PROCEDURE gosum.before_sth_insert();

CREATE TABLE gosum.record (
	db_id			int NOT NULL REFERENCES gosum.db,
	position		bigint NOT NULL,
	module			text NOT NULL,
	version			text NOT NULL,
	source_sha256		bytea NOT NULL,
	gomod_sha256		bytea NOT NULL,
	root_hash		bytea NOT NULL,
	observed_at		timestamptz NOT NULL DEFAULT statement_timestamp(),
	previous_position	bigint,
	PRIMARY KEY (db_id, position)
);
CREATE INDEX record_module ON gosum.record (module, version, db_id, position DESC);
CREATE INDEX duplicate_module ON gosum.record (db_id) WHERE previous_position IS NOT NULL;

CREATE FUNCTION gosum.before_record_insert() RETURNS trigger AS $$
BEGIN
	NEW.previous_position = (SELECT position FROM gosum.record WHERE (module,version,db_id) = (NEW.module,NEW.version,NEW.db_id) ORDER BY position DESC LIMIT 1);
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER record_insert BEFORE INSERT ON gosum.record FOR EACH ROW EXECUTE PROCEDURE gosum.before_record_insert();

COMMIT;
