// Package apperror defines transport-agnostic failure kinds shared by the
// domain and application layers. Handlers map a Kind onto a protocol status
// code, which keeps HTTP concerns out of the inner layers.
package apperror
