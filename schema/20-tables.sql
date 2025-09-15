-- Copyright (C) 2025 Opsmate, Inc.
--
-- Permission is hereby granted, free of charge, to any person obtaining a
-- copy of this software and associated documentation files (the "Software"),
-- to deal in the Software without restriction, including without limitation
-- the rights to use, copy, modify, merge, publish, distribute, sublicense,
-- and/or sell copies of the Software, and to permit persons to whom the
-- Software is furnished to do so, subject to the following conditions:
--
-- The above copyright notice and this permission notice shall be included
-- in all copies or substantial portions of the Software.
--
-- THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
-- IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
-- FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL
-- THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR
-- OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE,
-- ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
-- OTHER DEALINGS IN THE SOFTWARE.
--
-- Except as contained in this notice, the name(s) of the above copyright
-- holders shall not be used in advertising or otherwise to promote the
-- sale, use or other dealings in this Software without prior written
-- authorization.

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

CREATE TABLE authorized_record (
        pubkey          bytea NOT NULL,
        module          text NOT NULL,
        version         text NOT NULL,
        source_sha256   bytea NOT NULL,

        PRIMARY KEY (pubkey, module, version)
);

CREATE TYPE toolchain_build_status AS ENUM (
	'skipped',
	'equal',
	'unequal',
	'failed'
);

CREATE TABLE toolchain_build (
	version		text NOT NULL, -- e.g. "v0.0.1-go1.21.0.linux-amd64"
	inserted_at	timestamptz NOT NULL DEFAULT statement_timestamp(),
	status		toolchain_build_status NOT NULL,
	message		text,
	build_id	bytea,
	build_duration	interval,

	PRIMARY KEY (version)
);

CREATE INDEX toolchain_build_failures ON toolchain_build (inserted_at) WHERE status NOT IN ('equal', 'skipped');

CREATE TABLE toolchain_source (
	version		text NOT NULL, -- e.g. "go1.21.0"
	url		text NOT NULL,
	sha256		bytea,
	downloaded_at	timestamptz NOT NULL DEFAULT statement_timestamp(),

	PRIMARY KEY (version)
);

CREATE TABLE telemetry_config (
	version		text NOT NULL,
	inserted_at	timestamptz NOT NULL DEFAULT statement_timestamp(),
	error		text,

	PRIMARY KEY (version)
);

CREATE TABLE telemetry_counter (
	version text NOT NULL,
	program text NOT NULL,
	name	text NOT NULL,
	type	text NOT NULL,
	rate	real NOT NULL,
	depth	int
);
CREATE INDEX telemetry_counter_index ON telemetry_counter (program, name, version);

COMMIT;
