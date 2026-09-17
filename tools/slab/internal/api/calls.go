package api

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/repo"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

// Reset posts SeedState to State, narrated: the note saying why, the seed
// file as authored, the request, and the response, checked to be 200. It is
// every stack-backed scenario's first step, so the database starts each run
// from the same known rows.
func Reset(ctx context.Context, c *httpx.Client, r *scenario.Reporter) error {
	r.Note("This step posts the %q state to %s, so the database starts this run from a predictable, known set of data: the schema at its latest version and the seven-row acme tree below as the only rows, whatever the last run left behind.", SeedState, State)
	fsys, err := repo.FS(ctx)
	if err != nil {
		return err
	}
	file := path.Join(SeedsDir, SeedState+".json")
	text, err := fs.ReadFile(fsys, file)
	if err != nil {
		return err
	}
	r.JSON(file, text)
	body := map[string]string{"state": SeedState}
	r.Request(http.MethodPost, State, nil, body)
	res, err := c.Post(ctx, State, body)
	if err != nil {
		return err
	}
	r.Response(res)
	return res.Expect(http.StatusOK)
}

// List is the raw list: GET Organizations with no query, narrated, checked
// to be 200, and decoded.
func List(ctx context.Context, c *httpx.Client, r *scenario.Reporter) (Page, error) {
	r.Request(http.MethodGet, Organizations, nil, nil)
	res, err := c.Get(ctx, Organizations)
	if err != nil {
		return Page{}, err
	}
	r.Response(res)
	if err := res.Expect(http.StatusOK); err != nil {
		return Page{}, err
	}
	var p Page
	if err := res.JSON(&p); err != nil {
		return Page{}, err
	}
	return p, nil
}

// IdentityOf reads the Identity envelope off a command's response and checks
// it names the row the command targeted.
func IdentityOf(res *httpx.Response, wantID string) (Identity, error) {
	var id Identity
	if err := res.JSON(&id); err != nil {
		return Identity{}, err
	}
	if id.ID != wantID {
		return Identity{}, fmt.Errorf("the response names id %s, want %s", id.ID, wantID)
	}
	return id, nil
}

// ExpectVersion checks a command left its row at version want: one past the
// version its If-Match carried.
func ExpectVersion(id Identity, want int64) error {
	if id.Version != want {
		return fmt.Errorf("the row is at version %d, want %d", id.Version, want)
	}
	return nil
}

// OversizedBody returns a command body over MaxCommandBody, and the short
// stand-in a narration prints in its place: the payload is a create body
// whose code is MaxCommandBody+1 bytes of filler, well-formed JSON that the
// service's bounded reader rejects before the decoder sees its end, and the
// printable form names the filler's length instead of printing it.
func OversizedBody() (payload []byte, printable string) {
	n := MaxCommandBody + 1
	payload = []byte(`{"code":"` + strings.Repeat("a", n) + `"}`)
	printable = fmt.Sprintf(`{"code":"<%d bytes>"}`, n)
	return payload, printable
}
