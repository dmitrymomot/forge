package main

import (
	"context"
	"log/slog"

	"github.com/dmitrymomot/forge/async/eventbus"
	"github.com/dmitrymomot/forge/async/workflow"
)

// fulfillment is the workflow state: it flows through the steps and is
// checkpointed after each one, so a restarted worker resumes mid-order.
type fulfillment struct {
	OrderID   string `json:"order_id"`
	Item      string `json:"item"`
	PaymentID string `json:"payment_id"`
	Qty       int    `json:"qty"`
}

// newFulfillment builds the order.fulfill workflow: charge, reserve, publish.
// An out-of-stock item is a business failure no retry can fix, so
// reserve_stock returns workflow.Fail and the completed steps compensate in
// reverse order — the charge is refunded.
func newFulfillment(sh *shop, bus *eventbus.Bus, log *slog.Logger) *workflow.Workflow[fulfillment] {
	chargePayment := func(_ context.Context, f *fulfillment) error {
		if f.PaymentID == "" { // idempotent under step redelivery
			f.PaymentID = "pay-" + f.OrderID
		}
		log.Info("payment charged", slog.String("order", f.OrderID), slog.String("payment", f.PaymentID))
		return nil
	}
	refundPayment := func(_ context.Context, f *fulfillment) error {
		sh.setStatus(f.OrderID, "failed")
		log.Info("payment refunded", slog.String("order", f.OrderID), slog.String("payment", f.PaymentID))
		return nil
	}

	reserveStock := func(_ context.Context, f *fulfillment) error {
		if err := sh.reserve(f.OrderID, f.Item, f.Qty); err != nil {
			return workflow.Fail(err)
		}
		log.Info("stock reserved", slog.String("order", f.OrderID), slog.String("item", f.Item))
		return nil
	}
	releaseStock := func(_ context.Context, f *fulfillment) error {
		sh.release(f.OrderID, f.Item, f.Qty)
		return nil
	}

	complete := func(ctx context.Context, f *fulfillment) error {
		sh.setStatus(f.OrderID, "fulfilled")
		return eventbus.Publish(ctx, bus, evtOrderCompleted, orderEvent{OrderID: f.OrderID, Item: f.Item, Qty: f.Qty})
	}

	return workflow.New("order.fulfill",
		workflow.Step[fulfillment]{Name: "charge_payment", Run: chargePayment, Compensate: refundPayment},
		workflow.Step[fulfillment]{Name: "reserve_stock", Run: reserveStock, Compensate: releaseStock},
		workflow.Step[fulfillment]{Name: "complete", Run: complete},
	)
}

// subscribeOrderLifecycle wires the eventbus reactions to the two order
// events: order.placed starts the fulfillment workflow, order.completed sends
// the receipt.
func subscribeOrderLifecycle(bus *eventbus.Bus, eng *workflow.Engine, fulfill *workflow.Workflow[fulfillment], log *slog.Logger) {
	// Deliveries are at-least-once; the inbox seam collapses duplicates so one
	// order never starts two workflow runs (production:
	// async/eventbus/postgres inside the handler's transaction).
	inbox := eventbus.NewMemoryInbox()
	eventbus.Subscribe(bus, evtOrderPlaced, "fulfill", func(ctx context.Context, d eventbus.Delivery[orderEvent]) error {
		seen, err := inbox.Seen(ctx, nil, d.ID)
		if err != nil || seen {
			return err
		}
		_, err = workflow.Start(ctx, eng, fulfill, fulfillment{OrderID: d.Payload.OrderID, Item: d.Payload.Item, Qty: d.Payload.Qty})
		return err
	})

	eventbus.Subscribe(bus, evtOrderCompleted, "receipt", func(_ context.Context, d eventbus.Delivery[orderEvent]) error {
		log.Info("receipt emailed", slog.String("order", d.Payload.OrderID))
		return nil
	})
}
