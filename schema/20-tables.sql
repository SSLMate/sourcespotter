-- Copyright (C) 2025 Opsmate, Inc.
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
	log_file	text,
	build_duration	interval,

	PRIMARY KEY (version)
);

CREATE TABLE toolchain_source (
	version		text NOT NULL, -- e.g. "go1.21.0"
	url		text NOT NULL,
	sha256		bytea,
	downloaded_at	timestamptz NOT NULL DEFAULT statement_timestamp(),

	PRIMARY KEY (version)
);

COMMIT;
