package main

import (
	"context"
	"time"

	"github.com/xtls/xray-core/core"
	xstats "github.com/xtls/xray-core/features/stats"
)

const (
	trafficSampleInterval  = time.Second
	tunUploadCounterName   = "inbound>>>tun-in>>>traffic>>>uplink"
	tunDownloadCounterName = "inbound>>>tun-in>>>traffic>>>downlink"
)

type trafficSnapshot struct {
	UploadBytes   int64 `json:"uploadBytes"`
	DownloadBytes int64 `json:"downloadBytes"`
	UploadBPS     int64 `json:"uploadBps"`
	DownloadBPS   int64 `json:"downloadBps"`
}

func nonNegativeDelta(current, previous int64) int64 {
	if current <= previous {
		return 0
	}
	return current - previous
}

func bytesPerSecond(current, previous int64, elapsed time.Duration) int64 {
	if elapsed <= 0 {
		return 0
	}
	return int64(float64(nonNegativeDelta(current, previous)) / elapsed.Seconds())
}

func snapshotTraffic(baseUpload, baseDownload, previousUpload, previousDownload, currentUpload, currentDownload int64, elapsed time.Duration) trafficSnapshot {
	return trafficSnapshot{
		UploadBytes:   nonNegativeDelta(currentUpload, baseUpload),
		DownloadBytes: nonNegativeDelta(currentDownload, baseDownload),
		UploadBPS:     bytesPerSecond(currentUpload, previousUpload, elapsed),
		DownloadBPS:   bytesPerSecond(currentDownload, previousDownload, elapsed),
	}
}

func (w *worker) stopTrafficMonitor() {
	if w.trafficCancel == nil {
		return
	}
	w.trafficCancel()
	w.trafficCancel = nil
	w.trafficWG.Wait()
}

func (w *worker) startTrafficMonitor(parent context.Context, instance *core.Instance) {
	w.stopTrafficMonitor()
	feature := instance.GetFeature(xstats.ManagerType())
	manager, ok := feature.(xstats.Manager)
	if !ok {
		w.logf("traffic monitor unavailable: Xray stats manager missing")
		return
	}
	uploadCounter := manager.GetCounter(tunUploadCounterName)
	downloadCounter := manager.GetCounter(tunDownloadCounterName)
	if uploadCounter == nil || downloadCounter == nil {
		w.logf("traffic monitor unavailable: TUN counters missing")
		return
	}

	ctx, cancel := context.WithCancel(parent)
	w.trafficCancel = cancel
	baseUpload := uploadCounter.Value()
	baseDownload := downloadCounter.Value()
	previousUpload := baseUpload
	previousDownload := baseDownload
	previousAt := time.Now()
	w.send(map[string]any{"event": "traffic", "traffic": trafficSnapshot{}})
	w.trafficWG.Add(1)
	go func() {
		defer w.trafficWG.Done()
		ticker := time.NewTicker(trafficSampleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				currentUpload := uploadCounter.Value()
				currentDownload := downloadCounter.Value()
				snapshot := snapshotTraffic(baseUpload, baseDownload, previousUpload, previousDownload, currentUpload, currentDownload, now.Sub(previousAt))
				w.send(map[string]any{"event": "traffic", "traffic": snapshot})
				previousUpload = currentUpload
				previousDownload = currentDownload
				previousAt = now
			}
		}
	}()
	w.logf("traffic monitor active interval=%s", trafficSampleInterval)
}
