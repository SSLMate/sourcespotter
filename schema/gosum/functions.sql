BEGIN;

CREATE FUNCTION gosum.get_root_hash(arg_db_id int, arg_tree_size bigint) RETURNS bytea AS $$
	SELECT CASE
		WHEN arg_tree_size = 0 THEN '\xe3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'
		ELSE (SELECT root_hash FROM gosum.record WHERE db_id = arg_db_id AND position = arg_tree_size - 1)
	END
$$ LANGUAGE SQL STABLE;

CREATE FUNCTION gosum.verify_sths() RETURNS void AS $$
	UPDATE gosum.sth
	   SET consistent = (root_hash = gosum.get_root_hash(db_id, tree_size))
	 WHERE consistent IS NULL
	   AND tree_size <= (SELECT gosum.db.verified_position->>'size' FROM gosum.db WHERE gosum.db.id = gosum.sth.db_id)::bigint
$$ LANGUAGE SQL;

COMMIT;
