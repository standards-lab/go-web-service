//go:build integration

package integration_test

import (
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/standards-lab/go-web-service/integration"
)

// The organization read model and the command envelope, as the API
// presents them.
type organization struct {
	ID       string  `json:"id"`
	ParentID *string `json:"parent_id"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Version  int64   `json:"version"`
	Path     string  `json:"path"`
}

type identity struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
}

type organizationPage struct {
	Items []organization `json:"items"`
	Page  int            `json:"page"`
	Size  int            `json:"size"`
	Total int            `json:"total"`
}

// absentID is a well-formed id no row carries.
const absentID = "00000000-0000-7000-8000-000000000000"

// tree reads the seeded organizations by code.
func tree(t *testing.T, c *integration.Client) map[string]organization {
	t.Helper()
	p := integration.Decode[organizationPage](t, c.Get(t, organizations+"?size=100"), http.StatusOK)
	out := make(map[string]organization, len(p.Items))
	for _, o := range p.Items {
		out[o.Code] = o
	}
	return out
}

func codes(items []organization) []string {
	out := make([]string, len(items))
	for i, o := range items {
		out[i] = o.Code
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestOrganization(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: true})
	c := s.Client()

	// Every case starts from the seeded tree.
	run := func(name string, fn func(t *testing.T)) {
		t.Run(name, func(t *testing.T) {
			integration.Reset(t, c)
			fn(t)
		})
	}

	run("reads", func(t *testing.T) {
		all := tree(t, c)
		eng := integration.Decode[organization](t, c.Get(t, organizations+"/"+all["engineering"].ID), http.StatusOK)
		if eng.Path != "/acme/engineering" || eng.ParentID == nil || *eng.ParentID != all["acme"].ID {
			t.Errorf("engineering = %+v", eng)
		}
		byPath := integration.Decode[organization](t, c.Get(t, organizations+"/path/acme/engineering/platform"), http.StatusOK)
		if byPath.ID != all["platform"].ID {
			t.Errorf("path read = %+v", byPath)
		}
		c.Get(t, organizations+"/"+absentID).Problem(t, http.StatusNotFound)
		c.Get(t, organizations+"/path/acme/nowhere").Problem(t, http.StatusNotFound)
		c.Get(t, organizations+"/not-a-uuid").Problem(t, http.StatusBadRequest)
	})

	run("paging and sort", func(t *testing.T) {
		p := integration.Decode[organizationPage](t, c.Get(t, organizations+"?size=3&sort=-path"), http.StatusOK)
		if p.Total != seededTotal || p.Page != 1 || p.Size != 3 || !equal(codes(p.Items), []string{"logistics", "operations", "finance"}) {
			t.Errorf("page 1 by -path = %v (total %d)", codes(p.Items), p.Total)
		}
		p = integration.Decode[organizationPage](t, c.Get(t, organizations+"?size=3&page=3&sort=code"), http.StatusOK)
		if !equal(codes(p.Items), []string{"product"}) {
			t.Errorf("page 3 by code = %v", codes(p.Items))
		}
		c.Get(t, organizations+"?page=x").Problem(t, http.StatusBadRequest)
		c.Get(t, organizations+"?size=1000").Problem(t, http.StatusBadRequest)
	})

	run("filter grammar", func(t *testing.T) {
		cases := map[string][]string{
			"code=acme":                        {"acme"},
			"code=acme&code=finance&sort=code": {"acme", "finance"},
			"name[like]=" + url.QueryEscape("%ing") + "&sort=code":                {"engineering"},
			"path[like]=" + url.QueryEscape("/acme/engineering/%") + "&sort=code": {"platform", "product"},
			"parent_id[null]=&sort=code":                                          {"acme"},
		}
		for query, want := range cases {
			p := integration.Decode[organizationPage](t, c.Get(t, organizations+"?"+query), http.StatusOK)
			if !equal(codes(p.Items), want) || p.Total != len(want) {
				t.Errorf("?%s = %v (total %d), want %v", query, codes(p.Items), p.Total, want)
			}
		}
		for _, query := range []string{"nope=1", "code[between]=a", "created_at=not-a-time", "id=not-a-uuid"} {
			if p := c.Get(t, organizations+"?"+query).Problem(t, http.StatusBadRequest); p.Detail == "" {
				t.Errorf("?%s: 400 without detail", query)
			}
		}
	})

	run("create", func(t *testing.T) {
		all := tree(t, c)
		body := map[string]any{"parent_id": all["engineering"].ID, "code": "security", "name": "Security"}
		res := c.Post(t, organizations, body).Expect(t, http.StatusCreated)
		var ident identity
		res.JSON(t, &ident)
		if ident.Version != 1 || res.Header.Get("Location") != organizations+"/"+ident.ID {
			t.Errorf("created = %+v, Location %q", ident, res.Header.Get("Location"))
		}
		created := integration.Decode[organization](t, c.Get(t, organizations+"/path/acme/engineering/security"), http.StatusOK)
		if created.ID != ident.ID {
			t.Errorf("created row not readable by path: %+v", created)
		}

		c.Post(t, organizations, body).Problem(t, http.StatusConflict)                                                             // duplicate sibling
		c.Post(t, organizations, map[string]any{"parent_id": nil, "code": "acme", "name": "Acme"}).Problem(t, http.StatusConflict) // duplicate root
		c.Post(t, organizations, map[string]any{"parent_id": absentID, "code": "orphan", "name": "Orphan"}).Problem(t, http.StatusConflict)
		c.Post(t, organizations, map[string]any{"code": "Bad_Code", "name": "X"}).Problem(t, http.StatusBadRequest)
		c.Post(t, organizations, map[string]any{"code": "ok", "name": ""}).Problem(t, http.StatusBadRequest)
		c.Post(t, organizations, map[string]any{"parent_id": "nope", "code": "ok", "name": "X"}).Problem(t, http.StatusBadRequest)
		c.Post(t, organizations, map[string]any{"codex": "ok"}).Problem(t, http.StatusBadRequest)
		c.Post(t, organizations, "{").Problem(t, http.StatusBadRequest)
	})

	run("edit", func(t *testing.T) {
		all := tree(t, c)
		fin := all["finance"]
		path := organizations + "/" + fin.ID
		body := map[string]any{"code": "fin", "name": "Finance and Accounting"}

		c.Put(t, path, body).Problem(t, http.StatusPreconditionRequired)
		c.Put(t, path, body, integration.Header{Name: "If-Match", Value: `W/"1"`}).Problem(t, http.StatusBadRequest)
		c.Put(t, path, map[string]any{"code": "fin"}, integration.IfMatch(fin.Version)).Problem(t, http.StatusBadRequest)
		c.Put(t, path, body, integration.IfMatch(fin.Version+1)).Problem(t, http.StatusPreconditionFailed)
		c.Put(t, organizations+"/"+absentID, body, integration.IfMatch(1)).Problem(t, http.StatusNotFound)

		ident := integration.Decode[identity](t, c.Put(t, path, body, integration.IfMatch(fin.Version)), http.StatusOK)
		if ident.ID != fin.ID || ident.Version != fin.Version+1 {
			t.Errorf("edited = %+v, want version %d", ident, fin.Version+1)
		}
		after := integration.Decode[organization](t, c.Get(t, path), http.StatusOK)
		if after.Code != "fin" || after.Name != "Finance and Accounting" || after.Path != "/acme/fin" {
			t.Errorf("after edit = %+v", after)
		}
		c.Put(t, path, body, integration.IfMatch(fin.Version)).Problem(t, http.StatusPreconditionFailed) // the old version is stale now
	})

	run("transfer", func(t *testing.T) {
		all := tree(t, c)
		platform := all["platform"]
		path := organizations + "/" + platform.ID + "/transfer"

		c.Post(t, path, map[string]any{}, integration.IfMatch(platform.Version)).Problem(t, http.StatusBadRequest) // key required
		c.Post(t, path, map[string]any{"parent_id": all["operations"].ID}).Problem(t, http.StatusPreconditionRequired)
		c.Post(t, path, map[string]any{"parent_id": all["operations"].ID}, integration.IfMatch(9)).Problem(t, http.StatusPreconditionFailed)
		c.Post(t, path, map[string]any{"parent_id": absentID}, integration.IfMatch(platform.Version)).Problem(t, http.StatusConflict)

		// A cycle: acme under its own descendant, and a node under itself.
		acme := all["acme"]
		c.Post(t, organizations+"/"+acme.ID+"/transfer", map[string]any{"parent_id": all["product"].ID}, integration.IfMatch(acme.Version)).Problem(t, http.StatusConflict)
		c.Post(t, path, map[string]any{"parent_id": platform.ID}, integration.IfMatch(platform.Version)).Problem(t, http.StatusConflict)

		// The move, and the path recomposed beneath the new parent.
		ident := integration.Decode[identity](t, c.Post(t, path, map[string]any{"parent_id": all["operations"].ID}, integration.IfMatch(platform.Version)), http.StatusOK)
		if ident.Version != platform.Version+1 {
			t.Errorf("transferred = %+v", ident)
		}
		moved := integration.Decode[organization](t, c.Get(t, organizations+"/path/acme/operations/platform"), http.StatusOK)
		if moved.ID != platform.ID {
			t.Errorf("moved row = %+v", moved)
		}
		c.Get(t, organizations+"/path/acme/engineering/platform").Problem(t, http.StatusNotFound)

		// Moving a subtree recomposes every descendant's path.
		eng := all["engineering"]
		integration.Decode[identity](t, c.Post(t, organizations+"/"+eng.ID+"/transfer", map[string]any{"parent_id": all["finance"].ID}, integration.IfMatch(eng.Version)), http.StatusOK)
		if p := integration.Decode[organization](t, c.Get(t, organizations+"/"+all["product"].ID), http.StatusOK); p.Path != "/acme/finance/engineering/product" {
			t.Errorf("descendant path after subtree move = %q", p.Path)
		}

		// The root move: null parent.
		ident = integration.Decode[identity](t, c.Post(t, path, map[string]any{"parent_id": nil}, integration.IfMatch(ident.Version)), http.StatusOK)
		root := integration.Decode[organization](t, c.Get(t, organizations+"/path/platform"), http.StatusOK)
		if root.ParentID != nil || root.Path != "/platform" || root.Version != ident.Version {
			t.Errorf("root move = %+v", root)
		}
		c.Post(t, organizations, map[string]any{"parent_id": nil, "code": "platform", "name": "Dup"}).Problem(t, http.StatusConflict) // root code unique
	})

	run("concurrent transfers under the lock", func(t *testing.T) {
		all := tree(t, c)
		eng, ops := all["engineering"], all["operations"]

		// Each sibling moves under the other at once. Without the tree
		// lock both cycle checks pass and the tree gains a cycle; under
		// it the second transfer sees the first and is refused.
		var wg sync.WaitGroup
		results := make([]int, 2)
		move := func(i int, node, under organization) {
			defer wg.Done()
			cl := integration.NewClient(s.URL())
			results[i] = cl.Post(t, organizations+"/"+node.ID+"/transfer", map[string]any{"parent_id": under.ID}, integration.IfMatch(node.Version)).Status
		}
		wg.Add(2)
		go move(0, eng, ops)
		go move(1, ops, eng)
		wg.Wait()

		ok, conflict := 0, 0
		for _, st := range results {
			switch st {
			case http.StatusOK:
				ok++
			case http.StatusConflict:
				conflict++
			}
		}
		if ok != 1 || conflict != 1 {
			t.Fatalf("statuses = %v, want one 200 and one 409", results)
		}
		// The tree still resolves: every node has a path from a root.
		after := tree(t, c)
		if len(after) != seededTotal {
			t.Errorf("tree after concurrent transfers has %d nodes", len(after))
		}
		for code, o := range after {
			if o.Path == "" || o.Path[0] != '/' {
				t.Errorf("%s path = %q", code, o.Path)
			}
		}
	})

	run("delete", func(t *testing.T) {
		all := tree(t, c)
		leaf, parent := all["logistics"], all["operations"]

		c.Delete(t, organizations+"/"+leaf.ID).Problem(t, http.StatusPreconditionRequired)
		c.Delete(t, organizations+"/"+leaf.ID, integration.IfMatch(leaf.Version+1)).Problem(t, http.StatusPreconditionFailed)
		c.Delete(t, organizations+"/"+absentID, integration.IfMatch(1)).Problem(t, http.StatusNotFound)
		c.Delete(t, organizations+"/"+parent.ID, integration.IfMatch(parent.Version)).Problem(t, http.StatusConflict) // children block

		c.Delete(t, organizations+"/"+leaf.ID, integration.IfMatch(leaf.Version)).Expect(t, http.StatusNoContent)
		c.Get(t, organizations+"/"+leaf.ID).Problem(t, http.StatusNotFound)
		c.Delete(t, organizations+"/"+parent.ID, integration.IfMatch(parent.Version)).Expect(t, http.StatusNoContent) // now a leaf
		if p := integration.Decode[organizationPage](t, c.Get(t, organizations), http.StatusOK); p.Total != seededTotal-2 {
			t.Errorf("total after deletes = %d", p.Total)
		}
	})

	if code := s.Stop(t); code != 0 {
		t.Fatalf("exit = %d:\n%s", code, s.Output())
	}
}
