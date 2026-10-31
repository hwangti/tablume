package dbx

import (
	"reflect"
	"testing"
)

func TestDetectSource(t *testing.T) {
	cases := []struct {
		style, sql string
		want       []string
	}{
		{"mysql", "SELECT * FROM items", []string{"items"}},
		{"mysql", "select id, name from `shop`.`items` where id > 3 order by id limit 10", []string{"shop", "items"}},
		{"mysql", "SELECT * FROM shop.items i WHERE i.id = 1", []string{"shop", "items"}},
		{"postgres", `SELECT * FROM "Public"."My Table" AS t`, []string{"Public", "My Table"}},
		{"postgres", `SELECT * FROM Items`, []string{"items"}},
		{"oracle", `SELECT * FROM app.items WHERE ROWNUM <= 10`, []string{"APP", "ITEMS"}},
		{"oracle", `SELECT * FROM "APP"."ITEMS" FETCH FIRST 5 ROWS ONLY`, []string{"APP", "ITEMS"}},
		{"mssql", `SELECT TOP 1000 * FROM [master].[dbo].[items]`, []string{"master", "dbo", "items"}},
		{"mssql", `SELECT * FROM dbo.items;`, []string{"dbo", "items"}},
		{"mysql", "SELECT (SELECT max(id) FROM b) AS m, a.* FROM a WHERE x IN (SELECT y FROM c)", []string{"a"}},
		{"mysql", "SELECT * FROM a JOIN b ON a.id = b.id", nil},
		{"mysql", "SELECT * FROM a, b", nil},
		{"mysql", "SELECT DISTINCT x FROM a", nil},
		{"mysql", "SELECT x, count(*) FROM a GROUP BY x", nil},
		{"mysql", "SELECT 1 UNION SELECT 2", nil},
		{"mysql", "SELECT * FROM (SELECT * FROM a) t", nil},
		{"mysql", "SELECT 1", nil},
		{"mysql", "SHOW TABLES", nil},
		{"postgres", "WITH x AS (SELECT 1) SELECT * FROM x", nil},
	}
	for _, c := range cases {
		got := DetectSource(c.sql, c.style)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %q want %q", c.sql, got, c.want)
		}
	}
}
