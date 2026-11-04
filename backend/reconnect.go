package main

import (
	"context"
	"time"

	"github.com/xtls/xray-core/core"
)

// Auto-reconnect: while connected, a request through the tunnel every
// healthInterval shows whether it still carries traffic. After two failures in
// a row, or the first check after the computer slept, the worker reconnects
// with the last connect request. With the Kill Switch the WFP session is kept
// across attempts, so nothing leaks while the network comes back.
const (
	healthInterval      = 20 * time.Second
	healthProbeTimeout  = 10 * time.Second
	reconnectMaxDelay   = 60 * time.Second
	reconnectGiveUpTurn = 10
)

func (w *worker) startHealthMonitor(parent context.Context, instance *core.Instance) {
	w.stopHealthMonitor()
	ctx, cancel := context.WithCancel(parent)
	w.healthCancel = cancel
	w.healthWG.Add(1)
	go func() {
		defer w.healthWG.Done()
		ticker := time.NewTicker(healthInterval)
		defer ticker.Stop()
		// Wall-clock times: the monotonic clock does not advance while
		// Windows sleeps, so only the wall clock shows the gap.
		last := time.Now().Round(0)
		failures := 0
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				now = now.Round(0)
				slept := now.Sub(last) > 3*healthInterval
				last = now
				probeCtx, done := context.WithTimeout(ctx, healthProbeTimeout)
				err := probeThroughInstance(probeCtx, instance, healthProbeTimeout)
				done()
				if ctx.Err() != nil {
					return
				}
				if err == nil {
					failures = 0
					continue
				}
				failures++
				w.logf("tunnel health check failed attempt=%d slept=%t: %v", failures, slept, err)
				if failures < 2 && !slept {
					continue
				}
				reason := "connection lost"
				if slept {
					reason = "wake from sleep"
				}
				select {
				case w.internal <- request{Method: "reconnect", Reason: reason}:
				case <-ctx.Done():
				}
				return
			}
		}
	}()
}

func (w *worker) stopHealthMonitor() {
	if w.healthCancel == nil {
		return
	}
	w.healthCancel()
	w.healthCancel = nil
	w.healthWG.Wait()
}

// cancelReconnect stops a pending retry; the user's own connect or disconnect
// takes over. Forgetting lastConnect also voids a retry whose timer already
// fired and is waiting to be read.
func (w *worker) cancelReconnect() {
	if w.reconnectTimer != nil {
		w.reconnectTimer.Stop()
		w.reconnectTimer = nil
	}
	w.reconnectAttempt = 0
	w.lastConnect = nil
}

func (w *worker) reconnect(ctx context.Context, reason string) {
	w.reconnectTimer = nil
	if w.lastConnect == nil {
		return
	}
	last := *w.lastConnect
	w.reconnectAttempt++
	w.logf("auto reconnect attempt=%d reason=%s", w.reconnectAttempt, reason)
	w.setState("reconnecting")
	// Keep the Kill Switch session so traffic stays blocked between attempts.
	if err := w.closeTunnel(last.KillSwitch); err != nil {
		w.logf("auto reconnect: closing the old tunnel failed: %v", err)
	}
	err := w.connect(ctx, last)
	if err == nil {
		w.logf("auto reconnect completed attempt=%d", w.reconnectAttempt)
		w.reconnectAttempt = 0
		w.send(map[string]any{"event": "profile", "profile": w.currentActiveProfile(), "reason": "reconnect"})
		return
	}
	w.logf("auto reconnect attempt=%d failed: %v", w.reconnectAttempt, err)
	if !last.KillSwitch && w.reconnectAttempt >= reconnectGiveUpTurn {
		w.logf("auto reconnect gave up after %d attempts", w.reconnectAttempt)
		w.reconnectAttempt = 0
		w.lastConnect = nil
		_ = w.close()
		w.setState("disconnected")
		return
	}
	delay := 5 * time.Second << min(w.reconnectAttempt-1, 4)
	if delay > reconnectMaxDelay {
		delay = reconnectMaxDelay
	}
	w.setState("reconnecting")
	w.reconnectTimer = time.AfterFunc(delay, func() {
		select {
		case w.internal <- request{Method: "reconnect", Reason: "retry"}:
		case <-ctx.Done():
		}
	})
}
