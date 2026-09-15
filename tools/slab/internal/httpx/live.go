package httpx

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/standards-lab/go-web-sdk"
)

// probe is the client Live polls with: a short timeout, so a service that
// has bound its port but not yet served answers false on this poll and
// true on a later one, and its own connection, so a poll never holds a
// scenario client's one connection.
var probe = newHTTPClient(time.Second)

// Live returns nil when the service at base answers its liveness probe with
// 200, and otherwise the reason it did not: the transport error, or the
// status it answered instead. It is the check a scenario's Need states for
// the service, so it takes the Need's context and returns its error.
func Live(ctx context.Context, base string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+web.HealthPath, nil)
	if err != nil {
		return err
	}
	res, err := probe.Do(req)
	if err != nil {
		return err
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d %s", web.HealthPath, res.StatusCode, http.StatusText(res.StatusCode))
	}
	return nil
}
