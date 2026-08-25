package exchange

import "errors"

// ErrReadOnly is returned by PlaceOrder/CancelOrder on market-data-only adapters (P8).
var ErrReadOnly = errors.New("exchange: read-only adapter (orders not supported)")

// ErrUnknownAck means PlaceOrder timed out or the ack is unclear. Do not retry the same cloid.
var ErrUnknownAck = errors.New("exchange: unknown ack (reconcile, do not retry)")

// ErrOrderNotFound is returned by GetOrder when the venue has no matching order.
var ErrOrderNotFound = errors.New("exchange: order not found")
