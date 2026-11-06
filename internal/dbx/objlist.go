package dbx

import "tablume/internal/i18n"

// ObjectListSQL returns a catalog query listing a schema's tables
// (kind "tables") or routines ("routines"), with their comments and
// properties, for a tab to run and show as a grid.
func (m *Manager) ObjectListSQL(id, schema, kind string) (string, error) {
	p, err := m.pool(id)
	if err != nil {
		return "", err
	}
	drv := p.cfg.Driver
	lit := sqlQuote(schema, drv == "mysql" || drv == "mariadb")
	routines := kind == "routines"
	switch drv {
	case "mysql", "mariadb":
		if routines {
			return `SELECT ROUTINE_NAME AS name, NULLIF(ROUTINE_COMMENT, '') AS comment, ROUTINE_TYPE AS type,
  DTD_IDENTIFIER AS returns, DEFINER AS definer, IS_DETERMINISTIC AS is_deterministic,
  SECURITY_TYPE AS security, SQL_DATA_ACCESS AS sql_access, CREATED AS created, LAST_ALTERED AS altered
FROM information_schema.ROUTINES
WHERE ROUTINE_SCHEMA = ` + lit + `
ORDER BY ROUTINE_TYPE DESC, ROUTINE_NAME`, nil
		}
		return `SELECT TABLE_NAME AS name, CASE WHEN TABLE_TYPE = 'VIEW' THEN NULL ELSE NULLIF(TABLE_COMMENT, '') END AS comment, TABLE_TYPE AS type, ENGINE AS engine,
  TABLE_COLLATION AS collation, TABLE_ROWS AS rows_est,
  ROUND((DATA_LENGTH + INDEX_LENGTH) / 1048576, 2) AS size_mb, AUTO_INCREMENT AS auto_increment,
  NULLIF(CREATE_OPTIONS, '') AS options, CREATE_TIME AS created, UPDATE_TIME AS updated
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = ` + lit + `
ORDER BY TABLE_NAME`, nil
	case "postgres":
		if routines {
			return `SELECT p.proname AS name, pg_get_function_identity_arguments(p.oid) AS args,
  obj_description(p.oid, 'pg_proc') AS comment,
  CASE p.prokind WHEN 'p' THEN 'PROCEDURE' WHEN 'a' THEN 'AGGREGATE' WHEN 'w' THEN 'WINDOW' ELSE 'FUNCTION' END AS type,
  pg_get_function_result(p.oid) AS returns, l.lanname AS language, pg_get_userbyid(p.proowner) AS owner,
  CASE p.provolatile WHEN 'i' THEN 'IMMUTABLE' WHEN 's' THEN 'STABLE' ELSE 'VOLATILE' END AS volatility,
  p.prosecdef AS security_definer
FROM pg_proc p
JOIN pg_namespace n ON n.oid = p.pronamespace
JOIN pg_language l ON l.oid = p.prolang
WHERE n.nspname = ` + lit + `
ORDER BY 4 DESC, 1, 2`, nil
		}
		return `SELECT c.relname AS name, obj_description(c.oid, 'pg_class') AS comment,
  CASE c.relkind WHEN 'r' THEN 'TABLE' WHEN 'p' THEN 'PARTITIONED TABLE' WHEN 'v' THEN 'VIEW'
    WHEN 'm' THEN 'MATERIALIZED VIEW' WHEN 'f' THEN 'FOREIGN TABLE' END AS type,
  pg_get_userbyid(c.relowner) AS owner, t.spcname AS tablespace,
  CASE WHEN c.reltuples < 0 THEN NULL ELSE c.reltuples::bigint END AS rows_est,
  ROUND(pg_total_relation_size(c.oid) / 1048576.0, 2) AS size_mb,
  c.relpersistence = 'u' AS unlogged, c.relrowsecurity AS row_security
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_tablespace t ON t.oid = c.reltablespace
WHERE n.nspname = ` + lit + ` AND c.relkind IN ('r', 'p', 'v', 'm', 'f') AND NOT c.relispartition
ORDER BY 1`, nil
	case "mssql":
		d := msq(schema)
		if routines {
			return `SELECT s.name AS [schema], o.name AS name, CAST(ep.value AS nvarchar(4000)) AS comment,
  o.type_desc AS type, o.create_date AS created, o.modify_date AS altered
FROM ` + d + `.sys.objects o
JOIN ` + d + `.sys.schemas s ON s.schema_id = o.schema_id
LEFT JOIN ` + d + `.sys.extended_properties ep ON ep.major_id = o.object_id AND ep.minor_id = 0 AND ep.class = 1 AND ep.name = 'MS_Description'
WHERE o.type IN ('P', 'FN', 'IF', 'TF', 'PC', 'FS', 'FT') AND o.is_ms_shipped = 0
ORDER BY o.type_desc DESC, s.name, o.name`, nil
		}
		return `SELECT s.name AS [schema], o.name AS name, CAST(ep.value AS nvarchar(4000)) AS comment,
  CASE o.type WHEN 'U' THEN 'TABLE' ELSE 'VIEW' END AS type,
  (SELECT SUM(ps.row_count) FROM ` + d + `.sys.dm_db_partition_stats ps WHERE ps.object_id = o.object_id AND ps.index_id < 2) AS rows_est,
  (SELECT CAST(SUM(ps.reserved_page_count) * 8 / 1024.0 AS decimal(18, 2)) FROM ` + d + `.sys.dm_db_partition_stats ps WHERE ps.object_id = o.object_id) AS size_mb,
  o.create_date AS created, o.modify_date AS altered
FROM ` + d + `.sys.objects o
JOIN ` + d + `.sys.schemas s ON s.schema_id = o.schema_id
LEFT JOIN ` + d + `.sys.extended_properties ep ON ep.major_id = o.object_id AND ep.minor_id = 0 AND ep.class = 1 AND ep.name = 'MS_Description'
WHERE o.type IN ('U', 'V') AND o.is_ms_shipped = 0
ORDER BY s.name, o.name`, nil
	case "oracle":
		if routines {
			return `SELECT object_name AS name, object_type AS type, status, created, last_ddl_time AS altered
FROM all_objects
WHERE owner = ` + lit + ` AND object_type IN ('PROCEDURE', 'FUNCTION', 'PACKAGE')
ORDER BY CASE object_type WHEN 'PACKAGE' THEN 0 WHEN 'PROCEDURE' THEN 1 ELSE 2 END, object_name`, nil
		}
		return `SELECT o.object_name AS name, c.comments AS "COMMENT", o.object_type AS type, t.tablespace_name AS tablespace,
  t.num_rows AS rows_est, t.last_analyzed, o.status, o.created, o.last_ddl_time AS altered
FROM all_objects o
LEFT JOIN all_tab_comments c ON c.owner = o.owner AND c.table_name = o.object_name
LEFT JOIN all_tables t ON t.owner = o.owner AND t.table_name = o.object_name
WHERE o.owner = ` + lit + ` AND o.object_type IN ('TABLE', 'VIEW', 'MATERIALIZED VIEW')
ORDER BY o.object_name`, nil
	case "sqlite":
		if !routines {
			return `SELECT name, type, sql FROM ` + quoteWith(schema, `"`, `"`) + `.sqlite_master
WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite\_%' ESCAPE '\'
ORDER BY name`, nil
		}
	}
	return "", i18n.Error("이 DB에서는 목록을 볼 수 없습니다")
}
