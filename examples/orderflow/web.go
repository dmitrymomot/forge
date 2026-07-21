package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/async/collector"
	"github.com/dmitrymomot/forge/async/eventbus"
	"github.com/dmitrymomot/forge/async/eventrouter"
	"github.com/dmitrymomot/forge/web/httpserver"
)

// requestSeen is the collector's telemetry event: buffered on the request
// path, flushed in batches, loss tolerated by contract.
type requestSeen struct {
	Method string
	Path   string
}

// newTelemetry builds the write-behind request collector: Add never blocks
// the request path, and the sink just logs batch sizes.
func newTelemetry(log *slog.Logger) (*collector.Collector[requestSeen], error) {
	sink := collector.SinkFunc[requestSeen](func(_ context.Context, batch []requestSeen) error {
		log.Info("telemetry flushed", slog.Int("requests", len(batch)))
		return nil
	})
	cfg := collector.DefaultConfig()
	cfg.FlushInterval = 5 * time.Second
	return collector.New(sink, collector.WithConfig(cfg), collector.WithLogger(log))
}

// routeAnalytics streams both lifecycle events to an "analytics warehouse" —
// here the demo's own /analytics endpoint — batched by size or age through a
// real HTTP deliverer.
func routeAnalytics(bus *eventbus.Bus) error {
	deliverer, err := eventrouter.NewHTTPDeliverer("http://localhost" + addr + "/analytics")
	if err != nil {
		return err
	}
	dest := eventrouter.NewDestination("analytics", deliverer,
		eventrouter.WithBatchSize(8), eventrouter.WithBatchAge(2*time.Second))
	eventrouter.Route(bus, evtOrderPlaced, dest)
	eventrouter.Route(bus, evtOrderCompleted, dest)
	return nil
}

// newShopAPI assembles the HTTP server: the order intake, the analytics
// receiver, and the telemetry middleware around both.
func newShopAPI(sh *shop, bus *eventbus.Bus, telemetry *collector.Collector[requestSeen], log *slog.Logger) *httpserver.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", placeOrderHandler(sh, bus))
	mux.HandleFunc("POST /analytics", analyticsHandler(log))

	// Telemetry rides every request as a non-blocking side effect.
	withTelemetry := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = telemetry.Add(r.Context(), requestSeen{Method: r.Method, Path: r.URL.Path})
		mux.ServeHTTP(w, r)
	})

	return httpserver.New(withTelemetry, httpserver.WithAddr(addr), httpserver.WithName("shop-api"), httpserver.WithLogger(log))
}

// placeOrderHandler records the order and publishes order.placed. PublishTx
// writes an outbox intent row instead of touching the broker: with pgoutbox
// the row commits or rolls back with the order insert (tx is the caller's
// pgx.Tx; the memory store ignores it).
func placeOrderHandler(sh *shop, bus *eventbus.Bus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Item string `json:"item"`
			Qty  int    `json:"qty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Item == "" || req.Qty <= 0 {
			http.Error(w, `want body like {"item":"espresso","qty":1}`, http.StatusBadRequest)
			return
		}

		id := sh.place()
		event := orderEvent{OrderID: id, Item: req.Item, Qty: req.Qty}
		if err := eventbus.PublishTx(r.Context(), bus, nil, evtOrderPlaced, event); err != nil {
			http.Error(w, "publish failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"order_id": id, "status": "placed"})
	}
}

// analyticsHandler plays the external warehouse: it accepts the router's
// JSON batches and logs every event it ingests.
func analyticsHandler(log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var batch []eventrouter.Event
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			http.Error(w, "bad batch", http.StatusBadRequest)
			return
		}
		for _, e := range batch {
			log.Info("analytics ingested", slog.String("event", e.Name), slog.String("id", e.ID))
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// seed places two demo orders once the server is up: espresso fulfills the
// happy path, unicorn is out of stock and exercises compensation. Place more
// with curl.
func seed(ctx context.Context, log *slog.Logger) {
	httpc := &http.Client{Timeout: 2 * time.Second}
	for _, body := range []string{
		`{"item":"espresso","qty":2}`,
		`{"item":"unicorn","qty":1}`,
	} {
		for {
			resp, err := httpc.Post("http://localhost"+addr+"/orders", "application/json", bytes.NewReader([]byte(body)))
			if err == nil {
				//nolint:nilaway // resp is non-nil whenever err is nil, per http.Client.Do's contract
				_ = resp.Body.Close()
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	log.Info("demo orders placed", slog.String("hint", `curl -s localhost:8080/orders -d '{"item":"espresso","qty":1}'`))
}
