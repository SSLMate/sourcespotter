BEGIN;

CREATE FUNCTION get_root_hash(arg_db_id int, arg_tree_size bigint) RETURNS bytea AS $$
	SELECT CASE
		WHEN arg_tree_size = 0 THEN '\xe3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'
		ELSE (SELECT root_hash FROM gosum_record WHERE db_id = arg_db_id AND position = arg_tree_size - 1)
	END
$$ LANGUAGE SQL STABLE;

CREATE FUNCTION verify_sths() RETURNS void AS $$
	UPDATE gosum_sth
	   SET consistent = (root_hash = get_root_hash(db_id, tree_size))
	 WHERE consistent IS NULL
	   AND tree_size <= (SELECT gosum_db.verified_position->>'size' FROM gosum_db WHERE gosum_db.id = gosum_sth.db_id)::bigint
$$ LANGUAGE SQL;

COMMIT;
