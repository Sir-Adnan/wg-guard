package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/iface"
)

// Regression: the series endpoint queries traffic_samples for the samples
// granularity and traffic_rollups (rx/tx columns) for hourly/daily. A wrong
// column reference fails the SQL prepare and 500s the request.
func TestTrafficSeriesViaAPI(t *testing.T) {
	e := newEnv(t)
	if _, err := e.ifaces.Create(context.Background(), iface.CreateInput{Name: "awg0", ListenPort: 39001, Subnet: "10.77.0.0/24"}); err != nil {
		t.Fatal(err)
	}
	rec := e.do("POST", "/api/v1/users", `{"username": "ts-user"}`)
	if rec.Code != 201 {
		t.Fatalf("user: %d %s", rec.Code, rec.Body.String())
	}
	uid := decodeBody(t, rec)["id"].(string)
	rec = e.do("POST", "/api/v1/users/"+uid+"/devices", `{"name": "phone"}`)
	if rec.Code != 201 {
		t.Fatalf("device: %d %s", rec.Code, rec.Body.String())
	}
	devID := decodeBody(t, rec)["id"].(string)

	// Seed one sample and one hourly + daily rollup bucket "now".
	now := time.Now().UTC().Truncate(time.Hour)
	for _, q := range []string{
		fmt.Sprintf(`INSERT INTO traffic_samples (device_id, ts, rx_delta, tx_delta)
			VALUES ('%s', '%s', 1000, 500)`, devID, now.Format(time.RFC3339Nano)),
		fmt.Sprintf(`INSERT INTO traffic_rollups (device_id, granularity, bucket_start, rx, tx)
			VALUES ('%s', 'hourly', '%s', 7000, 3000)`, devID, now.Format(time.RFC3339Nano)),
		fmt.Sprintf(`INSERT INTO traffic_rollups (device_id, granularity, bucket_start, rx, tx)
			VALUES ('%s', 'daily', '%s', 90000, 10000)`, devID, now.Truncate(24*time.Hour).Format(time.RFC3339Nano)),
	} {
		if _, err := e.db.ExecContext(context.Background(), q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	check := func(gran string, wantRX int64) {
		t.Helper()
		rec := e.do("GET", "/api/v1/users/"+uid+"/traffic?granularity="+gran, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s series: %d %s", gran, rec.Code, rec.Body.String())
		}
		body := decodeBody(t, rec)
		series := body["series"].([]any)
		if len(series) != 1 {
			t.Fatalf("%s series len = %d body=%s", gran, len(series), rec.Body.String())
		}
		if got := int64(series[0].(map[string]any)["rx"].(float64)); got != wantRX {
			t.Fatalf("%s rx = %d, want %d", gran, got, wantRX)
		}
	}
	check("samples", 1000)
	check("hourly", 7000)
	check("daily", 90000)

	// Invalid granularity is a 400.
	if rec := e.do("GET", "/api/v1/users/"+uid+"/traffic?granularity=weekly", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad granularity: %d", rec.Code)
	}
}
