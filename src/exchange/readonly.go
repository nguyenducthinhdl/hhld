package exchange

import "errors"

// ErrReadOnly is returned by PlaceOrder/CancelOrder on market-data-only adapters (P8).
var ErrReadOnly = errors.New("exchange: read-only adapter (orders not supported)")
