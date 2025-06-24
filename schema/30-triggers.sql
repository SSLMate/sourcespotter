-- Copyright (C) 2025 Opsmate, Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla
-- Public License, v. 2.0. If a copy of the MPL was not distributed
-- with this file, You can obtain one at http://mozilla.org/MPL/2.0/.
--
-- This software is distributed WITHOUT A WARRANTY OF ANY KIND.
-- See the Mozilla Public License for details.

BEGIN;

CREATE FUNCTION after_verified_position_update() RETURNS trigger AS $$
BEGIN
	PERFORM pg_notify('events', jsonb_build_object('DBID', NEW.db_id, 'Event', 'new_position')::text);
	RETURN NULL;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER verified_position_updated AFTER UPDATE OF verified_position ON db FOR EACH ROW EXECUTE PROCEDURE after_verified_position_update();

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

CREATE FUNCTION before_record_insert() RETURNS trigger AS $$
BEGIN
	NEW.previous_position = (SELECT position FROM record WHERE (module,version,db_id) = (NEW.module,NEW.version,NEW.db_id) ORDER BY position DESC LIMIT 1);
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER record_insert BEFORE INSERT ON record FOR EACH ROW EXECUTE PROCEDURE before_record_insert();

COMMIT;
