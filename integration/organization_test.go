//go:build integration

package integration_test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/integration"
)

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
	Total *int           `json:"total"`
	More  bool           `json:"more"`
	Next  string         `json:"next"`
}

func (p organizationPage) total() int {
	if p.Total == nil {
		return -1
	}
	return *p.Total
}

const generalConflict = "the request conflicts with the current state"

func conflict(t *testing.T, r *webtest.Response, detail string) {
	t.Helper()
	if p := r.Problem(t, http.StatusConflict); p.Detail != detail {
		t.Errorf("detail = %q, want %q", p.Detail, detail)
	}
	for _, leak := range []string{"data:", "blobfs", "constraint", "_uq_", "_fk_", "_pk_", "violates"} {
		if strings.Contains(string(r.Body), leak) {
			t.Errorf("body %s carries %q", r.Body, leak)
		}
	}
}

const absentID = "00000000-0000-7000-8000-000000000000"

func tree(t *testing.T, c *webtest.Client) map[string]organization {
	t.Helper()
	p := webtest.Decode[organizationPage](t, c.Get(t, organizations+"?size=100"), http.StatusOK)
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

func TestOrganization(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()

	run := func(name string, fn func(t *testing.T)) {
		t.Run(name, func(t *testing.T) {
			integration.Reset(t, c, integration.Default)
			fn(t)
		})
	}

	run("reads", func(t *testing.T) {
		all := tree(t, c)
		eng := webtest.Decode[organization](t, c.Get(t, organizations+"/"+all["engineering"].ID), http.StatusOK)
		if eng.Path != "/acme/engineering" || eng.ParentID == nil || *eng.ParentID != all["acme"].ID {
			t.Errorf("engineering = %+v", eng)
		}
		byPath := webtest.Decode[organization](t, c.Get(t, organizations+"/lookup?path=/acme/engineering/platform"), http.StatusOK)
		if byPath.ID != all["platform"].ID {
			t.Errorf("path read = %+v", byPath)
		}
		_ = c.Get(t, organizations+"/"+absentID).Problem(t, http.StatusNotFound)
		_ = c.Get(t, organizations+"/lookup?path=/acme/nowhere").Problem(t, http.StatusNotFound)
		_ = c.Get(t, organizations+"/not-a-uuid").Problem(t, http.StatusBadRequest)
	})

	run("paging and sort", func(t *testing.T) {
		p := webtest.Decode[organizationPage](t, c.Get(t, organizations+"?size=3&sort=-path"), http.StatusOK)
		if p.total() != seededTotal || p.Page != 1 || p.Size != 3 || !p.More || !slices.Equal(codes(p.Items), []string{"logistics", "operations", "finance"}) {
			t.Errorf("page 1 by -path = %v (total %d)", codes(p.Items), p.total())
		}
		p = webtest.Decode[organizationPage](t, c.Get(t, organizations+"?size=3&page=3&sort=code"), http.StatusOK)
		if !slices.Equal(codes(p.Items), []string{"product"}) {
			t.Errorf("page 3 by code = %v", codes(p.Items))
		}
		_ = c.Get(t, organizations+"?page=x").Problem(t, http.StatusBadRequest)
		_ = c.Get(t, organizations+"?size=1000").Problem(t, http.StatusBadRequest)
	})

	run("a cursor walk reads every row once in sort order, unnumbered after the first page", func(t *testing.T) {
		p := webtest.Decode[organizationPage](t, c.Get(t, organizations+"?size=3&sort=-path"), http.StatusOK)
		walked := codes(p.Items)
		first := p
		for p.More {
			if p.Next == "" {
				t.Fatalf("a page with more carried no next: %+v", p)
			}
			p = webtest.Decode[organizationPage](t, c.Get(t, organizations+"?size=3&sort=-path&cursor="+url.QueryEscape(p.Next)), http.StatusOK)
			if p.Page != 0 {
				t.Errorf("a continued page carried page %d", p.Page)
			}
			walked = append(walked, codes(p.Items)...)
		}
		want := []string{"logistics", "operations", "finance", "product", "platform", "engineering", "acme"}
		if !slices.Equal(walked, want) || first.total() != seededTotal {
			t.Errorf("walk by -path = %v (first total %d), want %v", walked, first.total(), want)
		}
		t.Run("a cursor with a page, under another sort, or malformed is a 400", func(t *testing.T) {
			_ = c.Get(t, organizations+"?page=2&cursor="+url.QueryEscape(first.Next)).Problem(t, http.StatusBadRequest)
			_ = c.Get(t, organizations+"?size=3&sort=code&cursor="+url.QueryEscape(first.Next)).Problem(t, http.StatusBadRequest)
			_ = c.Get(t, organizations+"?cursor=not-a-cursor").Problem(t, http.StatusBadRequest)
		})
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
			p := webtest.Decode[organizationPage](t, c.Get(t, organizations+"?"+query), http.StatusOK)
			if !slices.Equal(codes(p.Items), want) || p.total() != len(want) {
				t.Errorf("?%s = %v (total %d), want %v", query, codes(p.Items), p.total(), want)
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
		created := webtest.Decode[organization](t, c.Get(t, organizations+"/lookup?path=/acme/engineering/security"), http.StatusOK)
		if created.ID != ident.ID {
			t.Errorf("created row not readable by path: %+v", created)
		}

		duplicateSibling := body
		conflict(t, c.Post(t, organizations, duplicateSibling), generalConflict)
		conflict(t, c.Post(t, organizations, map[string]any{"parent_id": nil, "code": "acme", "name": "Acme"}), generalConflict)
		conflict(t, c.Post(t, organizations, map[string]any{"parent_id": absentID, "code": "orphan", "name": "Orphan"}), generalConflict)
		_ = c.Post(t, organizations, map[string]any{"code": "Bad_Code", "name": "X"}).Problem(t, http.StatusBadRequest)
		_ = c.Post(t, organizations, map[string]any{"code": "ok", "name": ""}).Problem(t, http.StatusBadRequest)
		_ = c.Post(t, organizations, map[string]any{"parent_id": "nope", "code": "ok", "name": "X"}).Problem(t, http.StatusBadRequest)
		_ = c.Post(t, organizations, map[string]any{"codex": "ok"}).Problem(t, http.StatusBadRequest)
		_ = c.Post(t, organizations, "{").Problem(t, http.StatusBadRequest)
	})

	run("edit", func(t *testing.T) {
		all := tree(t, c)
		fin := all["finance"]
		path := organizations + "/" + fin.ID
		body := map[string]any{"code": "fin", "name": "Finance and Accounting"}

		_ = c.Put(t, path, body).Problem(t, http.StatusPreconditionRequired)
		_ = c.Put(t, path, body, webtest.Header{Name: "If-Match", Value: `W/"1"`}).Problem(t, http.StatusBadRequest)
		_ = c.Put(t, path, map[string]any{"code": "fin"}, webtest.IfMatch(fin.Version)).Problem(t, http.StatusBadRequest)
		_ = c.Put(t, path, body, webtest.IfMatch(fin.Version+1)).Problem(t, http.StatusPreconditionFailed)
		_ = c.Put(t, organizations+"/"+absentID, body, webtest.IfMatch(1)).Problem(t, http.StatusNotFound)

		ident := webtest.Decode[identity](t, c.Put(t, path, body, webtest.IfMatch(fin.Version)), http.StatusOK)
		if ident.ID != fin.ID || ident.Version != fin.Version+1 {
			t.Errorf("edited = %+v, want version %d", ident, fin.Version+1)
		}
		after := webtest.Decode[organization](t, c.Get(t, path), http.StatusOK)
		if after.Code != "fin" || after.Name != "Finance and Accounting" || after.Path != "/acme/fin" {
			t.Errorf("after edit = %+v", after)
		}
		staleVersion := fin.Version
		_ = c.Put(t, path, body, webtest.IfMatch(staleVersion)).Problem(t, http.StatusPreconditionFailed)
	})

	run("transfer", func(t *testing.T) {
		all := tree(t, c)
		platform := all["platform"]
		path := organizations + "/" + platform.ID + "/transfer"

		withoutParentKey := map[string]any{}
		_ = c.Post(t, path, withoutParentKey, webtest.IfMatch(platform.Version)).Problem(t, http.StatusBadRequest)
		_ = c.Post(t, path, map[string]any{"parent_id": all["operations"].ID}).Problem(t, http.StatusPreconditionRequired)
		_ = c.Post(t, path, map[string]any{"parent_id": all["operations"].ID}, webtest.IfMatch(9)).Problem(t, http.StatusPreconditionFailed)
		conflict(t, c.Post(t, path, map[string]any{"parent_id": absentID}, webtest.IfMatch(platform.Version)), generalConflict)

		t.Run("a cycle is refused: acme under its descendant, a node under itself", func(t *testing.T) {
			acme := all["acme"]
			conflict(t, c.Post(t, organizations+"/"+acme.ID+"/transfer", map[string]any{"parent_id": all["product"].ID}, webtest.IfMatch(acme.Version)), generalConflict)
			conflict(t, c.Post(t, path, map[string]any{"parent_id": platform.ID}, webtest.IfMatch(platform.Version)), generalConflict)
		})

		ident := webtest.Decode[identity](t, c.Post(t, path, map[string]any{"parent_id": all["operations"].ID}, webtest.IfMatch(platform.Version)), http.StatusOK)
		t.Run("the move recomposes the path beneath the new parent", func(t *testing.T) {
			if ident.Version != platform.Version+1 {
				t.Errorf("transferred = %+v", ident)
			}
			moved := webtest.Decode[organization](t, c.Get(t, organizations+"/lookup?path=/acme/operations/platform"), http.StatusOK)
			if moved.ID != platform.ID {
				t.Errorf("moved row = %+v", moved)
			}
			_ = c.Get(t, organizations+"/lookup?path=/acme/engineering/platform").Problem(t, http.StatusNotFound)
		})

		t.Run("a subtree move recomposes every descendant's path", func(t *testing.T) {
			eng := all["engineering"]
			webtest.Decode[identity](t, c.Post(t, organizations+"/"+eng.ID+"/transfer", map[string]any{"parent_id": all["finance"].ID}, webtest.IfMatch(eng.Version)), http.StatusOK)
			if p := webtest.Decode[organization](t, c.Get(t, organizations+"/"+all["product"].ID), http.StatusOK); p.Path != "/acme/finance/engineering/product" {
				t.Errorf("descendant path after subtree move = %q", p.Path)
			}
		})

		t.Run("a null parent moves the node to a root", func(t *testing.T) {
			rooted := webtest.Decode[identity](t, c.Post(t, path, map[string]any{"parent_id": nil}, webtest.IfMatch(ident.Version)), http.StatusOK)
			root := webtest.Decode[organization](t, c.Get(t, organizations+"/lookup?path=/platform"), http.StatusOK)
			if root.ParentID != nil || root.Path != "/platform" || root.Version != rooted.Version {
				t.Errorf("root move = %+v", root)
			}
			conflict(t, c.Post(t, organizations, map[string]any{"parent_id": nil, "code": "platform", "name": "Dup"}), generalConflict)
		})
	})

	run("siblings moved under each other at once: the tree lock refuses the second", func(t *testing.T) {
		all := tree(t, c)
		eng, ops := all["engineering"], all["operations"]

		var wg sync.WaitGroup
		results := make([]int, 2)
		move := func(i int, node, under organization) {
			defer wg.Done()
			cl := webtest.NewClient(s.URL())
			results[i] = cl.Post(t, organizations+"/"+node.ID+"/transfer", map[string]any{"parent_id": under.ID}, webtest.IfMatch(node.Version)).Status
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
		after := tree(t, c)
		if len(after) != seededTotal {
			t.Errorf("tree after concurrent transfers has %d nodes", len(after))
		}
		for code, o := range after {
			if o.Path == "" || o.Path[0] != '/' {
				t.Errorf("%s path = %q; want every node to resolve from a root", code, o.Path)
			}
		}
	})

	run("delete, each seeded logo blocking its organization's until the logo's own delete", func(t *testing.T) {
		all := tree(t, c)
		leaf, parent := all["logistics"], all["operations"]

		_ = c.Delete(t, organizations+"/"+leaf.ID).Problem(t, http.StatusPreconditionRequired)
		_ = c.Delete(t, organizations+"/"+leaf.ID, webtest.IfMatch(leaf.Version+1)).Problem(t, http.StatusPreconditionFailed)
		_ = c.Delete(t, organizations+"/"+absentID, webtest.IfMatch(1)).Problem(t, http.StatusNotFound)
		_ = c.Delete(t, organizations+"/"+parent.ID, webtest.IfMatch(parent.Version)).Problem(t, http.StatusConflict)
		_ = c.Delete(t, organizations+"/"+leaf.ID, webtest.IfMatch(leaf.Version)).Problem(t, http.StatusConflict)
		for _, o := range []organization{leaf, parent} {
			c.Delete(t, organizations+"/"+o.ID+"/logo").Expect(t, http.StatusNoContent)
		}

		c.Delete(t, organizations+"/"+leaf.ID, webtest.IfMatch(leaf.Version)).Expect(t, http.StatusNoContent)
		_ = c.Get(t, organizations+"/"+leaf.ID).Problem(t, http.StatusNotFound)
		c.Delete(t, organizations+"/"+parent.ID, webtest.IfMatch(parent.Version)).Expect(t, http.StatusNoContent)
		if p := webtest.Decode[organizationPage](t, c.Get(t, organizations), http.StatusOK); p.total() != seededTotal-2 {
			t.Errorf("total after deletes = %d", p.total())
		}
	})

	if code := s.Stop(t); code != 0 {
		t.Fatalf("exit = %d:\n%s", code, s.Output())
	}
}
