-- Copyright (C) 2023 Opsmate, Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla
-- Public License, v. 2.0. If a copy of the MPL was not distributed
-- with this file, You can obtain one at http://mozilla.org/MPL/2.0/.
--
-- This software is distributed WITHOUT A WARRANTY OF ANY KIND.
-- See the Mozilla Public License for details.

BEGIN;

CREATE TABLE db (
	db_id			serial NOT NULL,
	address			text NOT NULL,
	key			bytea NOT NULL,
	download_position	jsonb NOT NULL DEFAULT jsonb_build_object(),
	verified_position	jsonb NOT NULL DEFAULT jsonb_build_object(),
	enabled			boolean NOT NULL DEFAULT TRUE,
	PRIMARY KEY (db_id)
);
CREATE UNIQUE INDEX db_address ON db (address);

CREATE FUNCTION after_verified_position_update() RETURNS trigger AS $$
BEGIN
	PERFORM pg_notify('events', jsonb_build_object('DBID', NEW.db_id, 'Event', 'new_position')::text);
	RETURN NULL;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER verified_position_updated AFTER UPDATE OF verified_position ON db FOR EACH ROW EXECUTE PROCEDURE after_verified_position_update();

CREATE TABLE sth (
	sth_id			bigserial NOT NULL,
	db_id			int NOT NULL REFERENCES db,
	tree_size		bigint NOT NULL,
	root_hash		bytea NOT NULL,
	signature		bytea NOT NULL,
	observed_at		timestamptz NOT NULL DEFAULT statement_timestamp(),
	source			text NOT NULL,
	consistent		boolean,

	PRIMARY KEY (sth_id)
);
CREATE UNIQUE INDEX sth_unique ON sth (db_id, tree_size, root_hash);
CREATE INDEX sth_inconsistent ON sth (db_id) WHERE consistent = FALSE;
CREATE INDEX sth_unverified ON sth (db_id, tree_size) WHERE consistent IS NULL;

CREATE FUNCTION before_sth_insert() RETURNS trigger AS $$
BEGIN
	CASE
	WHEN NEW.tree_size = 0 THEN
		NEW.consistent = (NEW.root_hash = '\xe3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855');
	WHEN NEW.tree_size <= (SELECT (verified_position->>'size')::bigint FROM db WHERE db_id = NEW.db_id) THEN
		NEW.consistent = (NEW.root_hash = (SELECT root_hash FROM record WHERE db_id = NEW.db_id AND position = (NEW.tree_size - 1)));
	ELSE
	END CASE;
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER sth_insert BEFORE INSERT ON sth FOR EACH ROW EXECUTE PROCEDURE before_sth_insert();

CREATE FUNCTION after_unverified_sth_insert() RETURNS trigger AS $$
BEGIN
	PERFORM pg_notify('events', jsonb_build_object('DBID', NEW.db_id, 'Event', 'new_sth')::text);
	RETURN NULL;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER unverified_sth_inserted AFTER INSERT ON sth FOR EACH ROW WHEN (NEW.consistent IS NULL) EXECUTE PROCEDURE after_unverified_sth_insert();

CREATE TABLE record (
	db_id			int NOT NULL REFERENCES db,
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
CREATE INDEX record_module ON record (module, version, db_id, position DESC);
CREATE INDEX duplicate_module ON record (db_id) WHERE previous_position IS NOT NULL;

CREATE FUNCTION before_record_insert() RETURNS trigger AS $$
BEGIN
	NEW.previous_position = (SELECT position FROM record WHERE (module,version,db_id) = (NEW.module,NEW.version,NEW.db_id) ORDER BY position DESC LIMIT 1);
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER record_insert BEFORE INSERT ON record FOR EACH ROW EXECUTE PROCEDURE before_record_insert();

COMMIT;
