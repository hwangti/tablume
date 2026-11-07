package dbx

import "testing"

func TestBuildGraphCardinality(t *testing.T) {
	g := buildGraph(graphData{
		tables: []GraphTable{
			{Name: "users", Columns: []Column{{Name: "id", PK: true}, {Name: "code"}}},
			{Name: "profile", Columns: []Column{{Name: "user_id"}}},
			{Name: "orders", Columns: []Column{{Name: "user_id", Nullable: true}, {Name: "shop"}, {Name: "user_code"}}},
		},
		uniques: map[string][][]string{
			"users":   {{"id"}},
			"profile": {{"user_id"}},
			"orders":  {{"user_id", "shop"}}, // starts with user_id: user_id alone isn't unique
		},
		fks: []ForeignKey{
			{Table: "profile", Columns: []string{"user_id"}, RefTable: "users", RefColumns: []string{"id"}},
			{Table: "orders", Columns: []string{"user_id"}, RefTable: "users", RefColumns: []string{"id"}},
			{Table: "orders", Columns: []string{"user_id", "shop"}, RefTable: "users", RefColumns: []string{"id", "code"}},
			{Table: "orders", Columns: []string{"user_code"}, RefTable: "users", RefColumns: []string{"code"}},
			{Table: "orders", Columns: []string{"user_code"}, RefSchema: "other", RefTable: "users", RefColumns: []string{"code"}},
		},
	})
	want := []struct{ one, req, loose bool }{
		{true, true, false},   // 1:1, parent required
		{false, false, false}, // 1:N, nullable FK
		{true, false, false},  // the key (user_id, shop) is unique
		{false, true, true},   // users.code isn't unique
		{false, true, false},  // another schema: no verdict on the parent
	}
	for i, w := range want {
		f := g.FKs[i]
		if f.One != w.one || f.Required != w.req || f.LooseRef != w.loose {
			t.Errorf("fk %d: one=%v req=%v loose=%v, want %v %v %v", i, f.One, f.Required, f.LooseRef, w.one, w.req, w.loose)
		}
	}
	if g.Tables[0].Name != "orders" || g.Tables[2].Name != "users" {
		t.Errorf("tables not sorted: %v", g.Tables)
	}
}
